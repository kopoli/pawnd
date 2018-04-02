package pawnd

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	util "github.com/kopoli/go-util"
)

/*

Format:

;; Building:

[name]
file=*.go
changed=build
; removed=
; added=

[build]
exec=go build


;; Running a daemon

init=godoc

[godoc]
daemon=godoc -http=:6060


;; trigger chain

[html changed]
file=html/index.html
changed=generate

[source changed]
file=*go
changed=build

[generate]
exec=go generate
succeeded=build

[build]
exec=go build
succeeded=handledaemon

[handledaemon]
daemon=./daemond --foreground

*/

type BaseAction struct {
	name string
	bus  *EventBus
}

func (a *BaseAction) Identify(name string, eb *EventBus) {
	a.name = name
	a.bus = eb
}

func (a *BaseAction) Send(to, message string) {
	a.bus.Send(a.name, to, message)
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
	switch message {
	case MsgInit:
		a.Send(a.triggerName, MsgTrig)
	}
}

// type FileAction struct {
// }

// func NewFileAction() (*FileAction, error) {

// }

///

type SignalAction struct {
	sigchan  chan os.Signal
	termchan chan bool
	BaseAction
}

func NewSignalAction(sig os.Signal) *SignalAction {
	var ret = SignalAction{
		sigchan:  make(chan os.Signal),
		termchan: make(chan bool),
	}

	signal.Notify(ret.sigchan, sig)
	go func() {
	loop:
		for {
			select {
			case <-ret.sigchan:
				ret.Send("*", MsgTerm)
			case <-ret.termchan:
				break loop
			}
		}
	}()
	return &ret
}

func (a *SignalAction) Receive(from, message string) {
	switch message {
	case MsgTerm:
		a.termchan <- true
	}
}

///

type ExecAction struct {
	Args []string

	Daemon    bool
	Succeeded string
	Failed    string

	BaseAction
}

func NewExecAction(args ...string) *ExecAction {
	return &ExecAction{Args: args}

}

func (a *ExecAction) Receive(from, message string) {
	fmt.Println("Execaction", from, message)
	switch message {
	case MsgTrig:
		fmt.Println("Running command:", a.Args)

		// After succeeding
		if a.Succeeded != "" {
			a.Send(a.Succeeded, MsgTrig)
		}
		a.Send(ToOutput, a.name+"-ok")
	case MsgTerm:
		fmt.Println("Terminating command!")
	}
}

func ActionDemo(opts util.Options) {
	eb := NewEventBus()

	sa := NewSignalAction(os.Interrupt)
	eb.Register("sighandler", sa)

	f, err := ValidateConfig("Testfile")
	if err != nil {
		fmt.Println(err)
		return
	}

	err = CreateActions(f, eb)

	eb.Send("", ToAll, MsgInit)

	// time.Sleep(1 * time.Second)
	eb.Run()
}

func ActionDemo2(opts util.Options) {
	eb := NewEventBus()

	ia := NewInitAction("a")
	eb.Register("initer", ia)
	ea := NewExecAction("ls")
	eb.Register("a", ea)
	ea.Succeeded = "b"
	es := NewExecAction("Second", "command")
	eb.Register("b", es)

	eb.Send("jeje", "initer", MsgInit)

	time.Sleep(1 * time.Second)

	eb.Send("jeje", "*", MsgTerm)
	time.Sleep(1 * time.Second)
}
