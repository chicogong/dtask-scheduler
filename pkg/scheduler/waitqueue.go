package scheduler

import "sync"

// capacityNotifier is a broadcast primitive used by the wait queue: a single
// broadcast wakes every goroutine currently parked on the channel returned by
// waitChan. It lets schedule requests block until a worker reports free
// capacity instead of failing immediately when the pool is momentarily full.
//
// capacityNotifier is safe for concurrent use.
type capacityNotifier struct {
	mu sync.Mutex
	ch chan struct{}
}

// newCapacityNotifier creates a notifier with a fresh, un-fired channel.
func newCapacityNotifier() *capacityNotifier {
	return &capacityNotifier{ch: make(chan struct{})}
}

// waitChan returns a channel that is closed on the next broadcast. Callers must
// fetch the channel BEFORE re-checking the condition they are waiting on, so a
// broadcast that races with the check is observed rather than lost.
func (n *capacityNotifier) waitChan() <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.ch
}

// broadcast wakes every current waiter and arms a fresh channel for the next.
func (n *capacityNotifier) broadcast() {
	n.mu.Lock()
	defer n.mu.Unlock()
	close(n.ch)
	n.ch = make(chan struct{})
}
