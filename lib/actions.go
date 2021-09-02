package pawnd

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	zglob "github.com/mattn/go-zglob"
	"github.com/robfig/cron"
	fsnotify "gopkg.in/fsnotify.v1"
	"mvdan.cc/sh/interp"
	"mvdan.cc/sh/syntax"
)

func ActionName(name string) string {
	return fmt.Sprintf("act:%s", EscapeName(name))
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
	name = UnescapeName(name)
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

func (a *BaseAction) sendAll(ids []string, message string) {
	for i := range ids {
		fmt.Fprintln(a.Terminal().Verbose(), "Sending", message, "to", ids[i])
		a.Send(ActionName(ids[i]), message)
	}
}

func (a *BaseAction) trigger(ids []string) {
	a.sendAll(ids, MsgTrig)
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
	if message == MsgInit {
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

	files    []string
	dirs     []string
	watch    *fsnotify.Watcher
	dirWatch *fsnotify.Watcher

	BaseAction
}

// Get the list of files represented by the given list of glob patterns
func getFileList(patterns []string) []string {
	d := map[string]struct{}{}
	for _, pattern := range patterns {
		m, err := zglob.Glob(pattern)
		if err != nil {
			continue
		}

		for _, f := range m {
			// Add only if really exists. Dangling symlinks fail
			// to be watched.
			if pathExists(f) {
				d[f] = struct{}{}
			}
		}
	}

	ret := make([]string, 0, len(d))
	for k := range d {
		ret = append(ret, k)
	}
	sort.Strings(ret)

	return ret
}

func pathExists(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}
	return true
}

func getDirList(patterns, files []string) []string {
	d := map[string]struct{}{}
	for i := range patterns {
		patternDir := filepath.Dir(patterns[i])
		if pathExists(patternDir) {
			d[patternDir] = struct{}{}
		}
	}
	for i := range files {
		d[filepath.Dir(files[i])] = struct{}{}
	}

	ret := make([]string, 0, len(d))
	for k := range d {
		ret = append(ret, k)
	}
	sort.Strings(ret)

	return ret
}

// stopTimer stops time.Timer correctly
func stopTimer(t *time.Timer) {
	if !t.Stop() {
		<-t.C
	}
}

func NewFileAction(patterns ...string) (*FileAction, error) {
	var ret = &FileAction{
		Patterns:   patterns,
		Hysteresis: 1000 * time.Millisecond,
		termchan:   make(chan bool, 1),
	}

	err := ret.updateFiles()
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (a *FileAction) updateFiles() error {
	a.files = getFileList(a.Patterns)
	a.dirs = getDirList(a.Patterns, a.files)

	if len(a.files)+len(a.dirs) == 0 {
		return fmt.Errorf("no watched files or directories")
	}

	var err error

	if a.watch != nil {
		a.watch.Close()
	}

	a.watch, err = fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("could not create a new watcher: %v", err)
	}

	for i := range a.files {
		err := a.watch.Add(a.files[i])
		if err != nil {
			return fmt.Errorf("could not watch file %s", a.files[i])
		}
	}

	if a.dirWatch != nil {
		a.dirWatch.Close()
	}

	a.dirWatch, err = fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("could not create a new watcher: %v", err)
	}

	for i := range a.dirs {
		err := a.dirWatch.Add(a.dirs[i])
		if err != nil {
			return fmt.Errorf("could not watch directory %s", a.dirs[i])
		}
	}

	return nil
}

func (a *FileAction) Receive(from, message string) {
	if message == MsgTerm {
		a.termchan <- true
	}
}

func (a *FileAction) Run() {
	matchPattern := func(file string) bool {
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
		threshold := time.NewTimer(0)
		stopTimer(threshold)

		refreshTimer := func(filename string) {
			if matchPattern(filename) {
				threshold.Stop()
				threshold.Reset(a.Hysteresis)
			}
		}

	loop:
		for {
			select {
			case <-threshold.C:
				a.trigger(a.Changed)
			case event := <-a.watch.Events:
				refreshTimer(event.Name)
			case event := <-a.dirWatch.Events:
				err := a.updateFiles()
				if err != nil {
					fmt.Fprintln(a.Terminal().Stderr(), "Error updating watched files:", err)
				}

				refreshTimer(event.Name)
			case err := <-a.watch.Errors:
				fmt.Fprintln(a.Terminal().Stderr(), "Error Received:", err)
			case err := <-a.dirWatch.Errors:
				fmt.Fprintln(a.Terminal().Stderr(), "Error Received:", err)
			case <-a.termchan:
				break loop
			}
		}
		a.watch.Close()
		a.dirWatch.Close()
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
	termchan chan struct{}
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
		termchan: make(chan struct{}, 1),
		sig:      sig,
	}
	return &ret, nil
}

func (a *SignalAction) Receive(from, message string) {
	if message == MsgTerm {
		a.termchan <- struct{}{}
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
	OnChange  bool
	Succeeded []string
	Failed    []string

	script     *syntax.File
	startchan  chan struct{}
	termchan   chan struct{}
	prevStatus string

	cancelMutex sync.RWMutex
	cancel      context.CancelFunc

	BaseAction
}

func CheckShScript(script string, section string) error {
	r := strings.NewReader(script)
	_, err := syntax.NewParser(syntax.Variant(syntax.LangPOSIX)).Parse(r, section)
	if err != nil {
		err = fmt.Errorf("script parse error: %v", err)
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
		startchan: make(chan struct{}, 1),
		termchan:  make(chan struct{}, 1),
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

	// Send waiting signal to all dependencies
	if !a.OnChange {
		a.sendAll(a.Succeeded, MsgWait)
		a.sendAll(a.Failed, MsgWait)
	}

	status = statusOk
	triggered := &a.Succeeded
	info = ""

	err = i.Run(ctx, a.script)
	if err != nil {
		if exitStat, ok := err.(*interp.ShellExitStatus); ok {
			info = fmt.Sprintf("Failed with code: %d", exitStat)
		} else {
			info = err.Error()
		}
		triggered = &a.Failed
		status = statusFail
	}

	if !a.OnChange || (a.OnChange && (status != a.prevStatus)) {
		a.trigger(*triggered)
		term.SetStatus(status, info)
		a.prevStatus = status
	}

	cooldown := a.Cooldown - time.Since(starttime)
	if cooldown < 0 {
		cooldown = 0
	}

	select {
	case <-a.termchan:
		a.Cancel()
		return fmt.Errorf("terminated by user")

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
	case MsgWait:
		a.Terminal().SetStatus(statusWait, "")
		a.sendAll(a.Succeeded, MsgWait)
		a.sendAll(a.Failed, MsgWait)
	case MsgTrig:
		if a.Daemon {
			a.Cancel()
		}
		a.startchan <- struct{}{}
	case MsgTerm:
		a.Cancel()
		a.termchan <- struct{}{}
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
						"Running failed:", err)
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
	if message == MsgTerm {
		a.termchan <- true
	}
}

func (a *CronAction) Run() {
	resetTimer := func(tmr *time.Timer) {
		next := a.sched.Next(time.Now())
		fmt.Fprintln(a.Terminal().Verbose(), "Next:", next.String())
		dur := time.Until(next)
		tmr.Stop()
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
