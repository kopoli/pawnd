package pawnd

import (
	"fmt"
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

type InitAction struct {
	triggerName string

	BaseAction
}

func NewInitAction(triggerName string) (*InitAction, error) {
	var ret = InitAction{triggerName: triggerName}
	return &ret, nil
}

func (a *InitAction) Receive(from, message string) {
	fmt.Println("From", from, "msg", message)
	switch message {
	case MsgInit:
		a.Send(a.triggerName, MsgTrig)
	}
}

// type FileAction struct {
// }

// func NewFileAction() (*FileAction, error) {

// }

type ExecAction struct {
	Args []string

	Succeeded string

	BaseAction
}

func NewExecAction(args ...string) (*ExecAction, error) {
	var ret = ExecAction{Args: args}

	return &ret, nil
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
		a.Send(ToOutput, a.name + "-ok")
	case MsgTerm:
		fmt.Println("Terminating command!")
	}
}

func ActionDemo(opts util.Options) {
	eb := NewEventBus()

	ia, _ := NewInitAction("a")
	eb.Register("initer", ia)
	ea, _ := NewExecAction("ls")
	eb.Register("a", ea)
	ea.Succeeded = "b"
	es, _ := NewExecAction("Second", "command")
	eb.Register("b", es)

	eb.Send("jeje", "initer", MsgInit)

	time.Sleep(1 * time.Second)

	eb.Send("jeje", "*", MsgTerm)
	time.Sleep(1 * time.Second)
}
