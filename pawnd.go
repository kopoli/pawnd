package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/kopoli/go-util"
	"github.com/kopoli/pawnd/lib"
)

var (
	majorVersion     = "0"
	version          = "Undefined"
	timestamp        = "Undefined"
	progVersion      = majorVersion + "-" + version
	exitValue    int = 0
)

func printErr(err error, message string, arg ...string) {
	msg := ""
	if err != nil {
		msg = fmt.Sprintf(" (error: %s)", err)
	}
	fmt.Fprintf(os.Stderr, "Error: %s%s.%s\n", message, strings.Join(arg, " "), msg)
}

func fault(err error, message string, arg ...string) {
	printErr(err, message, arg...)

	// Exit goroutine and run all deferrals
	exitValue = 1
	runtime.Goexit()
}

func main() {
	opts := util.NewOptions()

	opts.Set("program-name", os.Args[0])
	opts.Set("program-version", progVersion)
	opts.Set("program-timestamp", timestamp)

	opts.Set("configuration-file", "pawnd.conf")

	// In the last deferred function, exit the program with given code
	defer func() {
		os.Exit(exitValue)
	}()

	_, err := pawnd.Cli(opts, os.Args)
	if err != nil {
		fault(err, "command line parsing failed")
	}

	if opts.IsSet("demo-mode") {
		pawnd.UiDemo(opts)
		exitValue = 25
		return
	}

	err = pawnd.TestRun(opts)
	if err != nil {
		fault(err, "Running chains failed")
	}
}
