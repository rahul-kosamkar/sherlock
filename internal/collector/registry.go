package collector

import (
	"context"
	"sync"

	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"github.com/rahulkosamkar/sherlock/internal/metrics"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type Registry struct {
	mu         sync.RWMutex
	collectors []contracts.Collector
	log        *zap.Logger
}

func NewRegistry(log *zap.Logger) *Registry {
	return &Registry{
		log: log,
	}
}

func (r *Registry) Register(c contracts.Collector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.collectors = append(r.collectors, c)
	r.log.Info("collector registered", zap.String("name", c.Name()))
}

func (r *Registry) CollectAll(ctx context.Context, req contracts.CollectRequest) ([]contracts.Evidence, error) {
	r.mu.RLock()
	snapshot := make([]contracts.Collector, len(r.collectors))
	copy(snapshot, r.collectors)
	r.mu.RUnlock()

	var (
		mu       sync.Mutex
		combined []contracts.Evidence
	)

	g, gctx := errgroup.WithContext(ctx)

	for _, c := range snapshot {
		g.Go(func() error {
			ev, err := c.Collect(gctx, req)
			if err != nil {
				r.log.Warn("collector failed, continuing with partial results",
					zap.String("collector", c.Name()),
					zap.Error(err),
				)
				return nil
			}
			mu.Lock()
			combined = append(combined, ev...)
			mu.Unlock()
			metrics.EvidenceCollected.WithLabelValues(c.Name()).Add(float64(len(ev)))
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return combined, err
	}
	return combined, nil
}
