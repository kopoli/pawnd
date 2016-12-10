package pawnd

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ahmetalpbalkan/go-cursor"
	"github.com/mattn/go-colorable"
	"github.com/mgutz/ansi"

	"github.com/gosuri/uilive"

	"github.com/gosuri/uiprogress"
	util "github.com/kopoli/go-util"
)

/////////////////////////////////////////////////////////////

type outWriter struct {
	ID   string
	Next io.Writer

	isBlocked bool
}

func (o *outWriter) Write(buf []byte) (n int, err error) {
	return o.Next.Write(buf)
}

func newOutWriter(ID string, next io.Writer) (ret *outWriter) {
	ret = &outWriter{
		ID:        ID,
		Next:      NewPrefixedWriter("["+ID+"] ", "", next),
		isBlocked: false,
	}
	return
}

// Caset:
// 1. ei printata mitään paitsi jos komento feilaa. Sitten printataan koko hoito.
// 2. Printataan koko ajan

type Output interface {
	Register(Node) error
	Update()

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

	outputs []*outputStatus
}

func newOutput(opts util.Options, emt Emitter) (ret *output) {
	ret = &output{
		// out: os.Stdout,
		out: colorable.NewColorableStdout(),
		// err: os.Stderr,
		Prefixer: NewPrefixedWriter("", "", nil),
		emt:      emt,
		width:    80,
	}

	ret.Prefixer.Out = ret.out

	return
}

func (o *output) WriteID(ID string, buf []byte) (n int, err error) {
	prefix := "[" + ID + "] "
	if strings.HasSuffix(ID, "-err") {
		prefix += ansi.ColorCode("red+b")
	}

	o.Prefixer.Prefix = []byte(prefix)
	n, err = o.Prefixer.Write(buf)

	o.Update()
	return
}

func (o *output) Update() {
	tmp := &bytes.Buffer{}

	for _, os := range o.outputs {
		// cursor
		// os.drawProgress()
		// cursor

		fmt.Fprintf(tmp, "[%s][%s][", os.ID, os.Status)
		fillwidth := o.width * os.Progress / 100
		for i := 0; i < fillwidth; i++ {
			tmp.WriteByte('=')
		}

		for i := 0; i < o.width-fillwidth; i++ {
			tmp.WriteByte('-')
		}
		fmt.Fprintf(tmp, "]\n")
	}

	fmt.Fprintf(tmp, "%s", cursor.MoveUp(len(o.outputs)))

	tmp.WriteTo(o.out)
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
	return
}

func (o *output) Start() (err error) {

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

func uiliveTest() {
	wr := uilive.New()
	wr.Start()

	for i := 0; i < 100; i++ {
		fmt.Fprintf(wr, "Jepjep %d / 100 jotain aika pitkää juttua\n", i)
		time.Sleep(time.Millisecond * 10)
	}
	wr.Stop()
}

func UiDemo(opts util.Options) {

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
