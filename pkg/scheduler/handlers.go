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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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

	w.Header().Set("Content-Type", "application/json")
	if resp.Error != "" {
		log.Printf("Schedule failed for task %s: %s", req.TaskID, resp.Error)
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		log.Printf("Task %s scheduled to %s", req.TaskID, resp.WorkerID)
		w.WriteHeader(http.StatusOK)
	}
	_ = json.NewEncoder(w).Encode(resp)
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
	_ = json.NewEncoder(w).Encode(workers)
}
