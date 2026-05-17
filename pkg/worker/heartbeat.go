// Package worker provides worker-side functionality for the dtask-scheduler system.
// It includes heartbeat sending and task count tracking for worker registration.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

// HeartbeatSender sends periodic heartbeats to the scheduler to maintain worker registration.
// It tracks the worker's current task load and sends updates every 3 seconds.
type HeartbeatSender struct {
	workerID     string
	address      string
	resourceTags []string
	maxTasks     int
	schedulerURL string
	interval     time.Duration
	currentTasks atomic.Int32
	httpClient   *http.Client

	mu          sync.Mutex
	metrics     types.WorkerMetrics // resource utilization, guarded by mu
	standbyURLs []string            // additional schedulers, guarded by mu
}

// NewHeartbeatSender creates a new heartbeat sender with the specified configuration.
// It initializes an HTTP client with a 5-second timeout to prevent hanging requests.
//
// Parameters:
//   - workerID: unique identifier for this worker
//   - address: network address where this worker can be reached
//   - tags: resource tags indicating worker capabilities (e.g., "cpu", "gpu")
//   - maxTasks: maximum number of concurrent tasks this worker can handle
//   - schedulerURL: base URL of the scheduler service
func NewHeartbeatSender(workerID, address string, tags []string, maxTasks int, schedulerURL string) *HeartbeatSender {
	return &HeartbeatSender{
		workerID:     workerID,
		address:      address,
		resourceTags: tags,
		maxTasks:     maxTasks,
		schedulerURL: schedulerURL,
		interval:     3 * time.Second,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Start begins sending periodic heartbeats to the scheduler.
// It sends an immediate heartbeat upon starting, then continues sending at the configured interval.
// The method blocks until the provided context is cancelled, allowing for graceful shutdown.
//
// The heartbeat loop can be stopped by cancelling the context, which will cause the method to return.
func (h *HeartbeatSender) Start(ctx context.Context) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	log.Printf("Heartbeat sender started (interval: %v)", h.interval)

	// Send immediate heartbeat
	h.send()

	for {
		select {
		case <-ticker.C:
			h.send()
		case <-ctx.Done():
			log.Println("Heartbeat sender stopping...")
			return
		}
	}
}

// UpdateTaskCount updates the current task count in a thread-safe manner.
// This method can be called concurrently from multiple goroutines.
//
// Parameters:
//   - count: the new task count to set
func (h *HeartbeatSender) UpdateTaskCount(count int) {
	h.currentTasks.Store(int32(count))
}

// UpdateMetrics updates the resource-utilization metrics (CPU/memory/GPU usage)
// reported in subsequent heartbeats. Safe for concurrent use.
func (h *HeartbeatSender) UpdateMetrics(m types.WorkerMetrics) {
	h.mu.Lock()
	h.metrics = m
	h.mu.Unlock()
}

// AddStandby registers an additional scheduler URL (a standby) that will
// receive a copy of every heartbeat. Call it before Start.
func (h *HeartbeatSender) AddStandby(schedulerURL string) {
	h.mu.Lock()
	h.standbyURLs = append(h.standbyURLs, schedulerURL)
	h.mu.Unlock()
}

// send sends a single heartbeat to the primary scheduler and every registered
// standby. Heartbeats are POSTed concurrently so a slow standby does not delay
// delivery to the primary. Errors are logged but never stop the sender.
func (h *HeartbeatSender) send() {
	h.mu.Lock()
	hb := &types.Heartbeat{
		WorkerID:     h.workerID,
		Address:      h.address,
		ResourceTags: h.resourceTags,
		MaxTasks:     h.maxTasks,
		CurrentTasks: int(h.currentTasks.Load()),
		Metrics:      h.metrics,
		Timestamp:    time.Now().Unix(),
	}
	targets := make([]string, 0, 1+len(h.standbyURLs))
	targets = append(targets, h.schedulerURL)
	targets = append(targets, h.standbyURLs...)
	h.mu.Unlock()

	body, err := json.Marshal(hb)
	if err != nil {
		log.Printf("Failed to marshal heartbeat: %v", err)
		return
	}

	var wg sync.WaitGroup
	for _, target := range targets {
		wg.Add(1)
		go func(schedulerURL string) {
			defer wg.Done()
			h.postHeartbeat(schedulerURL, body)
		}(target)
	}
	wg.Wait()

	log.Printf("Heartbeat sent: %d/%d tasks to %d scheduler(s)", h.currentTasks.Load(), h.maxTasks, len(targets))
}

// postHeartbeat delivers one heartbeat body to a single scheduler endpoint.
func (h *HeartbeatSender) postHeartbeat(schedulerURL string, body []byte) {
	url := fmt.Sprintf("%s/api/v1/heartbeat", schedulerURL)
	resp, err := h.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("Failed to send heartbeat to %s: %v", schedulerURL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Heartbeat to %s failed with status: %d", schedulerURL, resp.StatusCode)
	}
}
