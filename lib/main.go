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

	conffile := opts.Get("configuration-file", "Pawnfile")
	f, err := ValidateConfig(conffile)
	if err != nil {
		return err
	}

	err = CreateActions(f, eb)
	if err != nil {
		return err
	}

	// Watch the configuration file for updates
	fa, err := NewFileAction(conffile)
	if err != nil {
		return err
	}
	fa.Changed = []string{"pawnd-restart"}
	eb.Register("pawnfile-watcher", fa)
	ra := NewRestartAction(conffile)
	eb.Register(ActionName(fa.Changed[0]), ra)

	ta.Draw()
	err = eb.Run()
	ta.Stop()
	return err
}
