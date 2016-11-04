package pawnd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	util "github.com/kopoli/go-util"
	fsnotify "gopkg.in/fsnotify.v1"
)

type Link interface {

	// Registers the Link that triggers this one
	RegisterTrigger(Link) error

	// Registers the Link that this reports its readiness
	RegisterReady(Link) error

	// in on valmis
	Trigger(Trigger)

	// out on valmis
	Ready(Trigger)

	// Starts the link functionality
	Open() error

	// Stops the link functionality
	io.Closer

	// Name of the link for debugging
	Name() string
}

/////////////////////////////////////////////////////////////

// Basic functionality for a link
type BaseLink struct {
	in  Link
	out Link

	trigger chan Trigger
	ready   chan Trigger

	closeWg sync.WaitGroup
	close   chan Trigger

	name string
}

func (b *BaseLink) Name() string {
	return b.name
}

func (b *BaseLink) RegisterTrigger(out Link) (err error) {
	b.out = out
	return
}

func (b *BaseLink) RegisterReady(in Link) (err error) {
	b.in = in
	return
}

// func (b *BaseLink) Join(in Link, out Link) (err error) {

// 	fmt.Println("Joining", in, "and", out)
// 	if b.in != nil || b.out != nil {
// 		err = util.E.New("Links already registered.")
// 		return
// 	}

// 	b.in = in
// 	b.out = out

// 	return
// }

func (b *BaseLink) doTrigger(t Trigger) {
	fmt.Println("Would call trigger", b.out)
	if b.out != nil {
		b.out.Trigger(t)
	}
}

func (b *BaseLink) doReady(t Trigger) {
	if b.out != nil {
		b.in.Ready(t)
	}
}

func (b *BaseLink) createTriggerChannel() {
	if b.trigger == nil {
		b.trigger = make(chan Trigger)
	}
}

func (b *BaseLink) Trigger(t Trigger) {
	fmt.Println("Received trigger call,", b, "channel", b.trigger)
	if b.trigger != nil {
		b.trigger <- t
	}
}

func (b *BaseLink) createReadyChannel() {
	if b.ready == nil {
		b.ready = make(chan Trigger)
	}
}

func (b *BaseLink) Ready(t Trigger) {
	if b.ready != nil {
		b.ready <- t
	}
}
func (b *BaseLink) createCloseChannel() {
	if b.close == nil {
		b.close = make(chan Trigger)
	}
}

// Close the link and wait until it has stopped
func (b *BaseLink) Close() (err error) {
	if b.close == nil {
		err = util.E.New("Close channel hasn't been initialized")
		return
	}

	b.closeWg.Add(1)
	b.close <- Trigger{}
	b.closeWg.Wait()

	return
}

/////////////////////////////////////////////////////////////

type FileChangeLink struct {
	Patterns []string

	// Wait this amount of time from the file change to the actual
	// triggering
	Hysteresis time.Duration

	BaseLink
}

func (l *FileChangeLink) Open() (err error) {

	l.name = "FileChange"

	files := getFileList(l.Patterns)
	if len(files) == 0 {
		err = util.E.New("No watched files found")
		return
	}

	fmt.Println("Files", files)

	if l.Hysteresis == 0 {
		l.Hysteresis = time.Millisecond * 500
	}

	// Create a watcher
	watch, err := fsnotify.NewWatcher()
	if err != nil {
		err = util.E.Annotate(err, "Creating a file watcher failed")
		return
	}

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
		for _, p := range l.Patterns {
			m, er := path.Match(filepath.Base(p), file)
			if m && er == nil {
				return true
			}
		}
		return false
	}

	l.createCloseChannel()

	go func() {
		defer watch.Close()

		threshold := time.NewTimer(0)
		stopTimer(threshold)

		for _, name := range files {
			err = watch.Add(name)
			if err != nil {
				fmt.Println("Could not watch", name)
			}
		}

	loop:
		for {
			select {
			case <-threshold.C:
				fmt.Println("Would send an event")
				l.doTrigger(Trigger{})
			case event := <-watch.Events:
				fmt.Println("Event received:", event)
				if matchPattern(event.Name) {
					fmt.Println("Pattern matched.")
					stopTimer(threshold)
					threshold.Reset(l.Hysteresis)
				}

			case err := <-watch.Errors:
				fmt.Println("Error received", err)

			case <-l.close:
				break loop
			}
		}
		l.closeWg.Done()
	}()

	return
}

/////////////////////////////////////////////////////////////

type CommandLink struct {
	Args []string

	CoolDown time.Duration

	Stdout io.Writer
	Stderr io.Writer

	IsDaemon bool

	BaseLink
}

func (c *CommandLink) Open() (err error) {
	fmt.Println("Arguments", c.Args)
	if len(c.Args) == 0 || c.Args[0] == "" {
		err = util.E.New("Invalid arguments to run a command")
		return
	}

	c.name = "Command"

	var cmd *exec.Cmd

	wg := sync.WaitGroup{}

	runCmd := func() {
		cmd = exec.Command(c.Args[0], c.Args[1:]...)
		cmd.Stdout = c.Stdout
		cmd.Stderr = c.Stderr
		err := cmd.Start()
		if err != nil {
			cmd = nil
			util.E.Print(err, "Starting command failed: ", c.Args)
			return
		}
		err = cmd.Wait()

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
		c.doReady(Trigger{err: err})
		wg.Done()
	}

	killCmd := func() (err error) {
		if cmd != nil && cmd.Process != nil {
			err = cmd.Process.Kill()
		}
		return
	}

	c.createCloseChannel()
	c.createTriggerChannel()

	fmt.Println("Starting command executor")

	go func() {
	loop:
		for {
			select {
			case <-c.trigger:
				fmt.Println("Received trigger to run a command")
				if c.IsDaemon {
					_ = killCmd()
				} else {
					wg.Wait()
				}

				// Run the command
				wg.Add(1)
				go runCmd()

			case <-c.close:
				_ = killCmd()
				break loop
			}
		}
		c.closeWg.Done()
	}()

	return
}

/////////////////////////////////////////////////////////////

type OnceLink struct {
	BaseLink
}

func (o *OnceLink) Open() (err error) {
	fmt.Println("Triggering with the OnceLink")
	o.doTrigger(Trigger{})
	return
}

/////////////////////////////////////////////////////////////

func Run(opts util.Options) (err error) {

	cfgs, err := LoadConfigs(opts)
	if err != nil {
		util.E.Annotate(err, "Loading configurations failed")
		return
	}

	fmt.Println(cfgs)

	links := make([]Link, len(cfgs)*2)

	for _, cfg := range cfgs {
		var source, target Link

		if cfg.Exec == "" {
			continue
		}

		if cfg.Pattern != "" {
			source = &FileChangeLink{
				Patterns: strings.Split(cfg.Pattern, " "),
			}
		} else {
			source = &OnceLink{}
		}

		fmt.Println("Name", cfg.Name, "Exec", strings.Split(cfg.Exec, " "))

		target = &CommandLink{
			Args:     strings.Split(cfg.Exec, " "),
			Stdout:   os.Stdout,
			Stderr:   os.Stderr,
			IsDaemon: cfg.IsDaemon,
		}

		err = Join(source, target)
		if err != nil {
			util.E.Annotate(err, "Joining chain", cfg.Name, "Failed")
			return
		}

		links = append(links, source, target)
	}

	return
}

func Join(links ...Link) (err error) {
	if links == nil || len(links) <= 1 {
		err = util.E.New("Invalid arguments")
		return
	}

	fmt.Println("links on", links)

	var prev Link
	prev = nil

	for _, l := range links {
		if prev != nil {
			prev.RegisterTrigger(l)
			l.RegisterReady(prev)
		}
		prev = l
	}

	for i, l := range links {
		err = l.Open()
		if err != nil {
			err = util.E.Annotate(err, "Could not Open Link chain")
			for i = i - 1; i >= 0; i-- {
				l.Close()
			}
			return
		}
	}

	return
}

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

	fmt.Println("Event list", e.events[ID])
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
	wg sync.WaitGroup
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
	// c.doReady(Trigger{err: err})
	c.wg.Done()

	return
}

func (c *CommandEvent) killCmd() (err error) {
	if c.cmd != nil && c.cmd.Process != nil {
		err = c.cmd.Process.Kill()
	}
	return
}


func (c *CommandEvent) Run(ID string) (err error) {
	fmt.Println("Arguments", c.Args)
	fmt.Println("Starting command executor")

	switch ID {
	case "terminate":
		_ = c.killCmd()
	default:
		if c.IsDaemon {
			_ = c.killCmd()
		} else {
			c.wg.Wait()
		}
		_ = c.runCmd()
	}

	return
}

/////////////////////////////////////////////////////////////

type OnceSource struct {
	id string
	e Emitter
}

func (o *OnceSource) Init(id string, e Emitter) (err error) {
	o.id = id
	o.e = e
	return
}

func (o *OnceSource) Start() {
	o.e.Trigger(o.id)
}


/////////////////////////////////////////////////////////////



/////////////////////////////////////////////////////////////

func TestRun(opts util.Options) (err error) {

	cfgs, err := LoadConfigs(opts)
	if err != nil {
		util.E.Annotate(err, "Loading configurations failed")
		return
	}

	fmt.Println(cfgs)

	e := &emitter{}

	for _, cfg := range cfgs {
		var source Source

		if cfg.Exec == "" {
			continue
		}

		// if cfg.Pattern != "" {
		// 	source = &FileChangeLink{
		// 		Patterns: strings.Split(cfg.Pattern, " "),
		// 	}
		// } else {
			source = &OnceSource{}
		// }

		source.Init(cfg.Name, e)

		fmt.Println("Name", cfg.Name, "Exec", strings.Split(cfg.Exec, " "))

		target := &CommandEvent{
			Args:     strings.Split(cfg.Exec, " "),
			Stdout:   os.Stdout,
			Stderr:   os.Stderr,
			IsDaemon: cfg.IsDaemon,
		}

		e.On(target, cfg.Name, "terminate")
		source.Start()

		// err = Join(source, target)
		// if err != nil {
		// 	util.E.Annotate(err, "Joining chain", cfg.Name, "Failed")
		// 	return
		// }

		// links = append(links, source, target)
	}

	var input string
	fmt.Scanln(&input)

	e.Trigger("terminate")

	return
}
