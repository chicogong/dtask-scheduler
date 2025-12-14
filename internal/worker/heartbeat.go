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
