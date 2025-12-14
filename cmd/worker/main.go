package main

import (
	"flag"
	"log"
	"strings"

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
	sender.Start() // Blocks forever
}
