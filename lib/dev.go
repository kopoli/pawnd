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
	Run(ID string) error
}

// FEATURE EventFunc is to Event what http.HandlerFunc is to http.Handler
// type EventFunc func()

// Event emitter
type Emitter interface {

	// Register Event to string
	On(Event, ...string) error

	// Trigger an event
	Trigger(string)
}

// A "Daemon" that Triggers events on an emitter
type Source interface {
	Init(string, Emitter) error

	Start()
}

/////////////////////////////////////////////////////////////

type Signal int

const (
	SIGSELF Signal = iota
	SIGSTART
	SIGSUCCESS
	SIGFAIL
	SIGKILLED
)

var signals = []string{"", "-start", "-success", "-fail", "-killed"}

const (
	TRIGTERM = "terminate"
)

type Node interface {
	Event

	Init(string, Emitter) error
	SetIO(io.Writer, io.Writer)

	Start() error
	Stop() error

	ID() string

	Signals() []string
}

type node struct {
	id string
	e  Emitter

	Stdout io.Writer
	Stderr io.Writer
}

func (n *node) Init(id string, e Emitter) (err error) {
	n.id = id
	n.e = e
	return
}

func (n *node) SetIO(stdout, stderr io.Writer) {
	n.Stdout = stdout
	n.Stderr = stderr
}

func (n *node) signal(s Signal) {
	n.e.Trigger(n.id + signals[s])
}

func (n *node) Signals() (ret []string) {
	ret = make([]string, len(signals))

	for i := range signals {
		ret[i] = n.id + signals[i]
	}

	return
}

func (n *node) Start() error {
	return nil
}

func (n *node) Stop() error {
	return nil
}

func (n *node) ID() string {
	return n.id
}

func (n *node) Run(id string) error {
	return nil
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

	// Stdout io.Writer
	// Stderr io.Writer

	IsDaemon bool

	cmd *exec.Cmd
	wg  sync.WaitGroup

	// BaseSource

	node
}

// func (c *CommandEvent) Init(id string, e Emitter) (err error) {
// 	err = c.node.Init(id, e)
// 	if err != nil {
// 		return
// 	}

// 	// sources.Add(c)
// 	return
// }

// func (c *CommandEvent) Start() {

// }

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

	c.signal(SIGSTART)
	err = c.cmd.Wait()
	retsignal := SIGSUCCESS

	// Determine the exit code
	if err != nil {
		ret := 1
		if ee, ok := err.(*exec.ExitError); ok {
			if stat, ok := ee.Sys().(syscall.WaitStatus); ok {
				ret = stat.ExitStatus()
			}
		}
		err = util.E.Annotate(err, "Running a command failed with:", ret)
		retsignal = SIGFAIL
	}
	c.wg.Done()
	c.signal(retsignal)

	return
}

func (c *CommandEvent) killCmd() (err error) {
	if c.cmd != nil && c.cmd.Process != nil {
		err = c.cmd.Process.Kill()
		c.signal(SIGKILLED)
		fmt.Println("Killing process")
	}
	return
}

func (c *CommandEvent) Run(ID string) (err error) {
	fmt.Println("Arguments", c.Args)

	switch ID {
	case TRIGTERM:
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
// type sourceList struct {
// 	sources []Source
// }

// func (s *sourceList) Add(src Source) {
// 	s.sources = append(s.sources, src)
// 	fmt.Println("Adding", src)
// }

// func (s *sourceList) Start() {
// 	for _, src := range s.sources {
// 		fmt.Println("Starting", src)
// 		src.Start()
// 	}
// }

// var sources sourceList

/////////////////////////////////////////////////////////////

// type BaseSource struct {
// 	id string
// 	e  Emitter
// }

// func (o *BaseSource) Init(id string, e Emitter) (err error) {
// 	o.id = id
// 	o.e = e
// 	return
// }

/////////////////////////////////////////////////////////////

type OnceSource struct {
	// BaseSource
	node
}

func (o *OnceSource) Start() error {
	o.signal(SIGSELF)
	return nil
}

/////////////////////////////////////////////////////////////

type TerminateEvent struct {
	terminate chan bool

	node
}

func (t *TerminateEvent) Run(id string) (err error) {
	if t.terminate == nil {
		return
	}

	switch id {
	case TRIGTERM:
		t.terminate <- true
	}

	return
}

func (t *TerminateEvent) Init(id string, e Emitter) (err error) {
	err = t.node.Init(id, e)
	if err != nil {
		return
	}

	if t.terminate == nil {
		t.terminate = make(chan bool)
	}
	return
}

/////////////////////////////////////////////////////////////

type FileChangeSource struct {
	Patterns []string

	// Wait this amount of time from the file change to the actual
	// triggering
	Hysteresis time.Duration

	watch *fsnotify.Watcher
	files []string

	// BaseSource
	// node
	TerminateEvent
}

func (s *FileChangeSource) Init(id string, e Emitter) (err error) {
	err = s.TerminateEvent.Init(id, e)
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

	// sources.Add(s)

	return
}

func (s *FileChangeSource) Start() (err error) {

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
				s.signal(SIGSELF)
				// s.e.Trigger(s.id)
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

	TerminateEvent
}

func (s *SignalSource) Init(ID string, e Emitter) (err error) {
	err = s.TerminateEvent.Init(TRIGTERM, e)
	if err != nil {
		return
	}

	s.ch = make(chan os.Signal, 1)
	// sources.Add(s)
	signal.Notify(s.ch, s.Signal)

	return
}

func (s *SignalSource) Start() error {

	go func() {
	loop:
		for {
			select {
			case sig := <-s.ch:
				fmt.Println("Received signal:", sig, "Triggering", s.id)
				s.signal(SIGSELF)
			case <-s.terminate:
				signal.Reset(s.Signal)
				break loop
			}
		}
	}()
	return nil
}

/////////////////////////////////////////////////////////////

type TerminateBlocker struct {
	TerminateEvent
}

func (t *TerminateBlocker) Wait(e Emitter) (err error) {
	err = t.TerminateEvent.Init("", e)
	if err != nil {
		return
	}
	e.On(t, TRIGTERM)
	<-t.terminate

	return
}

/////////////////////////////////////////////////////////////

type Handler struct {
	emt   Emitter
	nodes []Node
	out   Output
}

func (h *Handler) JoinNodes(nodes ...Node) (err error) {
	var prev Node
	prev = nil

	for _, n := range nodes {
		err = n.Init(n.ID(), h.emt)
		if err != nil {
			err = util.E.Annotate(err, "Initializating node", n.ID(), "failed")
			return
		}
		n.SetIO(h.out.Stdout(), h.out.Stderr())

		h.emt.On(n, TRIGTERM)

		if prev != nil {
			h.emt.On(n, prev.Signals()...)
		}

		h.nodes = append(h.nodes, n)
		prev = n
	}

	for _, n := range nodes {
		_ = n.Start()
	}

	return
}

/////////////////////////////////////////////////////////////

func WaitOnInput() {
	var input string
	fmt.Scanln(&input)
}

func TestRun(opts util.Options) (err error) {
	cfgs, err := LoadConfigs(opts)
	if err != nil {
		err = util.E.Annotate(err, "Loading configurations failed")
		return
	}

	fmt.Println(cfgs)

	handler := &Handler{
		emt: &emitter{},
		out: &output{
			out: os.Stdout,
			err: os.Stderr,
		},
	}

	sig := &SignalSource{
		Signal: os.Interrupt,
	}
	sig.id = TRIGTERM

	err = handler.JoinNodes(sig)
	if err != nil {
		err = util.E.Annotate(err, "Initializing signal source failed")
		return
	}

	for _, cfg := range cfgs {
		var source Node
		if cfg.Exec == "" {
			continue
		}

		if cfg.Pattern != "" {
			fcs := &FileChangeSource{
				Patterns: strings.Split(cfg.Pattern, " "),
			}
			fcs.id = cfg.Name
			source = fcs
		} else {
			os := &OnceSource{}
			os.id = cfg.Name
			source = os
		}

		target := &CommandEvent{
			Args:     strings.Split(cfg.Exec, " "),
			IsDaemon: cfg.IsDaemon,
		}
		target.id = cfg.Name + "-cmd"

		err = handler.JoinNodes(source, target)
		if err != nil {
			err = util.E.Annotate(err, "Initializing configuration", cfg.Name, "failed")
			return
		}
	}

	// tb := &TerminateBlocker{}
	// tb.Wait(e)
	WaitOnInput()

	handler.emt.Trigger(TRIGTERM)
	return
}

// func TestRun2(opts util.Options) (err error) {

// 	cfgs, err := LoadConfigs(opts)
// 	if err != nil {
// 		err = util.E.Annotate(err, "Loading configurations failed")
// 		return
// 	}

// 	fmt.Println(cfgs)

// 	e := &emitter{}

// 	ss := &SignalSource{
// 		Signal: os.Interrupt,
// 	}

// 	ss.Init(e)
// 	// ss.Start()

// 	for _, cfg := range cfgs {
// 		var source Source

// 		if cfg.Exec == "" {
// 			continue
// 		}

// 		if cfg.Pattern != "" {
// 			source = &FileChangeSource{
// 				Patterns: strings.Split(cfg.Pattern, " "),
// 			}
// 		} else {
// 			// source = &OnceSource{}
// 		}

// 		source.Init(cfg.Name, e)

// 		fmt.Println("Name", cfg.Name, "Exec", strings.Split(cfg.Exec, " "))

// 		target := &CommandEvent{
// 			Args:     strings.Split(cfg.Exec, " "),
// 			IsDaemon: cfg.IsDaemon,
// 		}
// 		target.Stdout = os.Stdout
// 		target.Stderr = os.Stderr
// 		target.Init(cfg.Name+"-cmd", e)

// 		e.On(target, cfg.Name, TRIGTERM)
// 		// source.Start()
// 	}

// 	sources.Start()

// 	tb := &TerminateBlocker{}
// 	tb.Wait(e)
// 	// WaitOnInput()

// 	e.Trigger(TRIGTERM)

// 	return
// }
