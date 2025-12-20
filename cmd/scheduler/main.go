package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/scheduler"
)

func main() {
	port := flag.String("port", "8080", "Server port")
	flag.Parse()

	log.Println("dtask-scheduler starting...")

	// Create state manager
	state := scheduler.NewStateManager()

	// Create HTTP handler
	handler := scheduler.NewHandler(state)

	// Setup routes
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/heartbeat", handler.HandleHeartbeat)
	mux.HandleFunc("/api/v1/schedule", handler.HandleSchedule)
	mux.HandleFunc("/api/v1/workers", handler.HandleListWorkers)

	// Start timeout checker goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				state.CheckTimeouts()
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start HTTP server
	server := &http.Server{
		Addr:    ":" + *port,
		Handler: mux,
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down gracefully...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		cancel() // Stop timeout checker
		server.Shutdown(shutdownCtx)
	}()

	log.Printf("Scheduler listening on port %s", *port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}

	log.Println("Server stopped")
}
