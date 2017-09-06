package basicnet

import (
	"context"
	"fmt"

	"github.com/golang/glog"

	ds "github.com/ipfs/go-datastore"
	addrutil "github.com/libp2p/go-addr-util"
	crypto "github.com/libp2p/go-libp2p-crypto"
	host "github.com/libp2p/go-libp2p-host"
	kad "github.com/libp2p/go-libp2p-kad-dht"
	peer "github.com/libp2p/go-libp2p-peer"
	peerstore "github.com/libp2p/go-libp2p-peerstore"
	record "github.com/libp2p/go-libp2p-record"
	swarm "github.com/libp2p/go-libp2p-swarm"
	bhost "github.com/libp2p/go-libp2p/p2p/host/basic"
	rhost "github.com/libp2p/go-libp2p/p2p/host/routed"
	ma "github.com/multiformats/go-multiaddr"
)

type NetworkNode struct {
	Identity peer.ID // the local node's identity
	Kad      *kad.IpfsDHT
	PeerHost host.Host // the network host (server+client)
	streams  map[peer.ID]*BasicStream
}

//NewNode creates a new Livepeerd node.
func NewNode(listenPort int, priv crypto.PrivKey, pub crypto.PubKey, f *BasicNotifiee) (*NetworkNode, error) {
	pid, err := peer.IDFromPublicKey(pub)
	if err != nil {
		return nil, err
	}

	streams := make(map[peer.ID]*BasicStream)

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
	nn := &NetworkNode{Identity: pid, Kad: dht, PeerHost: rHost, streams: streams}
	f.HandleDisconnect(func(pid peer.ID) {
		nn.RemoveStream(pid)
	})

	return nn, nil
}

func constructDHTRouting(ctx context.Context, host host.Host, dstore ds.Batching) (*kad.IpfsDHT, error) {
	dhtRouting := kad.NewDHT(ctx, host, dstore)

	dhtRouting.Validator["v"] = &record.ValidChecker{
		Func: func(string, []byte) error {
			return nil
		},
		Sign: false,
	}
	dhtRouting.Selector["v"] = func(_ string, bs [][]byte) (int, error) { return 0, nil }

	if err := dhtRouting.Bootstrap(context.Background()); err != nil {
		glog.Errorf("Error bootstraping dht: %v", err)
		return nil, err
	}
	return dhtRouting, nil
}

func (n *NetworkNode) GetStream(pid peer.ID) *BasicStream {
	strm, ok := n.streams[pid]
	if !ok {
		strm = n.RefreshStream(pid)
	}
	return strm
}

func (n *NetworkNode) RefreshStream(pid peer.ID) *BasicStream {
	// glog.Infof("Creating stream from %v to %v", peer.IDHexEncode(n.Identity), peer.IDHexEncode(pid))
	ns, err := n.PeerHost.NewStream(context.Background(), pid, Protocol)
	if err != nil {
		glog.Errorf("Error creating stream to %v: %v", peer.IDHexEncode(pid), err)
		return nil
	}
	strm := NewBasicStream(ns)
	n.streams[pid] = strm
	return strm
}

func (n *NetworkNode) RemoveStream(pid peer.ID) {
	// glog.Infof("Removing stream for %v", peer.IDHexEncode(pid))
	delete(n.streams, pid)
}
