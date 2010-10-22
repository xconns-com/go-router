//
// Copyright (c) 2010 Yigong Liu
//
// Distributed under New BSD License
//

package router

import (
	"io"
	"os"
	"fmt"
)

type stream struct {
	peer peerIntf
	outputChan chan *genericMsg
	//
	rwc        io.ReadWriteCloser
	mar        Marshaler
	demar      Demarshaler
	//
	proxy *proxyImpl
	//others
	Logger
	FaultRaiser
	Closed bool
}

func newStream(rwc io.ReadWriteCloser, mp MarshalingPolicy, p *proxyImpl) *stream {
	s := new(stream)
	s.proxy = p
	//
	s.outputChan = make(chan *genericMsg, s.proxy.router.defChanBufSize + DefCmdChanBufSize)
	s.rwc = rwc
	s.mar = mp.NewMarshaler(rwc)
	s.demar = mp.NewDemarshaler(rwc, p)
	//
	ln := ""
	if len(p.router.name) > 0 {
		if len(p.name) > 0 {
			ln = p.router.name + p.name
		} else {
			ln = p.router.name + "_proxy"
		}
		ln += "_stream"
	}
	s.Logger.Init(p.router.SysID(RouterLogId), p.router, ln)
	s.FaultRaiser.Init(p.router.SysID(RouterFaultId), p.router, ln)
	return s
}

func (s *stream) start() {
	go s.outputMainLoop()
	go s.inputMainLoop()
}

func (s *stream) Close() {
	s.Log(LOG_INFO, "Close() is called")
	//shutdown outputMainLoop
	close(s.outputChan) 
	//notify peer
	s.peer.sendCtrlMsg(&genericMsg{s.proxy.router.SysID(RouterDisconnId), &ConnInfoMsg{}})
}

func (s *stream) closeImpl() {
	if !s.Closed {
		s.Log(LOG_INFO, "closeImpl called")
		s.Closed = true
		//shutdown outputMainLoop
		close(s.outputChan) 
		//shutdown inputMainLoop
		s.rwc.Close()
		//close logger
		s.FaultRaiser.Close()
		s.Logger.Close()
	}
}

func (s *stream) appMsgChanForId(id Id) (reflectChanValue, int) {
	if s.proxy.translator != nil {
		return &genericMsgChan{id, s.outputChan, func(id Id) Id { return s.proxy.translator.TranslateOutward(id) }, true}, 1
	}
	return &genericMsgChan{id, s.outputChan, nil, true}, 1
}

//send ctrl data to io.Writer
func (s *stream) sendCtrlMsg(m *genericMsg) (err os.Error) {
	s.outputChan <- m
	if m.Id.SysIdIndex() == RouterDisconnId {
		close(s.outputChan)
	}
	return
}

func (s *stream) outputMainLoop() {
	s.Log(LOG_INFO, "outputMainLoop start")
	//
	var err os.Error
	cont := true
	for cont {
		m := <-s.outputChan
		if closed(s.outputChan) {
			cont = false
		} else {
			if err = s.mar.Marshal(m.Id, m.Data); err != nil {
				s.LogError(err)
				//s.Raise(err)
				cont = false
			}
		}
	}
	if err != nil {
		//must be io conn fail or marshal fail
		//notify proxy disconn
		s.peer.sendCtrlMsg(&genericMsg{s.proxy.router.SysID(RouterDisconnId), &ConnInfoMsg{}})
	}
	s.closeImpl()
	s.Log(LOG_INFO, "outputMainLoop exit")
}

//read data from io.Reader, pass ctrlMsg to exportCtrlChan and dataMsg to peer
func (s *stream) inputMainLoop() {
	s.Log(LOG_INFO, "inputMainLoop start")
	cont := true
	for cont {
		if err := s.recv(); err != nil {
			cont = false
		}
	}
	//when reach here, must be io conn fail or demarshal fail
	//notify proxy disconn
	s.peer.sendCtrlMsg(&genericMsg{s.proxy.router.SysID(RouterDisconnId), &ConnInfoMsg{}})
	s.closeImpl() 
	s.Log(LOG_INFO, "inputMainLoop exit")
}

func (s *stream) recv() (err os.Error) {
	id, ctrlMsg, appMsg, err := s.demar.Demarshal()
	if err != nil {
		s.LogError(err)
		//s.Raise(err)
		return
	}
	if id.Scope() == NumScope && id.Member() == NumMembership { //chan is closed
		peerChan, _ := s.peer.appMsgChanForId(id)
		if peerChan != nil {
			peerChan.Close()
			s.Log(LOG_INFO, fmt.Sprintf("close proxy forwarding chan for %v", id))
		}
	}
	if ctrlMsg != nil {
		s.peer.sendCtrlMsg(&genericMsg{id, ctrlMsg})
	} else if appMsg != nil {
		peerChan, num := s.peer.appMsgChanForId(id)
		if peerChan != nil && num > 0 {
			peerChan.Send(appMsg)
			//s.Log(LOG_INFO, fmt.Sprintf("send appMsg for %v", id))
		}
	}
	//s.Log(LOG_INFO, fmt.Sprintf("input recv one msg for id %v", id))
	return
}
