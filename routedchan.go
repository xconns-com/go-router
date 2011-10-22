//
// Copyright (c) 2010 - 2011 Yigong Liu
//
// Distributed under New BSD License
//

package router

import (
	"sync"
)

type RoutedChanType int

const (
	Send RoutedChanType = iota
	Recv
)

type operType int

const (
	attachOp operType = iota
	detachOp
)

type oper struct {
	kind operType
	peer *RoutedChan
}

/*
 RoutedChan represents channels which are attached to router. 
 They expose Channel's interface: Send()/TrySend()/Recv()/TryRecv()/...
 and additional info:
 1. Id - the id which channel is attached to
 2. NumPeers() - return the number of bound peers
 3. Peers() - array of bound peers RoutedChan
 4. Detach() - detach the channel from router
*/
type RoutedChan struct {
	Kind         RoutedChanType
	Id           Id
	Channel      //external SendChan/RecvChan, attached by clients
	router       *routerImpl
	dispatcher   Dispatcher //current for push dispacher, only sender uses dispatcher
	bindChan     chan *BindEvent
	bindCond     chan bool //simulate a cond var for waiting for recver binding
	bindLock     sync.Mutex
	bindings     []*RoutedChan //binding_set
	inDisp       bool          //in a dispatch loop
	dispLock     sync.Mutex
	opBuf        []*oper
	internalChan bool
	detached     bool
	delayClose   func(*RoutedChan)
}

func newRoutedChan(id Id, t RoutedChanType, ch Channel, r *routerImpl, bc chan *BindEvent) *RoutedChan {
	routCh := &RoutedChan{}
	routCh.Kind = t
	routCh.Id = id
	routCh.Channel = ch
	routCh.router = r
	routCh.bindChan = bc
	if t == Send {
		routCh.bindCond = make(chan bool, 1)
	}
	return routCh
}

func (e *RoutedChan) Interface() interface{} {
	return e
}

//override Channel.Close() method
func (e *RoutedChan) Close() {
	if e.Kind == Send {
		//recover panic to handle race(close twice) when proxy destroy and a sender chan close from outside of router at the same time
		defer func() {
			_ = recover()
		}()
	}
	e.Channel.Close()
}

func (e *RoutedChan) NumPeers() int {
	e.bindLock.Lock()
	defer e.bindLock.Unlock()
	num := len(e.bindings)
	for i := 0; i < len(e.opBuf); i++ {
		op := e.opBuf[i]
		switch op.kind {
		case attachOp:
			num++
		case detachOp:
			num--
		}
	}
	return num
}

func (e *RoutedChan) Peers() (copySet []*RoutedChan) {
	e.bindLock.Lock()
	defer e.bindLock.Unlock()
	l := len(e.bindings)
	if l == 0 {
		return
	}
	copySet = make([]*RoutedChan, l)
	copy(copySet, e.bindings)
	return
}


func (e *RoutedChan) start(disp DispatchPolicy) {
	if e.Kind == Send {
		e.dispatcher = disp.NewDispatcher()
		go e.senderLoop()
	}
}

func (e *RoutedChan) senderLoop() {
	cont := true
	for cont {
		e.bindLock.Lock()
		//block here till we have recvers so that message will not be lost
		for len(e.bindings) == 0 {
			e.bindLock.Unlock()
			//wait here until some recver attach
			<-e.bindCond
			e.bindLock.Lock()
		}
		e.bindLock.Unlock()
		v, chOpen := e.Channel.Recv()
		if chOpen {
			e.dispLock.Lock()
			e.inDisp = true
			e.dispLock.Unlock()
			e.dispatcher.Dispatch(v, e.bindings)
			e.dispLock.Lock()
			e.inDisp = false
			if len(e.opBuf) > 0 {
				e.runPendingOps()
			}
			e.dispLock.Unlock()
		} else {
			e.router.detach(e, true)
			cont = false
		}
	}
}

func (e *RoutedChan) runPendingOps() {
	for i := 0; i < len(e.opBuf); i++ {
		op := e.opBuf[i]
		switch op.kind {
		case attachOp:
			e.attachImpl(op.peer)
			op.peer.attachImpl(e)
		case detachOp:
			e.detachImpl(op.peer)
			op.peer.detachImpl(e)
		}
		e.opBuf[i] = nil
	}
	//clean up
	e.opBuf = e.opBuf[0:0]
}

func (e *RoutedChan) Detach() {
	e.router.detach(e, false)
}

func (e *RoutedChan) attachImpl(p *RoutedChan) {
	e.bindLock.Lock()
	defer e.bindLock.Unlock()
	e.bindings = append(e.bindings, p)
	if e.bindChan != nil {
		//KeepLatest non-blocking send
	L:
		for {
			select {
			case e.bindChan <- &BindEvent{PeerAttach, len(e.bindings)}:
				break L
			default:
				<-e.bindChan //drop the oldest one
			}
		}
	}
	if e.Kind == Send && len(e.bindings) == 1 { //first recver attached
		//fill e.bindCond to notify we have bindings now
		select {
		case e.bindCond <- true:
		default:
		}
	}
}

func (e *RoutedChan) attach(p *RoutedChan) {
	e.dispLock.Lock()
	defer e.dispLock.Unlock()
	if e.inDisp {
		e.opBuf = append(e.opBuf, &oper{attachOp, p})
	} else {
		e.attachImpl(p)
		if e.Kind == Send {
			p.attachImpl(e)
		}
	}
}

func (e *RoutedChan) detachImpl(p *RoutedChan) {
	e.bindLock.Lock()
	n := len(e.bindings)
	for i, v := range e.bindings {
		if v == p {
			e.bindings[i] = nil
			e.bindings = append(e.bindings[:i], e.bindings[i+1:]...)
			if e.bindChan != nil {
				//KeepLatest non-blocking send
			L:
				for {
					select {
					case e.bindChan <- &BindEvent{PeerDetach, n - 1}: //chan full
						break L
					default:
						<-e.bindChan //drop the oldest one
					}
				}
			}
			if len(e.bindings) == 0 {
				switch e.Kind {
				case Recv:
					//for recver, if all senders Detached
					//send EndOfData to notify possible pending goroutine
					if e.bindChan != nil {
						//if bindChan exist, user is monitoring bind status
						//send EndOfData event and normally leave ext chan "ch" open
					L1:
						for {
							select {
							case e.bindChan <- &BindEvent{EndOfData, 0}:
								break L1
							default:
								<-e.bindChan
							}
						}
						if e.detached {
							e.Close()
						}
					} else {
						//since no bindChan, user code is not monitoring bind status
						//close ext chan to notify potential pending goroutine
						detached := e.detached
						e.bindLock.Unlock()
						e.Close()
						if !detached {
							e.Detach() //remove self from routing table
						}
						return
					}
				case Send:
					//for sender, if all recver Detached
					//drain e.bindCond so that dispatcher goroutine can wait
					select {
					case _ = <-e.bindCond:
					default:
					}
				}
			}
			e.bindLock.Unlock()
			return
		}
	}
	e.bindLock.Unlock()
}

func (e *RoutedChan) detach(p *RoutedChan) {
	e.dispLock.Lock()
	defer e.dispLock.Unlock()
	if e.inDisp {
		e.opBuf = append(e.opBuf, &oper{detachOp, p})
	} else {
		e.detachImpl(p)
		if e.Kind == Send {
			p.detachImpl(e)
		}
	}
}
