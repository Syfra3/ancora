package embedding

import "github.com/Syfra3/ancora/internal/store"

// StoreAdapter wraps *store.Store to satisfy the embedding.Store interface.
// This is the only place in the embedding package that imports the store package,
// keeping the dependency boundary clean and the service itself testable with mocks.
type StoreAdapter struct {
	s *store.Store
}

// NewStoreAdapter wraps a *store.Store for use with New().
func NewStoreAdapter(s *store.Store) *StoreAdapter {
	return &StoreAdapter{s: s}
}

// SetEmbedding delegates to store.Store.SetEmbedding.
func (a *StoreAdapter) SetEmbedding(observationID int64, vec []float32) error {
	return a.s.SetEmbedding(observationID, vec)
}

// ListObservationsForEmbedding delegates to store.Store.ListObservationsForEmbedding
// and maps the store.Observation slice to the embedding.Observation slice.
func (a *StoreAdapter) ListObservationsForEmbedding() ([]Observation, error) {
	storeObs, err := a.s.ListObservationsForEmbedding()
	if err != nil {
		return nil, err
	}
	result := make([]Observation, len(storeObs))
	for i, o := range storeObs {
		result[i] = Observation{
			ID:      o.ID,
			Title:   o.Title,
			Content: o.Content,
		}
	}
	return result, nil
}
