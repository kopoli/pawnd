package pawnd

import (
	"fmt"
	"sync"
	"time"

	glob "github.com/ryanuber/go-glob"
)

/*
Use-cases:

- file update triggers registered action
- everything is terminated
- action is triggered during startup
- action triggers a second action
- the display is updated when program starts, stops.

*/

// Common messages
var (
	MsgInit = "init"      // Start everything
	MsgTerm = "terminate" // Stop all processing
	MsgRest = "restart"   // Restart
	MsgTrig = "trigger"   // Make action happen

	ToAll            = "*"
	ErrMainRestarted = fmt.Errorf("Main restarted")
)

// EventBus conveys messages to Listeners
type EventBus struct {
	links         map[string]BusLink
	msgchan       chan Message
	mutex         sync.Mutex
	wg            sync.WaitGroup
	failsafeTimer *time.Timer
	done          chan struct{}
	restarting    bool
}

// BusLink is the interface for sending messages to the bus
type BusLink interface {
	// Receiving messages
	Receive(from, message string)

	// Sending messages
	Send(to, message string)

	// Registering the EventBus to the BusLink
	Identify(name string, bus *EventBus)

	// Start the BusLink specific goroutine
	Run()
}

// Register a node with given name
func (eb *EventBus) Register(name string, link BusLink) {
	eb.mutex.Lock()
	eb.links[name] = link
	eb.mutex.Unlock()

	link.Identify(name, eb)

	eb.wg.Add(1)
}

type Message struct {
	From     string
	To       string
	Contents string
}

func (eb *EventBus) Send(from, to, message string) {
	if message == MsgRest {
		eb.restarting = true
		message = MsgTerm
	}
	if message == MsgTerm {
		eb.failsafeTimer.Reset(2 * time.Second)
	}
	eb.msgchan <- Message{from, to, message}
}

// Run until terminated
func (eb *EventBus) Run() error {
	for k := range eb.links {
		eb.links[k].Run()
	}

	eb.Send("", ToAll, MsgInit)
	eb.wg.Wait()

	if eb.restarting {
		return ErrMainRestarted
	}
	return nil
}

// Action notifies that it has stopped. This is for waiting for all actions
// when terminating.
func (eb *EventBus) LinkStopped(name string) {
	eb.wg.Done()
}

func NewEventBus() *EventBus {
	var ret = EventBus{
		links:   make(map[string]BusLink),
		msgchan: make(chan Message),
		done:    make(chan struct{}),
	}

	ret.failsafeTimer = time.NewTimer(0)
	stopTimer(ret.failsafeTimer)

	go func() {
		select {
		case <-ret.failsafeTimer.C:
			deps.FailSafeExit()
			return
		case <-ret.done:
		}
		stopTimer(ret.failsafeTimer)
	}()

	ret.wg.Add(1)
	go func() {
	loop:
		for msg := range ret.msgchan {
			ret.mutex.Lock()
			isterm := msg.Contents == MsgTerm
			for k := range ret.links {
				if !glob.Glob(msg.To, k) {
					continue
				}
				if !isterm {
					go ret.links[k].Receive(msg.From, msg.Contents)
				} else {
					ret.links[k].Receive(msg.From, msg.Contents)
				}
			}
			ret.mutex.Unlock()
			if isterm {
				break loop
			}
		}
		ret.wg.Done()
	}()

	close(ret.done)

	return &ret
}
