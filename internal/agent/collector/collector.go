package collector

import "context"

// Collector defines the interface for all metric collectors.
// Each collector is responsible for gathering a specific category of metrics
// (CPU, memory, disk, network, docker, etc.).
type Collector interface {
	// Name returns the unique identifier for this collector (e.g. "cpu", "memory").
	Name() string

	// Collect gathers metrics and returns them as a map of key-value pairs.
	// The context allows cancellation of long-running collection operations.
	Collect(ctx context.Context) (map[string]any, error)
}

// Registry holds a set of registered collectors and provides thread-safe
// iteration over them.
type Registry struct {
	collectors []Collector
}

// NewRegistry creates a new empty collector registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a collector to the registry.
// This should be called during initialization before the collection loop starts.
func (r *Registry) Register(c Collector) {
	r.collectors = append(r.collectors, c)
}

// All returns all registered collectors for iteration.
func (r *Registry) All() []Collector {
	return r.collectors
}

// Count returns the number of registered collectors.
func (r *Registry) Count() int {
	return len(r.collectors)
}
