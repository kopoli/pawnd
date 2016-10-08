package pawnd

import (
	"fmt"
	"runtime"

	cli "github.com/jawher/mow.cli"

	"github.com/kopoli/go-util"
)

func Cli(opts util.Options, argsin []string) (args []string, err error) {
	progName := opts.Get("program-name", "pawnd")
	progVersion := opts.Get("program-version", "undefined")

	app := cli.App(progName, "For running errands and general minioning")

	app.Spec = "[OPTIONS]"

	app.Version("version v", fmt.Sprintf("%s: %s\nBuilt with: %s/%s on %s/%s",
		progName, progVersion, runtime.Compiler, runtime.Version(),
		runtime.GOOS, runtime.GOARCH))

	optConfFile := app.StringOpt("c conf", opts.Get("configuration-file", "pawnd.conf"),
		"File to read the configuration from.")

	app.Action = func() {
		opts.Set("configuration-file", *optConfFile)
	}

	err = app.Run(argsin)
	return
}
