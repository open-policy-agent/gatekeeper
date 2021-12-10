package client

type queryCfg struct {
	enableTracing bool
}

type QueryOpt func(*queryCfg)

func Tracing(enabled bool) QueryOpt {
	return func(cfg *queryCfg) {
		cfg.enableTracing = enabled
	}
}
