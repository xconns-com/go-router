//
// Copyright (c) 2010 - 2011 Yigong Liu
//
// Distributed under New BSD License
//

package router

import (
	"fmt"
)

type notifyChan struct {
	nchan  chan *ChanInfoMsg
	routCh *RoutedChan
}

func (n *notifyChan) Close() {
	n.routCh.Detach()
}

func newNotifyChan(idx int, r *routerImpl) *notifyChan {
	nc := new(notifyChan)
	nc.nchan = make(chan *ChanInfoMsg, r.defChanBufSize)
	nc.routCh, _ = r.AttachSendChan(r.NewSysID(idx, ScopeGlobal, MemberLocal), nc.nchan)
	return nc
}

type notifier struct {
	router      *routerImpl
	notifyChans [4]*notifyChan
}

func newNotifier(s *routerImpl) *notifier {
	n := new(notifier)
	n.router = s
	for i := 0; i < 4; i++ {
		n.notifyChans[i] = newNotifyChan(PubId+i, s)
	}
	return n
}

func (n *notifier) Close() {
	for i := 0; i < 4; i++ {
		n.notifyChans[i].Close()
	}
}

func (n notifier) notifyPub(info *ChanInfo) {
	n.router.Log(LOG_INFO, fmt.Sprintf("notifyPub: %v", info.Id))
	nc := n.notifyChans[0]
	if nc.routCh.NumPeers() > 0 {
		select {
		case nc.nchan <- &ChanInfoMsg{Info: []*ChanInfo{info}}:
		default:
			go func() {
				//async sending sys notif could fail during router closing
				//when notif chans could be closed while some notifier 
				//goroutines hang on
				defer func() {
					_ = recover()
				}()
				nc.nchan <- &ChanInfoMsg{Info: []*ChanInfo{info}}
			}()
		}
	}
}

func (n notifier) notifyUnPub(info *ChanInfo) {
	n.router.Log(LOG_INFO, fmt.Sprintf("notifyUnPub: %v", info.Id))
	nc := n.notifyChans[1]
	if nc.routCh.NumPeers() > 0 {
		select {
		case nc.nchan <- &ChanInfoMsg{Info: []*ChanInfo{info}}:
		default:
			go func() {
				//async sending sys notif could fail during router closing
				defer func() {
					_ = recover()
				}()
				nc.nchan <- &ChanInfoMsg{Info: []*ChanInfo{info}}
			}()
		}
	}
}

func (n notifier) notifySub(info *ChanInfo) {
	n.router.Log(LOG_INFO, fmt.Sprintf("notifySub: %v", info.Id))
	nc := n.notifyChans[2]
	if nc.routCh.NumPeers() > 0 {
		select {
		case nc.nchan <- &ChanInfoMsg{Info: []*ChanInfo{info}}:
		default:
			go func() {
				//async sending sys notif could fail during router closing
				defer func() {
					_ = recover()
				}()
				nc.nchan <- &ChanInfoMsg{Info: []*ChanInfo{info}}
			}()
		}
	}
}

func (n notifier) notifyUnSub(info *ChanInfo) {
	n.router.Log(LOG_INFO, fmt.Sprintf("notifyUnSub: %v", info.Id))
	nc := n.notifyChans[3]
	if nc.routCh.NumPeers() > 0 {
		select {
		case nc.nchan <- &ChanInfoMsg{Info: []*ChanInfo{info}}:
		default:
			go func() {
				//async sending sys notif could fail during router closing
				defer func() {
					_ = recover()
				}()
				nc.nchan <- &ChanInfoMsg{Info: []*ChanInfo{info}}
			}()
		}
	}
}
