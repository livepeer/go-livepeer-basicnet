package basicnet

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/multiformats/go-multihash"

	cid "github.com/ipfs/go-cid"
	ds "github.com/ipfs/go-datastore"
	crypto "github.com/libp2p/go-libp2p-crypto"
	host "github.com/libp2p/go-libp2p-host"
	net "github.com/libp2p/go-libp2p-net"
	peer "github.com/libp2p/go-libp2p-peer"
	peerstore "github.com/libp2p/go-libp2p-peerstore"
	swarm "github.com/libp2p/go-libp2p-swarm"
	bhost "github.com/libp2p/go-libp2p/p2p/host/basic"
	rhost "github.com/libp2p/go-libp2p/p2p/host/routed"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/golang/glog"
)

type SimpleMsg struct {
	Msg string
}

func simpleNodes(p1, p2 int) (*NetworkNode, *NetworkNode) {
	priv1, pub1, _ := crypto.GenerateKeyPair(crypto.RSA, 2048)
	priv2, pub2, _ := crypto.GenerateKeyPair(crypto.RSA, 2048)

	n1, _ := NewNode(p1, priv1, pub1, &BasicNotifiee{})
	n2, _ := NewNode(p2, priv2, pub2, &BasicNotifiee{})

	// n1.PeerHost.Peerstore().AddAddrs(n2.Identity, n2.PeerHost.Addrs(), peerstore.PermanentAddrTTL)
	// n2.PeerHost.Peerstore().AddAddrs(n1.Identity, n1.PeerHost.Addrs(), peerstore.PermanentAddrTTL)
	// n1.PeerHost.Connect(context.Background(), peerstore.PeerInfo{ID: n2.Identity})
	// n2.PeerHost.Connect(context.Background(), peerstore.PeerInfo{ID: n1.Identity})

	return n1, n2
}

func simpleHandler(ns net.Stream, txt string) {
	ws := NewBasicStream(ns)

	var msg SimpleMsg
	err := ws.ReceiveMessage(&msg)

	if err != nil {
		glog.Errorf("Got error decoding msg: %v", err)
		return
	}
	glog.Infof("%v Got msg: %v", ws.Stream.Conn().LocalPeer().Pretty(), msg)
	time.Sleep(500 * time.Millisecond)

	str := string(msg.Msg)

	newMsg := str + "|" + txt
	glog.Infof("Sending %v", newMsg)
	ws.SendMessage(0, newMsg)
}

func simpleHandlerLoop(ws *BasicStream, txt string) {
	for {
		var msg string
		err := ws.ReceiveMessage(&msg)

		if err != nil {
			glog.Errorf("Got error decoding msg: %v", err)
			return
		}
		glog.Infof("%v Got msg: %v", ws.Stream.Conn().LocalPeer().Pretty(), msg)

		time.Sleep(500 * time.Millisecond)

		newMsg := msg + "|" + txt

		glog.Infof("Sending %v", newMsg)
		err = ws.SendMessage(0, newMsg)
		if err != nil {
			glog.Errorf("Failed to send message %v: %v", newMsg, err)
		}
	}
}

func simpleSend(ns net.Stream, txt string, t *testing.T) {
	ws := NewBasicStream(ns)
	ws.SendMessage(0, txt)
}

func TestBackAndForth(t *testing.T) {
	glog.Infof("\n\nTest back and forth...")
	n1, n2 := simpleNodes(15003, 15004)
	connectHosts(n1.PeerHost, n2.PeerHost)
	time.Sleep(time.Second)

	n2.PeerHost.SetStreamHandler("/test/1.0", func(stream net.Stream) {
		simpleHandler(stream, "pong")
	})

	ns1, err := n1.PeerHost.NewStream(context.Background(), n2.Identity, "/test/1.0")
	if err != nil {
		t.Errorf("Cannot create stream: %v", err)
	}
	simpleSend(ns1, "ns1", t)
	simpleHandler(ns1, "ping")
}

func makeRandomHost(port int) host.Host {
	// Ignoring most errors for brevity
	// See echo example for more details and better implementation
	priv, pub, _ := crypto.GenerateKeyPair(crypto.RSA, 2048)
	pid, _ := peer.IDFromPublicKey(pub)
	listen, _ := ma.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port))
	ps := peerstore.NewPeerstore()
	ps.AddPrivKey(pid, priv)
	ps.AddPubKey(pid, pub)
	n, _ := swarm.NewNetwork(context.Background(),
		[]ma.Multiaddr{listen}, pid, ps, nil)
	basicHost := bhost.New(n)
	// return basicHost
	dht, err := constructDHTRouting(context.Background(), basicHost, ds.NewMapDatastore())
	if err != nil {
		glog.Errorf("Error constructing DHTRouting: %v", err)
	}
	rHost := rhost.Wrap(basicHost, dht)
	return rHost
}

func TestBasic(t *testing.T) {
	glog.Infof("\n\nTest Basic...")
	h1 := makeRandomHost(10000)
	h2 := makeRandomHost(10001)
	h1.Peerstore().AddAddrs(h2.ID(), h2.Addrs(), peerstore.PermanentAddrTTL)
	h2.Peerstore().AddAddrs(h1.ID(), h1.Addrs(), peerstore.PermanentAddrTTL)

	h2.SetStreamHandler(Protocol, func(stream net.Stream) {
		glog.Infof("h2 handler...")
		simpleHandler(stream, "pong")
	})

	glog.Infof("Before stream")
	stream, err := h1.NewStream(context.Background(), h2.ID(), Protocol)
	if err != nil {
		glog.Fatal(err)
	}
	glog.Infof("After stream")

	s1 := NewBasicStream(stream)
	s1.SendMessage(0, SimpleMsg{Msg: "ping!"})
	time.Sleep(time.Second * 2)
}

func TestProvider(t *testing.T) {
	n1, n2 := simpleNodes(15010, 15011)
	n3, n4 := simpleNodes(15012, 15013)
	connectHosts(n1.PeerHost, n2.PeerHost)
	connectHosts(n2.PeerHost, n3.PeerHost)
	connectHosts(n3.PeerHost, n4.PeerHost)

	time.Sleep(time.Second)
	buf, _ := hex.DecodeString("hello")
	mhashBuf, _ := multihash.EncodeName(buf, "sha1")
	glog.Infof("Declaring provider: %v", peer.IDHexEncode(n1.Identity))
	// if err := n1.Kad.Provide(context.Background(), cid.NewCidV1(cid.Raw, []byte("hello")), true); err != nil {
	if err := n1.Kad.Provide(context.Background(), cid.NewCidV1(cid.Raw, mhashBuf), true); err != nil {
		glog.Errorf("Error declaring provide: %v", err)
	}

	time.Sleep(time.Second)
	// pidc := n4.Kad.FindProvidersAsync(context.Background(), cid.NewCidV1(cid.Raw, []byte("hello")), 10)
	pidc := n4.Kad.FindProvidersAsync(context.Background(), cid.NewCidV1(cid.Raw, mhashBuf), 1)
	// if err != nil {
	// 	glog.Errorf("Error finding providers: %v", err)
	// }
	select {
	case pid := <-pidc:
		glog.Infof("Provider for hello: %v", peer.IDHexEncode(pid.ID))
	}
}
