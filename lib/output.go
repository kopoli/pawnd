package pawnd

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/ahmetalpbalkan/go-cursor"
	"github.com/mattn/go-colorable"
	"github.com/mgutz/ansi"

	"github.com/gosuri/uilive"

	"github.com/gosuri/uiprogress"
	util "github.com/kopoli/go-util"
)

/////////////////////////////////////////////////////////////

type Output interface {
	Stdout(string) io.Writer
	Stderr(string) io.Writer
}

type output struct {
	out io.Writer
	err io.Writer
}

func NewOutput() (ret *output) {
	ret = &output{}
	return
}

func (o *output) Stdout(ID string) io.Writer {
	return o.out
}

func (o *output) Stderr(ID string) io.Writer {
	return o.err
}

/////////////////////////////////////////////////////////////

type PrefixedWriter struct {
	prefix []byte
	eol    []byte
	out    io.Writer
}

func NewPrefixedWriter(prefix string, style string, out io.Writer) *PrefixedWriter {
	return &PrefixedWriter{
		prefix: []byte(prefix + ansi.ColorCode(style)),
		eol:    []byte("" + ansi.Reset + "\n"),
		out:    out,
	}
}

func (p *PrefixedWriter) Write(buf []byte) (n int, err error) {
	var wr = func(buf []byte) bool {
		_, err = p.out.Write(buf)
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

		if !(wr(p.prefix) && wr(buf[:pos]) && wr(p.eol)) {
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
