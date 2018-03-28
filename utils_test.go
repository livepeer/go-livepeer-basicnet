package basicnet

import (
	"bufio"
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	inet "gx/ipfs/QmNa31VPzC561NWwRsJLE7nGYZYuuD2QfpK2b1q9BK54J1/go-libp2p-net"
	net "gx/ipfs/QmNa31VPzC561NWwRsJLE7nGYZYuuD2QfpK2b1q9BK54J1/go-libp2p-net"
	peerstore "gx/ipfs/QmPgDWmTmuzvP7QE5zwo1TmjbJme9pmZHNujB2453jkCTr/go-libp2p-peerstore"
	netutil "gx/ipfs/QmQGX417WoxKxDJeHqouMEmmH4G1RCENNSzkZYHrXy3Xb3/go-libp2p-netutil"
	kb "gx/ipfs/QmSAFA8v42u4gpJNy1tb7vW3JiiXiaYDC2b845c2RnNSJL/go-libp2p-kbucket"
	ds "gx/ipfs/QmVSase1JP7cq9QkPT46oNwdp9pT6kBkG3oqS14y3QcZjG/go-datastore"
	dssync "gx/ipfs/QmVSase1JP7cq9QkPT46oNwdp9pT6kBkG3oqS14y3QcZjG/go-datastore/sync"
	peer "gx/ipfs/QmXYjuNuxVzXKJCfWasQk1RqkhVLDM9jtUKhqc2WPQmFSB/go-libp2p-peer"
	kad "gx/ipfs/QmYi2NvTAiv2xTNJNcnuz3iXDDT1ViBwLFXmDb2g7NogAD/go-libp2p-kad-dht"
	protocol "gx/ipfs/QmZNkThpqfVXs9GNbexPrfBbXSLNYeKrE7jwFM2oqHbyqN/go-libp2p-protocol"
	crypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"
	bhost "gx/ipfs/Qmbgce14YTWE2qhE49JVvTBPaHTyz3FaFmqQPyuZAz6C28/go-libp2p/p2p/host/basic"
	record "gx/ipfs/QmbxkgUceEcuSZ4ZdBA3x74VUDSSYjHYmmeEqkjxbtZ6Jg/go-libp2p-record"
	host "gx/ipfs/Qmc1XhrFEiSeBNn3mpfg6gEuYCt5im2gYmNVmncsvmpeAk/go-libp2p-host"

	multicodec "github.com/multiformats/go-multicodec"

	"github.com/golang/glog"
)

type StubConn struct {
}

func (c *StubConn) Close() error {
	return nil
}
func (c *StubConn) GetStreams() ([]net.Stream, error) {
	return []net.Stream{}, nil
}

/*
type StubStream struct {
	prot protocol.ID
}

func NewStubStream(pid protocol.ID) *StubStream {
	return &StubStream{prot: pid}
}
func (s *StubStream) Write(p []byte) (n int, err error) {
	return len(p), nil
}
func (s *StubStream) Read(p []byte) (n int, err error) {
	return len(p), nil
}
func (s *StubStream) Conn() inet.Conn {
	return &StubConn{}
}
func (s *StubStream) Close() error {
	return nil
}
func (s *StubStream) Reset() error {
	return nil
}
func (s *StubStream) Protocol() protocol.ID {
	return s.prot
}
func (s *StubStream) SetProtocol(pid protocol.ID) {
	s.prot = pid
}
func (s *StubStream) SetDeadline(t time.Time) error {
	return nil
}
func (s *StubStream) SetReadDeadline(t time.Time) error {
	return nil
}
func (s *StubStream) SetWriteDeadline(t time.Time) error {
	return nil
}
*/

type FooStream struct {
	enc  multicodec.Encoder
	w    *bufio.Writer
	el   *sync.Mutex
	peer peer.ID
}

func (s *FooStream) GetRemotePeer() peer.ID {
	return s.peer
}

func (s *FooStream) SendMessage(opCode Opcode, data interface{}) error {
	return nil
}

type StubNode struct {
	port           int
	id             peer.ID
	store          peerstore.Peerstore
	kad            *kad.IpfsDHT
	outStreams     map[peer.ID]OutStream
	outStreamsLock *sync.Mutex
	//peers []peerstore.PeerInfo
}

func NewStubNode(listen_port int) (*StubNode, error) {
	pid := peer.ID(strconv.Itoa(listen_port))
	s := StubNode{
		id:             pid,
		port:           listen_port,
		store:          peerstore.NewPeerstore(),
		outStreamsLock: &sync.Mutex{},
		outStreams:     make(map[peer.ID]*FooStream),
	}
	return &s, nil
}

func (s *StubNode) ID() peer.ID {
	return s.id
}
func (s *StubNode) GetDHT() *kad.IpfsDHT {
	return s.kad
}
func (s *StubNode) GetStore() peerstore.Peerstore {
	return s.store
}
func (s *StubNode) GetPeers() []peer.ID {
	return s.store.Peers()
}
func (s *StubNode) GetPeerInfo(p peer.ID) peerstore.PeerInfo {
	return s.store.PeerInfo(p)
}
func (s *StubNode) AddPeer(pi peerstore.PeerInfo, ttl time.Duration) {
	s.store.AddAddrs(pi.ID, pi.Addrs, ttl)
}
func (s *StubNode) RemovePeer(id peer.ID) {
	s.store.ClearAddrs(id)
}

func (s *StubNode) Connect(ctx context.Context, pi peerstore.PeerInfo) error {
	return nil
}
func (s *StubNode) ClosestLocalPeers(strmID string) ([]peer.ID, error) {
	targetPid, err := extractNodeID(strmID)
	if err != nil {
		glog.Errorf("Error extracting node id from streamID: %v", strmID)
		return nil, ErrSubscriber
	}
	localPeers := s.store.Peers()
	if len(localPeers) == 1 {
		glog.Errorf("No local peers")
		return nil, ErrSubscriber
	}

	return kb.SortClosestPeers(localPeers, kb.ConvertPeerID(targetPid)), nil
}
func (s *StubNode) GetOutStream(pid peer.ID) OutStream {
	s.outStreamsLock.Lock()
	strm, ok := s.outStreams[pid]
	if !ok {
		strm = s.RefreshOutStream(pid)
	}
	s.outStreamsLock.Unlock()
	return strm
}
func (s *StubNode) RefreshOutStream(pid peer.ID) *BasicOutStream {
	glog.Infof("%v Creating stream from to %v", s.id, pid)
	if s, ok := s.outStreams[pid]; ok {
		if err := s.Stream.Reset(); err != nil {
			glog.Errorf("Error resetting connetion: %v", err)
		}
	}
	ns := NewStubStream(Protocol)
	strm := NewBasicOutStream(ns)
	s.outStreams[pid] = strm
	return strm
}
func (n *StubNode) RemoveStream(pid peer.ID) {
	// glog.Infof("Removing stream for %v", peer.IDHexEncode(pid))
	n.outStreamsLock.Lock()
	delete(n.outStreams, pid)
	n.outStreamsLock.Unlock()
}
func (s *StubNode) SetStreamHandler(pid protocol.ID, handler inet.StreamHandler) {
}

func setupDHT(ctx context.Context, t *testing.T, client bool) (*kad.IpfsDHT, host.Host) {
	h := bhost.New(netutil.GenSwarmNetwork(t, ctx))

	dss := dssync.MutexWrap(ds.NewMapDatastore())
	var d *kad.IpfsDHT
	if client {
		d = kad.NewDHTClient(ctx, h, dss)
	} else {
		d = kad.NewDHT(ctx, h, dss)
	}

	d.Validator["v"] = &record.ValidChecker{
		Func: func(string, []byte) error {
			return nil
		},
		Sign: false,
	}
	d.Selector["v"] = func(_ string, bs [][]byte) (int, error) { return 0, nil }
	return d, h
}

func setupDHTS(ctx context.Context, n int, t *testing.T) ([]*kad.IpfsDHT, []host.Host) {
	dhts := make([]*kad.IpfsDHT, n)
	hosts := make([]host.Host, n)
	// addrs := make([]ma.Multiaddr, n)
	// peers := make([]peer.ID, n)

	sanityAddrsMap := make(map[string]struct{})
	sanityPeersMap := make(map[string]struct{})

	for i := 0; i < n; i++ {
		dht, h := setupDHT(ctx, t, false)
		dhts[i] = dht
		hosts[i] = h
		// peers[i] = h.ID()
		// addrs[i] = h.Addrs()[0]

		if _, lol := sanityAddrsMap[h.Addrs()[0].String()]; lol {
			t.Fatal("While setting up DHTs address got duplicated.")
		} else {
			sanityAddrsMap[h.Addrs()[0].String()] = struct{}{}
		}
		if _, lol := sanityPeersMap[h.ID().String()]; lol {
			t.Fatal("While setting up DHTs peerid got duplicated.")
		} else {
			sanityPeersMap[h.ID().String()] = struct{}{}
		}
	}

	return dhts, hosts
}

func setupNodes(t *testing.T, p1, p2 int) (*BasicVideoNetwork, *BasicVideoNetwork) {
	/*
		priv1, pub1, _ := crypto.GenerateKeyPair(crypto.RSA, 2048)
		no1, _ := NewNode(p1, priv1, pub1, &BasicNotifiee{})
	*/
	no1, _ := NewStubNode(p1)
	n1, _ := NewBasicVideoNetwork(no1, "")
	if err := n1.SetupProtocol(); err != nil {
		t.Errorf("Error creating node: %v", err)
	}

	/*
		priv2, pub2, _ := crypto.GenerateKeyPair(crypto.RSA, 2048)
		no2, _ := NewNode(p2, priv2, pub2, &BasicNotifiee{})
	*/
	no2, _ := NewStubNode(p2)
	n2, _ := NewBasicVideoNetwork(no2, "")
	if err := n2.SetupProtocol(); err != nil {
		t.Errorf("Error creating node: %v", err)
	}

	return n1, n2
}

func connectNoSync(t *testing.T, ctx context.Context, a, b host.Host) {
	idB := b.ID()
	addrB := b.Addrs()
	if len(addrB) == 0 {
		t.Fatal("peers setup incorrectly: no local address")
	}

	a.Peerstore().AddAddrs(idB, addrB, peerstore.TempAddrTTL)
	pi := peerstore.PeerInfo{ID: idB}
	if err := a.Connect(ctx, pi); err != nil {
		t.Fatal(err)
	}
}

func connect(t *testing.T, ctx context.Context, a, b *kad.IpfsDHT, ah, bh host.Host) {
	connectNoSync(t, ctx, ah, bh)

	// loop until connection notification has been received.
	// under high load, this may not happen as immediately as we would like.
	for a.FindLocal(bh.ID()).ID == "" {
		time.Sleep(time.Millisecond * 5)
	}

	for b.FindLocal(ah.ID()).ID == "" {
		time.Sleep(time.Millisecond * 5)
	}
}

func connectHosts(h1, h2 host.Host) {
	h1.Peerstore().AddAddrs(h2.ID(), h2.Addrs(), peerstore.PermanentAddrTTL)
	h2.Peerstore().AddAddrs(h1.ID(), h1.Addrs(), peerstore.PermanentAddrTTL)
	err := h1.Connect(context.Background(), peerstore.PeerInfo{ID: h2.ID()})
	if err != nil {
		glog.Errorf("Cannot connect h1 with h2: %v", err)
	}
	err = h2.Connect(context.Background(), peerstore.PeerInfo{ID: h1.ID()})
	if err != nil {
		glog.Errorf("Cannot connect h2 with h1: %v", err)
	}

	// Connection might not be formed right away under high load.  See https://github.com/libp2p/go-libp2p-kad-dht/blob/master/dht_test.go (func connect)
	time.Sleep(time.Millisecond * 100)
}

func simpleNodes(p1, p2 int) (*BasicNetworkNode, *BasicNetworkNode) {
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
