package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/kopoli/go-util"
	"github.com/kopoli/pawnd/lib"
)

func printErr(err error, message string, arg ...string) {
	msg := ""
	if err != nil {
		msg = fmt.Sprintf(" (error: %s)", err)
	}
	fmt.Fprintf(os.Stderr, "Error: %s%s.%s\n", message, strings.Join(arg, " "), msg)
}

func checkFault(err error, message string, arg ...string) {
	if err != nil {
		printErr(err, message, arg...)
		os.Exit(1)
	}
}

func main() {
	opts := util.GetOptions()

	opts.Set("configuration-file", "pawnd.conf")

	_, err := pawnd.Cli(opts, os.Args)
	checkFault(err, "command line parsing failed")

	if opts.IsSet("demo-mode") {
		pawnd.UiDemo(opts)
		os.Exit(25)
	}

	err = pawnd.TestRun(opts)
	checkFault(err, "Running chains failed")

	os.Exit(0)
}
