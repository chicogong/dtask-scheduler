package scheduler

import (
	"context"
	"log"
	"math/rand"
	"sort"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

const (
	// defaultSpreadThreshold is the maximum load-ratio gap within which workers
	// are treated as equally idle and one is picked at random (hot-spot spread).
	defaultSpreadThreshold = 0.05

	// maxScheduleWaitMS caps how long, in milliseconds, a waiting schedule
	// request may block, regardless of the MaxWaitMS the client requested.
	maxScheduleWaitMS = 60000
)

// Scheduler implements the core scheduling algorithm that selects the best
// worker for a given task based on resource tags and current load.
type Scheduler struct {
	state           *StateManager
	spreadThreshold float64
	// pickIndex selects an element in [0,n); it is a field so tests can make
	// the otherwise-random spread selection deterministic.
	pickIndex func(n int) int
}

// NewScheduler creates a new scheduler instance.
func NewScheduler(state *StateManager) *Scheduler {
	return &Scheduler{
		state:           state,
		spreadThreshold: defaultSpreadThreshold,
		pickIndex:       rand.Intn,
	}
}

// Schedule assigns a task to the best available worker using the following
// algorithm:
//  1. Filter workers by required resource tags (via the inverted index)
//  2. Filter out offline or full workers
//  3. Pick the least-loaded worker, spreading randomly among near-equal ones
//  4. Optimistically increment its task count
//
// When req.MaxWaitMS is positive and no worker is available, Schedule blocks
// until a worker frees capacity, the deadline elapses, or ctx is canceled.
// It returns a ScheduleResponse with worker details on success, or an error
// message on failure.
func (s *Scheduler) Schedule(ctx context.Context, req *types.ScheduleRequest) *types.ScheduleResponse {
	if err := req.Validate(); err != nil {
		return &types.ScheduleResponse{Error: err.Error()}
	}

	resp := s.scheduleOnce(req)
	if resp.Error == "" || req.MaxWaitMS <= 0 {
		return resp
	}

	// Resource shortage with an opt-in wait: park until a worker frees
	// capacity. Clamp the requested wait in milliseconds before converting to
	// a Duration, so an absurdly large MaxWaitMS cannot overflow the int64
	// nanosecond computation and silently bypass the cap.
	waitMS := req.MaxWaitMS
	if waitMS > maxScheduleWaitMS {
		waitMS = maxScheduleWaitMS
	}
	deadline := time.NewTimer(time.Duration(waitMS) * time.Millisecond)
	defer deadline.Stop()

	for {
		// Grab the wakeup channel before retrying so a capacity change that
		// races with the attempt is observed rather than lost.
		changed := s.state.CapacityChanged()

		resp = s.scheduleOnce(req)
		if resp.Error == "" {
			return resp
		}

		select {
		case <-changed:
			// A worker reported free capacity; retry.
		case <-deadline.C:
			return &types.ScheduleResponse{Error: "timed out waiting for an available worker"}
		case <-ctx.Done():
			return &types.ScheduleResponse{Error: "request canceled before a worker became available"}
		}
	}
}

// scheduleOnce performs a single, non-blocking scheduling attempt.
func (s *Scheduler) scheduleOnce(req *types.ScheduleRequest) *types.ScheduleResponse {
	// Filter by resource tags via the inverted index.
	candidates := s.state.CandidatesByTags(req.RequiredTags)

	// Filter out offline or full workers.
	candidates = s.filterAvailable(candidates)

	// No available workers.
	if len(candidates) == 0 {
		return &types.ScheduleResponse{Error: "no available worker matching requirements"}
	}

	// Select best worker (lowest load ratio, with hot-spot spread).
	best := s.selectBestWorker(candidates)
	if best == nil {
		return &types.ScheduleResponse{Error: "failed to select best worker"}
	}

	// Optimistic allocation: increment task count immediately. A failure here
	// only means the in-memory view raced ahead of a heartbeat; the worker is
	// still returned and will reject the task if it is genuinely full.
	if err := s.state.AllocateTask(best.WorkerID); err != nil {
		log.Printf("Warning: failed to allocate task to %s: %v", best.WorkerID, err)
	}

	return &types.ScheduleResponse{
		WorkerID: best.WorkerID,
		Address:  best.Address,
	}
}

// filterAvailable filters out offline or full workers.
func (s *Scheduler) filterAvailable(workers []*types.WorkerState) []*types.WorkerState {
	var result []*types.WorkerState
	for _, worker := range workers {
		if worker.Status == types.WorkerOnline && worker.Available > 0 {
			result = append(result, worker)
		}
	}

	return result
}

// selectBestWorker returns the least-loaded worker. When several workers have
// load ratios within spreadThreshold of the minimum, one of them is chosen at
// random so tasks do not all pile onto a single "best" worker (hot-spot spread).
func (s *Scheduler) selectBestWorker(workers []*types.WorkerState) *types.WorkerState {
	if len(workers) == 0 {
		return nil
	}

	// Make a copy to avoid mutating the caller's slice.
	workersCopy := make([]*types.WorkerState, len(workers))
	copy(workersCopy, workers)

	// Sort by load ratio (ascending).
	sort.Slice(workersCopy, func(i, j int) bool {
		return workersCopy[i].LoadRatio() < workersCopy[j].LoadRatio()
	})

	// Count workers within spreadThreshold of the least-loaded one, then pick
	// one of them at random.
	bestRatio := workersCopy[0].LoadRatio()
	tied := 1
	for tied < len(workersCopy) && workersCopy[tied].LoadRatio()-bestRatio <= s.spreadThreshold {
		tied++
	}

	return workersCopy[s.pickIndex(tied)]
}
