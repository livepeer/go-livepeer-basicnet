package basicnet

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/golang/glog"

	addrutil "gx/ipfs/QmNSWW3Sb4eju4o2djPQ1L1c2Zj9XN9sMYJL8r1cbxdc6b/go-addr-util"
	host "gx/ipfs/QmNmJZL7FQySMtE2BQuLMuZg2EB2CLEunJJUSVSc9YnnbV/go-libp2p-host"
	swarm "gx/ipfs/QmSwZMWwFZSUpe5muU2xgTUwppH24KfMwdPXiwbEp2c6G5/go-libp2p-swarm"
	kb "gx/ipfs/QmTH6VLu3WXfbH3nuLdmscgPWuiPZv3GMJ2YCdzBS5z91T/go-libp2p-kbucket"
	ma "gx/ipfs/QmWWQ2Txc2c6tqjsBpzg5Ar652cHPGNsQQp2SejkNmkUMb/go-multiaddr"
	kad "gx/ipfs/QmWY24HyokZFtW5bx36EwJ7rBPiwKmNdWNtWbwfDCgnHtp/go-libp2p-kad-dht"
	bhost "gx/ipfs/QmXGfPjhnro8tgANHDUg4gGgLGYnAz1zcDPAgNeUkzbsN1/go-libp2p/p2p/host/basic"
	rhost "gx/ipfs/QmXGfPjhnro8tgANHDUg4gGgLGYnAz1zcDPAgNeUkzbsN1/go-libp2p/p2p/host/routed"
	peerstore "gx/ipfs/QmXauCuJzmzapetmC6W4TuDJLL1yFFrVzSHoWv8YdbmnxH/go-libp2p-peerstore"
	inet "gx/ipfs/QmXfkENeeBvh3zYA51MaSdGUdBjhQ99cP5WQe8zgr6wchG/go-libp2p-net"
	protocol "gx/ipfs/QmZNkThpqfVXs9GNbexPrfBbXSLNYeKrE7jwFM2oqHbyqN/go-libp2p-protocol"
	peer "gx/ipfs/QmZoWKhxUmZ2seW4BzX6fJkNR8hh9PsGModr7q171yq2SS/go-libp2p-peer"
	crypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"
	record "gx/ipfs/QmcBSi3Zxa6ytDQxig2iMv4VMfiKKy7v4tibi1Sq6Z5u2x/go-libp2p-record"
	ds "gx/ipfs/QmeiCcJfDW1GJnWUArudsv5rQsihpi4oyddPhdqo3CfX6i/go-datastore"
)

type NetworkNode interface {
	ID() peer.ID
	Host() host.Host
	GetOutStream(pid peer.ID) *BasicOutStream
	RefreshOutStream(pid peer.ID) *BasicOutStream
	RemoveStream(pid peer.ID)
	GetPeers() []peer.ID
	GetPeerInfo(peer.ID) peerstore.PeerInfo
	Connect(context.Context, peerstore.PeerInfo) error
	AddPeer(peerstore.PeerInfo, time.Duration)
	RemovePeer(peer.ID)
	ClosestLocalPeers(strmID string) ([]peer.ID, error)
	GetDHT() *kad.IpfsDHT
	SetStreamHandler(pid protocol.ID, handler inet.StreamHandler)
	SetSignFun(sign func(data []byte) ([]byte, error))
	Sign(data []byte) ([]byte, error)
	SetVerifyTranscoderSig(verify func(data []byte, sig []byte, strmID string) bool)
	VerifyTranscoderSig(data []byte, sig []byte, strmID string) bool
}

type BasicNetworkNode struct {
	Identity       peer.ID // the local node's identity
	Kad            *kad.IpfsDHT
	PeerHost       host.Host // the network host (server+client)
	Network        *BasicVideoNetwork
	outStreams     map[peer.ID]*BasicOutStream
	outStreamsLock *sync.Mutex
	sign           func(data []byte) ([]byte, error)
	verify         func(data []byte, sig []byte, strmID string) bool
}

//NewNode creates a new Livepeerd node.
func NewNode(listenPort int, priv crypto.PrivKey, pub crypto.PubKey, f *BasicNotifiee) (*BasicNetworkNode, error) {
	pid, err := peer.IDFromPublicKey(pub)
	if err != nil {
		return nil, err
	}

	streams := make(map[peer.ID]*BasicOutStream)

	// Create a peerstore
	store := peerstore.NewPeerstore()
	store.AddPrivKey(pid, priv)
	store.AddPubKey(pid, pub)

	// Create multiaddresses.  I'm not sure if this is correct in all cases...
	uaddrs, err := addrutil.InterfaceAddresses()
	if err != nil {
		return nil, err
	}
	addrs := make([]ma.Multiaddr, len(uaddrs), len(uaddrs))
	for i, uaddr := range uaddrs {
		portAddr, err := ma.NewMultiaddr(fmt.Sprintf("/tcp/%d", listenPort))
		if err != nil {
			glog.Errorf("Error creating portAddr: %v %v", uaddr, err)
			return nil, err
		}
		addrs[i] = uaddr.Encapsulate(portAddr)
	}

	// Create swarm (implements libP2P Network)
	netwrk, err := swarm.NewNetwork(
		context.Background(),
		addrs,
		pid,
		store,
		&BasicReporter{})

	netwrk.Notify(f)
	basicHost := bhost.New(netwrk, bhost.NATPortMap)

	dht, err := constructDHTRouting(context.Background(), basicHost, ds.NewMapDatastore())
	if err != nil {
		glog.Errorf("Error constructing DHT: %v", err)
		return nil, err
	}
	rHost := rhost.Wrap(basicHost, dht)

	glog.V(2).Infof("Created node: %v at %v", peer.IDHexEncode(rHost.ID()), rHost.Addrs())
	nn := &BasicNetworkNode{Identity: pid, Kad: dht, PeerHost: rHost, outStreams: streams, outStreamsLock: &sync.Mutex{}}
	f.HandleDisconnect(func(pid peer.ID) {
		nn.RemoveStream(pid)
	})

	return nn, nil
}

func constructDHTRouting(ctx context.Context, host host.Host, dstore ds.Batching) (*kad.IpfsDHT, error) {
	dhtRouting := kad.NewDHT(ctx, host, dstore)

	dhtRouting.Validator["v"] = func(rec *record.ValidationRecord) error {
		return nil
	}
	dhtRouting.Selector["v"] = func(_ string, bs [][]byte) (int, error) { return 0, nil }

	// if err := dhtRouting.Bootstrap(context.Background()); err != nil {
	// 	glog.Errorf("Error bootstraping dht: %v", err)
	// 	return nil, err
	// }
	return dhtRouting, nil
}

func (n *BasicNetworkNode) GetOutStream(pid peer.ID) *BasicOutStream {
	n.outStreamsLock.Lock()
	strm, ok := n.outStreams[pid]
	if !ok {
		strm = n.RefreshOutStream(pid)
	}
	n.outStreamsLock.Unlock()
	return strm
}

func (n *BasicNetworkNode) RefreshOutStream(pid peer.ID) *BasicOutStream {
	// glog.Infof("Creating stream from %v to %v", peer.IDHexEncode(n.Identity), peer.IDHexEncode(pid))
	if s, ok := n.outStreams[pid]; ok {
		if err := s.Stream.Reset(); err != nil {
			glog.Errorf("Error resetting connetion: %v", err)
		}
	}

	ns, err := n.PeerHost.NewStream(context.Background(), pid, Protocol)
	if err != nil {
		glog.Errorf("%v Error creating stream to %v: %v", peer.IDHexEncode(n.Identity), peer.IDHexEncode(pid), err)
		return nil
	}
	strm := NewBasicOutStream(ns)
	n.outStreams[pid] = strm
	return strm
}

func (n *BasicNetworkNode) RemoveStream(pid peer.ID) {
	// glog.Infof("Removing stream for %v", peer.IDHexEncode(pid))
	n.outStreamsLock.Lock()
	delete(n.outStreams, pid)
	n.outStreamsLock.Unlock()
}

func (n *BasicNetworkNode) ID() peer.ID {
	return n.Identity
}

func (n *BasicNetworkNode) Host() host.Host {
	return n.PeerHost
}

func (n *BasicNetworkNode) GetPeers() []peer.ID {
	return n.PeerHost.Peerstore().Peers()
}

func (n *BasicNetworkNode) GetPeerInfo(p peer.ID) peerstore.PeerInfo {
	return n.PeerHost.Peerstore().PeerInfo(p)
}

func (n *BasicNetworkNode) Connect(ctx context.Context, pi peerstore.PeerInfo) error {
	return n.PeerHost.Connect(ctx, pi)
}

func (n *BasicNetworkNode) AddPeer(pi peerstore.PeerInfo, ttl time.Duration) {
	n.PeerHost.Peerstore().AddAddrs(pi.ID, pi.Addrs, ttl)
}

func (n *BasicNetworkNode) RemovePeer(id peer.ID) {
	n.PeerHost.Peerstore().ClearAddrs(id)
}

func (n *BasicNetworkNode) GetDHT() *kad.IpfsDHT {
	return n.Kad
}

func (n *BasicNetworkNode) SetStreamHandler(pid protocol.ID, handler inet.StreamHandler) {
	n.PeerHost.SetStreamHandler(pid, handler)
}

func (n *BasicNetworkNode) SetSignFun(sign func(data []byte) ([]byte, error)) {
	n.sign = sign
}

func (n *BasicNetworkNode) Sign(data []byte) ([]byte, error) {
	if n.sign == nil {
		return make([]byte, 0), nil // XXX not sure about this. error instead?
	}
	return n.sign(data)
}

func (n *BasicNetworkNode) SetVerifyTranscoderSig(verify func(data []byte, sig []byte, strmID string) bool) {
	n.verify = verify
}

func (n *BasicNetworkNode) VerifyTranscoderSig(data []byte, sig []byte, strmID string) bool {
	if n.verify == nil {
		return true // XXX change to false once we can verify sigs
	}
	return n.verify(data, sig, strmID)
}

func (bn *BasicNetworkNode) ClosestLocalPeers(strmID string) ([]peer.ID, error) {
	targetPid, err := extractNodeID(strmID)
	if err != nil {
		glog.Errorf("Error extracting node id from streamID: %v", strmID)
		return nil, ErrSubscriber
	}
	localPeers := bn.PeerHost.Peerstore().Peers()
	if len(localPeers) == 1 {
		glog.Errorf("No local peers")
		return nil, ErrSubscriber
	}

	return kb.SortClosestPeers(localPeers, kb.ConvertPeerID(targetPid)), nil
}
