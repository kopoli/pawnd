package pawnd

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	cursor "github.com/ahmetalpbalkan/go-cursor"
	tsize "github.com/kopoli/go-terminal-size"
	util "github.com/kopoli/go-util"
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
	out            io.Writer
	buffer         *termWriter
	terminals      []*terminal
	Width          int
	Verbose        bool
	TitleStatus    string
	ProgTitle      string

	defaultTerm Terminal

	termchan  chan bool
	readychan chan bool

	initialized bool
}

func NewTerminalOutput(opts util.Options) *TerminalOutput {
	if termOutput != nil {
		return termOutput
	}

	readychan := make(chan bool)

	var ret = &TerminalOutput{
		updateInterval: time.Second * 2,
		out:            colorable.NewColorableStdout(),
		Width:          60,
		Verbose:        opts.IsSet("verbose"),
		ProgTitle:      opts.Get("program-real-name", "pawnd"),
		readychan:      readychan,
		buffer: &termWriter{
			ready: readychan,
			out:   &bytes.Buffer{},
		},
		termchan: make(chan bool),
	}

	sl, slerr := tsize.NewSizeListener()
	limit := 4

	s, err := tsize.GetSize()
	if err == nil {
		ret.Width = s.Width - limit
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
			case s := <-sl.Change:
				// data race condition ?
				ret.Width = s.Width - limit
			case <-ret.termchan:
				break loop
			}
		}
	}()

	termOutput = ret
	ret.defaultTerm = RegisterTerminal("init", false)
	ret.terminals = nil

	// Print out the size listener error when the terminal is ready
	if slerr != nil {
		fmt.Fprintln(ret.defaultTerm.Stderr(),
			"Could not start terminal size listener:", err)
	}

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
		t.statusMutex.Lock()
		progress := t.Progress
		t.statusMutex.Unlock()
		if progress < 0 {
			progress = -((-progress + 1) % (len(spinner) + 1))
			if progress == 0 {
				progress = -1
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
	// There must be space at least for: [=]
	if width < 3 {
		width = 3
	}

	if progress < 0 {
		progress = 0
	} else if progress > 100 {
		progress = 100
	}

	width -= 2

	fillwidth := int(math.Ceil(float64(width) * float64(progress) / 100))

	out.WriteByte('[')
	if fillwidth > 0 {
		for i := 0; i < fillwidth-1; i++ {
			out.WriteByte('=')
		}
		if progress < 100 {
			out.WriteByte('>')
		} else {
			out.WriteByte('=')
		}
	}

	for i := 0; i < width-fillwidth; i++ {
		out.WriteByte('-')
	}
	out.WriteByte(']')
}

func drawStatus(t *terminal, maxwidth int, out *bytes.Buffer) string {
	t.statusMutex.Lock()
	status := t.Status
	info := t.Info
	progress := t.Progress
	t.statusMutex.Unlock()

	badge := formatStatus(status, t.Name)
	out.WriteString(badge)
	maxwidth -= 4 + 4 + len(t.Name)
	switch {
	case progress == 100 && info != "":
		fmt.Fprintf(out, "%s", info)
	case progress >= 0:
		drawProgressBar(maxwidth, progress, out)
	default:
		out.WriteByte(spinner[(progress*-1)%len(spinner)])
	}

	return status
}

func determineTitleStatus(wholeStatus, singleStatus string) string {

	switch wholeStatus {

	case statusRun:
		fallthrough
	case statusFail:
		return wholeStatus
	}

	switch singleStatus {
	case statusFail:
		return singleStatus
	case "":
		return wholeStatus
	}

	return singleStatus
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

	status := statusOk
	// Print the status lines
	for i := range a.terminals {
		if a.terminals[i].Visible {
			st := drawStatus(a.terminals[i], a.Width, tmp)
			tmp.WriteByte('\n')

			status = determineTitleStatus(status, st)
		}
	}

	// Write the title if it has changed
	if status != a.TitleStatus {
		fmt.Fprintf(tmp, "\033]0;%s[%s]\007", a.ProgTitle, status)
		a.TitleStatus = status
	}
	tmp.WriteTo(a.out)
}

func (t *TerminalOutput) Draw() {
	// When Draw is called, it is regarded as initial run
	t.initialized = false
	t.readychan <- true
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

	progressStopChan chan bool
	statusMutex      sync.Mutex

	out     *PrefixedWriter
	err     *PrefixedWriter
	verbose *VerboseWriter

	runtime   time.Duration
	startTime time.Time
}

// RegisterTerminal registers an interface to outputting
func RegisterTerminal(name string, visible bool) Terminal {
	prefix := fmt.Sprintf("[%s%s%s] ", ansi.ColorCode("default+hb"), name, ansi.Reset)
	var ret = terminal{
		Name:             name,
		Visible:          visible,
		out:              NewPrefixedWriter(prefix, termOutput.buffer),
		err:              NewPrefixedWriter(prefix+ansi.ColorCode("red"), termOutput.buffer),
		progressStopChan: make(chan bool),
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
	var progress int

	drainChan := func(ch chan bool) {
		select {
		case <-ch:
		default:
		}
	}

	switch status {
	case statusRun:
		t.startTime = time.Now()
		progress = 0
		if info == infoDaemon {
			progress = -1
		} else {
			redrawtime := time.Millisecond * 200
			go func() {
				drainChan(t.progressStopChan)
				runtimer := time.NewTimer(redrawtime)
			loop:
				for {
					select {
					case <-runtimer.C:
						var progress int
						runtimer.Reset(redrawtime)

						t.statusMutex.Lock()
						curduration := time.Since(t.startTime)
						if curduration >= t.runtime {
							progress = 100
						} else {
							progress = int((curduration * 100 / t.runtime))
						}
						t.Progress = progress
						t.statusMutex.Unlock()

						termOutput.readychan <- true
					case <-t.progressStopChan:
						stopTimer(runtimer)
						break loop
					}
				}
			}()
		}
	case statusFail:
		fallthrough
	case statusOk:
		t.statusMutex.Lock()
		t.runtime = time.Since(t.startTime)
		t.statusMutex.Unlock()
		progress = 100

		select {
		case t.progressStopChan <- true:
		default:
		}
	}

	t.statusMutex.Lock()
	t.Status = status
	t.Info = info
	t.Progress = progress
	t.statusMutex.Unlock()

	termOutput.readychan <- true
}

// PrefixedWriter is an io.Writer that prefixes and suffixes all lines given
// to it.
type PrefixedWriter struct {
	Prefix    []byte        // Prefix to add to each line
	Eol       []byte        // Suffix to add each line
	Out       io.Writer     // Write everything to this writer.
	TimeStamp bool          // Add timestamps to output
	buf       *bytes.Buffer // buffer to house incomplete lines

	mutex sync.Mutex
}

// NewPrefixedWriter create a PrefixedWriter with given prefix and write
// everything to out.
func NewPrefixedWriter(prefix string, out io.Writer) *PrefixedWriter {
	return &PrefixedWriter{
		Prefix:    []byte(prefix),
		Eol:       []byte("" + ansi.Reset + "\n"),
		Out:       out,
		TimeStamp: true,
		buf:       &bytes.Buffer{},
	}
}

// Write writes given buf with a prefix to the Out writer. Only write lines
// ending with a newline. If the input data doesn't contain a newline, the
// data is written to an internal buffer which is flushed the next time data
// with newline is given.
func (p *PrefixedWriter) Write(buf []byte) (n int, err error) {
	// If no bytes to write
	if len(buf) == 0 {
		return 0, nil
	}

	p.mutex.Lock()
	defer p.mutex.Unlock()

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
