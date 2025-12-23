package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHeartbeatSerialization(t *testing.T) {
	hb := &Heartbeat{
		WorkerID:     "worker-001",
		Timestamp:    time.Now().Unix(),
		ResourceTags: []string{"gpu", "cuda-12.0"},
		MaxTasks:     30,
		CurrentTasks: 15,
		Address:      "192.168.1.100:8080",
	}

	data, err := json.Marshal(hb)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded Heartbeat
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify all Heartbeat fields
	if decoded.WorkerID != hb.WorkerID {
		t.Errorf("WorkerID mismatch: got %s, want %s", decoded.WorkerID, hb.WorkerID)
	}
	if decoded.Timestamp != hb.Timestamp {
		t.Errorf("Timestamp mismatch: got %d, want %d", decoded.Timestamp, hb.Timestamp)
	}
	if len(decoded.ResourceTags) != len(hb.ResourceTags) {
		t.Errorf("ResourceTags length mismatch: got %d, want %d", len(decoded.ResourceTags), len(hb.ResourceTags))
	}
	for i, tag := range decoded.ResourceTags {
		if tag != hb.ResourceTags[i] {
			t.Errorf("ResourceTags[%d] mismatch: got %s, want %s", i, tag, hb.ResourceTags[i])
		}
	}
	if decoded.MaxTasks != hb.MaxTasks {
		t.Errorf("MaxTasks mismatch: got %d, want %d", decoded.MaxTasks, hb.MaxTasks)
	}
	if decoded.CurrentTasks != hb.CurrentTasks {
		t.Errorf("CurrentTasks mismatch: got %d, want %d", decoded.CurrentTasks, hb.CurrentTasks)
	}
	if decoded.Address != hb.Address {
		t.Errorf("Address mismatch: got %s, want %s", decoded.Address, hb.Address)
	}
}

func TestScheduleRequestValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     *ScheduleRequest
		wantErr bool
	}{
		{
			name: "valid request",
			req: &ScheduleRequest{
				TaskID:       "task-001",
				RequiredTags: []string{"gpu"},
			},
			wantErr: false,
		},
		{
			name: "missing task id",
			req: &ScheduleRequest{
				TaskID:       "",
				RequiredTags: []string{"gpu"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWorkerState_LoadRatio(t *testing.T) {
	tests := []struct {
		name         string
		currentTasks int
		maxTasks     int
		wantRatio    float64
	}{
		{
			name:         "half load",
			currentTasks: 15,
			maxTasks:     30,
			wantRatio:    0.5,
		},
		{
			name:         "full load",
			currentTasks: 30,
			maxTasks:     30,
			wantRatio:    1.0,
		},
		{
			name:         "zero tasks",
			currentTasks: 0,
			maxTasks:     30,
			wantRatio:    0.0,
		},
		{
			name:         "divide by zero protection",
			currentTasks: 5,
			maxTasks:     0,
			wantRatio:    1.0,
		},
		{
			name:         "overloaded worker",
			currentTasks: 35,
			maxTasks:     30,
			wantRatio:    1.1666666666666667,
		},
		{
			name:         "one task capacity",
			currentTasks: 1,
			maxTasks:     1,
			wantRatio:    1.0,
		},
		{
			name:         "minimal load",
			currentTasks: 1,
			maxTasks:     100,
			wantRatio:    0.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := &WorkerState{
				CurrentTasks: tt.currentTasks,
				MaxTasks:     tt.maxTasks,
			}
			got := ws.LoadRatio()
			if got != tt.wantRatio {
				t.Errorf("LoadRatio() = %v, want %v", got, tt.wantRatio)
			}
		})
	}
}
