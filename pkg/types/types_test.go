package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHeartbeatSerialization(t *testing.T) {
	hb := &Heartbeat{
		WorkerID:      "worker-001",
		Timestamp:     time.Now().Unix(),
		ResourceTags:  []string{"gpu", "cuda-12.0"},
		MaxTasks:      30,
		CurrentTasks:  15,
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

	if decoded.WorkerID != hb.WorkerID {
		t.Errorf("WorkerID mismatch: got %s, want %s", decoded.WorkerID, hb.WorkerID)
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
