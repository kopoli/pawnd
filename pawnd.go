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

	// ch := make(chan pawnd.Trigger)

	// p := pawnd.Process{
	// 	Args: []string{"ls"},
	// 	Stdout: os.Stdout,
	// 	Stderr: os.Stderr,
	// 	IsDaemon: false,
	// }

	// pm := pawnd.ProcessManager{}

	// err = pm.Add(p, ch)
	// if err != nil {
	// 	fault(err, "Adding process failed")
	// }

	// _, err = pawnd.TriggerOnFileChanges([]string{"**/*.go"}, ch)
	// if err != nil {
	// 	fault(err, "Trigger test failed")
	// }

	// fc := &pawnd.FileChangeLink{
	// 	Patterns: []string{"**/*.go"},
	// }

	// c := &pawnd.CommandLink{
	// 	Args:     []string{"ls"},
	// 	Stdout:   os.Stdout,
	// 	Stderr:   os.Stderr,
	// 	IsDaemon: false,
	// }

	// err = pawnd.Join(fc, c)
	// checkFault(err, "Starting chain failed")

	err = pawnd.Run(opts)
	checkFault(err, "Running chains failed")

	var input string
	fmt.Scanln(&input)
	// fc.Close()
	// c.Close()

	// for _, link := range links {
	// 	link.Close()
	// }

	os.Exit(0)
}
