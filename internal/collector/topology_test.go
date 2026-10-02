package collector_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
)

func TestTopologyCacheConcurrentReaders(t *testing.T) {
	cache := collector.NewTopologyCache("vcenter-test-01")

	// Pre-populate with initial snapshot
	initSnap := collector.NewEmptySnapshot("vcenter-test-01")
	initSnap.DatastoreIDByMoRef["datastore-1"] = "ds-uuid-001"
	cache.Swap(initSnap)

	var wg sync.WaitGroup
	stopCh := make(chan struct{})

	// Spawn 10 concurrent reader goroutines
	const numReaders = 10
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for {
				select {
				case <-stopCh:
					return
				default:
					snap := cache.Current()
					if snap == nil {
						t.Errorf("reader %d got nil snapshot", readerID)
						return
					}
					// Read from the immutable maps without locks
					_ = snap.DatastoreIDByMoRef["datastore-1"]
					_ = snap.FolderPathByMoRef["group-v10"]
					_ = len(snap.ByMoRef)
					time.Sleep(10 * time.Microsecond)
				}
			}
		}(i)
	}

	// Writer goroutine swapping snapshots atomically
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for version := 0; version < 50; version++ {
			newSnap := collector.NewEmptySnapshot("vcenter-test-01")
			for j := 0; j < 20; j++ {
				moref := fmt.Sprintf("datastore-%d", j)
				uuid := fmt.Sprintf("ds-uuid-%d-%d", version, j)
				newSnap.DatastoreIDByMoRef[moref] = uuid
				newSnap.ByMoRef[moref] = collector.TopologyNode{
					ID:    uuid,
					MoRef: moref,
					Name:  fmt.Sprintf("ds-name-%d", j),
				}
			}
			cache.Swap(newSnap)
			time.Sleep(2 * time.Millisecond)
		}
	}()

	<-writerDone
	close(stopCh)
	wg.Wait()

	finalSnap := cache.Current()
	if len(finalSnap.DatastoreIDByMoRef) != 20 {
		t.Errorf("expected 20 datastores in final snapshot, got %d", len(finalSnap.DatastoreIDByMoRef))
	}
}
