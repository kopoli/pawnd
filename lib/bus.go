package pawnd

import (
	"fmt"
	"sync"
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
}

// Interface that is registered to an EventBus
// type Listener interface {
// }

// BusLink is the interface for sending messages to the bus
type BusLink interface {
	Receive(from, message string)
	Send(to, message string)
	Identify(name string, bus *EventBus)
}

// link is the internal representation of the link to the EventBus
// type link struct {
// 	name     string
// 	bus      *EventBus
// 	listener Listener
// }

// func (l *link) Send(to, message string) {
// 	l.bus.Send(l.name, to, message)
// }

// Register a node with given name
func (eb *EventBus) Register(name string, link BusLink) error {
	eb.mutex.Lock()


	// eb.links[name] = &link{
	// 	name:     name,
	// 	bus:      eb,
	// 	listener: listener,
	// }
	eb.links[name] = link

	eb.mutex.Unlock()

	link.Identify(name, eb)

	// return eb.links[name], nil
	return nil
}

type Message struct {
	From     string
	To       string
	Contents string
}

func (eb *EventBus) Send(from, to, message string) {
	fmt.Println("chan on ", eb.msgchan)
	fmt.Println("message on", from, to, message)
	eb.msgchan <- Message{from, to, message}
}

func NewEventBus() *EventBus {
	var ret = EventBus{
		links:   make(map[string]BusLink),
		msgchan: make(chan Message),
	}

	go func() {
		for {
			select {
			case msg := <-ret.msgchan:
				ret.mutex.Lock()
				if val, ok := ret.links[msg.To]; ok {
					go val.Receive(msg.From, msg.Contents)
				}
				ret.mutex.Unlock()
			}
		}
	}()

	return &ret
}
