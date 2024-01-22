package metrics

import (
	"context"

	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics/exporters/common"
	"github.com/open-policy-agent/gatekeeper/v3/pkg/metrics/registry"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var log = logf.Log.WithName("metrics")

var _ manager.Runnable = &runner{}

type runner struct {
	mgr manager.Manager
}

func AddToManager(m manager.Manager) error {
	mr := create(m)
	return m.Add(mr)
}

func create(mgr manager.Manager) *runner {
	mr := &runner{
		mgr: mgr,
	}
	return mr
}

// Start implements the Runnable interface.
func (r *runner) Start(ctx context.Context) error {
	log.Info("Starting metrics runner")
	defer log.Info("Stopping metrics runner workers")
	errCh := make(chan error)
	exporters := registry.Exporters()
	common.SetRequiredReaders(len(exporters))
	for i := range exporters {
		startExporter := exporters[i]
		go func() {
			if err := startExporter(ctx); err != nil {
				errCh <- err
			}
		}()
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		if err != nil {
			return err
		}
	}
	return nil
}
