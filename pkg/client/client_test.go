package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

func TestHeartbeat_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/heartbeat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s", r.Method)
		}

		var hb types.Heartbeat
		if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if hb.WorkerID != "worker-001" {
			t.Errorf("unexpected worker ID: %s", hb.WorkerID)
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	hb := &types.Heartbeat{
		WorkerID:     "worker-001",
		Address:      "localhost:9001",
		ResourceTags: []string{"gpu"},
		MaxTasks:     30,
		CurrentTasks: 10,
		Timestamp:    time.Now().Unix(),
	}

	if err := client.Heartbeat(context.Background(), hb); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSchedule_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/schedule" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req types.ScheduleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.TaskID != "task-001" {
			t.Errorf("unexpected task ID: %s", req.TaskID)
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(&types.ScheduleResponse{
			WorkerID: "worker-001",
			Address:  "192.168.1.100:8080",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	resp, err := client.Schedule(context.Background(), &types.ScheduleRequest{
		TaskID:       "task-001",
		RequiredTags: []string{"gpu"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.WorkerID != "worker-001" {
		t.Errorf("unexpected worker: %s", resp.WorkerID)
	}
	if resp.Address != "192.168.1.100:8080" {
		t.Errorf("unexpected address: %s", resp.Address)
	}
}

func TestSchedule_NoWorkers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(&types.ScheduleResponse{
			Error: "no available worker matching requirements",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	resp, err := client.Schedule(context.Background(), &types.ScheduleRequest{
		TaskID:       "task-001",
		RequiredTags: []string{"gpu"},
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var schedErr *ErrSchedulingFailed
	if !IsSchedulingError(err) {
		t.Errorf("expected ErrSchedulingFailed, got %T: %v", err, err)
	}

	if resp.Error == "" {
		t.Error("expected error in response")
	}

	// Convert to *ErrSchedulingFailed to check reason
	if e, ok := err.(*ErrSchedulingFailed); ok {
		if schedErr = e; schedErr.Reason != "no available worker matching requirements" {
			t.Errorf("unexpected reason: %s", schedErr.Reason)
		}
	}
}

func TestListWorkers_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/workers" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "GET" {
			t.Errorf("unexpected method: %s", r.Method)
		}

		workers := []*types.WorkerState{
			{
				WorkerID:     "worker-001",
				Address:      "192.168.1.100:8080",
				ResourceTags: []string{"gpu"},
				MaxTasks:     30,
				CurrentTasks: 10,
				Status:       types.WorkerOnline,
			},
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(workers)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	workers, err := client.ListWorkers(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(workers) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(workers))
	}
	if workers[0].WorkerID != "worker-001" {
		t.Errorf("unexpected worker ID: %s", workers[0].WorkerID)
	}
}

func TestListWorkers_EmptyList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]*types.WorkerState{})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	workers, err := client.ListWorkers(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(workers) != 0 {
		t.Errorf("expected 0 workers, got %d", len(workers))
	}
}

func TestWithTimeout(t *testing.T) {
	timeout := 10 * time.Second
	client := NewClient("http://localhost:8080", WithTimeout(timeout))

	if client.httpClient.Timeout != timeout {
		t.Errorf("expected timeout %v, got %v", timeout, client.httpClient.Timeout)
	}
}

func TestWithHTTPClient(t *testing.T) {
	customClient := &http.Client{Timeout: 15 * time.Second}
	client := NewClient("http://localhost:8080", WithHTTPClient(customClient))

	if client.httpClient != customClient {
		t.Error("expected custom HTTP client to be set")
	}
	if client.httpClient.Timeout != 15*time.Second {
		t.Errorf("expected timeout 15s, got %v", client.httpClient.Timeout)
	}
}

func TestSchedule_InvalidRequest(t *testing.T) {
	client := NewClient("http://localhost:8080")
	_, err := client.Schedule(context.Background(), &types.ScheduleRequest{
		TaskID: "", // Invalid: empty task ID
	})

	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
}
