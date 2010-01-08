//
// Copyright (c) 2010 Yigong Liu
//
// Distributed under New BSD License
//
package router

import (
	"rand"
	"time"
)

//a dispatcher generator
type DispatchPolicy interface {
	NewDispatcher() Dispatcher
}

//all dispatchers should implement this interface
type Dispatcher interface {
	Dispatch(v interface{}, recvers []*Endpoint)
}

//a wrapper for plain dispatch function
type DispatchFunc func(v interface{}, recvers []*Endpoint)

func (f DispatchFunc) Dispatch(v interface{}, recvers []*Endpoint) {
	f(v, recvers)
}

type PolicyFunc func() Dispatcher

func (f PolicyFunc) NewDispatcher() Dispatcher {
	return f()
}

/*
 simple dispatching algorithms which are the base of more practical ones:
 broadcast, roundrobin, etc
*/

//simple broadcast is a plain function
func Broadcast(v interface{}, recvers []*Endpoint) {
	for _, rc := range recvers {
		if !closed(rc.Chan) {
			rc.Chan <- v
		}
	}
}

var BroadcastPolicy DispatchPolicy = PolicyFunc(func() Dispatcher { return DispatchFunc(Broadcast) })

//round robin is a object maintaining its state
type Roundrobin struct {
	next int
}

func NewRoundrobin() *Roundrobin { return &Roundrobin{0} }

func (r *Roundrobin) Dispatch(v interface{}, recvers []*Endpoint) {
	start := r.next
	for {
		rc := recvers[r.next]
		r.next = (r.next + 1) % len(recvers)
		if !closed(rc.Chan) {
			rc.Chan <- v
			break
		}
		if r.next == start {
			break
		}
	}
}

var RoundRobinPolicy DispatchPolicy = PolicyFunc(func() Dispatcher { return NewRoundrobin() })

//random dispatcher
type RandomDispatcher rand.Rand

func NewRandomDispatcher() *RandomDispatcher {
	return (*RandomDispatcher)(rand.New(rand.NewSource(time.Seconds())))
}

func (rd *RandomDispatcher) Dispatch(v interface{}, recvers []*Endpoint) {
	for {
		ind := ((*rand.Rand)(rd)).Intn(len(recvers))
		rc := recvers[ind]
		if !closed(rc.Chan) {
			rc.Chan <- v
			break
		}
	}
}

var RandomPolicy DispatchPolicy = PolicyFunc(func() Dispatcher { return NewRandomDispatcher() })

/*
 * dispatchers with a little considerations: timeouts, error reporting,
 * diff ways of keep/drop values: keep the oldest, keep the latest
 * real world dispatchers can be still more involving than this
 */

//TimeoutDropBroadcaster: when sending to some recv channels times out, give up and keep the old values
type TimeoutDropBroadcaster struct {
	timeNs int64 //timeout in nano seconds
}

func NewTimeoutDropBroadcaster(to int64) *TimeoutDropBroadcaster {
	return &TimeoutDropBroadcaster{to}
}

func (r *TimeoutDropBroadcaster) Dispatch(v interface{}, recvers []*Endpoint) {
	for _, rc := range recvers {
		if !closed(rc.Chan) {
			ticker := time.NewTicker(r.timeNs)
			select {
			case rc.Chan <- v:

			case <-ticker.C:

			}
			ticker.Stop()
		}
	}
}

//TimeoutReportBroadcaster could timeout at some recv channels. When this happens,
//it will give up new value / keep existing values in chan and
//report to a event channel, which upper level code should process
type TimeoutEvent struct {
	timeStamp *time.Time
	value     interface{}
	recvChan  chan interface{}
}

type TimeoutReportBroadcaster struct {
	timeNs    int64              //timeout in nano seconds
	EventChan chan *TimeoutEvent //upper level code should recv & process it
}

func NewTimeoutReportBroadcaster(to int64, bufSize int) *TimeoutReportBroadcaster {
	trb := new(TimeoutReportBroadcaster)
	trb.timeNs = to
	trb.EventChan = make(chan *TimeoutEvent, bufSize)
	return trb
}

//send timeout events using this method so that if the upper level code is lazy handling
//timeout events, dispatcher will not be blocked
func (r *TimeoutReportBroadcaster) SendAndKeepLatest(to *TimeoutEvent) {
	for !(r.EventChan <- to) {
		<-r.EventChan //drop one
	}
}

func (r *TimeoutReportBroadcaster) Dispatch(v interface{}, recvers []*Endpoint) {
	for _, rc := range recvers {
		if !closed(rc.Chan) {
			ticker := time.NewTicker(r.timeNs)
			select {
			case rc.Chan <- v:

			case <-ticker.C:
				toEvent := new(TimeoutEvent)
				toEvent.timeStamp = time.LocalTime()
				toEvent.value = v
				toEvent.recvChan = rc.Chan
				r.SendAndKeepLatest(toEvent)
			}
			ticker.Stop()
		}
	}
}

/*
 * KeepLatestBroadcaster: when sending value to a recv chan times out, remove the oldest
 * values from recv chan to give space to new value
 */

//utility:
//a channel with bounded size N which will always keep the latest N items
//so sender will never block
//just need the following type as a warpper to add the special Send method
//to normal buffered chan, recver will use normal channel recv operator
type KeepLatestChan chan interface{}

func (c KeepLatestChan) Send(v interface{}) {
	for !(c <- v) { //chan full
		<-c //drop one
	}
}

type KeepLatestBroadcaster struct {
	timeNs int64 //timeout in nano seconds
}

func NewKeepLatestBroadcaster(to int64) *KeepLatestBroadcaster {
	return &KeepLatestBroadcaster{to}
}

func (r *KeepLatestBroadcaster) Dispatch(v interface{}, recvers []*Endpoint) {
	for _, rc := range recvers {
		if !closed(rc.Chan) {
			ticker := time.NewTicker(r.timeNs)
			select {
			case rc.Chan <- v:

			case <-ticker.C:
				KeepLatestChan(rc.Chan).Send(v)
			}
			ticker.Stop()
		}
	}
}