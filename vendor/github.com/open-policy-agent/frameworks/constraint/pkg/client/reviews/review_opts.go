package reviews

type ReviewCfg struct {
	TracingEnabled   bool
	StatsEnabled     bool
	EnforcementPoint string
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
