package streamer

import "sync"

const chanBufferSize = 64

// Broadcaster fans out log lines to all subscribers of a given run ID.
// It is safe for concurrent use.
type Broadcaster struct {
	mu   sync.RWMutex
	subs map[string][]chan string
}

// NewBroadcaster returns an initialised, ready-to-use Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subs: make(map[string][]chan string),
	}
}

// Subscribe registers a new subscriber for runID. It returns a receive-only
// channel that will receive published lines and an unsub function that the
// caller must invoke when it is done consuming (e.g. on HTTP disconnect).
// The returned channel is buffered (size 64).
func (b *Broadcaster) Subscribe(runID string) (ch <-chan string, unsub func()) {
	buf := make(chan string, chanBufferSize)

	b.mu.Lock()
	b.subs[runID] = append(b.subs[runID], buf)
	b.mu.Unlock()

	unsub = func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		list := b.subs[runID]
		for i, c := range list {
			if c == buf {
				// Remove this channel from the slice.
				b.subs[runID] = append(list[:i], list[i+1:]...)
				close(buf)
				break
			}
		}
		// Clean up the map entry if no subscribers remain.
		if len(b.subs[runID]) == 0 {
			delete(b.subs, runID)
		}
	}

	return buf, unsub
}

// Publish sends line to every subscriber of runID. The send is non-blocking:
// if a subscriber's buffer is full the line is dropped for that subscriber
// (slow-consumer protection).
//
// The whole publish runs under the read lock. This is what makes it safe:
// unsub() and Close() take the write lock before they close a channel, so
// they cannot run while a Publish holds the read lock — eliminating the
// send-on-closed-channel panic. The send stays non-blocking (the `default`
// branch), so holding the read lock never blocks on a slow consumer.
func (b *Broadcaster) Publish(runID, line string) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subs[runID] {
		select {
		case ch <- line:
		default:
			// Drop: slow consumer.
		}
	}
}

// Close closes all subscriber channels for runID and removes its entry from
// the internal map. After Close returns, new Publish calls for runID are
// no-ops until a new subscriber registers.
func (b *Broadcaster) Close(runID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, ch := range b.subs[runID] {
		close(ch)
	}
	delete(b.subs, runID)
}

// IsActive reports whether runID has at least one active subscriber.
func (b *Broadcaster) IsActive(runID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs[runID]) > 0
}
