package pawnd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
	"mvdan.cc/sh/interp"
	"mvdan.cc/sh/syntax"
)

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
		termchan:   make(chan bool, 1),
	}

	var err error
	ret.watch, err = fsnotify.NewWatcher()
	if err != nil {
		err = util.E.Annotate(err, "Could not create a new watcher")
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

func CheckSignal(name string) error {
	_, ok := SupportedSignals[name]
	if !ok {
		return fmt.Errorf("signal name \"%s\" is not supported", name)
	}
	return nil
}

type SignalAction struct {
	sigchan  chan os.Signal
	termchan chan bool
	sig      os.Signal

	Triggered []string

	// If this signal should terminate the program
	Terminator bool

	BaseAction
}

func NewSignalAction(signame string) (*SignalAction, error) {
	if signame != "interrupt" {
		err := CheckSignal(signame)
		if err != nil {
			return nil, err
		}
	}

	var ret = SignalAction{
		sigchan:  make(chan os.Signal, 1),
		termchan: make(chan bool, 1),
	}
	if signame == "interrupt" {
		ret.sig = os.Interrupt
	} else {
		ret.sig = SupportedSignals[signame]
	}

	return &ret, nil
}

func (a *SignalAction) Receive(from, message string) {
	switch message {
	case MsgTerm:
		a.termchan <- true
	}
}

func (a *SignalAction) terminate() {
	fmt.Fprintln(a.Terminal().Verbose(), "Terminating due to signal.")
	a.Send(ToAll, MsgTerm)

	// Set a time limit to termination
	time.AfterFunc(2*time.Second, func() {
		// Triggering this is a bug.
		FailSafeExit()
	})
}

func (a *SignalAction) Run() {
	SignalNotify(a.sigchan, a.sig)
	go func() {
	loop:
		for {
			select {
			case <-a.sigchan:
				if a.Terminator {
					a.terminate()
				} else {
					a.trigger(a.Triggered)
				}
			case <-a.termchan:
				SignalReset(a.sig)
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
		startchan:    make(chan bool, 1),
		termchan:     make(chan bool, 1),
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
				_ = a.Kill()
			case <-a.startchan:
				if a.Daemon {
					_ = a.Kill()
				}
				err := a.RunCommand()
				if err != nil {
					fmt.Fprintln(a.Terminal().Stderr(),
						"Running command failed:", err)
					break loop
				}
			case <-a.termchan:
				_ = a.Kill()
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

	cooldown := a.Cooldown - time.Since(starttime)
	if cooldown < 0 {
		cooldown = 0
	}

	select {
	case <-a.termchan:
		return fmt.Errorf("Received terminate during cooldown")

	case <-time.After(cooldown):
	}

	return nil
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
		fmt.Fprintln(a.Terminal().Verbose(), "Received terminate!")
		a.termchan <- true
	}
}

///

type ShAction struct {
	Cooldown  time.Duration
	Timeout   time.Duration
	Succeeded []string
	Failed    []string

	script    *syntax.File
	startchan chan bool
	termchan  chan bool
	cancel    context.CancelFunc

	BaseAction
}

func CheckShScript(script string, section string) error {
	r := strings.NewReader(script)
	_, err := syntax.NewParser(syntax.Variant(syntax.LangPOSIX)).Parse(r, section)
	return err
}

func NewShAction(script string) (*ShAction, error) {
	r := strings.NewReader(script)
	scr, err := syntax.NewParser(syntax.Variant(syntax.LangPOSIX)).Parse(r, "")
	if err != nil {
		return nil, err
	}

	ret := &ShAction{
		Cooldown:  3000 * time.Millisecond,
		script:    scr,
		startchan: make(chan bool, 1),
		termchan:  make(chan bool, 1),
	}

	ret.Visible = true

	return ret, nil
}

func (a *ShAction) RunCommand() error {
	starttime := time.Now()

	term := a.Terminal()
	i, err := interp.New(interp.StdIO(nil, term.Stdout(), term.Stderr()))
	if err != nil {
		return err
	}

	i.Reset()
	i.KillTimeout = -1

	ctx := context.Background()
	if a.Timeout != 0 {
		ctx, a.cancel = context.WithTimeout(ctx, a.Timeout)
		defer func() {
			a.cancel()
			a.cancel = nil
		}()
	}

	info := ""
	term.SetStatus(statusRun, info)

	err = i.Run(ctx, a.script)
	if err == nil {
		a.trigger(a.Succeeded)
		term.SetStatus(statusOk, "")
	} else {
		if exitStat, ok := err.(*interp.ShellExitStatus); ok {
			info = fmt.Sprintf("Failed with code: %d", exitStat)
		} else {
			info = err.Error()
		}
		a.trigger(a.Failed)
		term.SetStatus(statusFail, info)
	}

	cooldown := a.Cooldown - time.Since(starttime)
	if cooldown < 0 {
		cooldown = 0
	}

	select {
	case <-a.termchan:
		return fmt.Errorf("Received terminate during cooldown")

	case <-time.After(cooldown):
	}
	return nil
}

func (a *ShAction) Receive(from, message string) {
	switch message {
	case MsgTrig:
		a.startchan <- true
	case MsgTerm:
		a.termchan <- true
	}
}

func (a *ShAction) Run() {
	go func() {
	loop:
		for {
			select {
			case <-a.startchan:
				err := a.RunCommand()
				if err != nil {
					fmt.Fprintln(a.Terminal().Stderr(),
						"Running script failed:", err)
					break loop
				}
			case <-a.termchan:
				c := a.cancel
				if c != nil {
					c()
					a.cancel = nil
				}
				break loop
			}
		}
		fmt.Fprintln(a.Terminal().Stdout(), "Script stopped")
		a.bus.LinkStopped(a.name)
	}()
}

///

type RestartAction struct {
	trigchan chan bool
	termchan chan bool

	conffile string

	BaseAction
}

func NewRestartAction(conffile string) *RestartAction {
	return &RestartAction{
		trigchan: make(chan bool, 1),
		termchan: make(chan bool, 1),
		conffile: conffile,
	}
}

func (a *RestartAction) Receive(from, message string) {
	switch message {
	case MsgTrig:
		a.trigchan <- true
	case MsgTerm:
		a.termchan <- true
	}
}

func (a *RestartAction) restart() {
	_, err := ValidateConfig(a.conffile)
	if err != nil {
		fmt.Fprintln(a.Terminal().Stderr(),
			"Configuration contained errors:", err)
		return
	}
	fmt.Fprintln(GetTerminal("").Stdout(), "Configuration updated. Restarting.")
	a.Send(ToAll, MsgRest)
}

func (a *RestartAction) Run() {
	go func() {
	loop:
		for {
			select {
			case <-a.trigchan:
				a.restart()
			case <-a.termchan:
				break loop
			}
		}
		a.bus.LinkStopped(a.name)
	}()
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
		termchan: make(chan bool, 1),
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
	resetTimer := func(tmr *time.Timer) {
		next := a.sched.Next(time.Now())
		fmt.Fprintln(a.Terminal().Verbose(), "Next:", next.String())
		dur := time.Until(next)
		stopTimer(tmr)
		tmr.Reset(dur)
	}

	go func() {
		trigtimer := time.NewTimer(0)
		stopTimer(trigtimer)
		resetTimer(trigtimer)
	loop:
		for {
			select {
			case <-trigtimer.C:
				a.trigger(a.Triggered)
				resetTimer(trigtimer)
			case <-a.termchan:
				stopTimer(trigtimer)
				break loop
			}
		}
		a.bus.LinkStopped(a.name)
	}()
}
