package pawnd

import (
	"sync"

	"github.com/ryanuber/go-glob"
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
	MsgTrig = "trigger"   // Make action happen

	ToAll = "*"
)

// EventBus conveys messages to Listeners
type EventBus struct {
	links   map[string]BusLink
	msgchan chan Message
	mutex   sync.Mutex
	wg      sync.WaitGroup
}

// BusLink is the interface for sending messages to the bus
type BusLink interface {
	// Receiving messages
	Receive(from, message string)

	// Sending messages
	Send(to, message string)

	// Registering the EventBus to the BusLink
	Identify(name string, bus *EventBus)
}

// Register a node with given name
func (eb *EventBus) Register(name string, link BusLink) error {
	eb.mutex.Lock()
	eb.links[name] = link
	eb.mutex.Unlock()

	link.Identify(name, eb)

	eb.wg.Add(1)

	return nil
}

type Message struct {
	From     string
	To       string
	Contents string
}

func (eb *EventBus) Send(from, to, message string) {
	eb.msgchan <- Message{from, to, message}
}

// Run until terminated
func (eb *EventBus) Run() {
	eb.Send("", ToAll, MsgInit)
	eb.wg.Wait()
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
	}

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

	return &ret
}
