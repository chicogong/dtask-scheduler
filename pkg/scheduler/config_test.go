package scheduler

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.SuspiciousThreshold != SuspiciousThreshold {
		t.Errorf("SuspiciousThreshold = %v, want %v", cfg.SuspiciousThreshold, SuspiciousThreshold)
	}
	if cfg.OfflineThreshold != OfflineThreshold {
		t.Errorf("OfflineThreshold = %v, want %v", cfg.OfflineThreshold, OfflineThreshold)
	}
	if cfg.TimeoutCheckInterval != DefaultTimeoutCheckInterval {
		t.Errorf("TimeoutCheckInterval = %v, want %v", cfg.TimeoutCheckInterval, DefaultTimeoutCheckInterval)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig() must be valid, got: %v", err)
	}
}

func TestConfig_withDefaults(t *testing.T) {
	// A zero config is fully populated with defaults.
	if got := (Config{}).withDefaults(); got != DefaultConfig() {
		t.Errorf("withDefaults() on zero config = %+v, want %+v", got, DefaultConfig())
	}

	// Explicit values are preserved untouched.
	custom := Config{
		SuspiciousThreshold:  5 * time.Second,
		OfflineThreshold:     15 * time.Second,
		TimeoutCheckInterval: time.Second,
	}
	if got := custom.withDefaults(); got != custom {
		t.Errorf("withDefaults() changed an explicit config: %+v != %+v", got, custom)
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "valid defaults",
			cfg:     DefaultConfig(),
			wantErr: false,
		},
		{
			name: "suspicious not less than offline",
			cfg: Config{
				SuspiciousThreshold:  20 * time.Second,
				OfflineThreshold:     20 * time.Second,
				TimeoutCheckInterval: 5 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "zero suspicious threshold",
			cfg: Config{
				SuspiciousThreshold:  0,
				OfflineThreshold:     20 * time.Second,
				TimeoutCheckInterval: 5 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "negative check interval",
			cfg: Config{
				SuspiciousThreshold:  10 * time.Second,
				OfflineThreshold:     20 * time.Second,
				TimeoutCheckInterval: -1 * time.Second,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
