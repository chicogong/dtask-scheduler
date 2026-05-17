package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

func TestStateManager_CandidatesByTags(t *testing.T) {
	sm := NewStateManager()

	// Register workers via heartbeats so the tag inverted index is populated.
	sm.UpdateFromHeartbeat(&types.Heartbeat{
		WorkerID:     "worker-001",
		ResourceTags: []string{"gpu", "cuda-12.0"},
		MaxTasks:     30,
		CurrentTasks: 10,
	})
	sm.UpdateFromHeartbeat(&types.Heartbeat{
		WorkerID:     "worker-002",
		ResourceTags: []string{"cpu", "avx2"},
		MaxTasks:     30,
		CurrentTasks: 15,
	})
	sm.UpdateFromHeartbeat(&types.Heartbeat{
		WorkerID:     "worker-003",
		ResourceTags: []string{"gpu", "cuda-11.0"},
		MaxTasks:     30,
		CurrentTasks: 25,
	})

	tests := []struct {
		name         string
		requiredTags []string
		wantCount    int
	}{
		{"gpu only", []string{"gpu"}, 2},                           // worker-001, worker-003
		{"specific cuda version", []string{"gpu", "cuda-12.0"}, 1}, // worker-001 only
		{"cpu only", []string{"cpu"}, 1},                           // worker-002
		{"no tags", []string{}, 3},                                 // all workers
		{"unknown tag", []string{"tpu"}, 0},                        // no match
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sm.CandidatesByTags(tt.requiredTags)
			if len(got) != tt.wantCount {
				t.Errorf("CandidatesByTags(%v) = %d workers, want %d",
					tt.requiredTags, len(got), tt.wantCount)
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

	// Add workers via heartbeats so the tag index is populated
	sm.UpdateFromHeartbeat(&types.Heartbeat{
		WorkerID:     "worker-001",
		Address:      "192.168.1.100:8080",
		ResourceTags: []string{"gpu", "cuda-12.0"},
		MaxTasks:     30,
		CurrentTasks: 10,
	})
	sm.UpdateFromHeartbeat(&types.Heartbeat{
		WorkerID:     "worker-002",
		Address:      "192.168.1.101:8080",
		ResourceTags: []string{"cpu"},
		MaxTasks:     30,
		CurrentTasks: 5,
	})

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
			resp := sched.Schedule(context.Background(), tt.req)

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

func TestScheduler_OptimisticAllocation(t *testing.T) {
	sm := NewStateManager()

	// Add a worker with specific capacity via heartbeat
	sm.UpdateFromHeartbeat(&types.Heartbeat{
		WorkerID:     "worker-001",
		Address:      "192.168.1.100:8080",
		ResourceTags: []string{"cpu"},
		MaxTasks:     10,
		CurrentTasks: 5,
	})

	sched := NewScheduler(sm)

	req := &types.ScheduleRequest{
		TaskID:       "task-001",
		RequiredTags: []string{"cpu"},
	}

	// Schedule a task
	resp := sched.Schedule(context.Background(), req)
	if resp.Error != "" {
		t.Fatalf("Schedule() failed: %s", resp.Error)
	}

	// Verify optimistic allocation updated the worker state
	worker, exists := sm.GetWorker("worker-001")
	if !exists {
		t.Fatal("Worker not found after scheduling")
	}

	if worker.CurrentTasks != 6 {
		t.Errorf("CurrentTasks = %d, want 6 (5 + 1 optimistic allocation)", worker.CurrentTasks)
	}

	if worker.Available != 4 {
		t.Errorf("Available = %d, want 4 (5 - 1 optimistic allocation)", worker.Available)
	}
}

func TestScheduler_SelectBestWorkerDoesNotMutateInput(t *testing.T) {
	sm := NewStateManager()
	sched := NewScheduler(sm)

	workers := []*types.WorkerState{
		{
			WorkerID:     "worker-001",
			MaxTasks:     30,
			CurrentTasks: 20,
			Available:    10,
			Status:       types.WorkerOnline,
		},
		{
			WorkerID:     "worker-002",
			MaxTasks:     30,
			CurrentTasks: 10,
			Available:    20,
			Status:       types.WorkerOnline,
		},
		{
			WorkerID:     "worker-003",
			MaxTasks:     30,
			CurrentTasks: 15,
			Available:    15,
			Status:       types.WorkerOnline,
		},
	}

	// Store original order
	originalFirstID := workers[0].WorkerID

	// Call selectBestWorker
	best := sched.selectBestWorker(workers)

	// Verify input slice was not mutated
	if workers[0].WorkerID != originalFirstID {
		t.Errorf("selectBestWorker() mutated input slice: first element changed from %s to %s",
			originalFirstID, workers[0].WorkerID)
	}

	// Verify best worker is correct
	if best.WorkerID != "worker-002" {
		t.Errorf("selectBestWorker() = %s, want worker-002 (lowest load)", best.WorkerID)
	}
}

func TestScheduler_SelectBestWorker_Spread(t *testing.T) {
	sched := NewScheduler(NewStateManager())

	// Three workers with near-equal load ratios (all within 0.05 of 0.30).
	workers := []*types.WorkerState{
		{WorkerID: "w1", MaxTasks: 100, CurrentTasks: 30, Available: 70, Status: types.WorkerOnline}, // 0.30
		{WorkerID: "w2", MaxTasks: 100, CurrentTasks: 32, Available: 68, Status: types.WorkerOnline}, // 0.32
		{WorkerID: "w3", MaxTasks: 100, CurrentTasks: 34, Available: 66, Status: types.WorkerOnline}, // 0.34
	}
	sortedIDs := []string{"w1", "w2", "w3"}

	// With all three within the spread threshold, every index in the tied set
	// must be reachable by the random pick.
	for idx := range sortedIDs {
		want := idx
		sched.pickIndex = func(n int) int {
			if n != len(sortedIDs) {
				t.Errorf("spread set size = %d, want %d", n, len(sortedIDs))
			}
			return want
		}
		best := sched.selectBestWorker(workers)
		if best.WorkerID != sortedIDs[idx] {
			t.Errorf("pickIndex=%d selected %s, want %s", idx, best.WorkerID, sortedIDs[idx])
		}
	}
}

func TestScheduler_SelectBestWorker_NoSpreadWhenGap(t *testing.T) {
	sched := NewScheduler(NewStateManager())
	sched.pickIndex = func(n int) int {
		if n != 1 {
			t.Errorf("tied set size = %d, want 1 (load ratios exceed spread threshold)", n)
		}
		return 0
	}

	workers := []*types.WorkerState{
		{WorkerID: "idle", MaxTasks: 100, CurrentTasks: 10, Available: 90, Status: types.WorkerOnline}, // 0.10
		{WorkerID: "busy", MaxTasks: 100, CurrentTasks: 80, Available: 20, Status: types.WorkerOnline}, // 0.80
	}
	if best := sched.selectBestWorker(workers); best.WorkerID != "idle" {
		t.Errorf("selectBestWorker() = %s, want idle", best.WorkerID)
	}
}

func TestScheduler_Schedule_WaitForCapacity(t *testing.T) {
	sm := NewStateManager()
	sched := NewScheduler(sm)

	resultCh := make(chan *types.ScheduleResponse, 1)
	go func() {
		resultCh <- sched.Schedule(context.Background(), &types.ScheduleRequest{
			TaskID:       "task-001",
			RequiredTags: []string{"gpu"},
			MaxWaitMS:    3000,
		})
	}()

	// No worker yet: the request must still be waiting.
	select {
	case resp := <-resultCh:
		t.Fatalf("Schedule returned early before any worker existed: %+v", resp)
	case <-time.After(100 * time.Millisecond):
	}

	// A GPU worker comes online.
	sm.UpdateFromHeartbeat(&types.Heartbeat{
		WorkerID:     "worker-001",
		Address:      "10.0.0.1:9000",
		ResourceTags: []string{"gpu"},
		MaxTasks:     10,
		CurrentTasks: 0,
	})

	select {
	case resp := <-resultCh:
		if resp.Error != "" {
			t.Fatalf("Schedule failed after capacity freed: %s", resp.Error)
		}
		if resp.WorkerID != "worker-001" {
			t.Errorf("scheduled to %s, want worker-001", resp.WorkerID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Schedule did not return after a worker came online")
	}
}

func TestScheduler_Schedule_WaitTimeout(t *testing.T) {
	sched := NewScheduler(NewStateManager())

	start := time.Now()
	resp := sched.Schedule(context.Background(), &types.ScheduleRequest{
		TaskID:       "task-001",
		RequiredTags: []string{"gpu"},
		MaxWaitMS:    150,
	})
	elapsed := time.Since(start)

	if resp.Error == "" {
		t.Fatal("expected timeout error, got success")
	}
	if elapsed < 150*time.Millisecond {
		t.Errorf("returned after %v, want >= 150ms (MaxWaitMS)", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("returned after %v, far longer than the deadline", elapsed)
	}
}

func TestScheduler_Schedule_WaitContextCanceled(t *testing.T) {
	sched := NewScheduler(NewStateManager())

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan *types.ScheduleResponse, 1)
	go func() {
		resultCh <- sched.Schedule(ctx, &types.ScheduleRequest{
			TaskID:       "task-001",
			RequiredTags: []string{"gpu"},
			MaxWaitMS:    5000,
		})
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case resp := <-resultCh:
		if resp.Error == "" {
			t.Fatal("expected cancellation error, got success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Schedule did not return after context cancellation")
	}
}
