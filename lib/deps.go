package pawnd

// Contains dependencies that can be overridden during tests

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/pprof"

	colorable "github.com/mattn/go-colorable"
)

func ErrAnnotate(err error, a ...interface{}) error {
	s := fmt.Sprint(a...)
	return fmt.Errorf("%s: %w", s, err)
}

func PrintGoroutines() {
	_ = pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
}

type Deps struct {
	FailSafeExit      func()
	NewTerminalStdout func() io.Writer
	SignalNotify      func(c chan<- os.Signal, sig ...os.Signal)
	SignalReset       func(sig ...os.Signal)
	SupportedSignal   func(name string) (os.Signal, error)
}

var deps = Deps{
	FailSafeExit: func() {
		fmt.Fprintf(os.Stderr, "Error: Failsafe exit triggered\n")
		os.Exit(2)
	},
	NewTerminalStdout: func() io.Writer {
		return colorable.NewColorableStdout()
	},
	SignalNotify: func(c chan<- os.Signal, sig ...os.Signal) {
		signal.Notify(c, sig...)
	},
	SignalReset: func(sig ...os.Signal) {
		signal.Reset(sig...)
	},
	SupportedSignal: func(name string) (os.Signal, error)  {
		_, ok := SupportedSignals[name]
		if !ok {
			return nil, fmt.Errorf("signal name \"%s\" is not supported", name)
		}
		return SupportedSignals[name], nil
	},
}

var defaultDeps = deps
