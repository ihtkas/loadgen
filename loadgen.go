package loadgen

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const MetricsPathStr = "/metrics"
const WorkersPathStr = "/workers"
const RampUpDurationPathStr = "/ramp_up_dur"
const RampUpFactorPathStr = "/ramp_up_factor"

// Function is a block of multiple statements with a weight associated to it.
// Loadgen randomly generates load based on the weight.
// Function object passed to one load gen shouldn't be used in another load generation concurrently
type Function struct {
	stmts        []*Stmt
	weight       uint16
	execLimit    int
	currExecs    int
	finallyBlock *Stmt
}

func NewFunction(stmts []*Stmt, weight uint16, opts ...FunctionOption) *Function {
	f := &Function{
		stmts:     stmts,
		weight:    weight,
		execLimit: -1,
	}
	for _, opt := range opts {
		opt.enable(f)
	}
	return f
}

type Evaluator func(ctx context.Context, iter uint, payload map[string]interface{}) (contRepeat bool, err error)

type Stmt struct {
	eval                   Evaluator
	repeat                 uint
	waitInRepeatRangeStart time.Duration
	waitInRepeatRangeEnd   time.Duration
	histogram              prometheus.Histogram
	errGauge               prometheus.Gauge
}

func (s *Stmt) Eval() Evaluator                  { return s.eval }
func (s *Stmt) Repeat() uint                     { return s.repeat }
func (s *Stmt) WaitInRepeatStart() time.Duration { return s.waitInRepeatRangeStart }
func (s *Stmt) WaitInRepeatEnd() time.Duration   { return s.waitInRepeatRangeEnd }
func (s *Stmt) Histogram() prometheus.Histogram  { return s.histogram }
func (s *Stmt) ErrGauge() prometheus.Gauge       { return s.errGauge }

func NewStmt(eval Evaluator, repeat uint,
	waitInRepeatRangeStart time.Duration,
	waitInRepeatRangeEnd time.Duration, opts ...StmtOption) *Stmt {
	stmt := &Stmt{
		eval:                   eval,
		repeat:                 repeat,
		waitInRepeatRangeStart: waitInRepeatRangeStart,
		waitInRepeatRangeEnd:   waitInRepeatRangeEnd,
	}
	for _, opt := range opts {
		opt.enable(stmt)
	}
	return stmt
}

func DefaultHistogram(histogramLabel string, startDuration, durationGap float64, count int) prometheus.Histogram {
	bucket := prometheus.LinearBuckets(startDuration, durationGap, count)
	return prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    histogramLabel,
		Buckets: bucket,
	})
}

func DefaultErrGauge(name string) prometheus.Gauge {
	return prometheus.NewGauge(prometheus.GaugeOpts{
		Name: name,
	})
}

type LoadGen struct {
	sync.RWMutex
	loadLabel    string
	funcs        []*Function
	prefixSum    []int64 // computed from funcs with their weights
	rand         *rand.Rand
	rampUpDur    time.Duration
	rampUpFactor float64
	// workersCancelFns will be used to scale down workers
	workersCancelFns []context.CancelFunc
	workersLimit     int
	workersProgress  int64
	workersGuage     prometheus.Gauge
	setWorkersCh     chan int
	getWorkersCh     chan chan int
	server           *http.Server
	cancel           context.CancelFunc
}

func GenerateLoad(ctx context.Context, cancel context.CancelFunc, loadLabel string, funcs []*Function, initialWorkers, workersLimit int, rampUpDur time.Duration, rampUpFactor float64) *LoadGen {
	lg := &LoadGen{
		loadLabel:    loadLabel,
		funcs:        funcs,
		prefixSum:    formPrefixSumArr(funcs),
		rand:         rand.New(rand.NewSource(time.Now().UnixNano())),
		rampUpDur:    rampUpDur,
		rampUpFactor: rampUpFactor,
		setWorkersCh: make(chan int),
		getWorkersCh: make(chan chan int),
		cancel:       cancel,
		workersLimit: workersLimit,
	}
	lg.registerMetrics()
	ch := make(chan *Function, 1)
	go lg.manageWorkers(ctx, initialWorkers, ch)
	go lg.producer(ctx, cancel, ch)
	return lg
}

func (lg *LoadGen) setWorkers(workers int) {
	lg.setWorkersCh <- workers
}
func (lg *LoadGen) getWorkersCount() int {
	ch := make(chan int)
	lg.getWorkersCh <- ch
	return <-ch
}
func (lg *LoadGen) setRampUpDuration(dur time.Duration) {
	atomic.StoreUint64((*uint64)(unsafe.Pointer(&lg.rampUpDur)), uint64(dur))
}
func (lg *LoadGen) getRampUpDuration() time.Duration {
	return time.Duration(atomic.LoadUint64((*uint64)(unsafe.Pointer(&lg.rampUpDur))))
}
func (lg *LoadGen) setRampUpFactor(factor float64) { storeFloat64(&lg.rampUpFactor, factor) }
func (lg *LoadGen) getRampUpFactor() float64       { return loadFloat64(&lg.rampUpFactor) }

func (lg *LoadGen) registerMetrics() {
	g := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: lg.loadLabel + "_workers_count",
	})
	prometheus.MustRegister(g)

	lg.workersGuage = g

	for _, fn := range lg.funcs {
		for _, stmt := range fn.stmts {
			if stmt.histogram != nil {
				prometheus.MustRegister(stmt.Histogram())
			}
			if stmt.errGauge != nil {
				prometheus.MustRegister(stmt.errGauge)
			}
		}
	}
}

func (lg *LoadGen) UnregisterMetrics() {
	prometheus.Unregister(lg.workersGuage)
	for _, fn := range lg.funcs {
		for _, stmt := range fn.stmts {
			if stmt.histogram != nil {
				prometheus.Unregister(stmt.Histogram())
			}
			if stmt.errGauge != nil {
				prometheus.Unregister(stmt.errGauge)
			}
		}
	}
}

// StartMetricsSever will register promethues metric handler and starts a http server.
// Use this if there is no other http server running or can't handle this handler path.
// LoadGen also implements http.Handler interface. If there is any existing server,
func (lg *LoadGen) StartSever(addr string) error {
	mux := http.NewServeMux()
	mux.Handle(MetricsPathStr, promhttp.Handler())
	mux.Handle(WorkersPathStr, lg)
	mux.Handle(RampUpDurationPathStr, lg)
	mux.Handle(RampUpFactorPathStr, lg)
	server := &http.Server{Handler: mux, Addr: addr}
	lg.server = server
	return server.ListenAndServe()
}

func (lg *LoadGen) StopServer() error {
	lg.UnregisterMetrics()
	return lg.server.Close()
}

func (lg *LoadGen) producer(ctx context.Context, cancel context.CancelFunc, ch chan<- *Function) {
	for {
		select {
		case <-ctx.Done():
			return
		case ch <- lg.randomPick(cancel):
		}
	}
}

func (lg *LoadGen) randomPick(cancel context.CancelFunc) *Function {

	f, exceeded := lg.randomPickHelper()
	if f == nil {
		// complete the execution
		return nil
	}
	if exceeded {
		lg.removeFunc(f)
		return lg.randomPick(cancel)
	}
	return f

}

func (lg *LoadGen) randomPickHelper() (f *Function, exceeded bool) {
	lg.RLock()
	defer lg.RUnlock()
	if len(lg.prefixSum) == 0 {
		return nil, false
	}
	max := lg.prefixSum[len(lg.prefixSum)-1]
	sum := lg.rand.Int63n(int64(max + 1))
	ind := sort.Search(len(lg.prefixSum), func(i int) bool { return lg.prefixSum[i] >= sum })
	f = lg.funcs[ind]
	f.currExecs++
	if f.execLimit == -1 {
		return f, false
	}
	return f, f.currExecs > f.execLimit
}

func (lg *LoadGen) manageWorkers(ctx context.Context, initialWorkers int, in <-chan *Function) {
	lg.spawnWorkers(ctx, initialWorkers, in)
	for {
		tick := time.NewTicker(lg.getRampUpDuration())
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			workersCount := len(lg.workersCancelFns)
			// find new workers count based on rampUpFactor
			newWorkers := int(float64(workersCount)*lg.getRampUpFactor()) - workersCount
			if newWorkers < 0 {
				lg.scaleDownWorkers(-newWorkers)
			} else {
				lg.spawnWorkers(ctx, newWorkers, in)
			}
			// fmt.Println("workers count....", len(lg.workersCancelFns))
		case count := <-lg.setWorkersCh:
			workersCount := len(lg.workersCancelFns)
			// find new workers count
			newWorkers := count - workersCount
			if newWorkers < 0 {
				lg.scaleDownWorkers(-newWorkers)
			} else {
				lg.spawnWorkers(ctx, newWorkers, in)
			}
			// log.Println("workers count....", len(lg.workersCancelFns), newWorkers)
		case ch := <-lg.getWorkersCh:
			ch <- len(lg.workersCancelFns)
		}

	}
}
func (lg *LoadGen) scaleDownWorkers(count int) {
	workersCount := len(lg.workersCancelFns)
	ind := workersCount - count
	// log.Println("===========", workersCount, count, ind)
	if ind < 0 {
		ind = 0
	}

	for i := ind; i < workersCount; i++ {
		lg.workersCancelFns[i]()
		// assigning nil to free object(aid GC) associated to cancel function if any
		lg.workersCancelFns[i] = nil
	}
	// log.Println("---=-=-sub workers", workersCount - ind)
	lg.workersGuage.Sub(float64(workersCount - ind))
	lg.workersCancelFns = lg.workersCancelFns[:ind]
}

func (lg *LoadGen) checkWorkersProgress() {
	if atomic.LoadInt64(&lg.workersProgress) == 0 {
		lg.cancel()
	}
}

func (lg *LoadGen) spawnWorkers(parentCtx context.Context, n int, in <-chan *Function) {
	currWorkers := len(lg.workersCancelFns)
	if currWorkers+n > lg.workersLimit {
		n = lg.workersLimit - currWorkers
	}
	lg.workersGuage.Add(float64(n))
	for i := 0; i < n; i++ {
		ctx, cancel := context.WithCancel(parentCtx)
		lg.workersCancelFns = append(lg.workersCancelFns, cancel)
		go lg.worker(ctx, in)
	}
}

func (lg *LoadGen) worker(ctx context.Context, in <-chan *Function) {
	// defer func() {
	// 	log.Println("Exiting worker.....")
	// }()
	atomic.AddInt64(&lg.workersProgress, 1)
	for {
		select {
		case <-ctx.Done():
			atomic.AddInt64(&lg.workersProgress, -1)
			return
		case fn := <-in:
			if fn == nil {
				atomic.AddInt64(&lg.workersProgress, -1)
				lg.checkWorkersProgress()
				return
			}
			lg.execFn(ctx, fn)
		}
	}
}

func (lg *LoadGen) execFn(ctx context.Context, fn *Function) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	payload := make(map[string]interface{})
	if fn.finallyBlock != nil {
		defer lg.execStmt(ctx, fn.finallyBlock, payload)
	}
	for _, stmt := range fn.stmts {
		err := lg.execStmt(ctx, stmt, payload)
		if err != nil {
			// discard err. This is just to stop the Function execution
			return
		}

	}
}

func (lg *LoadGen) execStmt(ctx context.Context, stmt *Stmt, payload map[string]interface{}) error {
	for i := uint(0); i < stmt.repeat; i++ {
		st := time.Now()
		contRepeat, err := eval(ctx, i, payload, stmt)
		if stmt.histogram != nil {
			stmt.histogram.Observe(time.Since(st).Seconds())
		}
		if err != nil {
			if stmt.errGauge != nil {
				fmt.Println(err)
				stmt.errGauge.Add(1)
			}
			return err
		}
		if !contRepeat {
			return nil
		}
		wait := stmt.waitInRepeatRangeStart
		wait += time.Duration(rand.Int63n(int64(stmt.waitInRepeatRangeEnd - wait + 1)))
		select {
		case <-time.After(wait):
		case <-ctx.Done():
		}
	}
	return nil
}

func (lg *LoadGen) removeFunc(fn *Function) {
	lg.Lock()
	defer lg.Unlock()
	for i := 0; i < len(lg.funcs); i++ {
		if fn == lg.funcs[i] {
			lg.funcs = append(lg.funcs[:i], lg.funcs[i+1:]...)
			lg.prefixSum = formPrefixSumArr(lg.funcs)
			return
		}
	}
}

func formPrefixSumArr(fns []*Function) []int64 {
	prefixSum := make([]int64, len(fns))
	prevSum := int64(0)
	for i := 0; i < len(fns); i++ {
		prevSum += int64(fns[i].weight)
		prefixSum[i] = prevSum
	}
	return prefixSum
}

func eval(ctx context.Context, iter uint, payload map[string]interface{}, stmt *Stmt) (contRepeat bool, err error) {
	errCh := make(chan error, 1)
	contRepeat, err = evalWithRecover(ctx, iter, payload, stmt, errCh)
	select {
	case err := <-errCh:
		return false, err
	default:
	}
	return contRepeat, err

}

func evalWithRecover(ctx context.Context, iter uint, payload map[string]interface{}, stmt *Stmt, errCh chan<- error) (contRepeat bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			errCh <- errors.New(fmt.Sprint(r))
		}
	}()
	return stmt.eval(ctx, iter, payload)
}

func storeFloat64(val *float64, new float64) {
	atomic.StoreUint64((*uint64)(unsafe.Pointer(val)), math.Float64bits(new))
}

func loadFloat64(val *float64) float64 {
	return math.Float64frombits(atomic.LoadUint64((*uint64)(unsafe.Pointer(val))))
}
