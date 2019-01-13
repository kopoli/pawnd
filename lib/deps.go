package pawnd

// Contains dependencies that can be overridden during tests

import (
	"fmt"
	"io"
	"os"
	"os/signal"

	colorable "github.com/mattn/go-colorable"
)

var FailSafeExit = func() {
	fmt.Fprintf(os.Stderr, "Error: Failsafe exit triggered\n")
	os.Exit(2)
}

var NewTerminalStdout = func() io.Writer {
	return colorable.NewColorableStdout()
}

var SignalNotify = func(c chan<- os.Signal, sig ...os.Signal) {
	signal.Notify(c, sig...)
}
