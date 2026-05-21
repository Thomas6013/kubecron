package streamer

import (
	"sync"
	"testing"
)

// TestBroadcaster_PublishSubscribe verifies that published lines reach all subscribers.
func TestBroadcaster_PublishSubscribe(t *testing.T) {
	b := NewBroadcaster()

	ch1, unsub1 := b.Subscribe("run-1")
	ch2, unsub2 := b.Subscribe("run-1")
	defer unsub1()
	defer unsub2()

	b.Publish("run-1", "hello")

	got1 := <-ch1
	got2 := <-ch2
	if got1 != "hello" || got2 != "hello" {
		t.Fatalf("unexpected: %q %q", got1, got2)
	}
}

// TestBroadcaster_UnsubRemovesChannel verifies that after unsub the channel is closed
// and no further publishes panic or block.
func TestBroadcaster_UnsubRemovesChannel(t *testing.T) {
	b := NewBroadcaster()
	ch, unsub := b.Subscribe("run-2")
	unsub()

	// Channel must be closed after unsub.
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel after unsub")
		}
	default:
		t.Fatal("channel not closed after unsub")
	}

	// Publish after unsub must not panic.
	b.Publish("run-2", "should not arrive")
}

// TestBroadcaster_Close closes all channels for a run and removes it from the map.
func TestBroadcaster_Close(t *testing.T) {
	b := NewBroadcaster()
	ch, _ := b.Subscribe("run-3")
	b.Close("run-3")

	if _, ok := <-ch; ok {
		t.Fatal("expected channel closed after Close()")
	}
	if b.IsActive("run-3") {
		t.Fatal("expected no active subscribers after Close()")
	}
}

// TestBroadcaster_ConcurrentPublishSubscribe exercises concurrent access under
// the race detector (run with go test -race).
func TestBroadcaster_ConcurrentPublishSubscribe(t *testing.T) {
	b := NewBroadcaster()
	const runID = "run-race"
	const publishers = 4
	const subscribers = 4
	const msgs = 20

	var wg sync.WaitGroup

	// Start subscribers.
	for range subscribers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch, unsub := b.Subscribe(runID)
			defer unsub()
			// Drain whatever arrives; do not block.
			for range msgs {
				select {
				case <-ch:
				default:
				}
			}
		}()
	}

	// Start publishers.
	for range publishers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range msgs {
				b.Publish(runID, "line")
			}
		}()
	}

	wg.Wait()
}
