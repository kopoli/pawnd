package pawnd

import "sync"

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
	links   map[string]*link
	msgchan chan Message
	mutex   sync.Mutex
}

// Interface that is registered to an EventBus
type Listener interface {
	Receive(from, message string)
}

// BusLink is the interface for sending messages to the bus
type BusLink interface {
	Send(to, message string)
}

// link is the internal representation of the link to the EventBus
type link struct {
	name     string
	bus      *EventBus
	listener Listener
}

func (l *link) Send(to, message string) {
	l.bus.Send(l.name, to, message)
}

// Register a node with given name
func (eb *EventBus) Register(name string, listener Listener) (BusLink, error) {
	eb.mutex.Lock()

	eb.links[name] = &link{
		name:     name,
		bus:      eb,
		listener: listener,
	}

	eb.mutex.Unlock()
	return eb.links[name], nil
}

type Message struct {
	From     string
	To       string
	Contents string
}

func (eb *EventBus) Send(from, to, message string) {
	eb.msgchan <- Message{from, to, message}
}

func NewEventBus() *EventBus {
	var ret = EventBus{
		links:   make(map[string]*link),
		msgchan: make(chan Message),
	}

	go func() {
		select {
		case msg := <-ret.msgchan:
			ret.mutex.Lock()
			if val, ok := ret.links[msg.To]; ok {
				go val.listener.Receive(msg.From, msg.Contents)
			}
			ret.mutex.Unlock()
		}
	}()

	return &ret
}
