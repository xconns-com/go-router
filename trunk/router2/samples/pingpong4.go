package main

import (
	"fmt"
	"strconv"
	"flag"
	"router"
	"net"
	"os"
)

//Msg instances are bounced between Pinger and Ponger as balls
type Msg struct {
	Data string
	Count int
}

//pinger: send to ping chan, recv from pong chan
type Pinger struct {
	//Pinger's public interface
	pingChan chan<- *Msg
	pongChan <-chan *Msg
	done     chan<- bool
	//Pinger's private state
	numRuns int //how many times should we ping-pong
}

func (p *Pinger) Run() {
	for v := range p.pongChan {
		fmt.Println("Pinger recv: ", v)
		if v.Count > p.numRuns {
			break
		}
		p.pingChan <- &Msg{"hello from Pinger", v.Count+1}
	}
	close(p.pingChan)
	p.done <- true
}

func newPinger(rot router.Router, done chan<- bool, numRuns int) {
	//attach chans to router
	pingChan := make(chan *Msg)
	pongChan := make(chan *Msg)
	rot.AttachSendChan(router.StrID("ping"), pingChan)
	rot.AttachRecvChan(router.StrID("pong"), pongChan)
	//start pinger
	ping := &Pinger{pingChan, pongChan, done, numRuns}
	go ping.Run()
}

//ponger: send to pong chan, recv from ping chan
type Ponger struct {
	//Ponger's public interface
	pongChan chan<- *Msg
	pingChan <-chan *Msg
	done     chan<- bool
	//Ponger's private state
}

func (p *Ponger) Run() {
	p.pongChan <- &Msg{"hello from Ponger", 0}  //initiate ping-pong
	for v := range p.pingChan {
		fmt.Println("Ponger recv: ", v)
		p.pongChan <- &Msg{"hello from Ponger", v.Count+1}
	}
	close(p.pongChan)
	p.done <- true
}

func newPonger(rot router.Router, done chan<- bool) {
	//attach chans to router
	pingChan := make(chan *Msg)
	pongChan := make(chan *Msg)
	bindChan := make(chan *router.BindEvent, 1)
	rot.AttachSendChan(router.StrID("pong"), pongChan, bindChan)
	rot.AttachRecvChan(router.StrID("ping"), pingChan)
	//wait for pinger connecting
	for {
		if (<-bindChan).Count > 0 {
			break
		}
	}
	//start ponger
	pong := &Ponger{pongChan, pingChan, done}
	go pong.Run()
}

func main() {
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Println("Usage: pingpong3 num_runs")
		return
	}
	numRuns, _ := strconv.Atoi(flag.Arg(0))
	done := make(chan bool)
	connNow := make(chan bool)
	//start two goroutines to setup a unix sock connection
	//connect two routers thru unix sock
	//and then hook up Pinger and Ponger to the routers
	go func() { //setup Pinger sock conn
		//wait for ponger up
		<-connNow
		//set up an io conn to ponger thru unix sock
		addr := "/tmp/pingpong.test"
		conn, _ := net.Dial("unix", "", addr)
		fmt.Println("ping conn up")

		//create router and connect it to io conn
		rot := router.New(router.StrID(), 32, router.BroadcastPolicy)
		rot.ConnectRemote(conn, router.GobMarshaling)

		//hook up Pinger and Ponger
		newPinger(rot, done, numRuns)
	}()
	go func() { //setup Ponger sock conn
		//wait to set up an io conn thru unix sock
		addr := "/tmp/pingpong.test"
		os.Remove(addr)
		l, _ := net.Listen("unix", addr)
		connNow <- true //notify pinger that ponger's ready to accept
		conn, _ := l.Accept()
		fmt.Println("pong conn up")

		//create router and connect it to io conn
		rot := router.New(router.StrID(), 32, router.BroadcastPolicy)
		rot.ConnectRemote(conn, router.GobMarshaling)

		//hook up Ponger
		newPonger(rot, done)
	}()
	//wait for ping-pong to finish
	<-done
	<-done
}