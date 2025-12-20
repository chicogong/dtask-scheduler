package scheduler

import (
	"log"
	"sort"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

// Scheduler implements the core scheduling algorithm that selects the best worker
// for a given task based on resource tags and current load.
type Scheduler struct {
	state *StateManager
}

// NewScheduler creates a new scheduler instance.
func NewScheduler(state *StateManager) *Scheduler {
	return &Scheduler{
		state: state,
	}
}

// Schedule assigns a task to the best available worker using the following algorithm:
//  1. Filter workers by required resource tags
//  2. Filter out offline or full workers
//  3. Select worker with lowest load ratio (current_tasks / max_tasks)
//  4. Optimistically increment task count
//
// Returns ScheduleResponse with worker details on success, or error message on failure.
func (s *Scheduler) Schedule(req *types.ScheduleRequest) *types.ScheduleResponse {
	if err := req.Validate(); err != nil {
		return &types.ScheduleResponse{
			Error: err.Error(),
		}
	}

	// Get all workers
	workers := s.state.ListWorkers()

	// Filter by resource tags
	candidates := s.filterByTags(workers, req.RequiredTags)

	// Filter out offline or full workers
	candidates = s.filterAvailable(candidates)

	// No available workers
	if len(candidates) == 0 {
		return &types.ScheduleResponse{
			Error: "no available worker matching requirements",
		}
	}

	// Select best worker (lowest load ratio)
	best := s.selectBestWorker(candidates)

	// Defensive nil check
	if best == nil {
		return &types.ScheduleResponse{
			Error: "failed to select best worker",
		}
	}

	// Optimistic allocation: increment task count immediately
	err := s.state.AllocateTask(best.WorkerID)
	if err != nil {
		log.Printf("Warning: failed to allocate task to %s: %v", best.WorkerID, err)
	}

	return &types.ScheduleResponse{
		WorkerID: best.WorkerID,
		Address:  best.Address,
	}
}

// filterByTags filters workers that have all required tags
func (s *Scheduler) filterByTags(workers []*types.WorkerState, requiredTags []string) []*types.WorkerState {
	if len(requiredTags) == 0 {
		return workers
	}

	var result []*types.WorkerState
	for _, worker := range workers {
		if hasAllTags(worker.ResourceTags, requiredTags) {
			result = append(result, worker)
		}
	}

	return result
}

// filterAvailable filters out offline or full workers
func (s *Scheduler) filterAvailable(workers []*types.WorkerState) []*types.WorkerState {
	var result []*types.WorkerState
	for _, worker := range workers {
		if worker.Status == types.WorkerOnline && worker.Available > 0 {
			result = append(result, worker)
		}
	}

	return result
}

// selectBestWorker selects the worker with the lowest load ratio
func (s *Scheduler) selectBestWorker(workers []*types.WorkerState) *types.WorkerState {
	if len(workers) == 0 {
		return nil
	}

	// Make a copy to avoid mutating input slice
	workersCopy := make([]*types.WorkerState, len(workers))
	copy(workersCopy, workers)

	// Sort by load ratio (ascending)
	sort.Slice(workersCopy, func(i, j int) bool {
		return workersCopy[i].LoadRatio() < workersCopy[j].LoadRatio()
	})

	return workersCopy[0]
}

// hasAllTags checks if worker has all required tags
func hasAllTags(workerTags, requiredTags []string) bool {
	tagSet := make(map[string]bool)
	for _, tag := range workerTags {
		tagSet[tag] = true
	}

	for _, required := range requiredTags {
		if !tagSet[required] {
			return false
		}
	}

	return true
}
