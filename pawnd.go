package main

//go:generate licrep -o licenses.go --prefix "pawnd" -i "mow.cli/internal" -i "pawnd/lib"

import (
	"fmt"
	"os"
	"sort"

	"github.com/kopoli/appkit"
	pawnd "github.com/kopoli/pawnd/lib"
)

var (
	version     = "Undefined"
	timestamp   = "Undefined"
	progVersion = "" + version
)

func fault(err error, message string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s: %s\n", message, err)
		os.Exit(1)
	}
}

func main() {
	opts := appkit.NewOptions()

	opts.Set("program-real-name", "Pawnd")
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

	if opts.IsSet("generate-templates") {
		err = pawnd.GenerateTemplates(opts)
		fault(err, "Generating templates failed")
		os.Exit(0)
		return
	}

	for {
		err = pawnd.Main(opts)
		if err != pawnd.ErrMainRestarted {
			break
		}
	}

	fault(err, "Running pawnd failed")
	os.Exit(0)
}
