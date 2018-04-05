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
	"syscall"
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

func (a *BaseAction) trigger(ids []string) {
	for i := range ids {
		fmt.Fprintln(a.Terminal().Verbose(), "Triggering", ids[i])
		a.Send(ActionName(ids[i]), MsgTrig)
	}
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

	Changed []string

	termchan chan bool
	watch    *fsnotify.Watcher

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

// stopTimer stops time.Timer correctly
func stopTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}

func NewFileAction(patterns ...string) (*FileAction, error) {

	var ret = FileAction{
		Patterns:   patterns,
		Hysteresis: 1000 * time.Millisecond,
		termchan:   make(chan bool),
	}

	var err error
	ret.watch, err = fsnotify.NewWatcher()
	if err != nil {
		util.E.Annotate(err, "Could not create a new watcher")
		return nil, err
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
				ret.trigger(ret.Changed)
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
	Succeeded []string
	Failed    []string

	CoolDown time.Duration

	cmd *exec.Cmd
	wg  sync.WaitGroup

	BaseAction
}

func NewExecAction(args ...string) *ExecAction {
	ret := &ExecAction{
		Args:     args,
		CoolDown: 3000 * time.Millisecond,
	}
	ret.statusVisible = true
	return ret

}

func (a *ExecAction) Run() error {
	a.wg.Add(1)
	defer a.wg.Done()

	starttime := time.Now()
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

	info := ""
	if a.Daemon {
		info = infoDaemon
	}
	term.SetStatus(statusRun, info)

	err = a.cmd.Wait()
	if err == nil {
		a.trigger(a.Succeeded)
		term.SetStatus(statusOk, "")
	} else {
		info = ""
		if exitError, ok := err.(*exec.ExitError); ok {
			waitstatus := exitError.Sys().(syscall.WaitStatus)
			info = fmt.Sprintf("Failed with code: %d",
				waitstatus.ExitStatus())
		}
		a.trigger(a.Failed)
		term.SetStatus(statusFail, info)
	}
	a.cmd = nil

	runtime := time.Since(starttime)
	if runtime < a.CoolDown {
		fmt.Fprintln(a.Terminal().Verbose(), "Waiting for cooldown:",
			a.CoolDown-runtime)
		time.Sleep(a.CoolDown - runtime)
	}
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

	ta := NewTerminalOutput()

	sa := NewSignalAction(os.Interrupt)
	eb.Register("sighandler", sa)

	f, err := ValidateConfig("Testfile")
	if err != nil {
		fmt.Println(err)
		return
	}

	CreateActions(f, eb)

	eb.Run()

	ta.Stop()
}
