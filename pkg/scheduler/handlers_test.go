package scheduler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

func TestHandleHeartbeat(t *testing.T) {
	sm := NewStateManager()
	handler := NewHandler(sm)

	hb := &types.Heartbeat{
		WorkerID:     "worker-001",
		Address:      "192.168.1.100:8080",
		ResourceTags: []string{"gpu"},
		MaxTasks:     30,
		CurrentTasks: 10,
		Timestamp:    time.Now().Unix(),
	}

	body, _ := json.Marshal(hb)
	req := httptest.NewRequest("POST", "/api/v1/heartbeat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleHeartbeat(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("HandleHeartbeat() status = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify worker was added
	worker, exists := sm.GetWorker("worker-001")
	if !exists {
		t.Fatal("Worker not found after heartbeat")
	}

	if worker.CurrentTasks != 10 {
		t.Errorf("Worker currentTasks = %d, want 10", worker.CurrentTasks)
	}
}

func TestHandleSchedule(t *testing.T) {
	sm := NewStateManager()
	handler := NewHandler(sm)

	// Add a worker
	sm.UpdateFromHeartbeat(&types.Heartbeat{
		WorkerID:     "worker-001",
		Address:      "192.168.1.100:8080",
		ResourceTags: []string{"gpu"},
		MaxTasks:     30,
		CurrentTasks: 10,
		Timestamp:    time.Now().Unix(),
	})

	req := &types.ScheduleRequest{
		TaskID:       "task-001",
		RequiredTags: []string{"gpu"},
	}

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/api/v1/schedule", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleSchedule(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("HandleSchedule() status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp types.ScheduleResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.WorkerID != "worker-001" {
		t.Errorf("Response workerID = %s, want worker-001", resp.WorkerID)
	}
}

func TestHandleListWorkers(t *testing.T) {
	sm := NewStateManager()
	handler := NewHandler(sm)

	// Add workers
	sm.UpdateFromHeartbeat(&types.Heartbeat{
		WorkerID:     "worker-001",
		Address:      "192.168.1.100:8080",
		ResourceTags: []string{"gpu"},
		MaxTasks:     30,
		CurrentTasks: 10,
		Timestamp:    time.Now().Unix(),
	})

	req := httptest.NewRequest("GET", "/api/v1/workers", nil)
	w := httptest.NewRecorder()

	handler.HandleListWorkers(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("HandleListWorkers() status = %d, want %d", w.Code, http.StatusOK)
	}

	var workers []*types.WorkerState
	json.NewDecoder(w.Body).Decode(&workers)

	if len(workers) != 1 {
		t.Errorf("Response has %d workers, want 1", len(workers))
	}
}

// Error scenario tests

func TestHandleHeartbeat_MethodNotAllowed(t *testing.T) {
	sm := NewStateManager()
	handler := NewHandler(sm)

	req := httptest.NewRequest("GET", "/api/v1/heartbeat", nil)
	w := httptest.NewRecorder()

	handler.HandleHeartbeat(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("HandleHeartbeat() with GET status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleHeartbeat_InvalidJSON(t *testing.T) {
	sm := NewStateManager()
	handler := NewHandler(sm)

	// Send malformed JSON
	body := []byte(`{"workerID": "test", invalid json}`)
	req := httptest.NewRequest("POST", "/api/v1/heartbeat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleHeartbeat(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("HandleHeartbeat() with invalid JSON status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSchedule_MethodNotAllowed(t *testing.T) {
	sm := NewStateManager()
	handler := NewHandler(sm)

	req := httptest.NewRequest("GET", "/api/v1/schedule", nil)
	w := httptest.NewRecorder()

	handler.HandleSchedule(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("HandleSchedule() with GET status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleSchedule_NoAvailableWorkers(t *testing.T) {
	sm := NewStateManager()
	handler := NewHandler(sm)

	// Don't add any workers - should have no available workers
	req := &types.ScheduleRequest{
		TaskID:       "task-001",
		RequiredTags: []string{"gpu"},
	}

	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest("POST", "/api/v1/schedule", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleSchedule(w, httpReq)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("HandleSchedule() with no workers status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	var resp types.ScheduleResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Error == "" {
		t.Error("Expected error message in response when no workers available")
	}
}

func TestHandleListWorkers_MethodNotAllowed(t *testing.T) {
	sm := NewStateManager()
	handler := NewHandler(sm)

	req := httptest.NewRequest("POST", "/api/v1/workers", nil)
	w := httptest.NewRecorder()

	handler.HandleListWorkers(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("HandleListWorkers() with POST status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}
