package watcher

import "sync"

// RunIndex is a thread-safe in-memory map from Kubernetes Job name to run ID.
// It eliminates the O(N) DB scan in PodHandler.findRunID by keeping a live
// index of all currently-running jobs.
type RunIndex struct {
	mu    sync.RWMutex
	index map[string]string // jobName → runID
}

// NewRunIndex returns a ready-to-use RunIndex.
func NewRunIndex() *RunIndex {
	return &RunIndex{index: make(map[string]string)}
}

// Set registers a mapping from jobName to runID.
func (r *RunIndex) Set(jobName, runID string) {
	r.mu.Lock()
	r.index[jobName] = runID
	r.mu.Unlock()
}

// Get returns the runID for jobName, or "" if not found.
func (r *RunIndex) Get(jobName string) string {
	r.mu.RLock()
	v := r.index[jobName]
	r.mu.RUnlock()
	return v
}

// Delete removes the entry for jobName.
func (r *RunIndex) Delete(jobName string) {
	r.mu.Lock()
	delete(r.index, jobName)
	r.mu.Unlock()
}
