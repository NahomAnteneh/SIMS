package collector_test

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
)

func TestBackoff_1000SamplesPerStage(t *testing.T) {
	cfg := collector.BackoffConfig{
		BaseDelay: 2 * time.Second,
		MaxDelay:  60 * time.Second,
		Factor:    2.0,
		Jitter:    0.2, // 20%
	}

	stages := []struct {
		attempt  int
		minBound time.Duration
		maxBound time.Duration
	}{
		{
			attempt:  0,
			minBound: time.Duration(float64(2*time.Second) * 0.8), // 1.6s
			maxBound: time.Duration(float64(2*time.Second) * 1.2), // 2.4s
		},
		{
			attempt:  1,
			minBound: time.Duration(float64(4*time.Second) * 0.8), // 3.2s
			maxBound: time.Duration(float64(4*time.Second) * 1.2), // 4.8s
		},
		{
			attempt:  2,
			minBound: time.Duration(float64(8*time.Second) * 0.8), // 6.4s
			maxBound: time.Duration(float64(8*time.Second) * 1.2), // 9.6s
		},
		{
			attempt:  3,
			minBound: time.Duration(float64(16*time.Second) * 0.8), // 12.8s
			maxBound: time.Duration(float64(16*time.Second) * 1.2), // 19.2s
		},
		{
			attempt:  4,
			minBound: time.Duration(float64(32*time.Second) * 0.8), // 25.6s
			maxBound: time.Duration(float64(32*time.Second) * 1.2), // 38.4s
		},
		{
			attempt:  5,
			minBound: time.Duration(float64(60*time.Second) * 0.8), // 48s (capped at 60s)
			maxBound: 60 * time.Second,
		},
		{
			attempt:  6,
			minBound: time.Duration(float64(60*time.Second) * 0.8), // 48s (capped at 60s)
			maxBound: 60 * time.Second,
		},
		{
			attempt:  10,
			minBound: time.Duration(float64(60*time.Second) * 0.8), // 48s (capped at 60s)
			maxBound: 60 * time.Second,
		},
	}

	const samplesPerStage = 1000

	for _, stage := range stages {
		src := rand.NewSource(42 + int64(stage.attempt))
		bo := collector.NewBackoff(cfg, src)

		var sum time.Duration
		var observedMin = time.Duration(1<<63 - 1)
		var observedMax time.Duration = 0

		for i := 0; i < samplesPerStage; i++ {
			d := bo.Delay(stage.attempt)

			if d < 0 {
				t.Fatalf("attempt %d sample %d: delay was negative: %v", stage.attempt, i, d)
			}
			if d > cfg.MaxDelay {
				t.Fatalf("attempt %d sample %d: delay exceeded max delay: %v > %v", stage.attempt, i, d, cfg.MaxDelay)
			}
			if d < stage.minBound {
				t.Fatalf("attempt %d sample %d: delay %v below theoretical min %v", stage.attempt, i, d, stage.minBound)
			}
			if d > stage.maxBound {
				t.Fatalf("attempt %d sample %d: delay %v exceeded theoretical max %v", stage.attempt, i, d, stage.maxBound)
			}

			sum += d
			if d < observedMin {
				observedMin = d
			}
			if d > observedMax {
				observedMax = d
			}
		}

		// Verify jitter actually varied the delays across 1,000 samples
		if observedMin == observedMax {
			t.Errorf("attempt %d: all 1,000 samples were identical (%v); jitter is not working", stage.attempt, observedMin)
		}
	}
}

func TestBackoff_NegativeAttemptClamped(t *testing.T) {
	bo := collector.NewBackoff(collector.DefaultBackoffConfig(), rand.NewSource(99))
	dNeg := bo.Delay(-5)
	if dNeg < time.Duration(float64(2*time.Second)*0.8) || dNeg > time.Duration(float64(2*time.Second)*1.2) {
		t.Errorf("negative attempt should clamp to attempt 0, got %v", dNeg)
	}
}

func TestBackoff_SleepContextCancellation(t *testing.T) {
	bo := collector.NewBackoff(collector.DefaultBackoffConfig(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := bo.Sleep(ctx, 0)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}
