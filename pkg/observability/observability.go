// Package observability provides runtime metrics collection and HTTP endpoints
// for health checks, Prometheus-format metrics, and cluster statistics in the
// dtask-scheduler system.
package observability

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

// ---------------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------------

// Metrics is a concurrency-safe runtime counter collector for the scheduler.
// All exported methods are safe to call from multiple goroutines simultaneously.
type Metrics struct {
	scheduleRequests  atomic.Int64
	scheduleSuccesses atomic.Int64
	scheduleFailures  atomic.Int64
	heartbeats        atomic.Int64

	latencyMu      sync.Mutex
	latencyCount   int64
	latencyTotalMS float64
	latencyMaxMS   float64
}

// NewMetrics constructs a ready-to-use Metrics instance with all counters at zero.
func NewMetrics() *Metrics {
	return &Metrics{}
}

// RecordSchedule records a single scheduling attempt.
// d is the end-to-end latency of the attempt; success indicates whether it
// resulted in a successful placement.
func (m *Metrics) RecordSchedule(d time.Duration, success bool) {
	m.scheduleRequests.Add(1)
	if success {
		m.scheduleSuccesses.Add(1)
	} else {
		m.scheduleFailures.Add(1)
	}

	ms := float64(d.Nanoseconds()) / 1e6

	m.latencyMu.Lock()
	m.latencyCount++
	m.latencyTotalMS += ms
	if ms > m.latencyMaxMS {
		m.latencyMaxMS = ms
	}
	m.latencyMu.Unlock()
}

// RecordHeartbeat increments the heartbeat counter by one.
func (m *Metrics) RecordHeartbeat() {
	m.heartbeats.Add(1)
}

// MetricsSnapshot is a consistent point-in-time view of all collected metrics.
type MetricsSnapshot struct {
	// ScheduleRequests is the total number of scheduling attempts recorded.
	ScheduleRequests int64 `json:"schedule_requests"`
	// ScheduleSuccesses is the number of successful scheduling attempts.
	ScheduleSuccesses int64 `json:"schedule_successes"`
	// ScheduleFailures is the number of failed scheduling attempts.
	ScheduleFailures int64 `json:"schedule_failures"`
	// Heartbeats is the total number of heartbeats recorded.
	Heartbeats int64 `json:"heartbeats"`
	// ScheduleLatencyAvgMS is the average scheduling latency in milliseconds.
	// Zero when no scheduling attempts have been recorded.
	ScheduleLatencyAvgMS float64 `json:"schedule_latency_avg_ms"`
	// ScheduleLatencyMaxMS is the maximum observed scheduling latency in milliseconds.
	ScheduleLatencyMaxMS float64 `json:"schedule_latency_max_ms"`
}

// Snapshot returns a consistent snapshot of all current metric values.
// Reading is lock-protected for the latency fields; atomic reads are used for counters.
func (m *Metrics) Snapshot() MetricsSnapshot {
	m.latencyMu.Lock()
	count := m.latencyCount
	total := m.latencyTotalMS
	maxMS := m.latencyMaxMS
	m.latencyMu.Unlock()

	var avgMS float64
	if count > 0 {
		avgMS = total / float64(count)
	}

	return MetricsSnapshot{
		ScheduleRequests:     m.scheduleRequests.Load(),
		ScheduleSuccesses:    m.scheduleSuccesses.Load(),
		ScheduleFailures:     m.scheduleFailures.Load(),
		Heartbeats:           m.heartbeats.Load(),
		ScheduleLatencyAvgMS: avgMS,
		ScheduleLatencyMaxMS: maxMS,
	}
}

// ---------------------------------------------------------------------------
// WorkerSource
// ---------------------------------------------------------------------------

// WorkerSource is satisfied by any type that can enumerate live workers.
// The scheduler's StateManager satisfies this interface structurally.
type WorkerSource interface {
	// ListWorkers returns a snapshot of all currently known workers.
	ListWorkers() []*types.WorkerState
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// Handler exposes HTTP endpoints for liveness, metrics, and cluster statistics.
type Handler struct {
	m   *Metrics
	src WorkerSource
}

// NewHandler constructs a Handler that reads metrics from m and worker state
// from src. Neither argument may be nil.
func NewHandler(m *Metrics, src WorkerSource) *Handler {
	return &Handler{m: m, src: src}
}

// Register mounts all endpoints on mux:
//
//	/healthz  → HandleHealth
//	/metrics  → HandleMetrics
//	/stats    → HandleStats
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.HandleHealth)
	mux.HandleFunc("/metrics", h.HandleMetrics)
	mux.HandleFunc("/stats", h.HandleStats)
}

// HandleHealth is a liveness probe endpoint.
// Accepts GET only (405 for any other method).
// Responds with 200 and JSON body {"status":"ok"}.
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, `{"status":"ok"}`)
}

// WorkerCounts holds the per-status worker headcount for a Stats response.
type WorkerCounts struct {
	// Online is the number of workers with status "online".
	Online int `json:"online"`
	// Suspicious is the number of workers with status "suspicious".
	Suspicious int `json:"suspicious"`
	// Offline is the number of workers with status "offline".
	Offline int `json:"offline"`
	// Total is the sum of all worker counts regardless of status.
	Total int `json:"total"`
}

// CapacitySummary aggregates task-slot totals across all known workers.
type CapacitySummary struct {
	// TotalSlots is the sum of MaxTasks across all workers.
	TotalSlots int `json:"total_slots"`
	// UsedSlots is the sum of CurrentTasks across all workers.
	UsedSlots int `json:"used_slots"`
	// AvailableSlots is the sum of Available across all workers.
	AvailableSlots int `json:"available_slots"`
}

// Stats is the response body for the /stats endpoint.
type Stats struct {
	// Workers contains per-status worker headcounts.
	Workers WorkerCounts `json:"workers"`
	// Capacity summarises the cluster's task-slot utilisation.
	Capacity CapacitySummary `json:"capacity"`
	// AvgLoadRatio is the arithmetic mean of LoadRatio() across all workers.
	// Zero when no workers are present.
	AvgLoadRatio float64 `json:"avg_load_ratio"`
	// Metrics is a consistent snapshot of the runtime metric counters.
	Metrics MetricsSnapshot `json:"metrics"`
}

// HandleStats returns a JSON Stats document describing the current cluster state.
// Accepts GET only (405 otherwise).
func (h *Handler) HandleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workers := h.src.ListWorkers()

	var counts WorkerCounts
	var capacity CapacitySummary
	var loadSum float64

	for _, ws := range workers {
		counts.Total++
		switch ws.Status {
		case types.WorkerOnline:
			counts.Online++
		case types.WorkerSuspicious:
			counts.Suspicious++
		case types.WorkerOffline:
			counts.Offline++
		}
		capacity.TotalSlots += ws.MaxTasks
		capacity.UsedSlots += ws.CurrentTasks
		capacity.AvailableSlots += ws.Available
		loadSum += ws.LoadRatio()
	}

	var avgLoad float64
	if counts.Total > 0 {
		avgLoad = loadSum / float64(counts.Total)
	}

	stats := Stats{
		Workers:      counts,
		Capacity:     capacity,
		AvgLoadRatio: avgLoad,
		Metrics:      h.m.Snapshot(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(stats)
}

// HandleMetrics writes a Prometheus text exposition (version 0.0.4) of all
// collected metrics. Accepts GET only (405 otherwise).
// Content-Type is set to "text/plain; version=0.0.4".
func (h *Handler) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snap := h.m.Snapshot()
	workers := h.src.ListWorkers()

	var online, suspicious, offline int
	var capTotal, capUsed, capAvail int
	for _, ws := range workers {
		switch ws.Status {
		case types.WorkerOnline:
			online++
		case types.WorkerSuspicious:
			suspicious++
		case types.WorkerOffline:
			offline++
		}
		capTotal += ws.MaxTasks
		capUsed += ws.CurrentTasks
		capAvail += ws.Available
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)

	// write appends one line of Prometheus exposition output, discarding the
	// write error (a broken client connection is not actionable here).
	write := func(format string, args ...interface{}) {
		_, _ = fmt.Fprintf(w, format, args...)
	}

	write("# HELP dtask_schedule_requests_total Total number of scheduling attempts.\n")
	write("# TYPE dtask_schedule_requests_total counter\n")
	write("dtask_schedule_requests_total %d\n", snap.ScheduleRequests)

	write("# HELP dtask_schedule_successes_total Total number of successful scheduling attempts.\n")
	write("# TYPE dtask_schedule_successes_total counter\n")
	write("dtask_schedule_successes_total %d\n", snap.ScheduleSuccesses)

	write("# HELP dtask_schedule_failures_total Total number of failed scheduling attempts.\n")
	write("# TYPE dtask_schedule_failures_total counter\n")
	write("dtask_schedule_failures_total %d\n", snap.ScheduleFailures)

	write("# HELP dtask_heartbeats_total Total number of heartbeats recorded.\n")
	write("# TYPE dtask_heartbeats_total counter\n")
	write("dtask_heartbeats_total %d\n", snap.Heartbeats)

	write("# HELP dtask_schedule_latency_avg_ms Average scheduling latency in milliseconds.\n")
	write("# TYPE dtask_schedule_latency_avg_ms gauge\n")
	write("dtask_schedule_latency_avg_ms %s\n", formatFloat(snap.ScheduleLatencyAvgMS))

	write("# HELP dtask_schedule_latency_max_ms Maximum observed scheduling latency in milliseconds.\n")
	write("# TYPE dtask_schedule_latency_max_ms gauge\n")
	write("dtask_schedule_latency_max_ms %s\n", formatFloat(snap.ScheduleLatencyMaxMS))

	write("# HELP dtask_workers Number of workers per status.\n")
	write("# TYPE dtask_workers gauge\n")
	write("dtask_workers{status=\"online\"} %d\n", online)
	write("dtask_workers{status=\"suspicious\"} %d\n", suspicious)
	write("dtask_workers{status=\"offline\"} %d\n", offline)

	write("# HELP dtask_capacity_total Total task slots across all workers.\n")
	write("# TYPE dtask_capacity_total gauge\n")
	write("dtask_capacity_total %d\n", capTotal)

	write("# HELP dtask_capacity_used Currently used task slots across all workers.\n")
	write("# TYPE dtask_capacity_used gauge\n")
	write("dtask_capacity_used %d\n", capUsed)

	write("# HELP dtask_capacity_available Available task slots across all workers.\n")
	write("# TYPE dtask_capacity_available gauge\n")
	write("dtask_capacity_available %d\n", capAvail)
}

// formatFloat renders f without scientific notation and without unnecessary
// trailing zeros, for use in Prometheus text exposition output.
func formatFloat(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Sprintf("%v", f)
	}
	return fmt.Sprintf("%g", f)
}
