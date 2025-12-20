// Package client provides an HTTP client for the dtask-scheduler API.
// It offers convenient methods to send heartbeats, schedule tasks, and list workers.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chicogong/dtask-scheduler/pkg/types"
)

// Client is an HTTP client for the dtask-scheduler API.
// It provides methods to send heartbeats, schedule tasks, and list workers.
// All methods are safe for concurrent use.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new scheduler client with the given base URL and options.
//
// Example:
//
//	client := client.NewClient("http://localhost:8080")
//	client := client.NewClient("http://localhost:8080", client.WithTimeout(10*time.Second))
func NewClient(baseURL string, opts ...Option) *Client {
	// Remove trailing slash from baseURL
	baseURL = strings.TrimRight(baseURL, "/")

	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}

	// Apply options
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Heartbeat sends a worker heartbeat to the scheduler.
// It registers or updates the worker's state, including capacity and resource tags.
//
// Example:
//
//	hb := &types.Heartbeat{
//	    WorkerID:     "worker-001",
//	    Address:      "192.168.1.100:8080",
//	    ResourceTags: []string{"gpu", "cuda-12.0"},
//	    MaxTasks:     30,
//	    CurrentTasks: 10,
//	    Timestamp:    time.Now().Unix(),
//	}
//	if err := client.Heartbeat(ctx, hb); err != nil {
//	    log.Fatal(err)
//	}
func (c *Client) Heartbeat(ctx context.Context, hb *types.Heartbeat) error {
	body, err := json.Marshal(hb)
	if err != nil {
		return fmt.Errorf("marshal heartbeat: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/api/v1/heartbeat", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send heartbeat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.parseError(resp)
	}

	return nil
}

// Schedule sends a scheduling request to assign a task to a worker.
// It returns the selected worker ID and address, or an error if no workers are available.
//
// Example:
//
//	req := &types.ScheduleRequest{
//	    TaskID:       "task-001",
//	    RequiredTags: []string{"gpu"},
//	}
//	resp, err := client.Schedule(ctx, req)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Task assigned to %s at %s\n", resp.WorkerID, resp.Address)
func (c *Client) Schedule(ctx context.Context, req *types.ScheduleRequest) (*types.ScheduleResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/api/v1/schedule", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send schedule request: %w", err)
	}
	defer resp.Body.Close()

	var schedResp types.ScheduleResponse
	if err := json.NewDecoder(resp.Body).Decode(&schedResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Check for scheduling errors in response
	if schedResp.Error != "" {
		return &schedResp, &ErrSchedulingFailed{Reason: schedResp.Error}
	}

	if resp.StatusCode != http.StatusOK {
		return &schedResp, c.parseError(resp)
	}

	return &schedResp, nil
}

// ListWorkers retrieves the current state of all registered workers.
//
// Example:
//
//	workers, err := client.ListWorkers(ctx)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, w := range workers {
//	    fmt.Printf("%s: %d/%d tasks (%.1f%%)\n",
//	        w.WorkerID, w.CurrentTasks, w.MaxTasks, w.LoadRatio()*100)
//	}
func (c *Client) ListWorkers(ctx context.Context) ([]*types.WorkerState, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		c.baseURL+"/api/v1/workers", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list workers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}

	var workers []*types.WorkerState
	if err := json.NewDecoder(resp.Body).Decode(&workers); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return workers, nil
}

// parseError extracts error information from HTTP response
func (c *Client) parseError(resp *http.Response) error {
	var errResp struct {
		Error string `json:"error"`
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(bodyBytes, &errResp); err == nil && errResp.Error != "" {
		return &ErrInvalidResponse{
			StatusCode: resp.StatusCode,
			Body:       errResp.Error,
		}
	}

	return &ErrInvalidResponse{
		StatusCode: resp.StatusCode,
		Body:       string(bodyBytes),
	}
}
