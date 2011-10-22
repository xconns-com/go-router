//
// Copyright (c) 2010 - 2011 Yigong Liu
//
// Distributed under New BSD License
//

package router

import (
	"reflect"
	"sync"
	"container/list"
	"os"
)

/* 
 Channel interface defines functional api of Go's channel:
 based on reflect.Value's channel related method set
 allow programming "generic" channels with reflect.Value as msgs
 add some utility Channel types
*/
type Channel interface {
	ChanState
	Sender
	Recver
}

//common interface for msg senders
//and senders are responsible for closing channels
type Sender interface {
	Send(reflect.Value)
	TrySend(reflect.Value) bool
	Close()
}

//common interface for msg recvers
//recvers will check if channels are closed or not
type Recver interface {
	Recv() (reflect.Value, bool)
	TryRecv() (reflect.Value, bool)
}

//basic chan state
type ChanState interface {
	Type() reflect.Type
	Interface() interface{}
	IsNil() bool
	Cap() int
	Len() int
}

//SendChan: sending end of Channel (chan<-)
type SendChan interface {
	ChanState
	Sender
}

//RecvChan: recving end of Channel (<-chan)
type RecvChan interface {
	ChanState
	Recver
}

//GenericMsgChan: 
//mux msgs with diff ids into a common "chan *genericMsg"
//and expose Channel api
//for close, will send a special msg to mark close of one sender
//other part is responsible for counting active senders and close underlying
//channels if all senders close
type genericMsgChan struct {
	id      Id
	Channel //must have element type: *genericMsg
}

func newGenMsgChan(id Id, ch Channel) *genericMsgChan {
	//add check to make sure Channel of type: chan *genericMsg
	return &genericMsgChan{id, ch}
}

func newGenericMsgChan(id Id, ch chan *genericMsg) *genericMsgChan {
	return &genericMsgChan{id, reflect.ValueOf(ch)}
}

/* delegate following calls to embeded Channel's api
func (gch *genericMsgChan) Type() reflect.Type
func (gch *genericMsgChan) IsNil() bool
func (gch *genericMsgChan) Len() int
func (gch *genericMsgChan) Cap() int
func (gch *genericMsgChan) Recv() reflect.Value
func (gch *genericMsgChan) TryRecv() reflect.Value
*/

//overwrite the following calls for new behaviour
func (gch *genericMsgChan) Interface() interface{} { return gch }

func (gch *genericMsgChan) Close() {
	//sending chanCloseMsg, do not do the real closing of genericMsgChan 
	id1, _ := gch.id.Clone(NumScope, NumMembership) //special id to mark chan close
	gch.Channel.Send(reflect.ValueOf(&genericMsg{id1, nil}))
}

func (gch *genericMsgChan) Send(v reflect.Value) {
	gch.Channel.Send(reflect.ValueOf(&genericMsg{gch.id, v.Interface()}))
}

func (gch *genericMsgChan) TrySend(v reflect.Value) bool {
	return gch.Channel.TrySend(reflect.ValueOf(&genericMsg{gch.id, v.Interface()}))
}

/*
 asyncChan:
 . unlimited internal buffering
 . senders never block
*/

//a trivial async chan
type asyncChan struct {
	Channel
	sync.Mutex
	buffer *list.List //when buffer!=nil, background forwarding active
	closed bool
}

func (ac *asyncChan) Close() {
	ac.Lock()
	defer ac.Unlock()
	if ac.closed {
		panic("Close a closed chan")
	}
	ac.closed = true
	if ac.buffer == nil { //no background forwarder running
		ac.Channel.Close()
	}
}

func (ac *asyncChan) Interface() interface{} {
	return ac
}

func (ac *asyncChan) Cap() int {
	return UnlimitedBuffer //unlimited
}

func (ac *asyncChan) Len() int {
	l := ac.Channel.Len()
	ac.Lock()
	defer ac.Unlock()
	if ac.buffer == nil {
		return l
	}
	return l + ac.buffer.Len()
}

//for async chan, Send() never block because of unlimited buffering
func (ac *asyncChan) Send(v reflect.Value) {
	ac.Lock()
	defer ac.Unlock()
	if ac.closed {
		panic("Send on closed chan")
	}
	if ac.buffer == nil {
		if ac.Channel.TrySend(v) {
			return
		}
		ac.buffer = new(list.List)
		ac.buffer.PushBack(v)
		//spawn forwarder
		go func() {
			for {
				ac.Lock()
				l := ac.buffer
				if l.Len() == 0 {
					ac.buffer = nil
					if ac.closed {
						ac.Channel.Close()
					}
					ac.Unlock()
					return
				}
				ac.buffer = new(list.List)
				ac.Unlock()
				for e := l.Front(); e != nil; e = l.Front() {
					ac.Channel.Send(e.Value.(reflect.Value))
					l.Remove(e)
				}
			}
		}()
	} else {
		ac.buffer.PushBack(v)
	}
}

func (ac *asyncChan) TrySend(v reflect.Value) bool {
	ac.Send(v)
	return true
}


/*
 flowChan: flow controlled channel
 . a pair of <Sender, Recver>
 . flow control between <Sender, Recver>: simple window protocol for lossless transport
 . the transport Channel between Sender, Recver should have capacity >= expected credit
*/

type flowChanSender struct {
	Channel
	creditChan chan bool
	creditCap  int
	credit     int
	sync.Mutex //protect credit/creditChan change
}

func newFlowChanSender(ch Channel, credit int) (*flowChanSender, os.Error) {
	fc := new(flowChanSender)
	fc.Channel = ch
	if credit <= 0 {
		return nil, os.ErrorString("Flow Controlled Chan: invalid credit")
	}
	fc.credit = credit
	fc.creditCap = credit
	//for unlimited buffer, ch.Cap() return UnlimitedBuffer(-1)
	if ch.Cap() != UnlimitedBuffer && ch.Cap() < credit {
		return nil, os.ErrorString("Flow Controlled Chan: do not have enough buffering")
	}
	fc.creditChan = make(chan bool, 1)
	fc.creditChan <- true //since we have credit, turn on creditChan
	return fc, nil
}

func (fc *flowChanSender) Send(v reflect.Value) {
	<-fc.creditChan //wait here for one credit 
	fc.Lock()
	fc.credit--
	if fc.credit > 0 {
		select {
		case fc.creditChan <- true:
		default:
		}
	}
	fc.Unlock()
	fc.Channel.Send(v)
}

func (fc *flowChanSender) TrySend(v reflect.Value) bool {
	select {
	case _ = <-fc.creditChan:
	default:
		return false
	}
	fc.Lock()
	fc.credit--
	if fc.credit > 0 {
		select {
		case fc.creditChan <- true:
		default:
		}
	}
	fc.Unlock()
	return fc.Channel.TrySend(v)
}

func (fc *flowChanSender) Len() int {
	fc.Lock()
	defer fc.Unlock()
	return fc.creditCap - fc.credit
}

func (fc *flowChanSender) Cap() int {
	return fc.creditCap
}

func (fc *flowChanSender) ack(n int) {
	fc.Lock()
	defer fc.Unlock()
	if fc.credit == 0 {
		select {
		case fc.creditChan <- true:
		default:
		}
	}
	fc.credit += n
	if fc.credit > fc.creditCap {
		fc.credit = fc.creditCap
	}
}

func (fc *flowChanSender) Interface() interface{} {
	return fc
}


type flowChanRecver struct {
	Channel
	ack func(int)
}

func (fc *flowChanRecver) Recv() (v reflect.Value, ok bool) {
	v, ok = fc.Channel.Recv()
	if ok {
		fc.ack(1)
	}
	return
}

func (fc *flowChanRecver) TryRecv() (v reflect.Value, ok bool) {
	v, ok = fc.Channel.TryRecv()
	if v.IsValid() && ok {
		fc.ack(1)
	}
	return
}

func (fc *flowChanRecver) Interface() interface{} {
	return fc
}
