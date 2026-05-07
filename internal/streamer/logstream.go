package streamer

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"regexp"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kubecron/kubecron/internal/storage"
)

var (
	// ansiRE strips terminal colour/formatting escape sequences.
	ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	// k8sTsRE strips the RFC3339Nano timestamp prefix added by Kubernetes
	// when PodLogOptions.Timestamps == true.
	k8sTsRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z `)
)

func cleanLine(s string) string {
	s = k8sTsRE.ReplaceAllString(s, "")
	s = ansiRE.ReplaceAllString(s, "")
	return s
}

const (
	flushInterval = 100 * time.Millisecond
	flushMaxLines = 50
)

// Streamer streams pod logs to the store and broadcasts them to live subscribers.
type Streamer struct {
	store       storage.Store
	broadcaster *Broadcaster
	mu          sync.Mutex
	streaming   map[string]bool // runID → is streaming
}

// NewStreamer creates a Streamer backed by store and broadcaster.
func NewStreamer(store storage.Store, broadcaster *Broadcaster) *Streamer {
	return &Streamer{
		store:       store,
		broadcaster: broadcaster,
		streaming:   make(map[string]bool),
	}
}

// Stream starts asynchronous log streaming for the given pod/run. It is
// idempotent: a second call for the same runID while streaming is active is a
// no-op.
func (s *Streamer) Stream(ctx context.Context, clientset kubernetes.Interface, namespace, podName, runID string) {
	s.mu.Lock()
	if s.streaming[runID] {
		s.mu.Unlock()
		return
	}
	s.streaming[runID] = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.streaming, runID)
			s.mu.Unlock()
			s.broadcaster.Close(runID)
		}()

		s.streamLogs(ctx, clientset, namespace, podName, runID)
	}()
}

// streamLogs performs the actual streaming work inside the goroutine.
func (s *Streamer) streamLogs(ctx context.Context, clientset kubernetes.Interface, namespace, podName, runID string) {
	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Follow:     true,
		Timestamps: true,
	})

	rc, err := req.Stream(ctx)
	if err != nil {
		slog.Warn("failed to open log stream", "pod", podName, "runID", runID, "err", err)
		return
	}
	defer rc.Close()

	// batch holds raw line strings; size tracks total byte count in the batch.
	var (
		batch []string
		size  int64
	)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := s.store.BatchInsertLogLines(ctx, runID, batch); err != nil {
			slog.Warn("failed to batch insert log lines", "runID", runID, "err", err)
		}
		if err := s.store.AddLogSize(ctx, runID, size); err != nil {
			slog.Warn("failed to update log size", "runID", runID, "err", err)
		}
		for _, line := range batch {
			s.broadcaster.Publish(runID, line)
		}
		batch = batch[:0]
		size = 0
	}

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	lineCh := make(chan string, 256)

	// Reader goroutine: scan lines and forward them to the main select loop.
	go func() {
		defer close(lineCh)
		scanner := bufio.NewScanner(rc)
		for scanner.Scan() {
			line := scanner.Text()
			select {
			case lineCh <- line:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil && err != io.EOF {
			slog.Warn("log scanner error", "runID", runID, "err", err)
		}
	}()

	for {
		select {
		case line, ok := <-lineCh:
			if !ok {
				// Stream ended — flush remaining lines.
				flush()
				return
			}
			batch = append(batch, cleanLine(line))
			size += int64(len(line)) + 1 // +1 for the stripped newline
			if len(batch) >= flushMaxLines {
				flush()
			}

		case <-ticker.C:
			flush()

		case <-ctx.Done():
			flush()
			return
		}
	}
}
