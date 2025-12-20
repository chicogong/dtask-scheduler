// Package scheduler implements the core scheduling logic for dtask-scheduler.
// It manages worker state, handles HTTP API requests, and executes the load-based
// scheduling algorithm.
package scheduler

import (
	"sync"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

const (
	SuspiciousThreshold = 10 * time.Second
	OfflineThreshold    = 20 * time.Second
)

// StateManager manages worker states in memory with thread-safe operations.
// It tracks heartbeats, worker availability, and handles timeout detection.
type StateManager struct {
	mu      sync.RWMutex
	workers map[string]*types.WorkerState
}

// NewStateManager creates a new state manager
func NewStateManager() *StateManager {
	return &StateManager{
		workers: make(map[string]*types.WorkerState),
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
	}

	// Update fields
	worker.Address = hb.Address
	worker.ResourceTags = hb.ResourceTags
	worker.MaxTasks = hb.MaxTasks
	worker.CurrentTasks = hb.CurrentTasks
	worker.Available = hb.MaxTasks - hb.CurrentTasks
	worker.LastHeartbeat = time.Now()
	worker.Status = types.WorkerOnline
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

// CheckTimeouts marks workers as suspicious or offline based on heartbeat age
func (sm *StateManager) CheckTimeouts() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	for _, worker := range sm.workers {
		elapsed := now.Sub(worker.LastHeartbeat)

		if elapsed > OfflineThreshold {
			worker.Status = types.WorkerOffline
		} else if elapsed > SuspiciousThreshold {
			worker.Status = types.WorkerSuspicious
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
