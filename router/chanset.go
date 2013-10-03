//
// Copyright (c) 2010 - 2012 Yigong Liu
//
// Distributed under New BSD License
//

package router

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

/*
* Groups of chans at proxy
 */

//ChanSet groups a set of chans at proxy with the same direction (Send or Recv)
//and same scope and membership
type chanInSet struct {
	id     Id
	ch     Channel
	routCh *RoutedChan
}

type chanSet struct {
	router *routerImpl
	proxy  *proxyImpl
	scope  int
	member int
	chans  map[interface{}]*chanInSet
}

func newChanSet(p *proxyImpl, s int, m int) *chanSet {
	cs := new(chanSet)
	cs.router = p.router
	cs.proxy = p
	cs.scope = s
	cs.member = m
	cs.chans = make(map[interface{}]*chanInSet)
	return cs
}

//findChan return ChanValue and its number of bindings
func (cs *chanSet) findChan(id Id) (Channel, int) {
	r, ok := cs.chans[id.Key()]
	if !ok {
		return nil, -1
	}
	return r.ch, r.routCh.NumPeers()
}

func (cs *chanSet) BindingCount(id Id) int {
	r, ok := cs.chans[id.Key()]
	if !ok {
		return -1
	}
	return r.routCh.NumPeers()
}

func (cs *chanSet) DelChan(id Id) error {
	r, ok := cs.chans[id.Key()]
	if !ok {
		return errors.New("router chanSet: DelChan id doesnt exist")
	}
	delete(cs.chans, id.Key())
	err := cs.router.DetachChan(r.id, r.ch.Interface())
	if err != nil {
		cs.router.LogError(err)
	}
	return nil
}

func (cs *chanSet) Close() {
	for k, r := range cs.chans {
		delete(cs.chans, k)
		err := cs.router.DetachChan(r.id, r.ch.Interface())
		if err != nil {
			cs.router.LogError(err)
		}
	}
}

//recvChanSet groups a set of recv chans at proxy together
type recvChanSet struct {
	*chanSet
}

func (rcs *recvChanSet) AddRecver(id Id, ch Channel, credit int) (err error) {
	_, ok := rcs.chans[id.Key()]
	if ok {
		err = errors.New("router recvChanSet: AddRecver duplicated id")
		return
	}
	rt := rcs.router
	r := new(chanInSet)
	r.id, _ = id.Clone(rcs.scope, rcs.member)
	rcs.router.Log(LOG_INFO, fmt.Sprintf("enter1 add recver for %v, credit %v, async %v, flow not null %v", r.id, credit, rcs.router.async, rcs.proxy.flowController != nil))
	r.ch = ch
	if !rcs.router.async && rcs.proxy.flowController != nil {
		rcs.router.Log(LOG_INFO, fmt.Sprintf("enter2 add recver for %v", r.id))
		//attach flow control adapter to stream chan recver
		r.ch, err = rcs.proxy.flowController.NewFlowSender(ch, credit)
		if err != nil {
			rcs.router.Log(LOG_INFO, fmt.Sprintf("fail to add flow sender: %v %v", r.id, credit))
			return
		}
		rcs.router.Log(LOG_INFO, fmt.Sprintf("add flow sender: %v %v", r.id, credit))
	}
	r.routCh, err = rt.AttachRecvChan(r.id, r.ch.Interface())
	if err != nil {
		return
	}
	rcs.chans[r.id.Key()] = r
	rcs.router.Log(LOG_INFO, fmt.Sprintf("add recver for %v", r.id))
	return
}

//sendChanSet groups a set of send chans at proxy together
type sendChanSet struct {
	*chanSet
}

func (scs *sendChanSet) AddSender(id Id, chanType reflect.Type) (err error) {
	_, ok := scs.chans[id.Key()]
	if ok {
		err = errors.New("router sendChanSet: AddSender duplicated id")
		return
	}
	rt := scs.router
	s := new(chanInSet)
	s.id, _ = id.Clone(scs.scope, scs.member)
	buflen := rt.recvChanBufSize(id)
	if id.SysIdIndex() >= 0 {
		buflen = DefCmdChanBufSize
	}
	s.ch = reflect.MakeChan(chanType, buflen)
	if s.id.SysIdIndex() >= 0 { 
		//NO flow control for sys chans, make it unlimited buffered
		//so that it will not block namespace change propogating goroutine
		s.ch = &asyncChan{Channel:s.ch}
		scs.router.Log(LOG_INFO, fmt.Sprintf("add async recver: %v", s.id))
	} else if !scs.router.async && scs.proxy.flowController != nil {
		//attach flow control adapters for stream send chans
		s.ch, err = scs.proxy.flowController.NewFlowRecver(
			s.ch,
			func(n int) {
				scs.proxy.peer.sendCtrlMsg(&genericMsg{rt.SysID(ReadyId), &ConnReadyMsg{[]*ChanReadyInfo{&ChanReadyInfo{s.id, n}}}})
			})
		if err != nil {
			return
		}
		scs.router.Log(LOG_INFO, fmt.Sprintf("add flow recver: %v", s.id))
	}
	s.routCh, err = rt.AttachSendChan(s.id, s.ch.Interface())
	if err != nil {
		return
	}
	scs.chans[s.id.Key()] = s
	scs.router.Log(LOG_INFO, fmt.Sprintf("add sender for %v", s.id))
	return
}

//sysChanSet: set of chans at proxy to send sys msgs
type sysChanSet struct {
	proxy      *proxyImpl
	pubSubInfo chan *genericMsg
	//chans to talk to local router
	sysRecvChans    *recvChanSet //recv from local router
	sysSendChans    *sendChanSet //send to local router
	msgHandlerChans []*msgHandlerChan
	sync.Mutex
}

func (sc *sysChanSet) Close() {
	sc.Lock()
	defer sc.Unlock()
	sc.proxy.Log(LOG_INFO, "proxy sysChan closing start")
	sc.sysRecvChans.Close()
	sc.sysSendChans.Close()
	sc.proxy.Log(LOG_INFO, "proxy sysChan closed")
}

func (sc *sysChanSet) SendSysMsg(idx int, msg interface{}) {
	sc.Lock()
	sch, nb := sc.sysSendChans.findChan(sc.proxy.router.SysID(idx))
	sc.Unlock()
	if sch != nil && nb > 0 {
		if idx >= PubId || idx <= UnSubId {
			data := msg.(*ChanInfoMsg)
			//filter out sys internal ids
			info := make([]*ChanInfo, len(data.Info))
			num := 0
			for i := 0; i < len(data.Info); i++ {
				if data.Info[i].Id.SysIdIndex() < 0 {
					info[num] = data.Info[i]
					num++
				}
			}
			msg = &ChanInfoMsg{info[0:num]}
		}
		sch.Send(reflect.ValueOf(msg))
	}
}

func (sc *sysChanSet) StartHandleLocalCtrlMsg() {
	sc.Lock()
	handlerChans := sc.msgHandlerChans
	sc.Unlock()
	for _, handler := range handlerChans {
		handler.Ready()
	}
}

func newSysChanSet(p *proxyImpl) *sysChanSet {
	sc := new(sysChanSet)
	sc.proxy = p
	r := p.router
	sc.Lock()
	defer sc.Unlock()
	sc.pubSubInfo = make(chan *genericMsg, DefCmdChanBufSize)
	//sys chans at proxy for forwarding namespace changes
	//there will be NO flow control for sys chans (flow control only for data chans)
	//because the amount of namespace change events is limited and we have to recv all
	//of them, so make them unlimited buffered
	sc.sysRecvChans = &recvChanSet{newChanSet(p, ScopeLocal, MemberRemote)}
	sc.sysSendChans = &sendChanSet{newChanSet(p, ScopeLocal, MemberRemote)}
	//
	pubSubChanType := reflect.TypeOf(make(chan *ChanInfoMsg))
	connChanType := reflect.TypeOf(make(chan *ConnInfoMsg))
	readyChanType := reflect.TypeOf(make(chan *ConnReadyMsg))
	//start recving local ctrl msgs first to avoid miss local namespace changes
	sc.msgHandlerChans = make([]*msgHandlerChan, 4)
	handler := func(m *genericMsg) { sc.proxy.handleLocalCtrlMsg(m) }
	sc.msgHandlerChans[0] = newMsgHandlerChan(r.SysID(PubId), handler)
	sc.sysRecvChans.AddRecver(r.SysID(PubId), sc.msgHandlerChans[0], -1 /*no flow control*/ )
	sc.msgHandlerChans[1] = newMsgHandlerChan(r.SysID(UnPubId), handler)
	sc.sysRecvChans.AddRecver(r.SysID(UnPubId), sc.msgHandlerChans[1], -1 /*no flow control*/ )
	sc.msgHandlerChans[2] = newMsgHandlerChan(r.SysID(SubId), handler)
	sc.sysRecvChans.AddRecver(r.SysID(SubId), sc.msgHandlerChans[2], -1 /*no flow control*/ )
	sc.msgHandlerChans[3] = newMsgHandlerChan(r.SysID(UnSubId), handler)
	sc.sysRecvChans.AddRecver(r.SysID(UnSubId), sc.msgHandlerChans[3], -1 /*no flow control*/ )
	//ready to send ctrl msgs to local router
	sc.sysSendChans.AddSender(r.SysID(ConnId), connChanType)
	sc.sysSendChans.AddSender(r.SysID(DisconnId), connChanType)
	sc.sysSendChans.AddSender(r.SysID(ErrorId), connChanType)
	sc.sysSendChans.AddSender(r.SysID(ReadyId), readyChanType)
	sc.sysSendChans.AddSender(r.SysID(PubId), pubSubChanType)
	sc.sysSendChans.AddSender(r.SysID(UnPubId), pubSubChanType)
	sc.sysSendChans.AddSender(r.SysID(SubId), pubSubChanType)
	sc.sysSendChans.AddSender(r.SysID(UnSubId), pubSubChanType)
	return sc
}
