package pawnd

import (
	"fmt"
	"strings"

	cli "github.com/jawher/mow.cli"

	"github.com/kopoli/appkit"
)

func Cli(opts appkit.Options, argsin []string) ([]string, error) {
	progName := opts.Get("program-name", "pawnd")

	app := cli.App(progName, "For running errands and general minioning")
	app.Spec = "[OPTIONS]"
	app.Version("version v", appkit.VersionString(opts))

	conffile := "Pawnfile"

	optConfFile := app.StringOpt("f file", opts.Get("configuration-file", conffile),
		"File to read the configuration from")

	optVerbose := app.BoolOpt("V verbose", false, "Verbose output")

	optLicenseSummary := app.BoolOpt("license-summary", false, "Display summary of the licenses of dependencies")
	optLicenseTexts := app.BoolOpt("license-texts", false, "Display full licenses of dependencies")

	app.Action = func() {
		opts.Set("configuration-file", *optConfFile)

		if *optVerbose {
			opts.Set("verbose", "t")
		}
		if *optLicenseSummary {
			opts.Set("license-summary", "t")
		}
		if *optLicenseTexts {
			opts.Set("license-texts", "t")
		}
	}

	tmp := Templates()
	keys := make([]string, 0, len(tmp))
	for k := range tmp {
		keys = append(keys, k)
	}

	genhelp := fmt.Sprintf("Generate %s from templates. The following templates are supported: %s",
		conffile, strings.Join(keys, ", "))

	app.Command("generate",
		genhelp,
		func(cmd *cli.Cmd) {
			cmd.Spec = "[OPTIONS] [TEMPLATE ...]"
			argTemplates := cmd.StringsArg("TEMPLATE", nil, "The names of the templates")
			optStdout := cmd.BoolOpt("c stdout", false, "Print generated to stdout.")
			optOverwrite := cmd.BoolOpt("o overwrite", false, "Overwrite the file.")
			optOutfile := cmd.StringOpt("f file", conffile, "File to generate.")
			cmd.Action = func() {
				opts.Set("generate-templates", strings.Join(*argTemplates, " "))

				if *optStdout {
					opts.Set("generate-stdout", "t")
				}
				if *optOverwrite {
					opts.Set("generate-overwrite", "t")
				}

				opts.Set("generate-configuration-file", *optOutfile)
			}
		})

	err := app.Run(argsin)
	return nil, err
}
