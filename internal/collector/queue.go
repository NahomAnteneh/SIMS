package collector

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/infrastructure/sims/internal/telemetry"
)

var (
	// ErrQueueClosed indicates that the BoundedQueue has been closed.
	ErrQueueClosed = errors.New("bounded queue is closed")
)

// BoundedQueue is a thread-safe, strictly blocking in-memory queue that enforces
// capacity limits and exposes real-time telemetry (depth, capacity, fill ratio, blocked producers).
// Invariants:
// - Never drops oldest
// - Never drops newest
// - Never silently overwrites
// - Never samples
type BoundedQueue[T any] struct {
	vcenterID string
	capacity  int
	metrics   *telemetry.Metrics

	ch               chan T
	closed           chan struct{}
	closeOnce        sync.Once
	isClosed         atomic.Bool
	blockedProducers atomic.Int64
}

// NewBoundedQueue constructs a new BoundedQueue with the given capacity.
func NewBoundedQueue[T any](vcenterID string, capacity int, metrics *telemetry.Metrics) *BoundedQueue[T] {
	if capacity <= 0 {
		capacity = 1000 // production-safe fallback default
	}

	q := &BoundedQueue[T]{
		vcenterID: vcenterID,
		capacity:  capacity,
		metrics:   metrics,
		ch:        make(chan T, capacity),
		closed:    make(chan struct{}),
	}
	q.updateMetrics()
	return q
}

// Push appends an item to the queue. If the queue is at capacity, it BLOCKS until
// space becomes available, context is canceled, or the queue is closed.
func (q *BoundedQueue[T]) Push(ctx context.Context, item T) (err error) {
	if q.isClosed.Load() {
		return ErrQueueClosed
	}

	defer func() {
		if r := recover(); r != nil {
			err = ErrQueueClosed
		}
	}()

	// Fast path: try non-blocking send unless closed
	select {
	case <-q.closed:
		return ErrQueueClosed
	case <-ctx.Done():
		return ctx.Err()
	case q.ch <- item:
		q.updateMetrics()
		return nil
	default:
	}

	// Slow path: queue full, producer blocks
	q.blockedProducers.Add(1)
	q.updateMetrics()
	defer func() {
		q.blockedProducers.Add(-1)
		q.updateMetrics()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-q.closed:
		return ErrQueueClosed
	case q.ch <- item:
		return nil
	}
}

// PushAll pushes a slice of items sequentially, blocking when full.
func (q *BoundedQueue[T]) PushAll(ctx context.Context, items []T) error {
	for i := range items {
		if err := q.Push(ctx, items[i]); err != nil {
			return err
		}
	}
	return nil
}

// Pop extracts a single item from the queue, blocking until an item is available.
func (q *BoundedQueue[T]) Pop(ctx context.Context) (T, error) {
	select {
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	case <-q.closed:
		// Drain remaining items from buffer before reporting closed
		select {
		case item := <-q.ch:
			q.updateMetrics()
			return item, nil
		default:
			var zero T
			return zero, ErrQueueClosed
		}
	case item := <-q.ch:
		q.updateMetrics()
		return item, nil
	}
}

// PopBatch extracts up to maxBatch items. It blocks until at least 1 item is available,
// then drains up to maxBatch items within the specified timeout.
func (q *BoundedQueue[T]) PopBatch(ctx context.Context, maxBatch int, timeout time.Duration) ([]T, error) {
	if maxBatch <= 0 {
		maxBatch = 100
	}

	first, err := q.Pop(ctx)
	if err != nil {
		return nil, err
	}

	batch := make([]T, 0, maxBatch)
	batch = append(batch, first)

	if len(batch) >= maxBatch {
		return batch, nil
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for len(batch) < maxBatch {
		select {
		case <-ctx.Done():
			q.updateMetrics()
			return batch, nil
		case <-timer.C:
			q.updateMetrics()
			return batch, nil
		case <-q.closed:
			for len(batch) < maxBatch {
				select {
				case item := <-q.ch:
					batch = append(batch, item)
				default:
					q.updateMetrics()
					return batch, nil
				}
			}
			q.updateMetrics()
			return batch, nil
		case item := <-q.ch:
			batch = append(batch, item)
		}
	}

	q.updateMetrics()
	return batch, nil
}

// Depth returns the current number of items buffered in the queue.
func (q *BoundedQueue[T]) Depth() int {
	return len(q.ch)
}

// Capacity returns the maximum capacity of the queue.
func (q *BoundedQueue[T]) Capacity() int {
	return q.capacity
}

// FillRatio returns the current fill ratio (depth / capacity).
func (q *BoundedQueue[T]) FillRatio() float64 {
	if q.capacity == 0 {
		return 0
	}
	return float64(len(q.ch)) / float64(q.capacity)
}

// BlockedProducers returns the number of producer goroutines currently blocked.
func (q *BoundedQueue[T]) BlockedProducers() int {
	return int(q.blockedProducers.Load())
}

// Close closes the queue and prevents new pushes.
func (q *BoundedQueue[T]) Close() {
	q.closeOnce.Do(func() {
		q.isClosed.Store(true)
		close(q.closed)
		q.updateMetrics()
	})
}

// IsClosed returns whether the queue has been closed.
func (q *BoundedQueue[T]) IsClosed() bool {
	return q.isClosed.Load()
}

func (q *BoundedQueue[T]) updateMetrics() {
	if q.metrics == nil {
		return
	}
	depth := float64(len(q.ch))
	capVal := float64(q.capacity)
	var fillRatio float64
	if capVal > 0 {
		fillRatio = depth / capVal
	}
	blocked := float64(q.blockedProducers.Load())

	q.metrics.QueueDepth.WithLabelValues(q.vcenterID).Set(depth)
	q.metrics.QueueCapacity.WithLabelValues(q.vcenterID).Set(capVal)
	q.metrics.QueueFillRatio.WithLabelValues(q.vcenterID).Set(fillRatio)
	q.metrics.QueueBlockedProducers.WithLabelValues(q.vcenterID).Set(blocked)
}
