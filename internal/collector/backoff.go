package collector

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"time"
)

// BackoffConfig defines exponential backoff and jitter parameters.
type BackoffConfig struct {
	BaseDelay time.Duration // Default: 2s
	MaxDelay  time.Duration // Default: 60s
	Factor    float64       // Default: 2.0
	Jitter    float64       // Default: 0.2 (20% bounded jitter)
}

// DefaultBackoffConfig returns verified production defaults.
func DefaultBackoffConfig() BackoffConfig {
	return BackoffConfig{
		BaseDelay: 2 * time.Second,
		MaxDelay:  60 * time.Second,
		Factor:    2.0,
		Jitter:    0.2,
	}
}

// Backoff computes bounded, jittered exponential backoff intervals with thread-safe randomness.
type Backoff struct {
	cfg BackoffConfig
	mu  sync.Mutex
	rng *rand.Rand
}

// NewBackoff constructs a Backoff generator. If src is nil, a time-seeded source is used.
func NewBackoff(cfg BackoffConfig, src rand.Source) *Backoff {
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = 2 * time.Second
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 60 * time.Second
	}
	if cfg.Factor <= 1.0 {
		cfg.Factor = 2.0
	}
	if cfg.Jitter < 0 || cfg.Jitter > 1.0 {
		cfg.Jitter = 0.2
	}
	if src == nil {
		src = rand.NewSource(time.Now().UnixNano())
	}
	return &Backoff{
		cfg: cfg,
		rng: rand.New(src),
	}
}

// Config returns the active backoff configuration.
func (b *Backoff) Config() BackoffConfig {
	return b.cfg
}

// Delay calculates the bounded, jittered backoff duration for a given attempt (0-indexed).
//
// Formula:
//
//	raw = min(MaxDelay, BaseDelay * Factor^attempt)
//	jitterDelta = (rand[-1, +1]) * Jitter * raw
//	delay = clamp(raw + jitterDelta, 0, MaxDelay)
func (b *Backoff) Delay(attempt int) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	if attempt < 0 {
		attempt = 0
	}

	multiplier := math.Pow(b.cfg.Factor, float64(attempt))
	rawNanos := float64(b.cfg.BaseDelay.Nanoseconds()) * multiplier
	maxNanos := float64(b.cfg.MaxDelay.Nanoseconds())

	cappedNanos := math.Min(rawNanos, maxNanos)

	// Bounded jitter in range [-Jitter, +Jitter]
	delta := (b.rng.Float64()*2.0 - 1.0) * b.cfg.Jitter * cappedNanos
	jitteredNanos := cappedNanos + delta

	if jitteredNanos < 0 {
		jitteredNanos = 0
	}
	if jitteredNanos > maxNanos {
		jitteredNanos = maxNanos
	}

	return time.Duration(jitteredNanos)
}

// Sleep pauses execution for the calculated Delay(attempt) duration,
// returning prematurely with ctx.Err() if ctx is canceled or expired.
func (b *Backoff) Sleep(ctx context.Context, attempt int) error {
	d := b.Delay(attempt)
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
