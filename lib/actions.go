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
succeeded=daemon

[daemon]
daemon=./daemond --foreground

*/

type BaseAction struct {
	Link BusLink
}

func (a *BaseAction) Receive(from, message string) {
	fmt.Println("Base functionality !!")
}

func (a *BaseAction) Register(eb *EventBus, name string) error {
	var err error
	a.Link, err = eb.Register(name, a)
	return err
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
	switch message {
	case MsgInit:
		a.Link.Send(a.triggerName, MsgTrig)
	}
}

// type FileAction struct {
// }

// func NewFileAction() (*FileAction, error) {

// }

type ExecAction struct {
	Args []string

	BaseAction
}

func NewExecAction(args ...string) (*ExecAction, error) {
	var ret = ExecAction{Args: args}

	return &ret, nil
}

func (a *ExecAction) Receive(from, message string) {
	switch message {
	case MsgTrig:
		fmt.Println("Running command:", a.Args)
	case MsgTerm:
		fmt.Println("Terminating command!")
	}
}

func ActionDemo(opts util.Options) {
	eb := NewEventBus()

	ia, _ := NewInitAction("a")
	ia.Register(eb, "initer")
	ea, _ := NewExecAction("ls")

	ea.Register(eb, "a")

	eb.Send("jeje", "initer", MsgInit)

	time.Sleep(1 * time.Second)
}
