package pawnd

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"time"

	cursor "github.com/ahmetalpbalkan/go-cursor"
	colorable "github.com/mattn/go-colorable"
	"github.com/mgutz/ansi"
)

var (
	spinner = `-/|\`

	//
	statusRun  = "run"
	statusOk   = "ok"
	statusFail = "fail"

	//
	infoDaemon = "daemon"
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
	SetStatus(status string, info string)
}

// the singleton termaction
var termOutput *TerminalOutput

type TerminalOutput struct {
	updateInterval time.Duration
	out        io.Writer
	buffer     *termWriter
	terminals  []*terminal
	Width      int
	Verbose    bool

	defaultTerm Terminal

	termchan  chan bool
	readychan chan bool

	initialized bool
}

func NewTerminalOutput() *TerminalOutput {
	if termOutput != nil {
		return termOutput
	}

	readychan := make(chan bool)

	var ret = &TerminalOutput{
		updateInterval: time.Second * 2,
		out:        colorable.NewColorableStdout(),
		Width:      60,
		Verbose:    false,
		readychan:  readychan,
		buffer: &termWriter{
			ready: readychan,
			out:   &bytes.Buffer{},
		},
		termchan: make(chan bool),
	}

	go func() {
		drawTimer := time.NewTimer(ret.updateInterval)
	loop:
		for {
			select {
			case <-drawTimer.C:
				ret.updateSpinners()
				ret.draw()
				stopTimer(drawTimer)
				drawTimer.Reset(ret.updateInterval)
			case <-ret.readychan:
				ret.draw()
			case <-ret.termchan:
				break loop
			}
		}
	}()

	termOutput = ret
	ret.defaultTerm = RegisterTerminal("init", false)
	ret.terminals = nil

	return termOutput
}

// Stop TerminalOutput. This cannot be stopped with the MsgTerm message as some
// other actions can print while they are terminating.
func (a *TerminalOutput) Stop() {
	a.termchan <- true
}

func GetTerminal(name string) Terminal {
	if name == "" {
		return termOutput.defaultTerm
	}
	for i := range termOutput.terminals {
		if name == termOutput.terminals[i].Name {
			return termOutput.terminals[i]
		}
	}
	return termOutput.defaultTerm
}

// updateProgress updates the progress-bars and spinners
func (a *TerminalOutput) updateSpinners() {
	for _, t := range a.terminals {
		if t.Progress < 0 {
			t.Progress = -((-t.Progress + 1) % (len(spinner) + 1))
			if t.Progress == 0 {
				t.Progress = -1
			}
		}
	}
}

func formatStatus(status, name string) string {
	switch status {
	case statusRun:
		status = ansi.ColorCode("yellow+h") + "RUN " + ansi.Reset
	case statusOk:
		status = ansi.ColorCode("green+h") + "OK  " + ansi.Reset
	case statusFail:
		status = ansi.ColorCode("red+h") + "FAIL" + ansi.Reset
	case "":
		status = ansi.ColorCode("grey+h") + "WAIT" + ansi.Reset
	}
	return fmt.Sprintf("[%s][%s] ", status, name)
}

func drawProgressBar(width int, progress int, out *bytes.Buffer) {
	out.WriteByte('[')
	fillwidth := width * progress / 100
	if progress >= 0 {
		for i := 0; i < fillwidth-1; i++ {
			out.WriteByte('=')
		}
		if progress < 100 {
			out.WriteByte('>')
		}
	}

	for i := 0; i < width-fillwidth; i++ {
		out.WriteByte('-')
	}
	out.WriteByte(']')
}

func drawStatus(t *terminal, maxwidth int, out *bytes.Buffer) {
	badge := formatStatus(t.Status, t.Name)
	out.WriteString(badge)
	maxwidth -= 4 + 4 + len(t.Name)
	switch {
	case t.Progress == 100 && t.Info != "":
		fmt.Fprintf(out, "%s", t.Info)
	case t.Progress >= 0:
		drawProgressBar(maxwidth, t.Progress, out)
	default:
		out.WriteByte(spinner[(t.Progress*-1)%len(spinner)])
	}
}

func (a *TerminalOutput) draw() {
	tmp := &bytes.Buffer{}

	if !a.initialized {
		// Make initial vertical space
		for i := range a.terminals {
			if a.terminals[i].Visible {
				tmp.WriteByte('\n')
			}
		}
		a.initialized = true
	}

	// Clear status lines
	for i := range a.terminals {
		if a.terminals[i].Visible {
			fmt.Fprintf(tmp, "%s%s\r", cursor.MoveUp(1),
				cursor.ClearEntireLine())
		}
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
		if a.terminals[i].Visible {
			drawStatus(a.terminals[i], a.Width, tmp)
			tmp.WriteByte('\n')
		}
	}
	tmp.WriteTo(a.out)
}

///

// VerboseWriter writes to Out only if Verbose is set.
type VerboseWriter struct {
	Out     io.Writer
	Verbose bool
}

func (w *VerboseWriter) Write(buf []byte) (int, error) {
	if w.Verbose {
		return w.Out.Write(buf)
	}
	return len(buf), nil
}

// terminal is the internal structure behinde the Terminal interface.
type terminal struct {
	Name     string
	Status   string // Current status of the process
	Info     string // additional info of the status
	Progress int    // progress bar from 0 - 100 or negative for a spinner
	Visible  bool   // Is a statusbar visible

	out     *PrefixedWriter
	err     *PrefixedWriter
	verbose *VerboseWriter

	runtime   time.Duration
	startTime time.Time
	runtimer  *time.Timer
}

// RegisterTerminal registers an interface to outputting
func RegisterTerminal(name string, visible bool) Terminal {
	prefix := fmt.Sprintf("[%s%s%s] ", ansi.ColorCode("default+hb"), name, ansi.Reset)
	var ret = terminal{
		Name:    name,
		Visible: visible,
		out:     NewPrefixedWriter(prefix, "", termOutput.buffer),
		err:     NewPrefixedWriter(prefix, "red", termOutput.buffer),
	}
	ret.verbose = &VerboseWriter{ret.out, termOutput.Verbose}
	termOutput.terminals = append(termOutput.terminals, &ret)
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

func (t *terminal) SetStatus(status string, info string) {
	t.Status = status
	t.Info = info

	switch status {
	case statusRun:
		t.startTime = time.Now()
		t.Progress = 0
		if info == infoDaemon {
			t.Progress = -1
		} else {
			redrawtime := time.Millisecond * 200
			t.runtimer = time.NewTimer(redrawtime)
			go func() {
				for range t.runtimer.C {
					t.runtimer.Reset(redrawtime)

					curduration := time.Since(t.startTime)
					if curduration >= t.runtime {
						t.Progress = 100
					} else {
						t.Progress = int((curduration * 100 / t.runtime))
					}
					termOutput.readychan <- true
				}
			}()
		}
	case statusFail:
		fallthrough
	case statusOk:
		t.runtime = time.Since(t.startTime)
		t.Progress = 100
		if t.runtimer != nil {
			stopTimer(t.runtimer)
		}
	}

	termOutput.readychan <- true
}

type PrefixedWriter struct {
	Prefix []byte
	Eol    []byte
	Out    io.Writer
	TimeStamp bool
	buf    *bytes.Buffer
}

func NewPrefixedWriter(prefix string, style string, out io.Writer) *PrefixedWriter {
	return &PrefixedWriter{
		Prefix: []byte(prefix + ansi.ColorCode(style)),
		Eol:    []byte("" + ansi.Reset + "\n"),
		Out:    out,
		TimeStamp: true,
		buf:    &bytes.Buffer{},
	}
}

// Write given buf with a prefix to the Out writer. Only write lines ending
// with a newline. If the input data doesn't contain a newline, the data is
// written to an internal buffer which is flushed the next time data with
// newline is given.
func (p *PrefixedWriter) Write(buf []byte) (n int, err error) {

	// If no bytes to write
	if len(buf) == 0 {
		return 0, nil
	}

	n = len(buf)

	stamp := ""
	if p.TimeStamp {
		stamp = time.Now().Format("Jan _2 15:04:05.000: ")
	}

	// If only one line to write without newline
	lastLineIdx := bytes.LastIndexByte(buf, '\n')
	if lastLineIdx < 0 {
		// If there is nothing in the buffer
		if p.buf.Len() == 0 {
			p.buf.WriteString(stamp)
			p.buf.Write(p.Prefix)
		}

		// Write only into the buffer
		p.buf.Write(buf)
		return n, nil
	}

	endsInNewline := (buf[len(buf)-1] == '\n')
	lines := bytes.Split(buf, []byte{'\n'})

	// If given data ends in newline, skip the last line
	if endsInNewline {
		lines = lines[:len(lines)-1]
	}

	// fmt.Printf("Lines on [%v] endsinnewline %v \n",lines, endsInNewline)
	for i := range lines {
		// If either not first line or first and nothing in buffer
		if i > 0 || (i == 0 && p.buf.Len() == 0) {
			p.buf.WriteString(stamp)
			p.buf.Write(p.Prefix)
		}

		// If either not last line or last line when ends in a newline
		if i < len(lines)-1 || (i == len(lines)-1 && endsInNewline) {
			p.buf.Write(lines[i])
			p.buf.Write(p.Eol)
		}
	}

	// Write to output
	p.buf.WriteTo(p.Out)

	// Write the last line to buffer for next time if newline isn't present
	if !endsInNewline {
		p.buf.Write(lines[len(lines)-1])
	}

	return n, nil
}
