package pawnd

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	cursor "github.com/ahmetalpbalkan/go-cursor"
	colorable "github.com/mattn/go-colorable"
	"github.com/mgutz/ansi"

	"github.com/gosuri/uilive"

	"github.com/gosuri/uiprogress"
	util "github.com/kopoli/go-util"
)

/////////////////////////////////////////////////////////////

// type outWriter struct {
// 	ID   string
// 	Next io.Writer

// 	isBlocked bool
// }

// func (o *outWriter) Write(buf []byte) (n int, err error) {
// 	return o.Next.Write(buf)
// }

// func newOutWriter(ID string, next io.Writer) (ret *outWriter) {
// 	ret = &outWriter{
// 		ID:        ID,
// 		Next:      NewPrefixedWriter("["+ID+"] ", "", next),
// 		isBlocked: false,
// 	}
// 	return
// }

// Caset:
// 1. ei printata mitään paitsi jos komento feilaa. Sitten printataan koko hoito.
// 2. Printataan koko ajan

type Output interface {
	Register(Node) error
	Start() error

	Event
}

type outputWriter struct {
	ID  string
	out *output
}

func (o *outputWriter) Write(buf []byte) (n int, err error) {
	return o.out.WriteID(o.ID, buf)
}

type outputStatus struct {
	ID string

	Out *outputWriter
	Err *outputWriter

	Status   string // Current status of the process
	Progress int    // progress bar from 0 - 100 or negative for a spinner
}

type output struct {
	out io.Writer
	// err io.Writer
	emt Emitter

	width int

	Prefixer *PrefixedWriter
	outputs  []*outputStatus

	cmdOutputCount int
	firstIteration bool

	drawTicker *time.Ticker
	terminate  chan bool

	writeLock  sync.Mutex
	updateLock sync.Mutex
}

func newOutput(opts util.Options, emt Emitter) (ret *output) {
	ret = &output{
		out:       colorable.NewColorableStdout(),
		Prefixer:  NewPrefixedWriter("", "", nil),
		emt:       emt,
		width:     40,
		terminate: make(chan bool),
	}

	ret.Prefixer.Out = ret.out

	return
}

func (o *output) WriteID(ID string, buf []byte) (n int, err error) {
	o.writeLock.Lock()
	prefix := "[" + ID + "] "
	if strings.HasSuffix(ID, "-err") {
		prefix += ansi.ColorCode("red+b")
	}

	o.Prefixer.Prefix = []byte(prefix)
	n, err = o.Prefixer.Write(buf)
	o.writeLock.Unlock()

	o.update()
	return
}

var spinner = `-/|\`

func drawProgress(os *outputStatus, maxwidth int, out *bytes.Buffer) {
	id := strings.TrimSuffix(os.ID, "-cmd")
	fmt.Fprintf(out, "[%s][%s] ", id, os.Status)
	if os.Progress >= 0 {
		out.WriteByte('[')
		fillwidth := maxwidth * os.Progress / 100
		for i := 0; i < fillwidth-1; i++ {
			out.WriteByte('=')
		}
		out.WriteByte('>')

		for i := 0; i < maxwidth-fillwidth; i++ {
			out.WriteByte('-')
		}
		out.WriteByte(']')
	} else {
		out.WriteByte(spinner[(os.Progress*-1)%len(spinner)])
	}
}

func clearLine(out io.Writer) {
	line := make([]byte, 62)

	for n := range line {
		line[n] = ' '
	}
	fmt.Fprintf(out, "\r%s\r", line)
}

func (o *output) update() {
	o.updateLock.Lock()
	tmp := &bytes.Buffer{}

	if !o.firstIteration {
		// fmt.Fprintf(tmp, "%s", cursor.MoveUp(o.cmdOutputCount))
	} else {
		o.cmdOutputCount = 0
		for _, os := range o.outputs {
			if strings.HasSuffix(os.ID, "-cmd") {
				o.cmdOutputCount += 1
			}
		}
	}

	for _, os := range o.outputs {
		if !strings.HasSuffix(os.ID, "-cmd") {
			continue
		}

		// fmt.Fprintf(tmp, "%s\r", cursor.ClearEntireLine())
		fmt.Fprintf(tmp, "%s", cursor.MoveUp(o.cmdOutputCount + 1))
		clearLine(tmp)
		drawProgress(os, o.width, tmp)
		fmt.Fprintf(tmp, "\n")
		// fmt.Fprintf(tmp, "%s\n", cursor.MoveUp(o.cmdOutputCount + 1) + cursor.ClearLineRight())
	}

	tmp.WriteTo(o.out)
	o.firstIteration = false
	o.updateLock.Unlock()
}

func (o *output) Start() (err error) {
	o.drawTicker = time.NewTicker(time.Millisecond * 1000)
	o.firstIteration = true

	go func() {
	loop:
		for {
			select {
			case <-o.drawTicker.C:
				o.update()
			case <-o.terminate:
				o.drawTicker.Stop()
				break loop
			}
		}
	}()

	return
}

func (o *output) Register(node Node) (err error) {
	os := &outputStatus{
		ID: node.ID(),
		Out: &outputWriter{
			ID:  node.ID() + "-out",
			out: o,
		},
		Err: &outputWriter{
			ID:  node.ID() + "-err",
			out: o,
		},
		Progress: 45,
	}
	o.outputs = append(o.outputs, os)

	node.SetIO(os.Out, os.Err)
	return
}

func (o *output) Run(ID string) (err error) {
	fmt.Fprintln(o.out, "Output received trigger on:", ID)

	switch ID {
	case TRIGTERM:
		o.terminate <- true
	}
	return
}

/////////////////////////////////////////////////////////////

type PrefixedWriter struct {
	Prefix []byte
	Eol    []byte
	Out    io.Writer
}

func NewPrefixedWriter(prefix string, style string, out io.Writer) *PrefixedWriter {
	return &PrefixedWriter{
		Prefix: []byte(prefix + ansi.ColorCode(style)),
		Eol:    []byte("" + ansi.Reset + "\n"),
		Out:    out,
	}
}

func (p *PrefixedWriter) Write(buf []byte) (n int, err error) {
	var wr = func(buf []byte) bool {
		_, err = p.Out.Write(buf)
		if err != nil {
			return false
		}
		return true
	}

	n = len(buf)

	pos := -1
	for len(buf) > 0 {
		pos = bytes.IndexRune(buf, '\n')
		if pos == -1 {
			pos = len(buf) - 1
		}

		if !(wr(p.Prefix) && wr(buf[:pos]) && wr(p.Eol)) {
			return
		}

		buf = buf[pos+1:]
	}
	return
}

/////////////////////////////////////////////////////////////

func UiDemo(opts util.Options) {

	emt := &emitter{}

	o := newOutput(opts, emt)
	n := node{
		id: "Dips-cmd",
		e:  emt,
	}

	o.Register(&n)

	pos := 1
	for {
		pos = ((pos - 1) % 15)
		o.outputs[0].Status = "Something"
		o.outputs[0].Progress = pos

		o.update()

		<-time.After(500 * time.Millisecond)
		// WaitOnInput()
		// fmt.Println("JEJE!", pos)
	}
}

/////////////////////////////////////////////////////////////

func uiliveTest() {
	wr := uilive.New()
	wr.Start()

	for i := 0; i < 100; i++ {
		fmt.Fprintf(wr, "Jepjep %d / 100 jotain aika pitkää juttua\n", i)
		time.Sleep(time.Millisecond * 10)
	}
	wr.Stop()
}

func UiDemo2(opts util.Options) {

	line := &bytes.Buffer{}

	warn := ansi.ColorFunc("red+b:white")
	out := colorable.NewColorableStdout()
	wrt := NewPrefixedWriter("[testi] ", "red", out)

	fmt.Fprintln(wrt, "String with warn colors", warn("JEEJEE jotain"))
	fmt.Fprintln(wrt, "")

	i := 0
	for i < 100 {
		fmt.Fprintf(line, "%spos %d / 100 %s\n", cursor.MoveUp(1)+cursor.ClearLineLeft()+"\r", i, warn("something"))
		line.WriteTo(out)
		time.Sleep(time.Millisecond * 20)
		i++
	}

	prog := uiprogress.New()
	// prog.Out = out

	prog.Start()
	bar := prog.AddBar(100)

	for bar.Incr() {
		time.Sleep(time.Millisecond * 20)
		if bar.Current() == 35 {
			fmt.Fprintln(prog.Bypass(), "\nInterrupt !!")
		}
		if bar.Current() == 70 {
			fmt.Fprintln(prog.Bypass(), "\nBreak away !!\n")
			break
		}
	}

	fmt.Fprintln(prog.Bypass(), "\nSomething else !!!\n")

	prog.Stop()
}

/////////////////////////////////////////////////////////////
// Outputtia:
// https://github.com/gosuri/uiprogress
// https://github.com/gosuri/uilive
// https://github.com/tj/go-spin
/////////////////////////////////////////////////////////////
