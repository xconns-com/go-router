### 1. History ###
The connection between routers uses a similar protocol as another project: **Channel** (http://channel.sourceforge.net) with minor variations.

The original protocol spec is here: http://channel.sourceforge.net/boost_channel/libs/channel/doc/protocol.html. To read from the context of go-router, we need to do some terminology conversions, such as Channel to router, Names to Ids, Interface to Proxies, etc.

Current implementations of router1/router2 since February, 2010 have been briefly described in Tutorial and UserGuide wikis, and reiterated here.

### 2. System Message Ids ###

In "router" there are eight system ids:
```
        ConnId	: the first message sent between routers, containing some type checking info (router's id type and router's type (async/flow control/...)
        DisconnId : message sent when one router actively disconnect from another
        ErrorId     : sent when one side detect any error, such as id type or chan type mismatch
        ReadyId	: sent when channels are ready to deliver app messages, such as initial name space merge complete or more receive buffer available
        PubId		: send new publications (set of <id, chan type info>) to connected routers
        UnPubId		: remove publications from connected routers
        SubId		: send new subscriptions (set of <id, chan type info>) to connected routers
        UnSubId		: remove subscriptions from connected routers
```
> user code can subscribe (attach recv chan to) to the above ids to learn system state changes and react accordingly

  * namespace monitoring

> user code can track router's namespace change by subscribing to these four system ids: PubId/UnPubId/SubId/UnSubId.

  * connection monitoring

> user code can track the connection status of its router to remote routers by subscribing to these four system ids: ConnId/DisconnId/ErrorId/ReadyId.

### 3. Protocol Service Overview ###

When two routers connect, their namespaces will merge as following to enable channels in one router to communicate to channels in the other router transparently:

  * Ids merging from router2 to router1:

> all ids in the intersection of router1's input interface (its set of recv ids with global / remote scope) and router2's output interface (its set of send ids with global / remote scope)

  * Ids merging from router1 to router2:

> all ids in the intersection of router2's input interface (its set of recv ids with global / remote scope) and router1's output interface (its set of send ids with global / remote scope)

  * new ids are propagated automatically to connected routers according to its id / scope / membership.
  * when routers are disconnected, routers' namespaces will be updated automatically so that all publications and subscriptions from remote routers will be removed.

To manage namespace changes, we can apply IdFilter and IdTranslator at Proxy for the following effects:

  * IdFilter: allow only a specific set of Ids (and their messages) to pass thru a connection
  * IdTranslator: relocate ids / messages from a connection to a "subspace" in local namespace

### 4. type-checking ###
> When channels are attached to / detached from ids in routers, and when routers are connected, the types of ids and chans are checked during runtime:
  * type checking inside a single router
> > when attaching/detaching chans to ids in router, verify that id is of same type of router's id type, and if id is already inside router, verify added chan is of same type as chans already attached to this id
  * type checking during router connection
> > when two routers are connected, verify they have the same id type; and if they have matching ids, verify matching ids have the same chan type in both routers.

### 5. Protocol Transactions ###

More details about protocol transactions can be found in original doc. Here are some sample transactions translated in router's terminology:

router connection:

> when two routers connect, they go thru the following steps:
  * send ConnId (with id type) msg to each other
  * recv peer's ConnId and check if having same id type, if not, returning a ErrId msg, and report to local name space connection error
  * send its current global publications/subscriptions to peer, using PubId/SubId msgs
  * recv inital PubId/SubId msgs from peer and merge it with local name space; and check matched channels to see if their types match; if not, send ConnErrMsg.
  * if no error happens during the process, send ReadyId msg
  * if any error, send ErrId msg, notify local name space and exit
  * if receiving ErrMsg from peer, notify local name space and exit.

publication of id:
> when a sending channel "chan1" is attached to an "id1" in router(A) with global/remote scope:
  * router(A) will send a PubId msg (containing <id1, chan1\_type\_info>) to all connected routers
  * if router(B) recv this msg, it will check its local namespace; if there is a local subscription for "id1" with matched chan type; the following will happen:
    * at router(B)'s proxy, a sending channel will be attached to router(B) with "id1" and (MemberRemote, ScopeLocal) which will forward msgs from router(A); a SubId msg with "id1" will be sent to router(A).
    * at router(A)'s proxy, after recving SubId msg from router(B), a recv chan will be attached to router(A) with "id1" and (MemberRemote, ScopeLocal) which will forward msgs from local sending "chan1" to remote.