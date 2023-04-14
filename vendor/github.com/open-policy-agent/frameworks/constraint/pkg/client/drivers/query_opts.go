package drivers

type QueryCfg struct {
	TracingEnabled bool
	StatsEnabled   bool
}

// QueryOpt specifies optional arguments for Query driver calls.
type QueryOpt func(*QueryCfg)

// Tracing enables Rego tracing for a single query.
// If tracing is enabled for the Driver, Tracing(false) does not disable Tracing.
func Tracing(enabled bool) QueryOpt {
	return func(cfg *QueryCfg) {
		cfg.TracingEnabled = enabled
	}
}

// Stats(true) enables the driver to return evaluation stats for a single
// query. If stats is enabled for the Driver at construction time, then
// Stats(false) does not disable Stats for this single query.
func Stats(enabled bool) QueryOpt {
	return func(cfg *QueryCfg) {
		cfg.StatsEnabled = enabled
	}
}
