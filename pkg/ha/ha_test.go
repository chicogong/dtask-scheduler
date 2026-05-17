package ha

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

// -----------------------------------------------------------------------------
// fakeStore — in-memory StateStore for testing
// -----------------------------------------------------------------------------

type fakeStore struct {
	mu        sync.Mutex
	workers   []*types.WorkerState
	snapshots [][]*types.WorkerState // records every LoadSnapshot call
}

func (f *fakeStore) ListWorkers() []*types.WorkerState {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*types.WorkerState, len(f.workers))
	copy(out, f.workers)
	return out
}

func (f *fakeStore) LoadSnapshot(ws []*types.WorkerState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.workers = ws
	f.snapshots = append(f.snapshots, ws)
}

func (f *fakeStore) snapshotCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.snapshots)
}

func (f *fakeStore) lastSnapshot() []*types.WorkerState {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.snapshots) == 0 {
		return nil
	}
	return f.snapshots[len(f.snapshots)-1]
}

// sampleWorkers returns a small deterministic slice of workers used by tests.
func sampleWorkers() []*types.WorkerState {
	return []*types.WorkerState{
		{
			WorkerID:     "w1",
			Address:      "10.0.0.1:9000",
			ResourceTags: []string{"gpu"},
			MaxTasks:     10,
			CurrentTasks: 3,
			Available:    7,
			Status:       types.WorkerOnline,
		},
		{
			WorkerID:     "w2",
			Address:      "10.0.0.2:9000",
			ResourceTags: []string{"cpu"},
			MaxTasks:     20,
			CurrentTasks: 0,
			Available:    20,
			Status:       types.WorkerOnline,
		},
	}
}

// -----------------------------------------------------------------------------
// RoleHolder tests
// -----------------------------------------------------------------------------

func TestRoleHolder_GetSet(t *testing.T) {
	tests := []struct {
		name    string
		initial Role
		set     Role
		wantGet Role
	}{
		{"master-to-standby", RoleMaster, RoleStandby, RoleStandby},
		{"standby-to-master", RoleStandby, RoleMaster, RoleMaster},
		{"master-stays-master", RoleMaster, RoleMaster, RoleMaster},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rh := NewRoleHolder(tt.initial)
			if got := rh.Get(); got != tt.initial {
				t.Fatalf("initial Get() = %q, want %q", got, tt.initial)
			}
			rh.Set(tt.set)
			if got := rh.Get(); got != tt.wantGet {
				t.Fatalf("after Set Get() = %q, want %q", got, tt.wantGet)
			}
		})
	}
}

func TestRoleHolder_IsMaster(t *testing.T) {
	tests := []struct {
		role Role
		want bool
	}{
		{RoleMaster, true},
		{RoleStandby, false},
	}
	for _, tt := range tests {
		rh := NewRoleHolder(tt.role)
		if got := rh.IsMaster(); got != tt.want {
			t.Errorf("NewRoleHolder(%q).IsMaster() = %v, want %v", tt.role, got, tt.want)
		}
	}
}

func TestRoleHolder_ConcurrentAccess(t *testing.T) {
	rh := NewRoleHolder(RoleMaster)
	var wg sync.WaitGroup
	const goroutines = 50

	// Half write standby, half write master; all call Get and IsMaster.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				rh.Set(RoleStandby)
			} else {
				rh.Set(RoleMaster)
			}
			_ = rh.Get()
			_ = rh.IsMaster()
		}()
	}
	wg.Wait()
	// No assertion on final value; the race detector is the real check here.
}

// -----------------------------------------------------------------------------
// SyncHandler tests
// -----------------------------------------------------------------------------

func TestSyncHandler_MethodNotAllowed(t *testing.T) {
	store := &fakeStore{}
	h := NewSyncHandler(store)

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/sync", nil)
		rr := httptest.NewRecorder()
		h(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /sync: got status %d, want 405", method, rr.Code)
		}
		if store.snapshotCount() != 0 {
			t.Errorf("%s: LoadSnapshot should not have been called", method)
		}
	}
}

func TestSyncHandler_BadJSON(t *testing.T) {
	store := &fakeStore{}
	h := NewSyncHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/sync", strings.NewReader("not-json{{{"))
	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: got status %d, want 400", rr.Code)
	}
	if store.snapshotCount() != 0 {
		t.Error("LoadSnapshot should not be called on bad JSON")
	}
}

func TestSyncHandler_ValidPost(t *testing.T) {
	store := &fakeStore{}
	h := NewSyncHandler(store)

	workers := sampleWorkers()
	body, err := json.Marshal(workers)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/sync", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("valid POST: got status %d, want 200", rr.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response body not valid JSON: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("response status = %q, want \"ok\"", resp["status"])
	}

	if store.snapshotCount() != 1 {
		t.Fatalf("LoadSnapshot called %d times, want 1", store.snapshotCount())
	}
	snap := store.lastSnapshot()
	if len(snap) != len(workers) {
		t.Fatalf("snapshot has %d workers, want %d", len(snap), len(workers))
	}
	for i, w := range snap {
		if w.WorkerID != workers[i].WorkerID {
			t.Errorf("worker[%d].WorkerID = %q, want %q", i, w.WorkerID, workers[i].WorkerID)
		}
	}
}

// -----------------------------------------------------------------------------
// Round-trip replication test
// -----------------------------------------------------------------------------

func TestReplicator_ReplicateOnce_RoundTrip(t *testing.T) {
	// standby side: a store and a real SyncHandler serving over httptest.
	standbyStore := &fakeStore{}
	srv := httptest.NewServer(NewSyncHandler(standbyStore))
	defer srv.Close()

	// master side: a store pre-populated with workers.
	masterStore := &fakeStore{workers: sampleWorkers()}
	rep := NewReplicator(masterStore, srv.URL, 10*time.Second) // long interval; we call once manually

	ctx := context.Background()
	if err := rep.ReplicateOnce(ctx); err != nil {
		t.Fatalf("ReplicateOnce: %v", err)
	}

	snap := standbyStore.lastSnapshot()
	if snap == nil {
		t.Fatal("standby LoadSnapshot was never called")
	}
	if len(snap) != len(masterStore.workers) {
		t.Fatalf("standby got %d workers, want %d", len(snap), len(masterStore.workers))
	}
	for i, w := range snap {
		if w.WorkerID != masterStore.workers[i].WorkerID {
			t.Errorf("worker[%d] ID mismatch: got %q, want %q", i, w.WorkerID, masterStore.workers[i].WorkerID)
		}
	}
}

func TestReplicator_Start_PeriodicReplication(t *testing.T) {
	standbyStore := &fakeStore{}
	srv := httptest.NewServer(NewSyncHandler(standbyStore))
	defer srv.Close()

	masterStore := &fakeStore{workers: sampleWorkers()}
	const interval = 20 * time.Millisecond
	rep := NewReplicator(masterStore, srv.URL, interval)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		rep.Start(ctx)
		close(done)
	}()

	<-done // Start must return after ctx expires.

	// With a 20 ms interval and ~120 ms window we expect at least 3 syncs.
	got := standbyStore.snapshotCount()
	if got < 3 {
		t.Errorf("expected >=3 replication cycles, got %d", got)
	}
}

func TestReplicator_ReplicateOnce_Error(t *testing.T) {
	// Point at a server that immediately returns 500.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	masterStore := &fakeStore{workers: sampleWorkers()}
	rep := NewReplicator(masterStore, srv.URL, 10*time.Second)

	err := rep.ReplicateOnce(context.Background())
	if err == nil {
		t.Fatal("expected error from 500 response, got nil")
	}
}

// -----------------------------------------------------------------------------
// FailoverMonitor tests
// -----------------------------------------------------------------------------

func TestFailoverMonitor_PromotesAfterThreshold(t *testing.T) {
	const threshold = 3
	var callCount int32 // counts health-check requests from the monitor

	// Always return 500.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	promoted := make(chan struct{}, 1)
	onPromote := func() {
		promoted <- struct{}{}
	}

	const interval = 20 * time.Millisecond
	mon := NewFailoverMonitor(srv.URL, interval, threshold, onPromote)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go mon.Start(ctx)

	select {
	case <-promoted:
		// Good — promoted fired.
	case <-ctx.Done():
		t.Fatal("onPromote was never called within the test window")
	}

	// After promotion the monitor stops; give it a moment then confirm onPromote
	// was called at most once (channel capacity 1 means a second send would block
	// and be lost, but we check the buffer isn't drained by a second send).
	time.Sleep(50 * time.Millisecond)
	if len(promoted) != 0 {
		t.Error("onPromote was called more than once")
	}

	// Exactly threshold requests must have been issued when onPromote fired.
	got := atomic.LoadInt32(&callCount)
	if got < int32(threshold) {
		t.Errorf("health-check count = %d, want >= %d", got, threshold)
	}
}

func TestFailoverMonitor_NoPromoteOnHealthy(t *testing.T) {
	// Always return 200.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	promoted := make(chan struct{}, 1)
	onPromote := func() { promoted <- struct{}{} }

	const interval = 20 * time.Millisecond
	mon := NewFailoverMonitor(srv.URL, interval, 3, onPromote)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go mon.Start(ctx)
	<-ctx.Done()

	select {
	case <-promoted:
		t.Error("onPromote was called despite healthy master")
	default:
		// Expected: no promotion.
	}
}

func TestFailoverMonitor_HealthyResetsMidSequence(t *testing.T) {
	// Verify that a healthy check resets the consecutive-failure counter.
	// The health server is driven in lock-step — it announces every request
	// and waits for the test to choose the response — so the outcome never
	// depends on how fast or how often the monitor polls.
	type reply struct{ ok bool }
	served := make(chan chan reply)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respCh := make(chan reply)
		select {
		case served <- respCh:
		case <-r.Context().Done():
			return // request canceled because the test has finished
		}
		if (<-respCh).ok {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	promoted := make(chan struct{}, 1)
	onPromote := func() { promoted <- struct{}{} }

	mon := NewFailoverMonitor(srv.URL, 5*time.Millisecond, 3, onPromote)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mon.Start(ctx)

	// answer responds to the monitor's next poll with the given health status.
	answer := func(ok bool) {
		t.Helper()
		select {
		case respCh := <-served:
			respCh <- reply{ok: ok}
		case <-time.After(2 * time.Second):
			t.Fatal("monitor did not poll within 2s")
		}
	}

	answer(false) // poll 1: fail  -> consecutive failures = 1
	answer(false) // poll 2: fail  -> consecutive failures = 2
	answer(true)  // poll 3: ok    -> must reset failures to 0
	answer(false) // poll 4: fail  -> 1 if the reset worked, 3 (-> promote) if not

	// A monitor that reset on poll 3 has only 1 failure after poll 4 and keeps
	// polling; a monitor that ignored the reset would have promoted at poll 4.
	select {
	case <-promoted:
		t.Error("onPromote fired after one post-reset failure; the healthy check did not reset the counter")
	case respCh := <-served:
		respCh <- reply{ok: true} // poll 5 arrived -> monitor still running -> reset worked
	case <-time.After(2 * time.Second):
		t.Fatal("monitor neither promoted nor polled again")
	}
}

func TestFailoverMonitor_PromoteExactlyOnce(t *testing.T) {
	// Always 500, very low threshold; verify onPromote is called exactly once
	// even though the monitor ticks many more times before the context expires.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var count int32
	onPromote := func() { atomic.AddInt32(&count, 1) }

	mon := NewFailoverMonitor(srv.URL, 20*time.Millisecond, 2, onPromote)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	go mon.Start(ctx)
	<-ctx.Done()

	if n := atomic.LoadInt32(&count); n != 1 {
		t.Errorf("onPromote called %d times, want exactly 1", n)
	}
}
