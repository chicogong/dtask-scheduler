// Package scheduler implements the core scheduling logic for dtask-scheduler.
// It manages worker state, handles HTTP API requests, and executes the load-based
// scheduling algorithm.
package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

const (
	// SuspiciousThreshold is the default delay without a heartbeat before a
	// worker is marked "suspicious".
	SuspiciousThreshold = 10 * time.Second

	// OfflineThreshold is the default delay without a heartbeat before a
	// worker is marked "offline".
	OfflineThreshold = 20 * time.Second

	// DefaultTimeoutCheckInterval is the default cadence of RunTimeoutChecker.
	DefaultTimeoutCheckInterval = 5 * time.Second
)

// StateManager manages worker states in memory with thread-safe operations.
// It tracks heartbeats, worker availability, and handles timeout detection.
type StateManager struct {
	mu       sync.RWMutex
	workers  map[string]*types.WorkerState
	tags     *tagIndex
	capacity *capacityNotifier
	cfg      Config
}

// NewStateManager creates a new state manager with the default configuration.
func NewStateManager() *StateManager {
	return NewStateManagerWithConfig(DefaultConfig())
}

// NewStateManagerWithConfig creates a new state manager with the given
// configuration. Zero-valued config fields fall back to their defaults.
func NewStateManagerWithConfig(cfg Config) *StateManager {
	return &StateManager{
		workers:  make(map[string]*types.WorkerState),
		tags:     newTagIndex(),
		capacity: newCapacityNotifier(),
		cfg:      cfg.withDefaults(),
	}
}

// UpdateFromHeartbeat updates worker state from heartbeat
func (sm *StateManager) UpdateFromHeartbeat(hb *types.Heartbeat) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	worker, exists := sm.workers[hb.WorkerID]
	if !exists {
		worker = &types.WorkerState{
			WorkerID: hb.WorkerID,
		}
		sm.workers[hb.WorkerID] = worker
		sm.tags.add(hb.WorkerID, hb.ResourceTags)
	} else {
		sm.tags.update(hb.WorkerID, worker.ResourceTags, hb.ResourceTags)
	}

	// Update fields
	worker.Address = hb.Address
	worker.ResourceTags = hb.ResourceTags
	worker.MaxTasks = hb.MaxTasks
	worker.CurrentTasks = hb.CurrentTasks
	worker.Available = hb.MaxTasks - hb.CurrentTasks
	worker.Metrics = hb.Metrics
	worker.LastHeartbeat = time.Now()
	worker.Status = types.WorkerOnline

	// Wake any schedule requests waiting for capacity.
	if worker.Available > 0 {
		sm.capacity.broadcast()
	}
}

// LoadSnapshot atomically replaces all worker state with the given snapshot and
// rebuilds the tag index. Standby schedulers call this to absorb the worker
// state replicated from the master.
func (sm *StateManager) LoadSnapshot(workers []*types.WorkerState) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.workers = make(map[string]*types.WorkerState, len(workers))
	sm.tags = newTagIndex()
	for _, w := range workers {
		copy := *w
		sm.workers[copy.WorkerID] = &copy
		sm.tags.add(copy.WorkerID, copy.ResourceTags)
	}

	// Wake any waiters so they re-evaluate against the freshly loaded state.
	sm.capacity.broadcast()
}

// CapacityChanged returns a channel that is closed the next time any worker
// reports free capacity. Schedule requests that opted to wait park on it.
func (sm *StateManager) CapacityChanged() <-chan struct{} {
	return sm.capacity.waitChan()
}

// GetWorker returns a worker by ID
func (sm *StateManager) GetWorker(workerID string) (*types.WorkerState, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	worker, exists := sm.workers[workerID]
	if !exists {
		return nil, false
	}

	// Return a copy to avoid race conditions
	copy := *worker
	return &copy, true
}

// ListWorkers returns all workers
func (sm *StateManager) ListWorkers() []*types.WorkerState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	workers := make([]*types.WorkerState, 0, len(sm.workers))
	for _, worker := range sm.workers {
		copy := *worker
		workers = append(workers, &copy)
	}

	return workers
}

// CandidatesByTags returns copies of every worker carrying ALL of the required
// tags. An empty required slice matches every worker. Results use the tag
// inverted index, so filtering cost scales with the matching set rather than
// the whole worker pool.
func (sm *StateManager) CandidatesByTags(required []string) []*types.WorkerState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if len(required) == 0 {
		result := make([]*types.WorkerState, 0, len(sm.workers))
		for _, worker := range sm.workers {
			copy := *worker
			result = append(result, &copy)
		}
		return result
	}

	ids := sm.tags.candidates(required)
	result := make([]*types.WorkerState, 0, len(ids))
	for _, id := range ids {
		if worker, ok := sm.workers[id]; ok {
			copy := *worker
			result = append(result, &copy)
		}
	}
	return result
}

// CheckTimeouts marks workers as suspicious or offline based on heartbeat age,
// using the thresholds from the manager's Config.
func (sm *StateManager) CheckTimeouts() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	for _, worker := range sm.workers {
		elapsed := now.Sub(worker.LastHeartbeat)

		if elapsed > sm.cfg.OfflineThreshold {
			worker.Status = types.WorkerOffline
		} else if elapsed > sm.cfg.SuspiciousThreshold {
			worker.Status = types.WorkerSuspicious
		}
	}
}

// RunTimeoutChecker periodically scans for stale workers until ctx is canceled.
// It blocks, so callers typically run it in its own goroutine. The scan cadence
// comes from Config.TimeoutCheckInterval.
func (sm *StateManager) RunTimeoutChecker(ctx context.Context) {
	ticker := time.NewTicker(sm.cfg.TimeoutCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sm.CheckTimeouts()
		case <-ctx.Done():
			return
		}
	}
}

// AllocateTask optimistically increments task count for a worker
func (sm *StateManager) AllocateTask(workerID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	worker, exists := sm.workers[workerID]
	if !exists {
		return types.ErrWorkerNotFound
	}

	if worker.Available <= 0 {
		return types.ErrNoCapacity
	}

	worker.CurrentTasks++
	worker.Available--
	return nil
}
