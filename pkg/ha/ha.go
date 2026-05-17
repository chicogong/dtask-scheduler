// Package ha implements high-availability primitives for dtask-scheduler:
// atomic role management, master-to-standby state replication, and
// failover detection with configurable thresholds.
//
// Typical wiring on the master side:
//
//	store := myStateManager           // implements ha.StateStore
//	rep   := ha.NewReplicator(store, "http://standby:8081/sync", 5*time.Second)
//	go rep.Start(ctx)
//
// Typical wiring on the standby side:
//
//	store   := myStateManager         // implements ha.StateStore
//	role    := ha.NewRoleHolder(ha.RoleStandby)
//	mux.Handle("/sync", ha.NewSyncHandler(store))
//	mon := ha.NewFailoverMonitor(
//	    "http://master:8080/health", 2*time.Second, 3,
//	    func() { role.Set(ha.RoleMaster); /* start scheduling */ },
//	)
//	go mon.Start(ctx)
package ha

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

// Role identifies the operational role of this scheduler node.
type Role string

const (
	// RoleMaster is the active scheduling node that accepts work and replicates
	// its state to the standby.
	RoleMaster Role = "master"

	// RoleStandby is the passive node that receives replicated state from the
	// master and monitors the master for failure.
	RoleStandby Role = "standby"
)

// StateStore is the interface that the scheduler's in-memory worker-state table
// must satisfy so the HA layer can snapshot and restore it without importing the
// scheduler package directly.
type StateStore interface {
	// ListWorkers returns a snapshot of all currently-registered workers.
	ListWorkers() []*types.WorkerState

	// LoadSnapshot replaces the current worker table with the provided slice.
	// It is called on the standby after a replication payload is received.
	LoadSnapshot([]*types.WorkerState)
}

// -----------------------------------------------------------------------------
// RoleHolder
// -----------------------------------------------------------------------------

// RoleHolder is a concurrency-safe container for the current node Role.
// Reads and writes use an atomic pointer so callers may access it from
// multiple goroutines without additional locking.
type RoleHolder struct {
	role atomic.Pointer[Role]
}

// NewRoleHolder creates a RoleHolder initialised to the given role.
func NewRoleHolder(initial Role) *RoleHolder {
	rh := &RoleHolder{}
	rh.role.Store(&initial)
	return rh
}

// Get returns the currently-held role. Safe for concurrent use.
func (rh *RoleHolder) Get() Role {
	return *rh.role.Load()
}

// Set atomically replaces the held role. Safe for concurrent use.
func (rh *RoleHolder) Set(r Role) {
	rh.role.Store(&r)
}

// IsMaster reports whether the current role is RoleMaster.
func (rh *RoleHolder) IsMaster() bool {
	return rh.Get() == RoleMaster
}

// -----------------------------------------------------------------------------
// Replicator (master side)
// -----------------------------------------------------------------------------

// Replicator runs on the master node.  Every replication interval it snapshots
// the local StateStore, JSON-encodes the result, and HTTP POSTs it to the
// standby's sync endpoint.  Errors are logged but never cause the loop to exit.
type Replicator struct {
	store       StateStore
	peerSyncURL string
	interval    time.Duration
	client      *http.Client
}

// NewReplicator creates a Replicator.  peerSyncURL is the full URL of the
// standby's sync endpoint (e.g. "http://standby:8081/sync").  interval
// controls how often automatic replication runs.
func NewReplicator(store StateStore, peerSyncURL string, interval time.Duration) *Replicator {
	return &Replicator{
		store:       store,
		peerSyncURL: peerSyncURL,
		interval:    interval,
		client:      &http.Client{Timeout: 5 * time.Second},
	}
}

// ReplicateOnce performs a single replication push: it snapshots the store,
// JSON-encodes the snapshot, and POSTs it to the peer sync URL.  The caller
// receives the raw error so tests and initialisation code can handle it.
func (r *Replicator) ReplicateOnce(ctx context.Context) error {
	workers := r.store.ListWorkers()

	body, err := json.Marshal(workers)
	if err != nil {
		return fmt.Errorf("ha: marshal workers: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.peerSyncURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ha: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("ha: post to peer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ha: peer returned status %d", resp.StatusCode)
	}
	return nil
}

// Start blocks and replicates on every tick of the configured interval.
// It returns when ctx is cancelled.  Any per-cycle error is logged and the
// loop continues so transient network blips do not interrupt the master.
func (r *Replicator) Start(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.ReplicateOnce(ctx); err != nil {
				log.Printf("ha: replication error: %v", err)
			}
		}
	}
}

// -----------------------------------------------------------------------------
// SyncHandler (standby side)
// -----------------------------------------------------------------------------

// NewSyncHandler returns an http.HandlerFunc that accepts a POST request
// carrying a JSON-encoded []*types.WorkerState body, deserialises it, and
// calls store.LoadSnapshot with the result.
//
// Response codes:
//   - 200 + {"status":"ok"} on success
//   - 400 if the request body cannot be decoded as JSON
//   - 405 if the HTTP method is not POST
func NewSyncHandler(store StateStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var workers []*types.WorkerState
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&workers); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}

		store.LoadSnapshot(workers)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Best-effort write; errors here cannot be meaningfully handled.
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}

// -----------------------------------------------------------------------------
// FailoverMonitor (standby side)
// -----------------------------------------------------------------------------

// FailoverMonitor runs on the standby node.  It polls the master's health
// endpoint at a configurable interval.  When consecutive failures reach the
// configured threshold, it calls onPromote exactly once and stops.
type FailoverMonitor struct {
	masterHealthURL string
	interval        time.Duration
	failThreshold   int
	onPromote       func()
	client          *http.Client
}

// NewFailoverMonitor creates a FailoverMonitor.
//
//   - masterHealthURL: full URL to the master's health endpoint (e.g. "http://master:8080/health").
//   - interval: how often to poll.
//   - failThreshold: number of consecutive failures required to trigger promotion.
//   - onPromote: called exactly once when the threshold is reached; must be safe
//     to call from the monitor goroutine.
func NewFailoverMonitor(masterHealthURL string, interval time.Duration, failThreshold int, onPromote func()) *FailoverMonitor {
	return &FailoverMonitor{
		masterHealthURL: masterHealthURL,
		interval:        interval,
		failThreshold:   failThreshold,
		onPromote:       onPromote,
		client:          &http.Client{Timeout: 3 * time.Second},
	}
}

// Start blocks and polls the master health endpoint every interval.
// A non-2xx HTTP status or any transport error counts as one consecutive
// failure.  A 2xx response resets the consecutive failure counter to zero.
// When the counter reaches failThreshold, onPromote is called once and Start
// returns.  Start also returns when ctx is cancelled.
func (m *FailoverMonitor) Start(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	consecutiveFails := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.checkHealth(ctx) {
				consecutiveFails = 0
			} else {
				consecutiveFails++
				if consecutiveFails >= m.failThreshold {
					m.onPromote()
					return
				}
			}
		}
	}
}

// checkHealth performs a single GET to masterHealthURL.  Returns true if the
// master responded with a 2xx status, false on any error or non-2xx status.
func (m *FailoverMonitor) checkHealth(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.masterHealthURL, nil)
	if err != nil {
		return false
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
