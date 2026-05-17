package observability

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

// fakeWorkerSource is a test double for WorkerSource.
type fakeWorkerSource struct {
	workers []*types.WorkerState
}

func (f *fakeWorkerSource) ListWorkers() []*types.WorkerState {
	return f.workers
}

// fixedWorkers returns a deterministic set of workers for tests.
func fixedWorkers() []*types.WorkerState {
	return []*types.WorkerState{
		{
			WorkerID:      "w1",
			Address:       "10.0.0.1:9000",
			MaxTasks:      10,
			CurrentTasks:  4,
			Available:     6,
			Status:        types.WorkerOnline,
			LastHeartbeat: time.Now(),
		},
		{
			WorkerID:      "w2",
			Address:       "10.0.0.2:9000",
			MaxTasks:      8,
			CurrentTasks:  8,
			Available:     0,
			Status:        types.WorkerOnline,
			LastHeartbeat: time.Now(),
		},
		{
			WorkerID:      "w3",
			Address:       "10.0.0.3:9000",
			MaxTasks:      5,
			CurrentTasks:  2,
			Available:     3,
			Status:        types.WorkerSuspicious,
			LastHeartbeat: time.Now().Add(-10 * time.Second),
		},
		{
			WorkerID:      "w4",
			Address:       "10.0.0.4:9000",
			MaxTasks:      5,
			CurrentTasks:  0,
			Available:     5,
			Status:        types.WorkerOffline,
			LastHeartbeat: time.Now().Add(-60 * time.Second),
		},
	}
}

func newTestHandler() *Handler {
	m := NewMetrics()
	m.RecordSchedule(10*time.Millisecond, true)
	m.RecordSchedule(20*time.Millisecond, false)
	m.RecordHeartbeat()
	m.RecordHeartbeat()
	src := &fakeWorkerSource{workers: fixedWorkers()}
	return NewHandler(m, src)
}

// ---------------------------------------------------------------------------
// /healthz
// ---------------------------------------------------------------------------

func TestHandleHealth_Get(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	h.HandleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: want application/json, got %q", ct)
	}
	body := w.Body.String()
	var resp map[string]string
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("invalid JSON: %v — body: %s", err, body)
	}
	if resp["status"] != "ok" {
		t.Errorf("status field: want \"ok\", got %q", resp["status"])
	}
}

func TestHandleHealth_MethodNotAllowed(t *testing.T) {
	h := newTestHandler()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/healthz", nil)
			w := httptest.NewRecorder()
			h.HandleHealth(w, req)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("method %s: want 405, got %d", method, w.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// /stats
// ---------------------------------------------------------------------------

func TestHandleStats_Get(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()
	h.HandleStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: want application/json, got %q", ct)
	}

	var stats Stats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode Stats: %v", err)
	}

	// Worker counts from fixedWorkers():  2 online, 1 suspicious, 1 offline, 4 total
	if stats.Workers.Total != 4 {
		t.Errorf("Workers.Total: want 4, got %d", stats.Workers.Total)
	}
	if stats.Workers.Online != 2 {
		t.Errorf("Workers.Online: want 2, got %d", stats.Workers.Online)
	}
	if stats.Workers.Suspicious != 1 {
		t.Errorf("Workers.Suspicious: want 1, got %d", stats.Workers.Suspicious)
	}
	if stats.Workers.Offline != 1 {
		t.Errorf("Workers.Offline: want 1, got %d", stats.Workers.Offline)
	}

	// Capacity: TotalSlots=10+8+5+5=28, UsedSlots=4+8+2+0=14, AvailSlots=6+0+3+5=14
	if stats.Capacity.TotalSlots != 28 {
		t.Errorf("Capacity.TotalSlots: want 28, got %d", stats.Capacity.TotalSlots)
	}
	if stats.Capacity.UsedSlots != 14 {
		t.Errorf("Capacity.UsedSlots: want 14, got %d", stats.Capacity.UsedSlots)
	}
	if stats.Capacity.AvailableSlots != 14 {
		t.Errorf("Capacity.AvailableSlots: want 14, got %d", stats.Capacity.AvailableSlots)
	}

	// Metrics: 2 requests (1 success, 1 failure), 2 heartbeats
	if stats.Metrics.ScheduleRequests != 2 {
		t.Errorf("Metrics.ScheduleRequests: want 2, got %d", stats.Metrics.ScheduleRequests)
	}
	if stats.Metrics.ScheduleSuccesses != 1 {
		t.Errorf("Metrics.ScheduleSuccesses: want 1, got %d", stats.Metrics.ScheduleSuccesses)
	}
	if stats.Metrics.ScheduleFailures != 1 {
		t.Errorf("Metrics.ScheduleFailures: want 1, got %d", stats.Metrics.ScheduleFailures)
	}
	if stats.Metrics.Heartbeats != 2 {
		t.Errorf("Metrics.Heartbeats: want 2, got %d", stats.Metrics.Heartbeats)
	}

	// AvgLoadRatio: (4/10 + 8/8 + 2/5 + 0/5)/4 = (0.4+1.0+0.4+0)/4 = 1.8/4 = 0.45
	const eps = 1e-9
	wantAvg := (0.4 + 1.0 + 0.4 + 0.0) / 4
	if diff := stats.AvgLoadRatio - wantAvg; diff > eps || diff < -eps {
		t.Errorf("AvgLoadRatio: want %f, got %f", wantAvg, stats.AvgLoadRatio)
	}
}

func TestHandleStats_EmptyWorkers(t *testing.T) {
	m := NewMetrics()
	h := NewHandler(m, &fakeWorkerSource{})
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()
	h.HandleStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	var stats Stats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stats.Workers.Total != 0 {
		t.Errorf("Total: want 0, got %d", stats.Workers.Total)
	}
	if stats.AvgLoadRatio != 0 {
		t.Errorf("AvgLoadRatio: want 0, got %f", stats.AvgLoadRatio)
	}
}

func TestHandleStats_MethodNotAllowed(t *testing.T) {
	h := newTestHandler()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/stats", nil)
			w := httptest.NewRecorder()
			h.HandleStats(w, req)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("method %s: want 405, got %d", method, w.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// /metrics
// ---------------------------------------------------------------------------

func TestHandleMetrics_Get(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	h.HandleMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "text/plain; version=0.0.4" {
		t.Errorf("Content-Type: want \"text/plain; version=0.0.4\", got %q", ct)
	}

	body := w.Body.String()

	// Expected metric names
	expectedMetrics := []string{
		"dtask_schedule_requests_total",
		"dtask_schedule_successes_total",
		"dtask_schedule_failures_total",
		"dtask_heartbeats_total",
		"dtask_schedule_latency_avg_ms",
		"dtask_schedule_latency_max_ms",
		"dtask_workers",
		"dtask_capacity_total",
		"dtask_capacity_used",
		"dtask_capacity_available",
	}
	for _, name := range expectedMetrics {
		if !strings.Contains(body, name) {
			t.Errorf("metric %q not found in output", name)
		}
	}

	// Verify # TYPE lines exist for each metric
	expectedTypeLines := []string{
		"# TYPE dtask_schedule_requests_total counter",
		"# TYPE dtask_schedule_successes_total counter",
		"# TYPE dtask_schedule_failures_total counter",
		"# TYPE dtask_heartbeats_total counter",
		"# TYPE dtask_schedule_latency_avg_ms gauge",
		"# TYPE dtask_schedule_latency_max_ms gauge",
		"# TYPE dtask_workers gauge",
		"# TYPE dtask_capacity_total gauge",
		"# TYPE dtask_capacity_used gauge",
		"# TYPE dtask_capacity_available gauge",
	}
	for _, line := range expectedTypeLines {
		if !strings.Contains(body, line) {
			t.Errorf("expected line %q not found in metrics output", line)
		}
	}

	// Verify status labels for dtask_workers
	statusLines := []string{
		`dtask_workers{status="online"}`,
		`dtask_workers{status="suspicious"}`,
		`dtask_workers{status="offline"}`,
	}
	for _, line := range statusLines {
		if !strings.Contains(body, line) {
			t.Errorf("expected label line %q not found in metrics output", line)
		}
	}

	// Verify # HELP lines appear before # TYPE lines for at least one metric
	helpIdx := strings.Index(body, "# HELP dtask_schedule_requests_total")
	typeIdx := strings.Index(body, "# TYPE dtask_schedule_requests_total")
	if helpIdx < 0 || typeIdx < 0 || helpIdx > typeIdx {
		t.Errorf("# HELP must appear before # TYPE for dtask_schedule_requests_total")
	}
}

func TestHandleMetrics_MethodNotAllowed(t *testing.T) {
	h := newTestHandler()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/metrics", nil)
			w := httptest.NewRecorder()
			h.HandleMetrics(w, req)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("method %s: want 405, got %d", method, w.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Register
// ---------------------------------------------------------------------------

func TestRegister(t *testing.T) {
	h := newTestHandler()
	mux := http.NewServeMux()
	h.Register(mux)

	endpoints := []struct {
		path string
		want int
	}{
		{"/healthz", http.StatusOK},
		{"/metrics", http.StatusOK},
		{"/stats", http.StatusOK},
	}

	for _, ep := range endpoints {
		t.Run(ep.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ep.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != ep.want {
				t.Errorf("%s: want %d, got %d", ep.path, ep.want, w.Code)
			}
		})
	}
}

// TestRegister_UnknownPath verifies that unregistered paths return 404.
func TestRegister_UnknownPath(t *testing.T) {
	h := newTestHandler()
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown path: want 404, got %d", w.Code)
	}
}
