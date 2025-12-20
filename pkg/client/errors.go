package client

import "fmt"

// ErrSchedulingFailed is returned when the scheduler cannot assign a task.
type ErrSchedulingFailed struct {
	Reason string // e.g., "no available worker matching requirements"
}

func (e *ErrSchedulingFailed) Error() string {
	return fmt.Sprintf("scheduling failed: %s", e.Reason)
}

// ErrInvalidResponse is returned when the server returns malformed data.
type ErrInvalidResponse struct {
	StatusCode int
	Body       string
}

func (e *ErrInvalidResponse) Error() string {
	return fmt.Sprintf("invalid response (HTTP %d): %s", e.StatusCode, e.Body)
}

// IsSchedulingError checks if an error is a scheduling failure (not a network error).
func IsSchedulingError(err error) bool {
	_, ok := err.(*ErrSchedulingFailed)
	return ok
}
