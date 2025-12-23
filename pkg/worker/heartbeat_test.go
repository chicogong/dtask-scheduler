package worker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

// TestHeartbeatSender_SendHeartbeat verifies the heartbeat format and content
func TestHeartbeatSender_SendHeartbeat(t *testing.T) {
	// Create test server to receive heartbeat
	var receivedHeartbeat *types.Heartbeat
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/heartbeat" {
			t.Errorf("Expected /api/v1/heartbeat path, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected application/json content type, got %s", r.Header.Get("Content-Type"))
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("Failed to read request body: %v", err)
		}

		var hb types.Heartbeat
		if err := json.Unmarshal(body, &hb); err != nil {
			t.Fatalf("Failed to unmarshal heartbeat: %v", err)
		}

		receivedHeartbeat = &hb
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create heartbeat sender
	sender := NewHeartbeatSender("test-worker", "localhost:9000", []string{"cpu", "gpu"}, 10, server.URL)

	// Send a heartbeat manually
	sender.send()

	// Verify heartbeat was received
	if receivedHeartbeat == nil {
		t.Fatal("No heartbeat received")
	}

	// Verify heartbeat fields
	if receivedHeartbeat.WorkerID != "test-worker" {
		t.Errorf("Expected WorkerID 'test-worker', got '%s'", receivedHeartbeat.WorkerID)
	}
	if receivedHeartbeat.Address != "localhost:9000" {
		t.Errorf("Expected Address 'localhost:9000', got '%s'", receivedHeartbeat.Address)
	}
	if len(receivedHeartbeat.ResourceTags) != 2 {
		t.Errorf("Expected 2 resource tags, got %d", len(receivedHeartbeat.ResourceTags))
	}
	if receivedHeartbeat.MaxTasks != 10 {
		t.Errorf("Expected MaxTasks 10, got %d", receivedHeartbeat.MaxTasks)
	}
	if receivedHeartbeat.CurrentTasks != 0 {
		t.Errorf("Expected CurrentTasks 0, got %d", receivedHeartbeat.CurrentTasks)
	}
	if receivedHeartbeat.Timestamp == 0 {
		t.Error("Expected non-zero timestamp")
	}

	// Verify timestamp is recent (within last 5 seconds)
	now := time.Now().Unix()
	if now-receivedHeartbeat.Timestamp > 5 {
		t.Errorf("Timestamp too old: %d vs %d", receivedHeartbeat.Timestamp, now)
	}
}

// TestHeartbeatSender_Interval verifies the 3s heartbeat interval
func TestHeartbeatSender_Interval(t *testing.T) {
	var mu sync.Mutex
	heartbeatCount := 0
	heartbeatTimes := []time.Time{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		heartbeatCount++
		heartbeatTimes = append(heartbeatTimes, time.Now())
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewHeartbeatSender("test-worker", "localhost:9000", []string{"cpu"}, 5, server.URL)

	// Start heartbeat sender with context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sender.Start(ctx)

	// Wait for multiple heartbeats (at least 3)
	time.Sleep(7 * time.Second)
	cancel()

	mu.Lock()
	count := heartbeatCount
	times := make([]time.Time, len(heartbeatTimes))
	copy(times, heartbeatTimes)
	mu.Unlock()

	// Verify we received at least 3 heartbeats (1 immediate + 2 from ticker)
	if count < 3 {
		t.Errorf("Expected at least 3 heartbeats, got %d", count)
	}

	// Verify intervals are approximately 3 seconds (allow 500ms tolerance)
	for i := 1; i < len(times); i++ {
		interval := times[i].Sub(times[i-1])
		if interval < 2500*time.Millisecond || interval > 3500*time.Millisecond {
			t.Errorf("Heartbeat interval %v is not close to 3s (expected 2.5-3.5s)", interval)
		}
	}
}

// TestHeartbeatSender_UpdateTaskCount verifies thread-safe task count updates
func TestHeartbeatSender_UpdateTaskCount(t *testing.T) {
	var receivedHeartbeat *types.Heartbeat
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var hb types.Heartbeat
		if err := json.Unmarshal(body, &hb); err != nil {
			t.Errorf("failed to unmarshal heartbeat: %v", err)
			return
		}
		receivedHeartbeat = &hb
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewHeartbeatSender("test-worker", "localhost:9000", []string{"cpu"}, 10, server.URL)

	// Update task count
	sender.UpdateTaskCount(5)

	// Send heartbeat
	sender.send()

	// Verify task count was updated
	if receivedHeartbeat == nil {
		t.Fatal("No heartbeat received")
	}
	if receivedHeartbeat.CurrentTasks != 5 {
		t.Errorf("Expected CurrentTasks 5, got %d", receivedHeartbeat.CurrentTasks)
	}

	// Update again
	sender.UpdateTaskCount(8)
	sender.send()

	if receivedHeartbeat.CurrentTasks != 8 {
		t.Errorf("Expected CurrentTasks 8, got %d", receivedHeartbeat.CurrentTasks)
	}
}

// TestHeartbeatSender_ContextCancellation verifies graceful shutdown
func TestHeartbeatSender_ContextCancellation(t *testing.T) {
	var mu sync.Mutex
	heartbeatCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		heartbeatCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewHeartbeatSender("test-worker", "localhost:9000", []string{"cpu"}, 5, server.URL)

	ctx, cancel := context.WithCancel(context.Background())

	// Start sender in goroutine
	done := make(chan bool)
	go func() {
		sender.Start(ctx)
		done <- true
	}()

	// Wait for initial heartbeat
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	initialCount := heartbeatCount
	mu.Unlock()

	// Cancel context
	cancel()

	// Wait for goroutine to finish (with timeout)
	select {
	case <-done:
		// Success - goroutine exited
	case <-time.After(2 * time.Second):
		t.Fatal("Start() did not exit after context cancellation")
	}

	// Verify no more heartbeats after cancellation
	time.Sleep(3500 * time.Millisecond) // Wait longer than interval
	mu.Lock()
	finalCount := heartbeatCount
	mu.Unlock()
	if finalCount > initialCount+1 {
		t.Errorf("Heartbeats continued after context cancellation: %d -> %d", initialCount, finalCount)
	}
}

// TestHeartbeatSender_HTTPTimeout verifies HTTP client timeout
func TestHeartbeatSender_HTTPTimeout(t *testing.T) {
	// Create slow server that doesn't respond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second) // Longer than timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewHeartbeatSender("test-worker", "localhost:9000", []string{"cpu"}, 5, server.URL)

	// Send should complete quickly due to timeout
	start := time.Now()
	sender.send()
	elapsed := time.Since(start)

	// Should timeout around 5s, not wait 10s
	if elapsed > 6*time.Second {
		t.Errorf("HTTP request took too long: %v (expected ~5s timeout)", elapsed)
	}
	if elapsed < 4*time.Second {
		t.Errorf("HTTP request completed too quickly: %v (expected ~5s timeout)", elapsed)
	}
}
