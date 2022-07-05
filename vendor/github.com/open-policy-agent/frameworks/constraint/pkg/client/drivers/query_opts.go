package drivers

type QueryCfg struct {
	TracingEnabled bool
}

// QueryOpt specifies optional arguments for Rego queries.
type QueryOpt func(*QueryCfg)

// Tracing enables Rego tracing for a single query.
// If tracing is enabled for the Driver, Tracing(false) does not disable Tracing.
func Tracing(enabled bool) QueryOpt {
	return func(cfg *QueryCfg) {
		cfg.TracingEnabled = enabled
	}
}
