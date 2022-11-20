package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ihtkas/loadgen"
)

func main() {

	histOpt1 := loadgen.WithHistogramOpt(loadgen.DefaultHistogram("foo1", 0.1, 0.1, 100))
	histOpt2 := loadgen.WithHistogramOpt(loadgen.DefaultHistogram("foo2", 0.1, 0.1, 100))
	histOpt3 := loadgen.WithHistogramOpt(loadgen.DefaultHistogram("bar", 0.1, 0.1, 100))
	histOpt4 := loadgen.WithHistogramOpt(loadgen.DefaultHistogram("foobar", 0.1, 0.1, 100))
	errOpt1 := loadgen.WithErrGaugeOpt(loadgen.DefaultErrGauge("foo1_err"))
	errOpt2 := loadgen.WithErrGaugeOpt(loadgen.DefaultErrGauge("foo2_err"))
	errOpt3 := loadgen.WithErrGaugeOpt(loadgen.DefaultErrGauge("bar_err"))
	errOpt4 := loadgen.WithErrGaugeOpt(loadgen.DefaultErrGauge("foobar_err"))

	opt5 := loadgen.WithExecLimitOpt(60)
	opt6 := loadgen.WithExecLimitOpt(30)
	opt7 := loadgen.WithExecLimitOpt(10)
	fn1 := loadgen.NewFunction(
		[]*loadgen.Stmt{
			loadgen.NewStmt(foo1, 1, time.Microsecond, time.Microsecond, histOpt1, errOpt1),
			loadgen.NewStmt(foo2, 1, time.Second, time.Second, histOpt2, errOpt2),
		},
		6,
		opt5,
	)
	fn2 := loadgen.NewFunction(
		[]*loadgen.Stmt{loadgen.NewStmt(bar, 1, time.Second, time.Second, histOpt3, errOpt3)},
		3, opt6,
	)
	fn3 := loadgen.NewFunction(
		[]*loadgen.Stmt{loadgen.NewStmt(foobar, 1, time.Second, time.Second, histOpt4, errOpt4)},
		1, opt7,
	)
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	lg := loadgen.GenerateLoad(ctx, cancel, "load_test", []*loadgen.Function{fn1, fn2, fn3}, 6, 15, 5*time.Second, 2)
	fmt.Println("error:", lg.StartSever(":2112"))
}

func foo1(ctx context.Context, iter uint, payload map[string]interface{}) (contRepeat bool, err error) {
	log.Println("foo1...")
	time.Sleep(1 * time.Second)
	return true, nil
}

func foo2(ctx context.Context, iter uint, payload map[string]interface{}) (contRepeat bool, err error) {
	log.Println("foo2...")
	time.Sleep(1 * time.Second)
	return true, nil
}

func bar(ctx context.Context, iter uint, payload map[string]interface{}) (contRepeat bool, err error) {
	log.Println("bar...")
	time.Sleep(2 * time.Second)
	return true, nil
}

func foobar(ctx context.Context, iter uint, payload map[string]interface{}) (contRepeat bool, err error) {
	log.Println("foobar...")
	time.Sleep(2 * time.Second)
	return true, nil
}
