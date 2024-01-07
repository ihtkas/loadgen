package loadgen

import "github.com/prometheus/client_golang/prometheus"

type StmtOption func(stmt *Stmt)

func WithHistogramOpt(h prometheus.Histogram) StmtOption {
	return func(s *Stmt) {
		s.histogram = h
	}
}

type stmtErrGaugeOpt struct {
	errGauge prometheus.Gauge
}

func WithErrGaugeOpt(g prometheus.Gauge) StmtOption {
	return func(s *Stmt) {
		s.errGauge = g
	}
}

type FunctionOption func(function *Function)

func WithExecLimitOpt(limit int) FunctionOption {
	return func(f *Function) {
		f.execLimit = limit
	}
}

// Finally block statement will be executed at the end if function even if any of the statement failed to execute.
func WithFinallyBlockOpt(stmt *Stmt) FunctionOption {
	return func(f *Function) {
		f.finallyBlock = stmt
	}
}

type LoadOption func(loadGen *LoadGen)

func WithCustomHistogram(buckets []float64) LoadOption {
	return func(loadGen *LoadGen) {
		loadGen.funcHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: loadGen.loadLabel,
			Name:      "function",
			Help:      "Histogram to track the duration of functions",
			Buckets:   buckets,
		}, []string{"function", "status_code"})
	}
}

func WithCustomStatusCodeMapper(fn func(err error) string, possibleCodes []string) LoadOption {
	return func(loadGen *LoadGen) {
		loadGen.statusCodeMapper = fn
		loadGen.statusCodes = possibleCodes
	}
}
