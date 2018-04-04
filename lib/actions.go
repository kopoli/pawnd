package pawnd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	util "github.com/kopoli/go-util"
	zglob "github.com/mattn/go-zglob"
	fsnotify "gopkg.in/fsnotify.v1"
)

/*

Format:

;; Building:

[name]
file=*.go
changed=build
; removed=
; added=

[build]
exec=go build


;; Running a daemon

init=godoc

[godoc]
daemon=godoc -http=:6060


;; trigger chain

[html changed]
file=html/index.html
changed=generate

[source changed]
file=*go
changed=build

[generate]
exec=go generate
succeeded=build

[build]
exec=go build
succeeded=handledaemon

[handledaemon]
daemon=./daemond --foreground

*/

func ActionName(name string) string {
	return fmt.Sprintf("act:%s", name)
}

type BaseAction struct {
	name          string
	bus           *EventBus
	term          Terminal
	statusVisible bool
}

func (a *BaseAction) Identify(name string, eb *EventBus) {
	a.name = name
	a.bus = eb
	name = strings.TrimPrefix(name, "act:")
	a.term = RegisterTerminal(name, a.statusVisible)
}

func (a *BaseAction) Send(to, message string) {
	a.bus.Send(a.name, to, message)
}

func (a *BaseAction) Terminal() Terminal {
	if a.term == nil {
		return GetTerminal("")
	}
	return a.term
}

///

type InitAction struct {
	triggerName string

	BaseAction
}

func NewInitAction(triggerName string) *InitAction {
	return &InitAction{triggerName: triggerName}
}

func (a *InitAction) Receive(from, message string) {
	switch message {
	case MsgInit:
		a.Send(a.triggerName, MsgTrig)
	}
}

///

type FileAction struct {
	Patterns   []string
	Hysteresis time.Duration

	Changed string

	triggerName string
	termchan    chan bool
	watch       *fsnotify.Watcher

	BaseAction
}

// Remove duplicate strings from the list
func uniqStr(in []string) (out []string) {
	set := make(map[string]bool, len(in))

	for _, item := range in {
		set[item] = true
	}
	out = make([]string, len(set))
	for item := range set {
		out = append(out, item)
		// fmt.Println("item:[", item, "]")
	}
	return
}

// Get the list of files represented by the given list of glob patterns
func getFileList(patterns []string) (ret []string) {
	for _, pattern := range patterns {
		// Recursive globbing support
		m, err := zglob.Glob(pattern)
		if err != nil {
			continue
		}

		ret = append(ret, m...)
	}

	for _, path := range ret {
		ret = append(ret, filepath.Dir(path))
	}

	ret = uniqStr(ret)

	return
}

func NewFileAction(patterns ...string) (*FileAction, error) {

	var ret = FileAction{
		Patterns:   patterns,
		Hysteresis: 500 * time.Millisecond,
		termchan:   make(chan bool),
	}

	var err error
	ret.watch, err = fsnotify.NewWatcher()
	if err != nil {
		util.E.Annotate(err, "Could not create a new watcher")
		return nil, err
	}

	stopTimer := func(t *time.Timer) {
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
	}

	matchPattern := func(file string) bool {

		// check if a dangling symlink
		if _, err := os.Stat(file); os.IsNotExist(err) {
			return false
		}

		file = filepath.Base(file)
		for _, p := range ret.Patterns {
			m, er := path.Match(filepath.Base(p), file)
			if m && er == nil {
				fmt.Fprintf(ret.Terminal().Verbose(), "File \"%s\" changed\n", file)
				return true
			}
		}
		return false
	}

	go func() {
		defer ret.watch.Close()

		threshold := time.NewTimer(0)
		stopTimer(threshold)

		files := getFileList(ret.Patterns)
		stderr := ret.Terminal().Stderr()
		if len(files) == 0 {
			fmt.Fprintln(stderr, "Error: No watched files")
		}
		for i := range files {
			err := ret.watch.Add(files[i])
			if err != nil {
				fmt.Fprintln(stderr, "Error: Could not watch", files[i])
			}
		}

	loop:
		for {
			select {
			case <-threshold.C:
				if ret.Changed != "" {
					ret.Send(ActionName(ret.Changed), MsgTrig)
				}
			case event := <-ret.watch.Events:
				if matchPattern(event.Name) {
					stopTimer(threshold)
					threshold.Reset(ret.Hysteresis)
				}
			case err := <-ret.watch.Errors:
				fmt.Fprintln(ret.Terminal().Stderr(), "Error Received:", err)
			case <-ret.termchan:
				break loop
			}
		}
	}()

	return &ret, err
}

func (a *FileAction) Receive(from, message string) {
	switch message {
	case MsgTerm:
		a.termchan <- true
	}
}

///

type SignalAction struct {
	sigchan  chan os.Signal
	termchan chan bool
	BaseAction
}

func NewSignalAction(sig os.Signal) *SignalAction {
	var ret = SignalAction{
		sigchan:  make(chan os.Signal),
		termchan: make(chan bool),
	}

	signal.Notify(ret.sigchan, sig)
	go func() {
	loop:
		for {
			select {
			case <-ret.sigchan:
				ret.Send("*", MsgTerm)
			case <-ret.termchan:
				break loop
			}
		}
	}()
	return &ret
}

func (a *SignalAction) Receive(from, message string) {
	switch message {
	case MsgTerm:
		a.termchan <- true
	}
}

///

type ExecAction struct {
	Args []string

	Daemon    bool
	Succeeded string
	Failed    string

	cmd *exec.Cmd
	wg  sync.WaitGroup

	BaseAction
}

func NewExecAction(args ...string) *ExecAction {
	ret := &ExecAction{
		Args: args,
	}
	ret.statusVisible = true
	return ret

}

func (a *ExecAction) Run() error {
	a.wg.Add(1)
	defer a.wg.Done()

	term := a.Terminal()

	a.cmd = exec.Command(a.Args[0], a.Args[1:]...)
	a.cmd.Stdout = term.Stdout()
	a.cmd.Stderr = term.Stderr()
	err := a.cmd.Start()
	if err != nil {
		a.cmd = nil
		fmt.Fprintln(term.Stderr(), "Error: Starting command failed:", err)
		return err
	}

	term.SetStatus("run")
	err = a.cmd.Wait()
	if err == nil {
		if a.Succeeded != "" {
			a.Send(ActionName(a.Succeeded), MsgTrig)
		}
		a.Send(ToOutput, a.name+"-ok")
		term.SetStatus("ok")
	} else {
		if a.Failed != "" {
			a.Send(ActionName(a.Failed), MsgTrig)
		}
		a.Send(ToOutput, a.name+"-fail")
		term.SetStatus("fail")
	}
	a.cmd = nil
	return err
}

func (a *ExecAction) Kill() error {
	if a.cmd != nil && a.cmd.Process != nil {
		return a.cmd.Process.Kill()
	}
	return nil
}

func (a *ExecAction) Receive(from, message string) {
	switch message {
	case MsgTrig:
		fmt.Fprintln(a.Terminal().Verbose(), "Running command:", a.Args)
		if a.Daemon {
			_ = a.Kill()
		}
		a.wg.Wait()
		a.Run()
	case MsgTerm:
		fmt.Fprintln(a.Terminal().Verbose(), "Terminating command!")
		a.Kill()
	}
}

func ActionDemo(opts util.Options) {
	eb := NewEventBus()

	ta := NewTermAction()

	sa := NewSignalAction(os.Interrupt)
	eb.Register("sighandler", sa)

	f, err := ValidateConfig("Testfile")
	if err != nil {
		fmt.Println(err)
		return
	}

	err = CreateActions(f, eb)

	// eb.Send("", ToAll, MsgInit)

	eb.Run()

	ta.Stop()
}
