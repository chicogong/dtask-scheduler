package scheduler

import (
	"testing"
	"time"
)

func TestCapacityNotifier_Broadcast(t *testing.T) {
	n := newCapacityNotifier()

	ch := n.waitChan()
	select {
	case <-ch:
		t.Fatal("channel closed before any broadcast")
	default:
	}

	n.broadcast()
	select {
	case <-ch:
		// expected: broadcast closed the waiter's channel
	case <-time.After(time.Second):
		t.Fatal("broadcast did not wake the waiter")
	}

	// After a broadcast a fresh channel is armed for the next waiter.
	next := n.waitChan()
	select {
	case <-next:
		t.Fatal("post-broadcast channel should still be open")
	default:
	}
}

func TestCapacityNotifier_WakesMultipleWaiters(t *testing.T) {
	n := newCapacityNotifier()

	const waiters = 5
	done := make(chan struct{}, waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			<-n.waitChan()
			done <- struct{}{}
		}()
	}

	// Give the goroutines a moment to park, then wake them all at once.
	time.Sleep(50 * time.Millisecond)
	n.broadcast()

	for i := 0; i < waiters; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("only %d/%d waiters woke up", i, waiters)
		}
	}
}
