package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

func TestStateManager_UpdateFromHeartbeat(t *testing.T) {
	sm := NewStateManager()

	hb := &types.Heartbeat{
		WorkerID:     "worker-001",
		Address:      "192.168.1.100:8080",
		ResourceTags: []string{"gpu", "cuda-12.0"},
		MaxTasks:     30,
		CurrentTasks: 15,
		Timestamp:    time.Now().Unix(),
	}

	sm.UpdateFromHeartbeat(hb)

	worker, exists := sm.GetWorker("worker-001")
	if !exists {
		t.Fatal("Worker not found after update")
	}

	if worker.CurrentTasks != 15 {
		t.Errorf("CurrentTasks = %d, want 15", worker.CurrentTasks)
	}

	if worker.Available != 15 {
		t.Errorf("Available = %d, want 15", worker.Available)
	}

	if worker.Status != types.WorkerOnline {
		t.Errorf("Status = %s, want online", worker.Status)
	}
}

func TestStateManager_ListWorkers(t *testing.T) {
	sm := NewStateManager()

	// Add two workers
	sm.UpdateFromHeartbeat(&types.Heartbeat{
		WorkerID:     "worker-001",
		Address:      "192.168.1.100:8080",
		ResourceTags: []string{"gpu"},
		MaxTasks:     30,
		CurrentTasks: 10,
		Timestamp:    time.Now().Unix(),
	})

	sm.UpdateFromHeartbeat(&types.Heartbeat{
		WorkerID:     "worker-002",
		Address:      "192.168.1.101:8080",
		ResourceTags: []string{"cpu"},
		MaxTasks:     30,
		CurrentTasks: 20,
		Timestamp:    time.Now().Unix(),
	})

	workers := sm.ListWorkers()
	if len(workers) != 2 {
		t.Errorf("ListWorkers() returned %d workers, want 2", len(workers))
	}
}

func TestStateManager_TimeoutDetection(t *testing.T) {
	sm := NewStateManager()

	// Add worker with old heartbeat
	sm.UpdateFromHeartbeat(&types.Heartbeat{
		WorkerID:     "worker-001",
		Address:      "192.168.1.100:8080",
		ResourceTags: []string{"gpu"},
		MaxTasks:     30,
		CurrentTasks: 10,
		Timestamp:    time.Now().Unix(),
	})

	// Manually set the LastHeartbeat to simulate old heartbeat
	sm.mu.Lock()
	if worker, exists := sm.workers["worker-001"]; exists {
		worker.LastHeartbeat = time.Now().Add(-25 * time.Second)
	}
	sm.mu.Unlock()

	sm.CheckTimeouts()

	worker, _ := sm.GetWorker("worker-001")
	if worker.Status != types.WorkerOffline {
		t.Errorf("Worker status = %s, want offline after timeout", worker.Status)
	}
}

func TestStateManager_RetagWorker(t *testing.T) {
	sm := NewStateManager()

	// Worker first registers as a GPU worker.
	sm.UpdateFromHeartbeat(&types.Heartbeat{
		WorkerID:     "worker-001",
		ResourceTags: []string{"gpu", "cuda-12.0"},
		MaxTasks:     30,
		CurrentTasks: 0,
	})
	if got := sm.CandidatesByTags([]string{"gpu"}); len(got) != 1 {
		t.Fatalf("expected worker to match gpu, got %d candidates", len(got))
	}

	// Same worker re-registers with a different tag set (e.g. hardware reassigned).
	sm.UpdateFromHeartbeat(&types.Heartbeat{
		WorkerID:     "worker-001",
		ResourceTags: []string{"cpu", "avx2"},
		MaxTasks:     30,
		CurrentTasks: 0,
	})

	if got := sm.CandidatesByTags([]string{"gpu"}); len(got) != 0 {
		t.Errorf("stale gpu tag still indexed after retag: %d candidates", len(got))
	}
	if got := sm.CandidatesByTags([]string{"cpu"}); len(got) != 1 {
		t.Errorf("new cpu tag not indexed after retag: %d candidates", len(got))
	}
}

func TestStateManager_RunTimeoutChecker(t *testing.T) {
	// Tight thresholds so the test completes quickly.
	sm := NewStateManagerWithConfig(Config{
		SuspiciousThreshold:  20 * time.Millisecond,
		OfflineThreshold:     40 * time.Millisecond,
		TimeoutCheckInterval: 10 * time.Millisecond,
	})

	sm.UpdateFromHeartbeat(&types.Heartbeat{
		WorkerID: "worker-001",
		MaxTasks: 10,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sm.RunTimeoutChecker(ctx)

	// With no further heartbeats, the background checker must eventually mark
	// the worker offline once the offline threshold elapses.
	deadline := time.After(2 * time.Second)
	for {
		if w, ok := sm.GetWorker("worker-001"); ok && w.Status == types.WorkerOffline {
			return // success
		}
		select {
		case <-deadline:
			t.Fatal("RunTimeoutChecker did not mark the stale worker offline")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
