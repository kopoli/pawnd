package pawnd

import (
	"os"

	util "github.com/kopoli/go-util"
)

func Main(opts util.Options) error {
	eb := NewEventBus()
	ta := NewTerminalOutput()
	ta.Verbose = opts.IsSet("verbose")

	sa := NewSignalAction(os.Interrupt)
	eb.Register("sighandler", sa)

	f, err := ValidateConfig(opts.Get("configuration-file", "Pawnfile"))
	if err != nil {
		return err
	}

	err = CreateActions(f, eb)
	if err != nil {
		return err
	}

	eb.Run()
	ta.Stop()
	return nil
}
