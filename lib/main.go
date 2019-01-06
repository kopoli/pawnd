package pawnd

import (
	util "github.com/kopoli/go-util"
)

func Main(opts util.Options) error {
	eb := NewEventBus()
	ta := NewTerminalOutput(opts)

	sa, _ := NewSignalAction("interrupt")
	sa.Terminator = true
	eb.Register("sighandler", sa)

	f, err := ValidateConfig(opts.Get("configuration-file", "Pawnfile"))
	if err != nil {
		return err
	}

	err = CreateActions(f, eb)
	if err != nil {
		return err
	}

	ta.Draw()
	eb.Run()
	ta.Stop()
	return nil
}
