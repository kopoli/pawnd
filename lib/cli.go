package pawnd

import (
	cli "github.com/jawher/mow.cli"

	"github.com/kopoli/go-util"
)

func Cli(opts util.Options, argsin []string) (args []string, err error) {
	progName := opts.Get("program-name", "pawnd")

	app := cli.App(progName, "For running errands and general minioning")

	app.Spec = "[OPTIONS]"

	app.Version("version v", util.VersionString(opts))

	// app.Command("define", "Define a task to run", func(cmd *cli.Cmd) {})
	// app.Command("modify", "Modify a defined task", func(cmd *cli.Cmd) {})
	// app.Command("delete", "Delete a defined task", func(cmd *cli.Cmd) {})

	// app.Command("start", "Start a task", func(cmd *cli.Cmd) {})
	// app.Command("stop", "Stop a task", func(cmd *cli.Cmd) {})
	// app.Command("restart", "Restart a task", func(cmd *cli.Cmd) {})

	optConfFile := app.StringOpt("c conf", opts.Get("configuration-file", "Pawnfile"),
		"File to read the configuration from")

	optDemo := app.BoolOpt("d demo", false, "Demo functionality")

	app.Action = func() {
		opts.Set("configuration-file", *optConfFile)

		if *optDemo {
			opts.Set("demo-mode", "t")
		}
	}

	err = app.Run(argsin)
	return
}
