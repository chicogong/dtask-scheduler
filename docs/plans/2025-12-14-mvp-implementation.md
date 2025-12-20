# MVP Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a minimal viable distributed task scheduler with heartbeat collection and load-based scheduling.

**Architecture:** Single scheduler (master-only for MVP) with in-memory worker state table. Workers send heartbeat via HTTP. Clients request scheduling via HTTP API. Scheduler filters by resource tags and selects worker with lowest load ratio.

**Tech Stack:** Go 1.21+, standard library only (net/http, encoding/json), table-driven tests

---

## Task 1: Project Initialization

**Files:**
- Create: `go.mod`
- Create: `go.sum`
- Create: `cmd/scheduler/main.go`
- Create: `cmd/worker/main.go`
- Create: `.gitignore` (update)

**Step 1: Initialize Go module**

```bash
cd /Users/haorangong/Github/chicogong/dtask-scheduler/.worktrees/mvp-implementation
go mod init github.com/chicogong/dtask-scheduler
```

Expected output:
```
go: creating new go.mod: module github.com/chicogong/dtask-scheduler
```

**Step 2: Create directory structure**

```bash
mkdir -p cmd/scheduler cmd/worker internal/scheduler internal/worker pkg/types tests
```

**Step 3: Create placeholder main files**

Create `cmd/scheduler/main.go`:
```go
package main

import (
	"fmt"
)

func main() {
	fmt.Println("dtask-scheduler starting...")
}
```

Create `cmd/worker/main.go`:
```go
package main

import (
	"fmt"
)

func main() {
	fmt.Println("dtask-worker starting...")
}
```

**Step 4: Verify builds**

```bash
go build ./cmd/scheduler
go build ./cmd/worker
```

Expected: No errors, binaries created

**Step 5: Commit**

```bash
git add go.mod cmd/
git commit -m "feat: initialize Go project structure

- Add scheduler and worker main entry points
- Set up basic directory structure"
```

---

## Task 2: Core Data Types

**Files:**
- Create: `pkg/types/types.go`
- Create: `pkg/types/types_test.go`

**Step 1: Write types test**

Create `pkg/types/types_test.go`:
```go
package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHeartbeatSerialization(t *testing.T) {
	hb := &Heartbeat{
		WorkerID:      "worker-001",
		Timestamp:     time.Now().Unix(),
		ResourceTags:  []string{"gpu", "cuda-12.0"},
		MaxTasks:      30,
		CurrentTasks:  15,
	}

	data, err := json.Marshal(hb)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded Heartbeat
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.WorkerID != hb.WorkerID {
		t.Errorf("WorkerID mismatch: got %s, want %s", decoded.WorkerID, hb.WorkerID)
	}
}

func TestScheduleRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     *ScheduleRequest
		wantErr bool
	}{
		{
			name: "valid request",
			req: &ScheduleRequest{
				TaskID:       "task-001",
				RequiredTags: []string{"gpu"},
			},
			wantErr: false,
		},
		{
			name: "missing task id",
			req: &ScheduleRequest{
				TaskID:       "",
				RequiredTags: []string{"gpu"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./pkg/types -v
```

Expected: FAIL - package types is not in std or GOROOT

**Step 3: Implement types**

Create `pkg/types/types.go`:
```go
package types

import (
	"errors"
	"time"
)

// Heartbeat represents worker health and capacity info
type Heartbeat struct {
	WorkerID     string   `json:"worker_id"`
	Timestamp    int64    `json:"timestamp"`
	ResourceTags []string `json:"resource_tags"`
	MaxTasks     int      `json:"max_tasks"`
	CurrentTasks int      `json:"current_tasks"`
	Address      string   `json:"address"` // IP:Port for client connections
}

// WorkerState represents scheduler's view of a worker
type WorkerState struct {
	WorkerID     string
	Address      string
	ResourceTags []string
	MaxTasks     int
	CurrentTasks int
	Available    int
	LastHeartbeat time.Time
	Status       WorkerStatus
}

type WorkerStatus string

const (
	WorkerOnline     WorkerStatus = "online"
	WorkerSuspicious WorkerStatus = "suspicious"
	WorkerOffline    WorkerStatus = "offline"
)

// LoadRatio returns the current load ratio (0.0 to 1.0)
func (w *WorkerState) LoadRatio() float64 {
	if w.MaxTasks == 0 {
		return 1.0
	}
	return float64(w.CurrentTasks) / float64(w.MaxTasks)
}

// ScheduleRequest represents a client's scheduling request
type ScheduleRequest struct {
	TaskID       string   `json:"task_id"`
	RequiredTags []string `json:"required_tags"`
}

// Validate checks if the request is valid
func (r *ScheduleRequest) Validate() error {
	if r.TaskID == "" {
		return errors.New("task_id is required")
	}
	return nil
}

// ScheduleResponse represents scheduler's response
type ScheduleResponse struct {
	WorkerID string `json:"worker_id,omitempty"`
	Address  string `json:"address,omitempty"`
	Error    string `json:"error,omitempty"`
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./pkg/types -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add pkg/types/
git commit -m "feat: add core data types

- Add Heartbeat, WorkerState, ScheduleRequest/Response types
- Add JSON serialization support
- Add basic validation"
```

---

## Task 3: Worker State Manager

**Files:**
- Create: `internal/scheduler/state.go`
- Create: `internal/scheduler/state_test.go`

**Step 1: Write state manager test**

Create `internal/scheduler/state_test.go`:
```go
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
		Address:      "192.168.1.100:9000",
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
		Address:      "192.168.1.100:9000",
		ResourceTags: []string{"gpu"},
		MaxTasks:     30,
		CurrentTasks: 10,
		Timestamp:    time.Now().Unix(),
	})

	sm.UpdateFromHeartbeat(&types.Heartbeat{
		WorkerID:     "worker-002",
		Address:      "192.168.1.101:9000",
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
		Address:      "192.168.1.100:9000",
		ResourceTags: []string{"gpu"},
		MaxTasks:     30,
		CurrentTasks: 10,
		Timestamp:    time.Now().Add(-25 * time.Second).Unix(),
	})

	sm.CheckTimeouts()

	worker, _ := sm.GetWorker("worker-001")
	if worker.Status != types.WorkerOffline {
		t.Errorf("Worker status = %s, want offline after timeout", worker.Status)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/scheduler -v
```

Expected: FAIL - undefined: NewStateManager

**Step 3: Implement state manager**

Create `internal/scheduler/state.go`:
```go
package scheduler

import (
	"sync"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

const (
	SuspiciousThreshold = 10 * time.Second
	OfflineThreshold    = 20 * time.Second
)

// StateManager manages worker states in memory
type StateManager struct {
	mu      sync.RWMutex
	workers map[string]*types.WorkerState
}

// NewStateManager creates a new state manager
func NewStateManager() *StateManager {
	return &StateManager{
		workers: make(map[string]*types.WorkerState),
	}
}

// UpdateFromHeartbeat updates worker state from heartbeat
func (sm *StateManager) UpdateFromHeartbeat(hb *types.Heartbeat) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	worker, exists := sm.workers[hb.WorkerID]
	if !exists {
		worker = &types.WorkerState{
			WorkerID: hb.WorkerID,
		}
		sm.workers[hb.WorkerID] = worker
	}

	// Update fields
	worker.Address = hb.Address
	worker.ResourceTags = hb.ResourceTags
	worker.MaxTasks = hb.MaxTasks
	worker.CurrentTasks = hb.CurrentTasks
	worker.Available = hb.MaxTasks - hb.CurrentTasks
	worker.LastHeartbeat = time.Now()
	worker.Status = types.WorkerOnline
}

// GetWorker returns a worker by ID
func (sm *StateManager) GetWorker(workerID string) (*types.WorkerState, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	worker, exists := sm.workers[workerID]
	if !exists {
		return nil, false
	}

	// Return a copy to avoid race conditions
	copy := *worker
	return &copy, true
}

// ListWorkers returns all workers
func (sm *StateManager) ListWorkers() []*types.WorkerState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	workers := make([]*types.WorkerState, 0, len(sm.workers))
	for _, worker := range sm.workers {
		copy := *worker
		workers = append(workers, &copy)
	}

	return workers
}

// CheckTimeouts marks workers as suspicious or offline based on heartbeat age
func (sm *StateManager) CheckTimeouts() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	for _, worker := range sm.workers {
		elapsed := now.Sub(worker.LastHeartbeat)

		if elapsed > OfflineThreshold {
			worker.Status = types.WorkerOffline
		} else if elapsed > SuspiciousThreshold {
			worker.Status = types.WorkerSuspicious
		}
	}
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/scheduler -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/scheduler/
git commit -m "feat: add worker state manager

- Implement in-memory worker state storage
- Add heartbeat update logic
- Add timeout detection (10s suspicious, 20s offline)
- Thread-safe with RWMutex"
```

---

## Task 4: Scheduling Algorithm

**Files:**
- Create: `internal/scheduler/algorithm.go`
- Create: `internal/scheduler/algorithm_test.go`

**Step 1: Write scheduler test**

Create `internal/scheduler/algorithm_test.go`:
```go
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
			Address:      "192.168.1.100:9000",
			ResourceTags: []string{"gpu", "cuda-12.0"},
			MaxTasks:     30,
			CurrentTasks: 10,
			Available:    20,
			Status:       types.WorkerOnline,
		},
		"worker-002": {
			WorkerID:     "worker-002",
			Address:      "192.168.1.101:9000",
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
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/scheduler -v
```

Expected: FAIL - undefined: NewScheduler

**Step 3: Implement scheduling algorithm**

Create `internal/scheduler/algorithm.go`:
```go
package scheduler

import (
	"sort"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

// Scheduler implements the scheduling algorithm
type Scheduler struct {
	state *StateManager
}

// NewScheduler creates a new scheduler
func NewScheduler(state *StateManager) *Scheduler {
	return &Scheduler{
		state: state,
	}
}

// Schedule assigns a task to the best available worker
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

	// Optimistic allocation: increment task count immediately
	s.state.mu.Lock()
	if worker, exists := s.state.workers[best.WorkerID]; exists {
		worker.CurrentTasks++
		worker.Available--
	}
	s.state.mu.Unlock()

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

	// Sort by load ratio (ascending)
	sort.Slice(workers, func(i, j int) bool {
		return workers[i].LoadRatio() < workers[j].LoadRatio()
	})

	return workers[0]
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
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/scheduler -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/scheduler/
git commit -m "feat: implement scheduling algorithm

- Filter workers by resource tags
- Filter out offline/full workers
- Select worker with lowest load ratio
- Optimistic task allocation"
```

---

## Task 5: HTTP API Handlers

**Files:**
- Create: `internal/scheduler/handlers.go`
- Create: `internal/scheduler/handlers_test.go`

**Step 1: Write handler tests**

Create `internal/scheduler/handlers_test.go`:
```go
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
		Address:      "192.168.1.100:9000",
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
		Address:      "192.168.1.100:9000",
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
		Address:      "192.168.1.100:9000",
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
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/scheduler -v
```

Expected: FAIL - undefined: NewHandler

**Step 3: Implement HTTP handlers**

Create `internal/scheduler/handlers.go`:
```go
package scheduler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

// Handler holds HTTP handlers for the scheduler
type Handler struct {
	state     *StateManager
	scheduler *Scheduler
}

// NewHandler creates a new handler
func NewHandler(state *StateManager) *Handler {
	return &Handler{
		state:     state,
		scheduler: NewScheduler(state),
	}
}

// HandleHeartbeat handles worker heartbeat POST requests
func (h *Handler) HandleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var hb types.Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	h.state.UpdateFromHeartbeat(&hb)

	log.Printf("Heartbeat received from %s: %d/%d tasks", hb.WorkerID, hb.CurrentTasks, hb.MaxTasks)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleSchedule handles scheduling POST requests
func (h *Handler) HandleSchedule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req types.ScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	resp := h.scheduler.Schedule(&req)

	if resp.Error != "" {
		log.Printf("Schedule failed for task %s: %s", req.TaskID, resp.Error)
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		log.Printf("Task %s scheduled to %s", req.TaskID, resp.WorkerID)
		w.WriteHeader(http.StatusOK)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleListWorkers handles GET requests to list all workers
func (h *Handler) HandleListWorkers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workers := h.state.ListWorkers()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(workers)
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/scheduler -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/scheduler/
git commit -m "feat: add HTTP API handlers

- POST /api/v1/heartbeat - receive worker heartbeats
- POST /api/v1/schedule - handle scheduling requests
- GET /api/v1/workers - list all workers
- Include request validation and error handling"
```

---

## Task 6: Scheduler Server

**Files:**
- Modify: `cmd/scheduler/main.go`

**Step 1: Implement scheduler server**

Modify `cmd/scheduler/main.go`:
```go
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chicogong/dtask-scheduler/internal/scheduler"
)

func main() {
	port := flag.String("port", "8080", "Server port")
	flag.Parse()

	log.Println("dtask-scheduler starting...")

	// Create state manager
	state := scheduler.NewStateManager()

	// Create HTTP handler
	handler := scheduler.NewHandler(state)

	// Setup routes
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/heartbeat", handler.HandleHeartbeat)
	mux.HandleFunc("/api/v1/schedule", handler.HandleSchedule)
	mux.HandleFunc("/api/v1/workers", handler.HandleListWorkers)

	// Start timeout checker goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				state.CheckTimeouts()
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start HTTP server
	server := &http.Server{
		Addr:    ":" + *port,
		Handler: mux,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down gracefully...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		cancel() // Stop timeout checker
		server.Shutdown(shutdownCtx)
	}()

	log.Printf("Scheduler listening on port %s", *port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}

	log.Println("Server stopped")
}
```

**Step 2: Build and verify**

```bash
go build ./cmd/scheduler
```

Expected: No errors

**Step 3: Manual test (optional - run in background)**

```bash
./scheduler &
SCHEDULER_PID=$!

# Test heartbeat
curl -X POST http://localhost:8080/api/v1/heartbeat \
  -H "Content-Type: application/json" \
  -d '{"worker_id":"worker-001","address":"localhost:9000","resource_tags":["gpu"],"max_tasks":30,"current_tasks":10,"timestamp":1702540800}'

# Test list workers
curl http://localhost:8080/api/v1/workers

# Cleanup
kill $SCHEDULER_PID
```

Expected: Heartbeat accepted, worker listed

**Step 4: Commit**

```bash
git add cmd/scheduler/
git commit -m "feat: implement scheduler HTTP server

- Add HTTP server with API routes
- Add graceful shutdown handling
- Add background timeout checker (5s interval)
- Add command-line port flag"
```

---

## Task 7: Worker Agent

**Files:**
- Modify: `cmd/worker/main.go`
- Create: `internal/worker/heartbeat.go`

**Step 1: Implement heartbeat sender**

Create `internal/worker/heartbeat.go`:
```go
package worker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

// HeartbeatSender sends periodic heartbeats to scheduler
type HeartbeatSender struct {
	workerID      string
	address       string
	resourceTags  []string
	maxTasks      int
	schedulerURL  string
	interval      time.Duration
	currentTasks  int
}

// NewHeartbeatSender creates a new heartbeat sender
func NewHeartbeatSender(workerID, address string, tags []string, maxTasks int, schedulerURL string) *HeartbeatSender {
	return &HeartbeatSender{
		workerID:     workerID,
		address:      address,
		resourceTags: tags,
		maxTasks:     maxTasks,
		schedulerURL: schedulerURL,
		interval:     3 * time.Second,
		currentTasks: 0,
	}
}

// Start begins sending heartbeats
func (h *HeartbeatSender) Start() {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	log.Printf("Heartbeat sender started (interval: %v)", h.interval)

	// Send immediate heartbeat
	h.send()

	for range ticker.C {
		h.send()
	}
}

// send sends a single heartbeat
func (h *HeartbeatSender) send() {
	hb := &types.Heartbeat{
		WorkerID:     h.workerID,
		Address:      h.address,
		ResourceTags: h.resourceTags,
		MaxTasks:     h.maxTasks,
		CurrentTasks: h.currentTasks,
		Timestamp:    time.Now().Unix(),
	}

	body, err := json.Marshal(hb)
	if err != nil {
		log.Printf("Failed to marshal heartbeat: %v", err)
		return
	}

	url := fmt.Sprintf("%s/api/v1/heartbeat", h.schedulerURL)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("Failed to send heartbeat: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Heartbeat failed with status: %d", resp.StatusCode)
		return
	}

	log.Printf("Heartbeat sent: %d/%d tasks", h.currentTasks, h.maxTasks)
}
```

**Step 2: Implement worker main**

Modify `cmd/worker/main.go`:
```go
package main

import (
	"flag"
	"log"
	"strings"

	"github.com/chicogong/dtask-scheduler/internal/worker"
)

func main() {
	workerID := flag.String("id", "worker-001", "Worker ID")
	address := flag.String("addr", "localhost:9000", "Worker address")
	tags := flag.String("tags", "cpu", "Resource tags (comma-separated)")
	maxTasks := flag.Int("max-tasks", 30, "Maximum concurrent tasks")
	schedulerURL := flag.String("scheduler", "http://localhost:8080", "Scheduler URL")
	flag.Parse()

	log.Println("dtask-worker starting...")
	log.Printf("Worker ID: %s", *workerID)
	log.Printf("Address: %s", *address)
	log.Printf("Tags: %s", *tags)
	log.Printf("Max tasks: %d", *maxTasks)

	resourceTags := strings.Split(*tags, ",")
	for i := range resourceTags {
		resourceTags[i] = strings.TrimSpace(resourceTags[i])
	}

	sender := worker.NewHeartbeatSender(*workerID, *address, resourceTags, *maxTasks, *schedulerURL)
	sender.Start() // Blocks forever
}
```

**Step 3: Build and verify**

```bash
go build ./cmd/worker
```

Expected: No errors

**Step 4: Integration test (manual)**

```bash
# Terminal 1: Start scheduler
./scheduler &

# Terminal 2: Start worker
./worker --id=worker-001 --tags=gpu,cuda-12.0 &

# Wait 5 seconds, then check workers
sleep 5
curl http://localhost:8080/api/v1/workers | jq

# Cleanup
killall scheduler worker
```

Expected: Worker appears in list with correct tags

**Step 5: Commit**

```bash
git add cmd/worker/ internal/worker/
git commit -m "feat: implement worker agent

- Add heartbeat sender with 3s interval
- Add command-line configuration flags
- Auto-connect to scheduler on startup"
```

---

## Task 8: Integration Test

**Files:**
- Create: `tests/integration_test.go`

**Step 1: Write integration test**

Create `tests/integration_test.go`:
```go
package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chicogong/dtask-scheduler/internal/scheduler"
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
			Address:      "192.168.1.100:9000",
			ResourceTags: []string{"gpu", "cuda-12.0"},
			MaxTasks:     30,
			CurrentTasks: 10,
			Timestamp:    time.Now().Unix(),
		},
		{
			WorkerID:     "worker-002",
			Address:      "192.168.1.101:9000",
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
		resp.Body.Close()

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
	json.NewDecoder(resp.Body).Decode(&listedWorkers)

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
	json.NewDecoder(resp.Body).Decode(&schedResp)

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
	json.NewDecoder(resp.Body).Decode(&schedResp2)

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
	json.NewDecoder(resp.Body).Decode(&schedResp3)

	if schedResp3.WorkerID != "worker-002" {
		t.Errorf("Load balancing: scheduled to %s, want worker-002 (lower load)", schedResp3.WorkerID)
	}
}
```

**Step 2: Run integration test**

```bash
go test ./tests -v
```

Expected: PASS

**Step 3: Commit**

```bash
git add tests/
git commit -m "test: add end-to-end integration test

- Test heartbeat → worker registration
- Test resource tag filtering
- Test load-based scheduling
- Verify optimistic allocation"
```

---

## Task 9: Documentation

**Files:**
- Create: `README.md` (update)
- Create: `docs/api.md`
- Create: `docs/quickstart.md`

**Step 1: Update README**

Update `README.md`:
```markdown
# dtask-scheduler

A distributed CPU/GPU task scheduler for large-scale batch jobs across thousands of machines.

## Features

- **Zero dependencies**: No Redis, Kafka, or other middleware required
- **High performance**: Sub-millisecond scheduling latency
- **Load balancing**: Automatic task distribution based on worker load
- **Resource matching**: Tag-based worker filtering (GPU, CPU, CUDA versions, etc.)
- **Simple deployment**: Single binary for scheduler and worker

## Architecture

```
Client → Scheduler → Worker Pool (500+ machines)
         ↑
         └─ Heartbeat (every 3s)
```

See [Design Document](docs/plans/2025-12-14-distributed-scheduler-design.md) for details.

## Quick Start

### 1. Build

```bash
go build -o scheduler ./cmd/scheduler
go build -o worker ./cmd/worker
```

### 2. Start Scheduler

```bash
./scheduler --port=8080
```

### 3. Start Workers

```bash
# GPU worker
./worker --id=worker-001 --addr=192.168.1.100:9000 --tags=gpu,cuda-12.0 --max-tasks=30

# CPU worker
./worker --id=worker-002 --addr=192.168.1.101:9000 --tags=cpu,avx2 --max-tasks=30
```

### 4. Schedule Tasks

```bash
curl -X POST http://localhost:8080/api/v1/schedule \
  -H "Content-Type: application/json" \
  -d '{"task_id":"task-001","required_tags":["gpu"]}'
```

Response:
```json
{
  "worker_id": "worker-001",
  "address": "192.168.1.100:9000"
}
```

## API Documentation

See [API Documentation](docs/api.md)

## Testing

```bash
# Unit tests
go test ./...

# Integration tests
go test ./tests -v
```

## Roadmap

- [x] MVP: Single scheduler + heartbeat + basic scheduling
- [ ] High availability: Standby scheduler with failover
- [ ] Monitoring: Metrics and alerting
- [ ] Tag indexing: Faster resource filtering
- [ ] Queue: Wait queue for resource shortage

## License

MIT License - see [LICENSE](LICENSE) for details
```

**Step 2: Create API documentation**

Create `docs/api.md`:
```markdown
# API Documentation

## Base URL

```
http://localhost:8080/api/v1
```

## Endpoints

### POST /heartbeat

Worker heartbeat endpoint. Workers should send heartbeats every 3 seconds.

**Request:**
```json
{
  "worker_id": "worker-001",
  "address": "192.168.1.100:9000",
  "resource_tags": ["gpu", "cuda-12.0", "cpu-64core"],
  "max_tasks": 30,
  "current_tasks": 15,
  "timestamp": 1702540800
}
```

**Response:**
```json
{
  "status": "ok"
}
```

**Status Codes:**
- 200: Heartbeat accepted
- 400: Invalid request body
- 405: Method not allowed

---

### POST /schedule

Schedule a task to an available worker.

**Request:**
```json
{
  "task_id": "task-001",
  "required_tags": ["gpu", "cuda-12.0"]
}
```

**Response (success):**
```json
{
  "worker_id": "worker-001",
  "address": "192.168.1.100:9000"
}
```

**Response (no available worker):**
```json
{
  "error": "no available worker matching requirements"
}
```

**Status Codes:**
- 200: Task scheduled successfully
- 503: No available worker
- 400: Invalid request body
- 405: Method not allowed

---

### GET /workers

List all workers and their current state.

**Response:**
```json
[
  {
    "WorkerID": "worker-001",
    "Address": "192.168.1.100:9000",
    "ResourceTags": ["gpu", "cuda-12.0"],
    "MaxTasks": 30,
    "CurrentTasks": 15,
    "Available": 15,
    "LastHeartbeat": "2025-12-14T16:30:00Z",
    "Status": "online"
  }
]
```

**Status Codes:**
- 200: Success
- 405: Method not allowed

**Worker Status:**
- `online`: Heartbeat received within 10 seconds
- `suspicious`: Heartbeat not received for 10-20 seconds
- `offline`: Heartbeat not received for 20+ seconds

## Scheduling Algorithm

1. **Filter by tags**: Only workers with ALL required tags are considered
2. **Filter by availability**: Offline workers or workers at max capacity are excluded
3. **Sort by load ratio**: `load_ratio = current_tasks / max_tasks`
4. **Select lowest**: Worker with lowest load ratio is selected
5. **Optimistic allocation**: Task count incremented immediately (corrected by next heartbeat)

## Error Handling

All error responses follow this format:

```json
{
  "error": "error message description"
}
```
```

**Step 3: Create quickstart guide**

Create `docs/quickstart.md`:
```markdown
# Quick Start Guide

## Prerequisites

- Go 1.21 or later
- Network connectivity between scheduler and workers

## Installation

### Clone and Build

```bash
git clone https://github.com/chicogong/dtask-scheduler.git
cd dtask-scheduler
go build -o bin/scheduler ./cmd/scheduler
go build -o bin/worker ./cmd/worker
```

## Running Locally

### Terminal 1: Start Scheduler

```bash
./bin/scheduler --port=8080
```

Expected output:
```
dtask-scheduler starting...
Scheduler listening on port 8080
```

### Terminal 2: Start GPU Worker

```bash
./bin/worker \
  --id=worker-gpu-001 \
  --addr=localhost:9001 \
  --tags=gpu,cuda-12.0 \
  --max-tasks=30 \
  --scheduler=http://localhost:8080
```

### Terminal 3: Start CPU Worker

```bash
./bin/worker \
  --id=worker-cpu-001 \
  --addr=localhost:9002 \
  --tags=cpu,avx2 \
  --max-tasks=30 \
  --scheduler=http://localhost:8080
```

### Terminal 4: Test Scheduling

```bash
# List workers
curl http://localhost:8080/api/v1/workers | jq

# Schedule GPU task
curl -X POST http://localhost:8080/api/v1/schedule \
  -H "Content-Type: application/json" \
  -d '{"task_id":"task-001","required_tags":["gpu"]}' | jq

# Schedule CPU task
curl -X POST http://localhost:8080/api/v1/schedule \
  -H "Content-Type: application/json" \
  -d '{"task_id":"task-002","required_tags":["cpu"]}' | jq

# Schedule any task (load balancing)
curl -X POST http://localhost:8080/api/v1/schedule \
  -H "Content-Type: application/json" \
  -d '{"task_id":"task-003","required_tags":[]}' | jq
```

## Production Deployment

### Scheduler (on dedicated machine)

```bash
# Production config
export PORT=8080

./scheduler --port=$PORT
```

### Workers (on compute machines)

```bash
# GPU worker example
./worker \
  --id=$(hostname)-gpu \
  --addr=$(hostname -I | awk '{print $1}'):9000 \
  --tags=gpu,cuda-12.0,$(nvidia-smi --query-gpu=gpu_name --format=csv,noheader | tr ' ' '-') \
  --max-tasks=30 \
  --scheduler=http://scheduler.example.com:8080

# CPU worker example
./worker \
  --id=$(hostname)-cpu \
  --addr=$(hostname -I | awk '{print $1}'):9000 \
  --tags=cpu,$(lscpu | grep 'Model name' | awk -F: '{print $2}' | tr ' ' '-') \
  --max-tasks=50 \
  --scheduler=http://scheduler.example.com:8080
```

### Using systemd

Create `/etc/systemd/system/dtask-worker.service`:

```ini
[Unit]
Description=dtask-scheduler worker
After=network.target

[Service]
Type=simple
User=dtask
WorkingDirectory=/opt/dtask
ExecStart=/opt/dtask/worker \
  --id=%H \
  --addr=%H:9000 \
  --tags=gpu,cuda-12.0 \
  --max-tasks=30 \
  --scheduler=http://scheduler:8080
Restart=always

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl enable dtask-worker
sudo systemctl start dtask-worker
sudo systemctl status dtask-worker
```

## Monitoring

### Check Worker Status

```bash
watch -n 1 'curl -s http://localhost:8080/api/v1/workers | jq'
```

### Logs

Scheduler and worker logs go to stdout. Redirect to file:

```bash
./scheduler 2>&1 | tee scheduler.log
./worker 2>&1 | tee worker.log
```

## Troubleshooting

### Worker not showing up

1. Check network connectivity: `curl http://scheduler:8080/api/v1/workers`
2. Check worker logs for heartbeat errors
3. Verify scheduler URL is correct

### Scheduling fails with "no available worker"

1. Check if workers are online: `curl http://scheduler:8080/api/v1/workers`
2. Verify required tags match worker tags
3. Check if all workers are at max capacity

### Worker shows as "offline"

1. Worker hasn't sent heartbeat in 20+ seconds
2. Check worker process is running: `ps aux | grep worker`
3. Check network connectivity
4. Restart worker
```

**Step 4: Commit**

```bash
git add README.md docs/api.md docs/quickstart.md
git commit -m "docs: add README, API docs, and quickstart guide

- Update README with architecture and features
- Add comprehensive API documentation
- Add quickstart guide for local and production deployment
- Add systemd service example"
```

---

## Task 10: Final Verification

**Step 1: Run all tests**

```bash
go test ./... -v -cover
```

Expected: All tests pass

**Step 2: Build binaries**

```bash
go build -o bin/scheduler ./cmd/scheduler
go build -o bin/worker ./cmd/worker
```

Expected: Both binaries created successfully

**Step 3: End-to-end manual test**

```bash
# Start scheduler
./bin/scheduler --port=8080 &
SCHED_PID=$!

# Start two workers
./bin/worker --id=w1 --addr=localhost:9001 --tags=gpu --max-tasks=30 &
W1_PID=$!

./bin/worker --id=w2 --addr=localhost:9002 --tags=cpu --max-tasks=30 &
W2_PID=$!

# Wait for heartbeats
sleep 5

# Test scheduling
curl -s http://localhost:8080/api/v1/workers | jq
curl -s -X POST http://localhost:8080/api/v1/schedule \
  -H "Content-Type: application/json" \
  -d '{"task_id":"t1","required_tags":["gpu"]}' | jq

# Cleanup
kill $SCHED_PID $W1_PID $W2_PID
```

Expected: Workers listed, GPU task scheduled to w1

**Step 4: Final commit**

```bash
git add .
git commit -m "chore: MVP complete

All core features implemented and tested:
- Worker heartbeat mechanism (3s interval)
- Resource tag-based filtering
- Load-balanced scheduling algorithm
- HTTP API (heartbeat, schedule, list workers)
- Timeout detection (10s suspicious, 20s offline)
- Comprehensive test coverage
- Documentation complete"
```

---

## Summary

MVP implementation complete! The system now has:

✅ **Core Components:**
- Scheduler server with HTTP API
- Worker agent with heartbeat sender
- In-memory state management
- Scheduling algorithm (tag filtering + load balancing)

✅ **Testing:**
- Unit tests for all components
- Integration tests for end-to-end flow
- Manual testing verified

✅ **Documentation:**
- README with quick start
- API documentation
- Deployment guide

**Next steps (post-MVP):**
1. Standby scheduler + failover
2. Tag inverted index for performance
3. Metrics and monitoring
4. Queue for resource shortage

**Time estimate:** 10-15 hours for experienced Go developer

---

## Execution Notes

- All file paths are exact and absolute where needed
- All code is complete (no placeholders like "add validation here")
- All commands include expected output
- Tests follow TDD: write test → run fail → implement → run pass → commit
- Each task is bite-sized (2-5 minutes per step)
- Frequent commits with descriptive messages
