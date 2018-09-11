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
	"github.com/robfig/cron"
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
	name    string
	bus     *EventBus
	term    Terminal
	Visible bool
}

func (a *BaseAction) Identify(name string, eb *EventBus) {
	a.name = name
	a.bus = eb
	name = strings.TrimPrefix(name, "act:")
	a.term = RegisterTerminal(name, a.Visible)
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

func (a *InitAction) Run() {
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

	files := getFileList(ret.Patterns)
	if len(files) == 0 {
		return nil, fmt.Errorf("No watched files")
	}

	for i := range files {
		err := ret.watch.Add(files[i])
		if err != nil {
			return nil, fmt.Errorf("Could not watch file %s", files[i])
		}
	}

	return &ret, err
}

func (a *FileAction) Receive(from, message string) {
	switch message {
	case MsgTerm:
		a.termchan <- true
	}
}

func (a *FileAction) Run() {
	matchPattern := func(file string) bool {

		// check if a dangling symlink
		if _, err := os.Stat(file); os.IsNotExist(err) {
			return false
		}

		file = filepath.Base(file)
		for _, p := range a.Patterns {
			m, er := path.Match(filepath.Base(p), file)
			if m && er == nil {
				fmt.Fprintf(a.Terminal().Verbose(), "File \"%s\" changed\n", file)
				return true
			}
		}
		return false
	}

	go func() {
		defer a.watch.Close()

		threshold := time.NewTimer(0)
		stopTimer(threshold)

	loop:
		for {
			select {
			case <-threshold.C:
				a.trigger(a.Changed)
			case event := <-a.watch.Events:
				if matchPattern(event.Name) {
					stopTimer(threshold)
					threshold.Reset(a.Hysteresis)
				}
			case err := <-a.watch.Errors:
				fmt.Fprintln(a.Terminal().Stderr(), "Error Received:", err)
			case <-a.termchan:
				break loop
			}
		}
		a.bus.LinkStopped(a.name)
	}()
}

///

type SignalAction struct {
	sigchan  chan os.Signal
	termchan chan bool
	sig      os.Signal
	BaseAction
}

func NewSignalAction(sig os.Signal) *SignalAction {
	var ret = SignalAction{
		sigchan:  make(chan os.Signal, 1),
		termchan: make(chan bool),
		sig:      sig,
	}

	return &ret
}

func (a *SignalAction) Receive(from, message string) {
	switch message {
	case MsgTerm:
		a.termchan <- true
	}
}

func (a *SignalAction) Run() {
	signal.Notify(a.sigchan, a.sig)
	go func() {
	loop:
		for {
			select {
			case <-a.sigchan:
				a.Send(ToAll, MsgTerm)
			case <-a.termchan:
				break loop
			}
		}
		a.bus.LinkStopped(a.name)
	}()
}

///

type ExecAction struct {
	Args []string

	Daemon    bool
	Succeeded []string
	Failed    []string

	Cooldown time.Duration
	Timeout  time.Duration

	cmd          *exec.Cmd
	wg           sync.WaitGroup
	startchan    chan bool
	termchan     chan bool
	timeoutTimer *time.Timer

	BaseAction
}

func NewExecAction(args ...string) *ExecAction {
	ret := &ExecAction{
		Args:         args,
		Cooldown:     3000 * time.Millisecond,
		startchan:    make(chan bool),
		termchan:     make(chan bool),
		timeoutTimer: time.NewTimer(0),
	}
	stopTimer(ret.timeoutTimer)
	ret.Visible = true

	return ret
}

func (a *ExecAction) Run() {
	go func() {
	loop:
		for {
			select {
			case <-a.timeoutTimer.C:
				fmt.Fprintln(a.Terminal().Verbose(), "Timed out")
				a.Kill()
			case <-a.startchan:
				if a.Daemon {
					_ = a.Kill()
				}
				a.RunCommand()
			case <-a.termchan:
				a.Kill()
				break loop
			}
		}
		a.bus.LinkStopped(a.name)
	}()
}

func (a *ExecAction) RunCommand() error {
	a.wg.Add(1)
	defer a.wg.Done()

	starttime := time.Now()
	term := a.Terminal()

	if a.Timeout != 0 {
		a.timeoutTimer.Reset(a.Timeout)
		defer stopTimer(a.timeoutTimer)
	}
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
	if runtime < a.Cooldown {
		fmt.Fprintln(a.Terminal().Verbose(), "Waiting for cooldown:",
			a.Cooldown-runtime)
		time.Sleep(a.Cooldown - runtime)
	}
	return err
}

func (a *ExecAction) Kill() error {
	var err error
	if a.cmd != nil && a.cmd.Process != nil {
		err = a.cmd.Process.Kill()
		fmt.Fprintln(a.Terminal().Verbose(), "Terminated process")
	}
	return err
}

func (a *ExecAction) Receive(from, message string) {
	switch message {
	case MsgTrig:
		fmt.Fprintln(a.Terminal().Verbose(), "Running command:", a.Args)

		// If not a daemon, wait until completion
		if !a.Daemon {
			a.wg.Wait()
		}
		a.startchan <- true

	case MsgTerm:
		fmt.Fprintln(a.Terminal().Stderr(), "Received terminate!")
		a.termchan <- true
	}
}

///

type CronAction struct {
	spec     string
	sched    cron.Schedule
	termchan chan bool

	Triggered []string
	BaseAction
}

func CheckCronSpec(spec string) error {
	_, err := cron.Parse(spec)
	return err
}

func NewCronAction(spec string) (*CronAction, error) {
	sched, err := cron.Parse(spec)
	if err != nil {
		return nil, err
	}
	var ret = &CronAction{
		spec:     spec,
		sched:    sched,
		termchan: make(chan bool),
	}

	return ret, nil
}

func (a *CronAction) Receive(from, message string) {
	switch message {
	case MsgTerm:
		a.termchan <- true
	}
}

func (a *CronAction) Run() {
	trigtimer := time.NewTimer(0)
	stopTimer(trigtimer)

	resetTimer := func() {
		next := a.sched.Next(time.Now())
		fmt.Fprintln(a.Terminal().Verbose(), "Next:", next.String())
		dur := time.Until(next)
		stopTimer(trigtimer)
		trigtimer.Reset(dur)
	}

	go func() {
		resetTimer()
	loop:
		for {
			select {
			case <-trigtimer.C:
				a.trigger(a.Triggered)
				resetTimer()
			case <-a.termchan:
				break loop
			}
		}
		a.bus.LinkStopped(a.name)
	}()
}

///

func ActionDemo(opts util.Options) {
	eb := NewEventBus()

	ta := NewTerminalOutput(opts)

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
