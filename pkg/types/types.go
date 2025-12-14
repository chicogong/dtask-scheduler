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
