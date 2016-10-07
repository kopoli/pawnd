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

func fault(err error, message string, arg ...string) {
	printErr(err, message, arg...)
	os.Exit(1)
}

func main() {
	opts := util.GetOptions()

	args, err := pawnd.Cli(opts, os.Args)
	if err != nil {
		fault(err, "command line parsing failed")
	}
	os.Exit(1)
}
