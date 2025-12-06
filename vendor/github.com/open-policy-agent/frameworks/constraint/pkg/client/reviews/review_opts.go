package reviews

// ReviewCfg contains configuration options for a single review query.
type ReviewCfg struct {
	TracingEnabled   bool
	StatsEnabled     bool
	EnforcementPoint string
	// Namespace is the namespace object for the resource being reviewed.
	// For namespaced resources, this contains the full namespace object
	// including metadata and labels. For cluster-scoped resources, this is nil.
	Namespace map[string]interface{}
}

// ReviewOpt specifies optional arguments for Query driver calls.
type ReviewOpt func(*ReviewCfg)

// Tracing enables Rego tracing for a single query.
// If tracing is enabled for the Driver, Tracing(false) does not disable Tracing.
func Tracing(enabled bool) ReviewOpt {
	return func(cfg *ReviewCfg) {
		cfg.TracingEnabled = enabled
	}
}

// Stats(true) enables the driver to return evaluation stats for a single
// query. If stats is enabled for the Driver at construction time, then
// Stats(false) does not disable Stats for this single query.
func Stats(enabled bool) ReviewOpt {
	return func(cfg *ReviewCfg) {
		cfg.StatsEnabled = enabled
	}
}

// EnforcementPoint specifies the enforcement point to use for the query.
func EnforcementPoint(ep string) ReviewOpt {
	return func(cfg *ReviewCfg) {
		cfg.EnforcementPoint = ep
	}
}

// Namespace sets the namespace object for the review.
// This makes the namespace available to policy templates.
func Namespace(ns map[string]interface{}) ReviewOpt {
	return func(cfg *ReviewCfg) {
		cfg.Namespace = ns
	}
}
