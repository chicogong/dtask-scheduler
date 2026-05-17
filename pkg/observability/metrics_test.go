package observability

import (
	"sync"
	"testing"
	"time"
)

func TestNewMetrics_ZeroValues(t *testing.T) {
	m := NewMetrics()
	snap := m.Snapshot()

	if snap.ScheduleRequests != 0 {
		t.Errorf("ScheduleRequests: want 0, got %d", snap.ScheduleRequests)
	}
	if snap.ScheduleSuccesses != 0 {
		t.Errorf("ScheduleSuccesses: want 0, got %d", snap.ScheduleSuccesses)
	}
	if snap.ScheduleFailures != 0 {
		t.Errorf("ScheduleFailures: want 0, got %d", snap.ScheduleFailures)
	}
	if snap.Heartbeats != 0 {
		t.Errorf("Heartbeats: want 0, got %d", snap.Heartbeats)
	}
	if snap.ScheduleLatencyAvgMS != 0 {
		t.Errorf("ScheduleLatencyAvgMS: want 0, got %f", snap.ScheduleLatencyAvgMS)
	}
	if snap.ScheduleLatencyMaxMS != 0 {
		t.Errorf("ScheduleLatencyMaxMS: want 0, got %f", snap.ScheduleLatencyMaxMS)
	}
}

func TestRecordSchedule_Counters(t *testing.T) {
	tests := []struct {
		name          string
		calls         []bool // success flag per call
		wantRequests  int64
		wantSuccesses int64
		wantFailures  int64
	}{
		{
			name:          "all successes",
			calls:         []bool{true, true, true},
			wantRequests:  3,
			wantSuccesses: 3,
			wantFailures:  0,
		},
		{
			name:          "all failures",
			calls:         []bool{false, false},
			wantRequests:  2,
			wantSuccesses: 0,
			wantFailures:  2,
		},
		{
			name:          "mixed",
			calls:         []bool{true, false, true, false, false},
			wantRequests:  5,
			wantSuccesses: 2,
			wantFailures:  3,
		},
		{
			name:          "single success",
			calls:         []bool{true},
			wantRequests:  1,
			wantSuccesses: 1,
			wantFailures:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMetrics()
			for _, success := range tc.calls {
				m.RecordSchedule(time.Millisecond, success)
			}
			snap := m.Snapshot()
			if snap.ScheduleRequests != tc.wantRequests {
				t.Errorf("ScheduleRequests: want %d, got %d", tc.wantRequests, snap.ScheduleRequests)
			}
			if snap.ScheduleSuccesses != tc.wantSuccesses {
				t.Errorf("ScheduleSuccesses: want %d, got %d", tc.wantSuccesses, snap.ScheduleSuccesses)
			}
			if snap.ScheduleFailures != tc.wantFailures {
				t.Errorf("ScheduleFailures: want %d, got %d", tc.wantFailures, snap.ScheduleFailures)
			}
		})
	}
}

func TestRecordHeartbeat_Counter(t *testing.T) {
	m := NewMetrics()
	const n = 7
	for i := 0; i < n; i++ {
		m.RecordHeartbeat()
	}
	if got := m.Snapshot().Heartbeats; got != n {
		t.Errorf("Heartbeats: want %d, got %d", n, got)
	}
}

func TestLatency_AverageAndMax(t *testing.T) {
	tests := []struct {
		name      string
		durations []time.Duration
		wantAvgMS float64
		wantMaxMS float64
	}{
		{
			name:      "single 10ms",
			durations: []time.Duration{10 * time.Millisecond},
			wantAvgMS: 10,
			wantMaxMS: 10,
		},
		{
			name: "two values 10ms and 20ms",
			// avg = 15, max = 20
			durations: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
			wantAvgMS: 15,
			wantMaxMS: 20,
		},
		{
			name: "three values",
			// 5 + 15 + 10 = 30, avg = 10, max = 15
			durations: []time.Duration{5 * time.Millisecond, 15 * time.Millisecond, 10 * time.Millisecond},
			wantAvgMS: 10,
			wantMaxMS: 15,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMetrics()
			for _, d := range tc.durations {
				m.RecordSchedule(d, true)
			}
			snap := m.Snapshot()

			const eps = 1e-9
			if diff := snap.ScheduleLatencyAvgMS - tc.wantAvgMS; diff > eps || diff < -eps {
				t.Errorf("ScheduleLatencyAvgMS: want %f, got %f", tc.wantAvgMS, snap.ScheduleLatencyAvgMS)
			}
			if diff := snap.ScheduleLatencyMaxMS - tc.wantMaxMS; diff > eps || diff < -eps {
				t.Errorf("ScheduleLatencyMaxMS: want %f, got %f", tc.wantMaxMS, snap.ScheduleLatencyMaxMS)
			}
		})
	}
}

func TestLatency_ZeroWhenNoRequests(t *testing.T) {
	m := NewMetrics()
	snap := m.Snapshot()
	if snap.ScheduleLatencyAvgMS != 0 {
		t.Errorf("avg latency should be 0 with no requests, got %f", snap.ScheduleLatencyAvgMS)
	}
}

// TestConcurrency_Race checks that concurrent RecordSchedule and RecordHeartbeat
// calls do not race. Run with -race to exercise the race detector.
func TestConcurrency_Race(t *testing.T) {
	m := NewMetrics()
	const goroutines = 50
	const callsEach = 200

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Half goroutines call RecordSchedule
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			for j := 0; j < callsEach; j++ {
				m.RecordSchedule(time.Duration(j)*time.Microsecond, j%2 == 0)
			}
		}(i)
	}

	// Half goroutines call RecordHeartbeat
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsEach; j++ {
				m.RecordHeartbeat()
			}
		}()
	}

	wg.Wait()

	snap := m.Snapshot()
	wantRequests := int64(goroutines * callsEach)
	if snap.ScheduleRequests != wantRequests {
		t.Errorf("ScheduleRequests: want %d, got %d", wantRequests, snap.ScheduleRequests)
	}
	wantHeartbeats := int64(goroutines * callsEach)
	if snap.Heartbeats != wantHeartbeats {
		t.Errorf("Heartbeats: want %d, got %d", wantHeartbeats, snap.Heartbeats)
	}
	// successes + failures must equal requests
	if snap.ScheduleSuccesses+snap.ScheduleFailures != snap.ScheduleRequests {
		t.Errorf("successes+failures (%d) != requests (%d)", snap.ScheduleSuccesses+snap.ScheduleFailures, snap.ScheduleRequests)
	}
}

// TestSnapshot_Concurrent verifies that Snapshot can be called concurrently
// with RecordSchedule without panicking or racing.
func TestSnapshot_Concurrent(t *testing.T) {
	m := NewMetrics()

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			m.RecordSchedule(time.Millisecond, i%3 != 0)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			m.RecordHeartbeat()
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = m.Snapshot()
		}
	}()

	wg.Wait()
}
