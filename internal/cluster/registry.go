package cluster

import "sync"

// Registry is a thread-safe store of ClusterClient instances keyed by cluster ID.
type Registry struct {
	mu      sync.RWMutex
	clients map[string]*ClusterClient
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		clients: make(map[string]*ClusterClient),
	}
}

// Register adds or replaces the client for its cluster ID.
func (r *Registry) Register(c *ClusterClient) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[c.ID] = c
}

// Get retrieves the client for the given cluster ID.
func (r *Registry) Get(id string) (*ClusterClient, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clients[id]
	return c, ok
}

// All returns a snapshot slice of all registered clients.
func (r *Registry) All() []*ClusterClient {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*ClusterClient, 0, len(r.clients))
	for _, c := range r.clients {
		out = append(out, c)
	}
	return out
}
