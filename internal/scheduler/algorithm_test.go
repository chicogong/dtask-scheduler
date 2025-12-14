package scheduler

import (
	"testing"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

func TestScheduler_FilterByTags(t *testing.T) {
	sm := NewStateManager()

	// Add workers with different tags
	workers := []*types.WorkerState{
		{
			WorkerID:     "worker-001",
			ResourceTags: []string{"gpu", "cuda-12.0"},
			MaxTasks:     30,
			CurrentTasks: 10,
			Available:    20,
			Status:       types.WorkerOnline,
		},
		{
			WorkerID:     "worker-002",
			ResourceTags: []string{"cpu", "avx2"},
			MaxTasks:     30,
			CurrentTasks: 15,
			Available:    15,
			Status:       types.WorkerOnline,
		},
		{
			WorkerID:     "worker-003",
			ResourceTags: []string{"gpu", "cuda-11.0"},
			MaxTasks:     30,
			CurrentTasks: 25,
			Available:    5,
			Status:       types.WorkerOnline,
		},
	}

	for _, w := range workers {
		sm.workers[w.WorkerID] = w
	}

	sched := NewScheduler(sm)

	tests := []struct {
		name         string
		requiredTags []string
		wantCount    int
	}{
		{
			name:         "gpu only",
			requiredTags: []string{"gpu"},
			wantCount:    2, // worker-001, worker-003
		},
		{
			name:         "specific cuda version",
			requiredTags: []string{"gpu", "cuda-12.0"},
			wantCount:    1, // worker-001 only
		},
		{
			name:         "cpu only",
			requiredTags: []string{"cpu"},
			wantCount:    1, // worker-002
		},
		{
			name:         "no tags",
			requiredTags: []string{},
			wantCount:    3, // all workers
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates := sched.filterByTags(workers, tt.requiredTags)
			if len(candidates) != tt.wantCount {
				t.Errorf("filterByTags() returned %d workers, want %d", len(candidates), tt.wantCount)
			}
		})
	}
}

func TestScheduler_SelectBestWorker(t *testing.T) {
	sm := NewStateManager()
	sched := NewScheduler(sm)

	workers := []*types.WorkerState{
		{
			WorkerID:     "worker-001",
			MaxTasks:     30,
			CurrentTasks: 15, // load ratio: 0.50
			Available:    15,
			Status:       types.WorkerOnline,
		},
		{
			WorkerID:     "worker-002",
			MaxTasks:     30,
			CurrentTasks: 10, // load ratio: 0.33 (lowest, should be selected)
			Available:    20,
			Status:       types.WorkerOnline,
		},
		{
			WorkerID:     "worker-003",
			MaxTasks:     30,
			CurrentTasks: 20, // load ratio: 0.67
			Available:    10,
			Status:       types.WorkerOnline,
		},
	}

	best := sched.selectBestWorker(workers)
	if best == nil {
		t.Fatal("selectBestWorker() returned nil")
	}

	if best.WorkerID != "worker-002" {
		t.Errorf("selectBestWorker() = %s, want worker-002 (lowest load)", best.WorkerID)
	}
}

func TestScheduler_Schedule(t *testing.T) {
	sm := NewStateManager()

	// Add workers
	sm.workers = map[string]*types.WorkerState{
		"worker-001": {
			WorkerID:     "worker-001",
			Address:      "192.168.1.100:8080",
			ResourceTags: []string{"gpu", "cuda-12.0"},
			MaxTasks:     30,
			CurrentTasks: 10,
			Available:    20,
			Status:       types.WorkerOnline,
		},
		"worker-002": {
			WorkerID:     "worker-002",
			Address:      "192.168.1.101:8080",
			ResourceTags: []string{"cpu"},
			MaxTasks:     30,
			CurrentTasks: 5,
			Available:    25,
			Status:       types.WorkerOnline,
		},
	}

	sched := NewScheduler(sm)

	tests := []struct {
		name         string
		req          *types.ScheduleRequest
		wantWorkerID string
		wantError    bool
	}{
		{
			name: "schedule to gpu worker",
			req: &types.ScheduleRequest{
				TaskID:       "task-001",
				RequiredTags: []string{"gpu"},
			},
			wantWorkerID: "worker-001",
			wantError:    false,
		},
		{
			name: "schedule to cpu worker",
			req: &types.ScheduleRequest{
				TaskID:       "task-002",
				RequiredTags: []string{"cpu"},
			},
			wantWorkerID: "worker-002",
			wantError:    false,
		},
		{
			name: "no available worker",
			req: &types.ScheduleRequest{
				TaskID:       "task-003",
				RequiredTags: []string{"tpu"}, // no worker has this
			},
			wantWorkerID: "",
			wantError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := sched.Schedule(tt.req)

			if tt.wantError {
				if resp.Error == "" {
					t.Error("Schedule() expected error, got none")
				}
			} else {
				if resp.Error != "" {
					t.Errorf("Schedule() unexpected error: %s", resp.Error)
				}
				if resp.WorkerID != tt.wantWorkerID {
					t.Errorf("Schedule() workerID = %s, want %s", resp.WorkerID, tt.wantWorkerID)
				}
			}
		})
	}
}
