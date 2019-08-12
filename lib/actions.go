package pawnd

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
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
	_, err := deps.SupportedSignal(name)
	return err
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
	var sig os.Signal
	if signame == "interrupt" {
		sig = os.Interrupt
	} else {
		var err error
		sig, err = deps.SupportedSignal(signame)
		if err != nil {
			return nil, err
		}
	}

	var ret = SignalAction{
		sigchan:  make(chan os.Signal, 1),
		termchan: make(chan bool, 1),
		sig:      sig,
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
}

func (a *SignalAction) Run() {
	deps.SignalNotify(a.sigchan, a.sig)
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
				deps.SignalReset(a.sig)
				break loop
			}
		}
		a.bus.LinkStopped(a.name)
	}()
}

///

type ShAction struct {
	Cooldown  time.Duration
	Timeout   time.Duration
	Daemon    bool
	Succeeded []string
	Failed    []string

	script    *syntax.File
	startchan chan bool
	termchan  chan bool

	cancelMutex sync.RWMutex
	cancel      context.CancelFunc

	BaseAction
}

func CheckShScript(script string, section string) error {
	r := strings.NewReader(script)
	_, err := syntax.NewParser(syntax.Variant(syntax.LangPOSIX)).Parse(r, section)
	if err != nil {
		err = fmt.Errorf("Script parse error: %v", err)
	}
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
	a.cancelMutex.Lock()
	if a.Timeout != 0 {
		ctx, a.cancel = context.WithTimeout(ctx, a.Timeout)
		defer func() {
			a.Cancel()
		}()
	} else {
		ctx, a.cancel = context.WithCancel(ctx)
	}
	a.cancelMutex.Unlock()

	info := ""
	status := statusRun
	if a.Daemon {
		info = infoDaemon
		status = statusDaemon
	}
	term.SetStatus(status, info)

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

func (a *ShAction) Cancel() {
	a.cancelMutex.Lock()
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.cancelMutex.Unlock()
}

func (a *ShAction) Receive(from, message string) {
	switch message {
	case MsgTrig:
		if a.Daemon {
			a.Cancel()
		}
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
				a.Cancel()
				break loop
			}
		}
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
