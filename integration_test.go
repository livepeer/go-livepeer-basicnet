package basicnet

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang/glog"
	lpcommon "github.com/livepeer/go-livepeer/common"
	"github.com/livepeer/go-livepeer/net"
)

var strmID1 string
var strmID2 string
var strmID3 string

func setupN() (n1, n2, n3, n4 *BasicVideoNetwork) {
	n1, n2 = setupNodes(15000, 15001)
	n3, n4 = setupNodes(15002, 15003)
	connectHosts(n1.NetworkNode.PeerHost, n2.NetworkNode.PeerHost)
	connectHosts(n2.NetworkNode.PeerHost, n3.NetworkNode.PeerHost)
	go n1.SetupProtocol()
	go n2.SetupProtocol()
	go n3.SetupProtocol()

	strmID1 = fmt.Sprintf("%vOriginalStrm", n1.GetNodeID())
	strmID2 = fmt.Sprintf("%vTranscodedStrm1", n1.GetNodeID())
	strmID3 = fmt.Sprintf("%vTranscodedStrm2", n1.GetNodeID())

	return n1, n2, n3, n4
}

//Broadcast 1 stream to n1
func setupBroadcaster(n1 *BasicVideoNetwork, t *testing.T) net.Broadcaster {
	n1b1, err := n1.GetBroadcaster(strmID1)
	if err != nil {
		t.Errorf("Error: %v", err)
	}
	return n1b1
}
func TestPublish(t *testing.T) {
	//Set up 3 nodes.  n1=broadcaster, n2=transcoder, n3=subscriber
	n1, n2, n3, n4 := setupN()
	defer n1.NetworkNode.PeerHost.Close()
	defer n2.NetworkNode.PeerHost.Close()
	defer n3.NetworkNode.PeerHost.Close()
	defer n4.NetworkNode.PeerHost.Close()
	_ = setupBroadcaster(n1, t)

	if len(n1.broadcasters) != 1 {
		t.Errorf("Expecting 1 broadcaster for n1 but got :%v", n1.broadcasters)
	}
	if len(n1.relayers) != 0 {
		t.Errorf("Expecting 0 relayers for n1 but got :%v", n1.broadcasters)
	}
	if len(n1.subscribers) != 0 {
		t.Errorf("Expecting 0 subscribers for n1 but got :%v", n1.broadcasters)
	}
	if len(n1.broadcasters[strmID1].listeners) != 0 {
		t.Errorf("Expecting 0 listeners for n1 broadcaster, but got %v", n1.broadcasters[strmID1].listeners)
	}
}

func setupTranscoder(n2 *BasicVideoNetwork, t *testing.T) (net.Subscriber, net.Broadcaster, net.Broadcaster, chan struct{}) {
	sub, err := n2.GetSubscriber(strmID1)
	if err != nil {
		glog.Errorf("Error: %v", err)
	}
	n2b1, err := n2.GetBroadcaster(strmID2)
	if err != nil {
		glog.Errorf("Error: %v", err)
	}
	n2b2, err := n2.GetBroadcaster(strmID3)
	if err != nil {
		glog.Errorf("Error: %v", err)
	}

	//n2 subscribes to the stream, creates 2 streams locally (trasncoded streams)
	n2gotdata := make(chan struct{})
	sub.Subscribe(context.Background(), func(seqNo uint64, data []byte, eof bool) {
		glog.Infof("n2 got video data: %v, %s", seqNo, data)
		glog.Infof("Sending transcoded data: %v", fmt.Sprintf("%strans1", data))
		n2b1.Broadcast(seqNo, []byte(fmt.Sprintf("%strans1", data)))
		time.Sleep(time.Millisecond * 500)
		glog.Infof("Sending transcoded data: %v", fmt.Sprintf("%strans2", data))
		n2b2.Broadcast(seqNo, []byte(fmt.Sprintf("%strans2", data)))
		n2gotdata <- struct{}{}
	})
	return sub, n2b1, n2b2, n2gotdata
}

func TestTranscode(t *testing.T) {
	n1, n2, n3, n4 := setupN()
	defer n1.NetworkNode.PeerHost.Close()
	defer n2.NetworkNode.PeerHost.Close()
	defer n3.NetworkNode.PeerHost.Close()
	defer n4.NetworkNode.PeerHost.Close()
	n1b1 := setupBroadcaster(n1, t)
	_, n2b1, n2b2, n2gotdata := setupTranscoder(n2, t)

	//Wait until n2 gets data, this ensures everything is hooked up.
	n1b1.Broadcast(0, []byte("test"))
	timer := time.NewTimer(time.Millisecond * 500)
	select {
	case <-n2gotdata:
	case <-timer.C:
		t.Errorf("Timed out")
	}
	if len(n2.broadcasters) != 2 {
		t.Errorf("Expecting 2 broadcaster for n2 but got :%v", n1.broadcasters)
	}
	for _, b := range n2.broadcasters {
		if len(b.listeners) != 0 {
			t.Errorf("Expecting 0 listeners in n2 broadcasters, got %v", b.listeners)
		}
	}
	if len(n2.relayers) != 0 {
		t.Errorf("Expecting 0 relayers for n2 but got :%v", n1.broadcasters)
	}
	if len(n2.subscribers) != 1 {
		t.Errorf("Expecting 1 subscribers for n2 but got :%v", n1.broadcasters)
	}
	if len(n1.broadcasters[strmID1].listeners) != 1 {
		t.Errorf("Expecting 1 listener for n1 broadcaster, but got %v", n1.broadcasters[strmID1].listeners)
	}
	if l, ok := n1.broadcasters[strmID1].listeners[n2.GetNodeID()]; !ok {
		t.Errorf("Expecting listener for n1 broadcaster to be %v, but got %v", n2.GetNodeID(), l)
	}
	if n2b1.(*BasicBroadcaster).lastMsgs[2] == nil {
		t.Errorf("Expecting 1 msg in n2b1, got %v", n2b1.(*BasicBroadcaster).lastMsgs)
	}
	if n2b2.(*BasicBroadcaster).lastMsgs[2] == nil {
		t.Errorf("Expecting 1 msg in n2b2, got %v", n2b2.(*BasicBroadcaster).lastMsgs)
	}
}

func setupStreamer(n3 *BasicVideoNetwork) (net.Subscriber, net.Subscriber, net.Subscriber, chan struct{}, chan struct{}, chan struct{}) {
	n3sub1, err := n3.GetSubscriber(strmID1)
	if err != nil {
		glog.Errorf("Error: %v", err)
	}
	n3sub2, err := n3.GetSubscriber(strmID2)
	if err != nil {
		glog.Errorf("Error: %v", err)
	}
	n3sub3, err := n3.GetSubscriber(strmID3)
	if err != nil {
		glog.Errorf("Error: %v", err)
	}
	n3sub1gotData := make(chan struct{})
	n3sub1.Subscribe(context.Background(), func(seqNo uint64, data []byte, eof bool) {
		glog.Infof("n3sub1 got data: %v, %s", seqNo, data)
		n3sub1gotData <- struct{}{}
	})
	n3sub2gotData := make(chan struct{})
	n3sub2.Subscribe(context.Background(), func(seqNo uint64, data []byte, eof bool) {
		glog.Infof("n3sub2 got data: %v, %s", seqNo, data)
		n3sub2gotData <- struct{}{}
	})
	time.Sleep(time.Millisecond * 500)
	n3sub3gotdata := make(chan struct{})
	n3sub3.Subscribe(context.Background(), func(seqNo uint64, data []byte, eof bool) {
		glog.Infof("n3sub3 got data: %v, %s", seqNo, data)
		n3sub3gotdata <- struct{}{}
	})
	return n3sub1, n3sub3, n3sub3, n3sub1gotData, n3sub2gotData, n3sub3gotdata
}
func TestABS(t *testing.T) {
	fmt.Println("TestABS...")
	//Set up 3 nodes.  n1=broadcaster, n2=transcoder, n3=subscriber
	n1, n2, n3, n4 := setupN()
	defer n1.NetworkNode.PeerHost.Close()
	defer n2.NetworkNode.PeerHost.Close()
	defer n3.NetworkNode.PeerHost.Close()
	defer n4.NetworkNode.PeerHost.Close()
	n1b1 := setupBroadcaster(n1, t)
	_, _, _, n2gotdata := setupTranscoder(n2, t)
	fmt.Println("After setup transcoder")

	//n3 subscribes to all 3 streams.  We expect:
	//	1 broadcaster in n1
	//	2 broadcasters in n2
	//	1 subscriber in n2
	//	1 relayer in n2
	//	3 subscribers in n3
	_, _, _, n3sub1gotdata, n3sub2gotdata, n3sub3gotdata := setupStreamer(n3)

	if err := n1b1.Broadcast(0, []byte("seg1")); err != nil {
		t.Errorf("Error: %v", err)
	}
	var sub1gotdata, sub2gotdata, sub3gotdata, n2sub1gotdata bool
	for sub1gotdata == false || sub2gotdata == false || sub3gotdata == false || n2sub1gotdata == false {
		timer := time.NewTimer(time.Second * 5)
		select {
		case <-n3sub1gotdata:
			sub1gotdata = true
		case <-n3sub2gotdata:
			sub2gotdata = true
		case <-n3sub3gotdata:
			sub3gotdata = true
		case <-n2gotdata:
			n2sub1gotdata = true
		case <-timer.C:
			t.Errorf("Timed out")
			return
		}
	}

	if len(n1.broadcasters) != 1 {
		t.Errorf("Expecting 1 broadcaster in n1")
	}
	if len(n1.broadcasters[strmID1].listeners) != 1 {
		t.Errorf("Expecting 1 listener for n1's broadcaster")
	}
	if len(n2.relayers) != 1 {
		t.Errorf("Expecting 1 relayer in n2, but got %v", n2.relayers)
	}
	if len(n2.relayers[relayerMapKey(strmID1, SubReqID)].listeners) != 1 {
		t.Errorf("Expecting 1 listener in n2 relayer, got %v", n2.relayers[relayerMapKey(strmID1, SubReqID)].listeners)
	}
	if len(n2.broadcasters) != 2 {
		t.Errorf("Expecting 2 broadcasters in n2, got %v", n2.broadcasters)
	}
	if len(n2.broadcasters[strmID2].listeners) != 1 {
		t.Errorf("Expecting 1 listener in n2 broadcaster, got %v", n2.broadcasters[strmID2].listeners)
	}
	if len(n2.broadcasters[strmID3].listeners) != 1 {
		t.Errorf("Expecting 1 listener in n2 broadcaster, got %v", n2.broadcasters[strmID3].listeners)
	}
	if len(n3.subscribers) != 3 {
		t.Errorf("Expecting 3 subscribers in n3, but got %v", n3.subscribers)
	}
	if len(n3.relayers) != 0 {
		t.Errorf("Expecting 0 relayers in n3, but got %v", n3.relayers)
	}
	if len(n3.broadcasters) != 0 {
		t.Errorf("Expecting 0 broadcasters in n3, but got %v", n3.broadcasters)
	}
}
func TestCancel(t *testing.T) {
	//Set up 3 nodes.  n1=broadcaster, n2=transcoder, n3=subscriber
	n1, n2, n3, n4 := setupN()
	defer n1.NetworkNode.PeerHost.Close()
	defer n2.NetworkNode.PeerHost.Close()
	defer n3.NetworkNode.PeerHost.Close()
	defer n4.NetworkNode.PeerHost.Close()
	n1b1 := setupBroadcaster(n1, t)
	_, _, _, n2gotdata := setupTranscoder(n2, t)

	//n3 subscribes to all 3 streams.  We expect:
	//	1 broadcaster in n1
	//	2 broadcasters in n2
	//	1 subscriber in n2
	//	1 relayer in n2
	//	3 subscribers in n3
	n3sub1, n3sub2, _, n3sub1gotdata, n3sub2gotdata, n3sub3gotdata := setupStreamer(n3)

	n1b1.Broadcast(0, []byte("seg1"))
	var sub1gotdata, sub2gotdata, sub3gotdata, n2sub1gotdata bool
	for sub1gotdata == false && sub2gotdata == false && sub3gotdata == false && n2sub1gotdata == false {
		timer := time.NewTimer(time.Millisecond * 500)
		select {
		case <-n3sub1gotdata:
			sub1gotdata = true
		case <-n3sub2gotdata:
			sub2gotdata = true
		case <-n3sub3gotdata:
			sub3gotdata = true
		case <-n2gotdata:
			n2sub1gotdata = true
		case <-timer.C:
			t.Errorf("Timed out")
			return
		}
	}

	//n3 drops the subscription for 2 streams half-way through.  We expect:
	//	1 broadcaster in n1 (with 1 listener)
	//	1 subscriber in n2
	//	1 subscriber in n3
	if err := n3sub1.Unsubscribe(); err != nil {
		glog.Infof("Error unsubscribing: %v", err)
	}
	if err := n3sub2.Unsubscribe(); err != nil {
		glog.Infof("Error unsubscribing: %v", err)
	}
	//Suppose to get eof
	timer := time.NewTimer(time.Millisecond * 500)
	select {
	case <-n3sub3gotdata:
	case <-timer.C:
		t.Errorf("Timed out")
	}

	lpcommon.WaitUntil(time.Second, func() bool {
		return len(n2.relayers) == 0
	})

	if len(n1.broadcasters) != 1 {
		t.Errorf("Expecting 1 broadcaster in n1, got %v", len(n1.broadcasters))
	}
	if len(n1.broadcasters[strmID1].listeners) != 1 {
		t.Errorf("Expecting 1 listener in n1's broadcaster, got %v", n1.broadcasters[strmID1].listeners)
	}
	if len(n2.subscribers) != 1 {
		t.Errorf("Expecting 1 subscriber in n2, got %v", n2.subscribers)
	}
	time.Sleep(time.Second)
	if len(n2.relayers) != 0 {
		t.Errorf("Expecting 0 relayer in n2, got %v", n2.relayers)
	}
	if len(n3.subscribers) != 1 {
		t.Errorf("Expecting 1 subscriber in n3, got %v", n3.subscribers)
	}
}
