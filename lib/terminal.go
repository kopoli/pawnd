package pawnd

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"time"

	cursor "github.com/ahmetalpbalkan/go-cursor"
	colorable "github.com/mattn/go-colorable"
)

// Terminal output handling

// termWriter protects the single output buffer in TermAction. It also
// notifies when data is written into it.
type termWriter struct {
	out   *bytes.Buffer
	mutex sync.Mutex
	ready chan<- bool
}

func (w *termWriter) Write(buf []byte) (int, error) {
	w.mutex.Lock()
	defer func() {
		w.mutex.Unlock()
		w.ready <- true
	}()
	return w.out.Write(buf)
}

// Terminal is the interface for outputting data to a terminal
type Terminal interface {
	Stderr() io.Writer
	Stdout() io.Writer
	Verbose() io.Writer
	SetStatus(status string)
}

// the singleton termaction that can be registered to an EventBus
var termAction *TermAction

type TermAction struct {
	drawTicker *time.Ticker
	out        io.Writer
	buffer     *termWriter
	terminals  []*terminal
	Width      int
	Verbose    bool

	defaultTerm Terminal

	termchan  chan bool
	readychan chan bool

	initialized bool

	BaseAction
}

func NewTermAction() *TermAction {
	if termAction != nil {
		return termAction
	}

	readychan := make(chan bool)

	var ret = &TermAction{
		drawTicker: time.NewTicker(time.Millisecond * 3000),
		out:        colorable.NewColorableStdout(),
		Width:      50,
		Verbose:    false,
		readychan:  readychan,
		buffer: &termWriter{
			ready: readychan,
			out:   &bytes.Buffer{},
		},
		termchan: make(chan bool),
	}

	go func() {
	loop:
		for {
			select {
			case <-ret.drawTicker.C:
				ret.updateProgress()
				ret.draw()
			case <-ret.readychan:
				ret.draw()
			case <-ret.termchan:
				break loop
			}
		}
	}()

	termAction = ret
	ret.defaultTerm = RegisterTerminal("init")
	ret.terminals = nil

	return termAction
}

func (a *TermAction) Receive(from, message string) {
	switch message {
	case MsgInit:
		a.drawTicker.Stop()
		a.drawTicker = time.NewTicker(time.Millisecond * 500)
	case MsgTerm:
		a.termchan <- true
	}
}

func GetTerminal(name string) Terminal {
	if name == "" {
		return termAction.defaultTerm
	}
	for i := range termAction.terminals {
		if name == termAction.terminals[i].Name {
			return termAction.terminals[i]
		}
	}
	return termAction.defaultTerm
}

// updateProgress updates the progress-bars and spinners
func (a *TermAction) updateProgress() {
	for _, t := range a.terminals {
		if t.Progress < 0 {
			t.Progress = -((-t.Progress + 1) % (len(spinner) + 1))
			if t.Progress == 0 {
				t.Progress = -1
			}
		}
	}
}

func drawStatus(t *terminal, maxwidth int, out *bytes.Buffer) {
	fmt.Fprintf(out, "[%s][%s] ", t.Name, t.Status)
	if t.Progress >= 0 {
		out.WriteByte('[')
		fillwidth := maxwidth * t.Progress / 100
		if t.Progress > 0 {
			for i := 0; i < fillwidth-1; i++ {
				out.WriteByte('=')
			}
			if t.Progress < 100 {
				out.WriteByte('>')
			}
		}

		for i := 0; i < maxwidth-fillwidth; i++ {
			out.WriteByte('-')
		}
		out.WriteByte(']')
	} else {
		out.WriteByte(spinner[(t.Progress*-1)%len(spinner)])
	}
}

func (a *TermAction) draw() {
	tmp := &bytes.Buffer{}

	if !a.initialized {
		// Make initial vertical space
		for range a.terminals {
			tmp.WriteByte('\n')
		}
	}

	// Clear status lines
	for range a.terminals {
		fmt.Fprintf(tmp, "%s%s\r", cursor.MoveUp(1),
			cursor.ClearEntireLine())
	}

	// Get the trace output from registered Terminals
	a.buffer.mutex.Lock()
	trace := a.buffer.out.Bytes()
	if len(trace) > 0 {
		if trace[len(trace)-1] != '\n' {
			a.buffer.out.WriteByte('\n')
		}
		a.buffer.out.WriteTo(tmp)
	}
	a.buffer.mutex.Unlock()

	// Print the status lines
	for i := range a.terminals {
		drawStatus(a.terminals[i], a.Width, tmp)
		tmp.WriteByte('\n')
	}
	tmp.WriteTo(a.out)
	a.initialized = true
}

///

type VerboseWriter struct {
	out     io.Writer
	verbose bool
}

func (w *VerboseWriter) Write(buf []byte) (int, error) {
	if w.verbose {
		return w.out.Write(buf)
	}
	return len(buf), nil
}

type terminal struct {
	Name     string
	Status   string // Current status of the process
	Progress int    // progress bar from 0 - 100 or negative for a spinner

	out     *PrefixedWriter
	err     *PrefixedWriter
	verbose *VerboseWriter
}

//
func RegisterTerminal(name string) Terminal {
	name = fmt.Sprintf("[%s]", name)
	var ret = terminal{
		Name: name,
		out:  NewPrefixedWriter(name, "", termAction.buffer),
		err:  NewPrefixedWriter(name, "red", termAction.buffer),
	}
	ret.verbose = &VerboseWriter{ret.out, termAction.Verbose}
	termAction.terminals = append(termAction.terminals, &ret)
	return &ret
}

func (t *terminal) Stdout() io.Writer {
	return t.out
}

func (t *terminal) Stderr() io.Writer {
	return t.err
}

func (t *terminal) Verbose() io.Writer {
	return t.verbose
}

func (t *terminal) SetStatus(status string) {
	t.Status = status
}
