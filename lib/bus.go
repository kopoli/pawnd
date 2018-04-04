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

	ToAll    = "*"
	ToOutput = "out:*"
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

func NewEventBus() *EventBus {
	var ret = EventBus{
		links:   make(map[string]BusLink),
		msgchan: make(chan Message),
	}

	go func() {
		ret.wg.Add(1)
	loop:
		for {
			select {
			case msg := <-ret.msgchan:
				ret.mutex.Lock()
				if msg.Contents != MsgTerm {
					for k := range ret.links {
						if glob.Glob(msg.To, k) {
							go ret.links[k].Receive(msg.From, msg.Contents)
						}
					}
				} else {
					for k := range ret.links {
						if glob.Glob(msg.To, k) {
							ret.links[k].Receive(msg.From, msg.Contents)
						}
					}
				}
				ret.mutex.Unlock()
				if msg.Contents == MsgTerm {
					break loop
				}
			}
		}
		ret.wg.Done()
	}()

	return &ret
}
