package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chicogong/dtask-scheduler/internal/worker"
)

func main() {
	workerID := flag.String("id", "worker-001", "Worker ID")
	address := flag.String("addr", "localhost:9000", "Worker address")
	tags := flag.String("tags", "cpu", "Resource tags (comma-separated)")
	maxTasks := flag.Int("max-tasks", 30, "Maximum concurrent tasks")
	schedulerURL := flag.String("scheduler", "http://localhost:8080", "Scheduler URL")
	flag.Parse()

	log.Println("dtask-worker starting...")
	log.Printf("Worker ID: %s", *workerID)
	log.Printf("Address: %s", *address)
	log.Printf("Tags: %s", *tags)
	log.Printf("Max tasks: %d", *maxTasks)

	resourceTags := strings.Split(*tags, ",")
	for i := range resourceTags {
		resourceTags[i] = strings.TrimSpace(resourceTags[i])
	}

	sender := worker.NewHeartbeatSender(*workerID, *address, resourceTags, *maxTasks, *schedulerURL)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start heartbeat sender in goroutine
	go sender.Start(ctx)

	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutting down gracefully...")

	// Cancel context to stop heartbeat sender
	cancel()

	// Give a short grace period for cleanup
	time.Sleep(500 * time.Millisecond)

	log.Println("Worker stopped")
}
