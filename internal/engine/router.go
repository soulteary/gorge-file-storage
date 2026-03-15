package engine

import (
	"fmt"
	"sort"
)

type Router struct {
	engines []StorageEngine
	byID    map[string]StorageEngine
}

func NewRouter(engines []StorageEngine) *Router {
	sorted := make([]StorageEngine, len(engines))
	copy(sorted, engines)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority() < sorted[j].Priority()
	})

	byID := make(map[string]StorageEngine, len(engines))
	for _, e := range engines {
		byID[e.Identifier()] = e
	}

	return &Router{engines: sorted, byID: byID}
}

func (r *Router) SelectForWrite(size int64) (StorageEngine, error) {
	for _, eng := range r.engines {
		if !eng.CanWrite() {
			continue
		}
		if eng.HasSizeLimit() && size > eng.MaxFileSize() {
			continue
		}
		return eng, nil
	}
	return nil, fmt.Errorf("no writable engine available for file of size %d", size)
}

func (r *Router) GetEngine(identifier string) (StorageEngine, error) {
	eng, ok := r.byID[identifier]
	if !ok {
		return nil, fmt.Errorf("unknown storage engine: %q", identifier)
	}
	return eng, nil
}

func (r *Router) ListEngines() []EngineStatus {
	result := make([]EngineStatus, 0, len(r.engines))
	for _, eng := range r.engines {
		result = append(result, EngineStatus{
			Identifier: eng.Identifier(),
			Priority:   eng.Priority(),
			CanWrite:   eng.CanWrite(),
			SizeLimit:  eng.MaxFileSize(),
		})
	}
	return result
}

type EngineStatus struct {
	Identifier string `json:"identifier"`
	Priority   int    `json:"priority"`
	CanWrite   bool   `json:"canWrite"`
	SizeLimit  int64  `json:"sizeLimit"`
}
