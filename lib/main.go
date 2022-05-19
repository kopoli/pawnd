package pawnd

import (
	"strconv"
	"time"

	"github.com/kopoli/appkit"
)

func Main(opts appkit.Options) error {
	eb := NewEventBus()
	defer eb.Close()
	ta := NewTerminalOutput(opts)
	defer ta.Stop()

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
	hyst, err := strconv.Atoi(opts.Get("pawnfile-hysteresis", "1000"))
	if err != nil {
		hyst = 1000
	}
	fa.Hysteresis = time.Millisecond * time.Duration(hyst)
	eb.Register("pawnfile-watcher", fa)
	ra := NewRestartAction(conffile)
	eb.Register(ActionName(fa.Changed[0]), ra)

	ta.Ready()
	err = eb.Run()
	return err
}
