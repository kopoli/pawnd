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

	opts.Set("configuration-file", "pawnd.conf")

	_, err := pawnd.Cli(opts, os.Args)
	if err != nil {
		fault(err, "command line parsing failed")
	}

	ch := make(chan pawnd.Trigger)

	p := pawnd.Process{
		Args: []string{"ls"},
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		IsDaemon: false,
	}

	pm := pawnd.ProcessManager{}

	err = pm.Add(p, ch)
	if err != nil {
		fault(err, "Adding process failed")
	}

	_, err = pawnd.TriggerOnFileChanges([]string{"**/*.go"}, ch)
	if err != nil {
		fault(err, "Trigger test failed")
	}

	var input string
	fmt.Scanln(&input)
	close(ch)

	os.Exit(0)
}
