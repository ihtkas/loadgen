package loadgen

import "github.com/prometheus/client_golang/prometheus"

type StmtOption interface {
	enable(stmt *Stmt)
}

type stmtHistogramOpt struct {
	historgram prometheus.Histogram
}

func (o *stmtHistogramOpt) enable(s *Stmt) {
	s.histogram = o.historgram
}

func WithHistogramOpt(h prometheus.Histogram) StmtOption {
	return &stmtHistogramOpt{historgram: h}
}

type stmtErrGaugeOpt struct {
	errGauge prometheus.Gauge
}

func (o *stmtErrGaugeOpt) enable(s *Stmt) {
	s.errGauge = o.errGauge
}

func WithErrGaugeOpt(g prometheus.Gauge) StmtOption {
	return &stmtErrGaugeOpt{errGauge: g}
}

type FunctionOption interface {
	enable(f *Function)
}

type execLimitOpt struct {
	limit int
}

func (o *execLimitOpt) enable(f *Function) {
	f.execLimit = o.limit
}

func WithExecLimitOpt(limit int) FunctionOption {
	return &execLimitOpt{limit: limit}
}

type finallyBlockOpt struct {
	stmt *Stmt
}

func (o *finallyBlockOpt) enable(f *Function) {
	f.finallyBlock = o.stmt
}

// Finally block statement will be executed at the end if function even if any of the statement failed to execute.
func WithFinallyBlockOpt(stmt *Stmt) FunctionOption {
	return &finallyBlockOpt{stmt: stmt}
}
