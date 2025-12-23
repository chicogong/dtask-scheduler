package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/scheduler"
	"github.com/chicogong/dtask-scheduler/pkg/types"
)

func TestEndToEndScheduling(t *testing.T) {
	// Setup
	state := scheduler.NewStateManager()
	handler := scheduler.NewHandler(state)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/heartbeat", handler.HandleHeartbeat)
	mux.HandleFunc("/api/v1/schedule", handler.HandleSchedule)
	mux.HandleFunc("/api/v1/workers", handler.HandleListWorkers)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Step 1: Send heartbeats from two workers
	workers := []types.Heartbeat{
		{
			WorkerID:     "worker-001",
			Address:      "192.168.1.100:8080",
			ResourceTags: []string{"gpu", "cuda-12.0"},
			MaxTasks:     30,
			CurrentTasks: 10,
			Timestamp:    time.Now().Unix(),
		},
		{
			WorkerID:     "worker-002",
			Address:      "192.168.1.101:8080",
			ResourceTags: []string{"cpu", "avx2"},
			MaxTasks:     30,
			CurrentTasks: 5,
			Timestamp:    time.Now().Unix(),
		},
	}

	for _, hb := range workers {
		body, _ := json.Marshal(hb)
		resp, err := http.Post(server.URL+"/api/v1/heartbeat", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("Failed to send heartbeat: %v", err)
		}
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Heartbeat status = %d, want 200", resp.StatusCode)
		}
	}

	// Step 2: List workers
	resp, err := http.Get(server.URL + "/api/v1/workers")
	if err != nil {
		t.Fatalf("Failed to list workers: %v", err)
	}
	defer resp.Body.Close()

	var listedWorkers []*types.WorkerState
	if err := json.NewDecoder(resp.Body).Decode(&listedWorkers); err != nil {
		t.Fatalf("Failed to decode workers: %v", err)
	}

	if len(listedWorkers) != 2 {
		t.Errorf("Listed %d workers, want 2", len(listedWorkers))
	}

	// Step 3: Schedule task to GPU worker
	schedReq := types.ScheduleRequest{
		TaskID:       "task-001",
		RequiredTags: []string{"gpu"},
	}

	body, _ := json.Marshal(schedReq)
	resp, err = http.Post(server.URL+"/api/v1/schedule", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to schedule: %v", err)
	}
	defer resp.Body.Close()

	var schedResp types.ScheduleResponse
	if err := json.NewDecoder(resp.Body).Decode(&schedResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if schedResp.Error != "" {
		t.Errorf("Schedule error: %s", schedResp.Error)
	}

	if schedResp.WorkerID != "worker-001" {
		t.Errorf("Scheduled to %s, want worker-001 (GPU worker)", schedResp.WorkerID)
	}

	// Step 4: Schedule task to CPU worker
	schedReq2 := types.ScheduleRequest{
		TaskID:       "task-002",
		RequiredTags: []string{"cpu"},
	}

	body, _ = json.Marshal(schedReq2)
	resp, err = http.Post(server.URL+"/api/v1/schedule", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to schedule: %v", err)
	}
	defer resp.Body.Close()

	var schedResp2 types.ScheduleResponse
	if err := json.NewDecoder(resp.Body).Decode(&schedResp2); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if schedResp2.WorkerID != "worker-002" {
		t.Errorf("Scheduled to %s, want worker-002 (CPU worker)", schedResp2.WorkerID)
	}

	// Step 5: Verify load balancing - lower load worker should be selected
	// worker-001: 11/30 = 0.37 (after first schedule)
	// worker-002: 6/30 = 0.20 (after second schedule, lower load)

	schedReq3 := types.ScheduleRequest{
		TaskID:       "task-003",
		RequiredTags: []string{}, // any worker
	}

	body, _ = json.Marshal(schedReq3)
	resp, err = http.Post(server.URL+"/api/v1/schedule", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to schedule: %v", err)
	}
	defer resp.Body.Close()

	var schedResp3 types.ScheduleResponse
	if err := json.NewDecoder(resp.Body).Decode(&schedResp3); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if schedResp3.WorkerID != "worker-002" {
		t.Errorf("Load balancing: scheduled to %s, want worker-002 (lower load)", schedResp3.WorkerID)
	}
}
