package pawnd

import (
	"fmt"
	"net/url"
	"path"
	"sync"
	"time"
)

// Common messages
var (
	MsgInit = "init"      // Start everything
	MsgTerm = "terminate" // Stop all processing
	MsgRest = "restart"   // Restart
	MsgTrig = "trigger"   // Make action happen
	MsgWait = "wait"      // Reset status to wait

	ToAll            = "*"
	ErrMainRestarted = fmt.Errorf("main restarted")
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

func EscapeName(name string) string {
	return url.PathEscape(name)
}

func UnescapeName(name string) string {
	ret, err := url.PathUnescape(name)
	if err != nil {
		panic(fmt.Sprintf("Internal error: could not unescape [%s]: %v", name, err))
	}
	return ret
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
	go func() {
		eb.msgchan <- Message{from, to, message}
	}()
}

// Run until terminated
func (eb *EventBus) Run() error {
	go func() {
		select {
		case <-eb.failsafeTimer.C:
			deps.FailSafeExit()
			return
		case <-eb.done:
		}
		stopTimer(eb.failsafeTimer)
	}()

	for k := range eb.links {
		eb.links[k].Run()
	}

	eb.Send("", ToAll, MsgInit)
	eb.wg.Wait()

	eb.done <- struct{}{}

	if eb.restarting {
		return ErrMainRestarted
	}
	return nil
}

func (eb *EventBus) Close() error {
	eb.Send("", ToAll, MsgTerm)
	return nil
}

// Action notifies that it has stopped. This is for waiting for all actions
// when terminating.
func (eb *EventBus) LinkStopped() {
	eb.wg.Done()
}

func NewEventBus() *EventBus {
	var ret = EventBus{
		links:   make(map[string]BusLink),
		msgchan: make(chan Message, 1),
		done:    make(chan struct{}, 1),
	}

	ret.failsafeTimer = time.NewTimer(0)
	stopTimer(ret.failsafeTimer)

	ret.wg.Add(1)
	go func() {
	loop:
		for msg := range ret.msgchan {
			ret.mutex.Lock()
			isterm := msg.Contents == MsgTerm
			for k := range ret.links {
				matched, err := path.Match(msg.To, k)
				if err != nil {
					msg := fmt.Sprintf("internal error, bad pattern: %v", err)
					panic(msg)
				}
				if !matched {
					continue
				}
				ret.links[k].Receive(msg.From, msg.Contents)
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
