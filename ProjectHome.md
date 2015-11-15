### moved to [github.com/go-router/router](https://github.com/go-router/router) ###
"router" is a [Go package for distributed peer-peer publish/subscribe message passing. We attach a send chan to an id in router to send msgs, and attach a recv chan to an id to recv msgs. If these 2 ids match, the msgs from send chan will be "routed" to recv chan, e.g.
```
   rot := router.New(...)
   chan1 := make(chan string)
   chan2 := make(chan string)
   chan3 := make(chan string)
   rot.AttachSendChan(PathID("/sports/basketball"), chan1)
   rot.AttachRecvChan(PathID("/sports/basketball"), chan2)
   rot.AttachRecvChan(PathID("/sports/*"), chan3)
```
We can use integers, strings, pathnames, or structs as Ids in router (maybe regex ids and tuple id in future).

we can connect two routers so that chans attached to routerA can communicate with chans attached to routerB transparently.

In wiki, there are more detailed [Tutorial](http://code.google.com/p/go-router/wiki/Tutorial) and UserGuide; also [an experiment to implement highly available services](http://code.google.com/p/go-router/wiki/a_dummy_server) and [notes on flow control](http://code.google.com/p/go-router/wiki/DispatcherAndFlowControl). There are some sample apps: [chat, ping-pong, dummy-server](http://code.google.com/p/go-router/source/browse/trunk/apps).

Installation.
```
go get github.com/go-router/router
```

Example.
```
package main

import (
       "fmt"
       "github.com/go-router/router"
)

func main() {
     rot := router.New(router.StrID(), 32, router.BroadcastPolicy)
     chin := make(chan int)
     chout := make(chan int)
     rot.AttachSendChan(router.StrID("A"), chin)
     rot.AttachRecvChan(router.StrID("A"), chout)
     go func() {
        for i:=0; i<=10; i++ {
            chin <- i;
        }
        close(chin)
     }()
     for v := range chout {
         fmt.Println("recv ", v)
     }
}
```

App [ping-pong](https://code.google.com/p/go-router/source/browse/trunk/apps/pingpong) shows how router allows pinger/ponger goroutines remain unchanged while their connections change from local channels, to routers connected thru unix domain sockets or tcp sockets.