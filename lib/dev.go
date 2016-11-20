package pawnd

import (
	"fmt"
	"io"
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
	fsnotify "gopkg.in/fsnotify.v1"
)

/////////////////////////////////////////////////////////////

// Event that is triggered
type Event interface {
	Run(string) error
}

// FEATURE EventFunc is to Event what http.HandlerFunc is to http.Handler
// type EventFunc func()

// Event emitter
type Emitter interface {

	// Register Event to string
	On(Event, ...string) error

	// Emit a trigger
	Trigger(string)
}

// A "Daemon" that Triggers events on an emitter
type Source interface {
	Init(string, Emitter) error

	Start()
}

/////////////////////////////////////////////////////////////

type emitter struct {
	events map[string][]Event

	mutex sync.Mutex
}

func (e *emitter) initialize() {
	if e.events == nil {
		e.events = make(map[string][]Event)
	}
}

func (e *emitter) On(event Event, IDs ...string) (err error) {
	e.mutex.Lock()
	e.initialize()
	for _, ID := range IDs {
		e.events[ID] = append(e.events[ID], event)
	}
	e.mutex.Unlock()
	return
}

func (e *emitter) Trigger(ID string) {
	e.mutex.Lock()
	e.initialize()

	fmt.Println("Event list on ID", ID, ":", e.events[ID])
	for _, ev := range e.events[ID] {
		go func(ID string, ev Event) {
			fmt.Println("ID", ID, "Event", ev)
			_ = ev.Run(ID)
		}(ID, ev)
	}
	e.mutex.Unlock()
}

/////////////////////////////////////////////////////////////

type CommandEvent struct {
	Args []string

	CoolDown time.Duration

	Stdout io.Writer
	Stderr io.Writer

	IsDaemon bool

	cmd *exec.Cmd
	wg  sync.WaitGroup

	BaseSource
}

func (c *CommandEvent) Init(id string, e Emitter) (err error) {
	err = c.BaseSource.Init(id, e)
	if err != nil {
		return
	}

	sources.Add(c)
	return
}

func (c *CommandEvent) Start() {

}

func (c *CommandEvent) runCmd() (err error) {
	c.wg.Add(1)
	c.cmd = exec.Command(c.Args[0], c.Args[1:]...)
	c.cmd.Stdout = c.Stdout
	c.cmd.Stderr = c.Stderr
	err = c.cmd.Start()
	if err != nil {
		c.cmd = nil
		util.E.Print(err, "Starting command failed: ", c.Args)
		return
	}
	c.e.Trigger(c.id + "-start")
	err = c.cmd.Wait()

	// Determine the exit code
	if err != nil {
		ret := 1
		if ee, ok := err.(*exec.ExitError); ok {
			if stat, ok := ee.Sys().(syscall.WaitStatus); ok {
				ret = stat.ExitStatus()
			}
		}
		err = util.E.Annotate(err, "Running a command failed with:", ret)
	}
	c.wg.Done()
	c.e.Trigger(c.id + "-stop")

	return
}

func (c *CommandEvent) killCmd() (err error) {
	if c.cmd != nil && c.cmd.Process != nil {
		err = c.cmd.Process.Kill()
		c.e.Trigger(c.id + "-stop")
		fmt.Println("Killing process")
	}
	return
}

func (c *CommandEvent) Run(ID string) (err error) {
	fmt.Println("Arguments", c.Args)

	switch ID {
	case "terminate":
		_ = c.killCmd()
	default:
		if c.IsDaemon {
			_ = c.killCmd()
		} else {
			fmt.Println("Waiting process to run")
		}
		c.wg.Wait()
		fmt.Println("Running command")
		_ = c.runCmd()
	}

	return
}

/////////////////////////////////////////////////////////////

// List of sources that can be started together
type sourceList struct {
	sources []Source
}

func (s *sourceList) Add(src Source) {
	s.sources = append(s.sources, src)
	fmt.Println("Adding", src)
}

func (s *sourceList) Start() {
	for _, src := range s.sources {
		fmt.Println("Starting", src)
		src.Start()
	}
}

var sources sourceList

/////////////////////////////////////////////////////////////

type BaseSource struct {
	id string
	e  Emitter
}

func (o *BaseSource) Init(id string, e Emitter) (err error) {
	o.id = id
	o.e = e
	return
}

/////////////////////////////////////////////////////////////

type OnceSource struct {
	BaseSource
}

func (o *OnceSource) Start() {
	o.e.Trigger(o.id)
}

/////////////////////////////////////////////////////////////

type TerminateEvent struct {
	terminate chan bool
}

func (t *TerminateEvent) Run(id string) (err error) {
	if t.terminate == nil {
		return
	}

	switch id {
	case "terminate":
		t.terminate <- true
	}

	return
}

func (t *TerminateEvent) init() {
	if t.terminate == nil {
		t.terminate = make(chan bool)
	}
}

/////////////////////////////////////////////////////////////

type FileChangeSource struct {
	Patterns []string

	// Wait this amount of time from the file change to the actual
	// triggering
	Hysteresis time.Duration

	watch *fsnotify.Watcher
	files []string

	BaseSource
	TerminateEvent
}

func (s *FileChangeSource) Init(id string, e Emitter) (err error) {
	err = s.BaseSource.Init(id, e)
	if err != nil {
		return
	}

	s.files = getFileList(s.Patterns)
	if len(s.files) == 0 {
		err = util.E.New("No watched files found")
		return
	}

	fmt.Println("Files", s.files)

	if s.Hysteresis == 0 {
		s.Hysteresis = time.Millisecond * 500
	}

	// Create a watcher
	s.watch, err = fsnotify.NewWatcher()
	if err != nil {
		err = util.E.Annotate(err, "Creating a file watcher failed")
		return
	}

	s.TerminateEvent.init()
	sources.Add(s)

	return
}

func (s *FileChangeSource) Start() {

	stopTimer := func(t *time.Timer) {
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
	}

	// Match non-recursive parts of the patterns against the given file
	matchPattern := func(file string) bool {
		file = filepath.Base(file)
		for _, p := range s.Patterns {
			m, er := path.Match(filepath.Base(p), file)
			if m && er == nil {
				return true
			}
		}
		return false
	}

	go func() {
		defer s.watch.Close()

		threshold := time.NewTimer(0)
		stopTimer(threshold)

		for _, name := range s.files {
			err := s.watch.Add(name)
			if err != nil {
				fmt.Println("Could not watch", name)
			}
		}

	loop:
		for {
			select {
			case <-threshold.C:
				fmt.Println("Would send an event")
				s.e.Trigger(s.id)
			case event := <-s.watch.Events:
				fmt.Println("Event received:", event)
				if matchPattern(event.Name) {
					fmt.Println("Pattern matched.")
					stopTimer(threshold)
					threshold.Reset(s.Hysteresis)
				}
			case err := <-s.watch.Errors:
				fmt.Println("Error received", err)
			case <-s.terminate:
				break loop
			}
		}
	}()

	return
}

/////////////////////////////////////////////////////////////

type SignalSource struct {
	Signal os.Signal
	ch     chan os.Signal

	BaseSource
	TerminateEvent
}

func (s *SignalSource) Init(id string, e Emitter) (err error) {
	err = s.BaseSource.Init(id, e)
	if err != nil {
		return
	}

	s.ch = make(chan os.Signal, 1)
	s.TerminateEvent.init()
	sources.Add(s)
	signal.Notify(s.ch, s.Signal)

	return
}

func (s *SignalSource) Start() {

	go func() {
	loop:
		for {
			select {
			case sig := <-s.ch:
				fmt.Println("Received signal:", sig, "Triggering", s.id)
				s.e.Trigger(s.id)
			case <-s.terminate:
				signal.Reset(s.Signal)
				break loop
			}
		}
	}()
}


/////////////////////////////////////////////////////////////

type TerminateBlocker struct {
	TerminateEvent
}

func (t *TerminateBlocker) Wait(e Emitter) {
	t.init()
	e.On(t, "terminate")
	<-t.terminate
}

/////////////////////////////////////////////////////////////

func WaitOnInput() {
	var input string
	fmt.Scanln(&input)
}

func TestRun(opts util.Options) (err error) {

	cfgs, err := LoadConfigs(opts)
	if err != nil {
		util.E.Annotate(err, "Loading configurations failed")
		return
	}

	fmt.Println(cfgs)

	e := &emitter{}

	ss := &SignalSource{
		Signal: os.Interrupt,
	}

	ss.Init("terminate", e)
	// ss.Start()

	for _, cfg := range cfgs {
		var source Source

		if cfg.Exec == "" {
			continue
		}

		if cfg.Pattern != "" {
			source = &FileChangeSource{
				Patterns: strings.Split(cfg.Pattern, " "),
			}
		} else {
			source = &OnceSource{}
		}

		source.Init(cfg.Name, e)

		fmt.Println("Name", cfg.Name, "Exec", strings.Split(cfg.Exec, " "))

		target := &CommandEvent{
			Args:     strings.Split(cfg.Exec, " "),
			Stdout:   os.Stdout,
			Stderr:   os.Stderr,
			IsDaemon: cfg.IsDaemon,
		}
		target.Init(cfg.Name+"-cmd", e)

		e.On(target, cfg.Name, "terminate")
		// source.Start()
	}

	sources.Start()

	tb := &TerminateBlocker{}
	tb.Wait(e)
	// WaitOnInput()

	e.Trigger("terminate")

	return
}
