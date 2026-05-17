package scheduler

import (
	"errors"
	"time"
)

// Config holds tunable parameters that govern scheduler behavior. Pass it to
// NewStateManagerWithConfig; any zero-valued field falls back to its default.
type Config struct {
	// SuspiciousThreshold is how long a worker may go without a heartbeat
	// before it is marked "suspicious".
	SuspiciousThreshold time.Duration

	// OfflineThreshold is how long a worker may go without a heartbeat before
	// it is marked "offline" and dropped from the scheduling pool.
	OfflineThreshold time.Duration

	// TimeoutCheckInterval is how often RunTimeoutChecker scans for stale
	// workers.
	TimeoutCheckInterval time.Duration
}

// DefaultConfig returns the configuration used by NewStateManager.
func DefaultConfig() Config {
	return Config{
		SuspiciousThreshold:  SuspiciousThreshold,
		OfflineThreshold:     OfflineThreshold,
		TimeoutCheckInterval: DefaultTimeoutCheckInterval,
	}
}

// withDefaults returns a copy of c with every non-positive field replaced by
// its default value.
func (c Config) withDefaults() Config {
	if c.SuspiciousThreshold <= 0 {
		c.SuspiciousThreshold = SuspiciousThreshold
	}
	if c.OfflineThreshold <= 0 {
		c.OfflineThreshold = OfflineThreshold
	}
	if c.TimeoutCheckInterval <= 0 {
		c.TimeoutCheckInterval = DefaultTimeoutCheckInterval
	}
	return c
}

// Validate reports whether the configuration is internally consistent. It is
// intended for fail-fast checking of operator-supplied values at startup.
func (c Config) Validate() error {
	if c.SuspiciousThreshold <= 0 || c.OfflineThreshold <= 0 || c.TimeoutCheckInterval <= 0 {
		return errors.New("config: thresholds and intervals must be positive")
	}
	if c.SuspiciousThreshold >= c.OfflineThreshold {
		return errors.New("config: SuspiciousThreshold must be less than OfflineThreshold")
	}
	return nil
}
