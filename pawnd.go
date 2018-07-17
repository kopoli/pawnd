package main

//go:generate licrep -o licenses.go --prefix "pawnd" -i "mow.cli/internal" -i "pawnd/lib"

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/kopoli/go-util"
	"github.com/kopoli/pawnd/lib"
)

var (
	majorVersion     = "0"
	version          = "Undefined"
	timestamp        = "Undefined"
	progVersion      = majorVersion + "-" + version
)

func fault(err error, message string, arg ...string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s%s: %s\n", message, strings.Join(arg, " "), err)
		os.Exit(1)
	}
}

func main() {
	opts := util.NewOptions()

	opts.Set("program-name", os.Args[0])
	opts.Set("program-version", progVersion)
	opts.Set("program-timestamp", timestamp)

	_, err := pawnd.Cli(opts, os.Args)
	fault(err, "Parsing command line failed")

	if opts.IsSet("license-summary") || opts.IsSet("license-texts") {
		licenses, err := pawndGetLicenses()
		fault(err, "Internal error getting embedded licenses")

		var names []string
		for i := range licenses {
			names = append(names, i)
		}
		sort.Strings(names)

		if opts.IsSet("license-summary") {
			fmt.Println("Licenses:")
			for _, i := range names {
				fmt.Printf("%s: %s\n", i, licenses[i].Name)
			}
			fmt.Println("")
		} else {
			fmt.Println("License texts of depending packages:")
			for _, i := range names {
				fmt.Printf("* %s:\n\n%s\n\n", i, licenses[i].Text)
			}
		}

		return
	}

	if opts.IsSet("demo-mode") {
		pawnd.ActionDemo(opts)
		os.Exit(25)
	}

	if opts.IsSet("generate-templates") {
		err = pawnd.GenerateTemplates(opts)
		fault(err, "Generating templates failed")
		os.Exit(0)
		return
	}

	err = pawnd.Main(opts)
	fault(err, "Running pawnd failed")
	os.Exit(0)
}
