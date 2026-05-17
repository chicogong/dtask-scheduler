package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/ha"
	"github.com/chicogong/dtask-scheduler/pkg/middleware"
	"github.com/chicogong/dtask-scheduler/pkg/observability"
	"github.com/chicogong/dtask-scheduler/pkg/scheduler"
)

func main() {
	var (
		port                = flag.String("port", "8080", "Server port")
		roleFlag            = flag.String("role", "master", "Scheduler role: master or standby")
		peer                = flag.String("peer", "", "Peer scheduler base URL (master replicates to it; standby health-checks it)")
		suspicious          = flag.Duration("suspicious-threshold", scheduler.SuspiciousThreshold, "Heartbeat age before a worker is marked suspicious")
		offline             = flag.Duration("offline-threshold", scheduler.OfflineThreshold, "Heartbeat age before a worker is marked offline")
		checkInterval       = flag.Duration("timeout-check-interval", scheduler.DefaultTimeoutCheckInterval, "How often to scan for stale workers")
		maxBodyBytes        = flag.Int64("max-body-bytes", 1<<20, "Maximum accepted request body size in bytes")
		replicationInterval = flag.Duration("replication-interval", 2*time.Second, "How often the master replicates state to the standby")
		failoverInterval    = flag.Duration("failover-interval", 2*time.Second, "How often the standby health-checks the master")
		failoverThreshold   = flag.Int("failover-threshold", 3, "Consecutive failed health checks before the standby self-promotes")
	)
	flag.Parse()

	role := ha.Role(*roleFlag)
	if role != ha.RoleMaster && role != ha.RoleStandby {
		log.Fatalf("invalid --role %q: must be 'master' or 'standby'", *roleFlag)
	}

	cfg := scheduler.Config{
		SuspiciousThreshold:  *suspicious,
		OfflineThreshold:     *offline,
		TimeoutCheckInterval: *checkInterval,
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	log.Printf("dtask-scheduler starting (role=%s)...", role)

	// Core components.
	state := scheduler.NewStateManagerWithConfig(cfg)
	metrics := observability.NewMetrics()
	handler := scheduler.NewHandlerWithMetrics(state, metrics)
	obs := observability.NewHandler(metrics, state)
	roleHolder := ha.NewRoleHolder(role)

	// Routes.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/heartbeat", handler.HandleHeartbeat)
	mux.HandleFunc("/api/v1/schedule", requireRole(roleHolder, ha.RoleMaster, handler.HandleSchedule))
	mux.HandleFunc("/api/v1/workers", handler.HandleListWorkers)
	mux.HandleFunc("/api/v1/sync", requireRole(roleHolder, ha.RoleStandby, ha.NewSyncHandler(state)))
	obs.Register(mux) // /healthz, /metrics, /stats

	// Middleware chain (Recover outermost, BodyLimit closest to the handler).
	logger := log.Default()
	root := middleware.Chain(mux,
		middleware.Recover(logger),
		middleware.RequestID(),
		middleware.AccessLog(logger),
		middleware.BodyLimit(*maxBodyBytes),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Background: stale-worker detection.
	go state.RunTimeoutChecker(ctx)

	// Background: HA replication (master) or failover monitoring (standby).
	startHA(ctx, role, *peer, *replicationInterval, *failoverInterval, *failoverThreshold, state, roleHolder)

	server := &http.Server{
		Addr:              ":" + *port,
		Handler:           root,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Graceful shutdown.
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down gracefully...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		cancel() // stop background goroutines
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	}()

	log.Printf("Scheduler listening on port %s", *port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}

	log.Println("Server stopped")
}

// requireRole rejects requests with 503 unless the node currently holds the
// wanted role. It lets a standby refuse scheduling and a master refuse state
// replication, while both roles still serve heartbeats and health checks.
func requireRole(holder *ha.RoleHolder, want ha.Role, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if got := holder.Get(); got != want {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "scheduler role is " + string(got) + ", endpoint requires " + string(want),
			})
			return
		}
		next(w, r)
	}
}

// startHA wires background HA behavior: a master replicates worker state to its
// peer; a standby health-checks the master and self-promotes on sustained
// failure. It is a no-op when --peer is not set.
func startHA(ctx context.Context, role ha.Role, peer string, replicationInterval, failoverInterval time.Duration, failoverThreshold int, state *scheduler.StateManager, roleHolder *ha.RoleHolder) {
	if peer == "" {
		if role == ha.RoleStandby {
			log.Println("Warning: standby role set but --peer is empty; failover monitoring disabled")
		}
		return
	}

	switch role {
	case ha.RoleMaster:
		replicator := ha.NewReplicator(state, peer+"/api/v1/sync", replicationInterval)
		go replicator.Start(ctx)
		log.Printf("Replicating worker state to %s every %v", peer, replicationInterval)
	case ha.RoleStandby:
		monitor := ha.NewFailoverMonitor(peer+"/healthz", failoverInterval, failoverThreshold, func() {
			log.Println("Master unreachable; promoting this node to master")
			roleHolder.Set(ha.RoleMaster)
		})
		go monitor.Start(ctx)
		log.Printf("Monitoring master at %s (promote after %d failed checks)", peer, failoverThreshold)
	}
}
