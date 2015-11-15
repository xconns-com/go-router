## Dispatcher and Flow Control ##

Zero buffered channels are Go's basic primitive for synchronization and communication. It is the rendezouv point where both sender goroutine and receiver goroutine meet and pass data, missing of either of sender and receiver will block the other.

Buffered channels have bounded internal buffer to contain some burst of sender messages. However it is synchronous or flow controlled in that the slower one among sender and receiver will throttle down the other so the transfer rate is determined by the slower peer.

When attaching channels to an id in router, a send channel can connect to multiple receive channels, with a dispatcher goroutine forwarding messages between them - which forms a "nucleus" of message passing identified by its id:
  * For broadcast dispatcher, the data rate is determined by the slowest among sender and all receivers.
  * For KeepLatestBroadcast dispatcher (for some applications such as tracking stock quotes, only the latest results will be interesting), the sender will never block. If some receivers cannot catch up, their old data are dropped to keep latest values.
  * For Round-robin and Random dispatchers, for ideal distribution, the data rate is determined by the slower one of sender and the sum of the rates of all receivers.

One particular note is that the "nucleuses" for 2 separate ids are totally separate, so the slowness of data passing for id "ping" will not have any effect on data passing on id "pong".

When 2 routers are connected locally in the same process, there is one separate (bounded) buffered chan (one for each id) to connect nucleuses with the same id at 2 routers. The buffer sizes of these connecting chans are determined by the buffer size we set at router.New(...) which is the default, or the specific receive buffer size we set when attaching receive chans at the receive router.
Since these connecting chans are bounded buffered, so the connected nucleuses at both routers are still flow controlled by the slowest link, depending on the dispatcher used.
And since there is one separate connecting chan for each id, the data passings on separate ids are kept separate in both routers.

When 2 routers are connected remotely thru streams (such as tcp connections), all the above mentioned connecting chans between routers are multiplexed over the same stream connection. we can turn on flow control on streams (by setting FlowControl flag) to allow all these remote connecting chans keep the same behaviour of flow control and separation as local connecting chans.

Or we can forget about flow control inside router by going asynchronous, although this may be inconsistent with Go's style. If we create a router - call router.New(...) by setting the buffer size to UnlimitedBuffer, the created router is asynchronous:
  * the attached send chans will never block, the sender goroutines are guaranteed to return immediately, as long as RAM is available for buffering
  * even inside the same nucleus or connected nucleuses, the message flows on all receive chans and even remote receive chans are totally separate, so they will not interfere with each other
  * all pending messages are buffered right before the attached receive chans if receiver is slow

If using async routers, we may need implement application level flow control, similar to what applications written in Erlang will do.