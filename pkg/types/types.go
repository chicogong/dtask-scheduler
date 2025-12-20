// Package types defines the core data structures used by the dtask-scheduler system.
// It includes worker heartbeat data, scheduling requests/responses, and worker state management.
package types

import (
	"errors"
	"time"
)

// Error definitions
var (
	ErrWorkerNotFound = errors.New("worker not found")
	ErrNoCapacity     = errors.New("worker has no available capacity")
)

// Heartbeat represents worker health and capacity info sent periodically to the scheduler.
// Workers send heartbeats every 3 seconds to maintain their registration and report current load.
type Heartbeat struct {
	WorkerID     string   `json:"worker_id"`
	Timestamp    int64    `json:"timestamp"`
	ResourceTags []string `json:"resource_tags"`
	MaxTasks     int      `json:"max_tasks"`
	CurrentTasks int      `json:"current_tasks"`
	Address      string   `json:"address"` // IP:Port for client connections
}

// WorkerState represents the scheduler's view of a worker, including its current load,
// resource tags, and availability status. This is maintained in-memory by the StateManager.
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

// WorkerStatus represents the health state of a worker based on heartbeat freshness.
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

// ScheduleRequest represents a client's request to schedule a task on a worker.
// RequiredTags filters workers to only those matching ALL specified tags.
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

// ScheduleResponse represents the scheduler's response to a scheduling request.
// On success, WorkerID and Address are populated. On failure, Error contains the reason.
type ScheduleResponse struct {
	WorkerID string `json:"worker_id,omitempty"`
	Address  string `json:"address,omitempty"`
	Error    string `json:"error,omitempty"`
}
