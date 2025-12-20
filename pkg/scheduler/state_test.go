package scheduler

import (
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
