package main

import (
	"flag"
	"time"
        "fmt"
	"context"
	peerstore "gx/ipfs/QmPgDWmTmuzvP7QE5zwo1TmjbJme9pmZHNujB2453jkCTr/go-libp2p-peerstore"
	net "gx/ipfs/QmNa31VPzC561NWwRsJLE7nGYZYuuD2QfpK2b1q9BK54J1/go-libp2p-net"
	kb "gx/ipfs/QmSAFA8v42u4gpJNy1tb7vW3JiiXiaYDC2b845c2RnNSJL/go-libp2p-kbucket"
	peer "gx/ipfs/QmXYjuNuxVzXKJCfWasQk1RqkhVLDM9jtUKhqc2WPQmFSB/go-libp2p-peer"
	crypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"
        host "gx/ipfs/Qmc1XhrFEiSeBNn3mpfg6gEuYCt5im2gYmNVmncsvmpeAk/go-libp2p-host"
	"github.com/golang/glog"
	basicnet "github.com/livepeer/go-livepeer-basicnet"
)

var timer time.Time

func main() {
    n := flag.Int("n", 10, "No. of nodes")
    flag.Parse()
    flag.Lookup("logtostderr").Value.Set("true")
    flag.Lookup("v").Value.Set("3")
    nodes := make([]*basicnet.NetworkNode,*n,*n) 
    vn := make([]*basicnet.BasicVideoNetwork, *n,*n)
    for  i:= 0; i < *n ; i++ {
        priv, pub, _ := crypto.GenerateKeyPair(crypto.RSA, 2048)
	nodes[i], _ = basicnet.NewNode(15000+i, priv, pub, &basicnet.BasicNotifiee{})
        vn[i],_ = basicnet.NewBasicVideoNetwork(nodes[i], "")
	if err := vn[i].SetupProtocol(); err != nil {
		glog.Errorf("Error creating node: %v", err)
	}
        if i > 0 {
		connectHosts(vn[i-1].NetworkNode.PeerHost, vn[i].NetworkNode.PeerHost)
	}
//        setHandler(nodes[i])
    }
    connectHosts(vn[0].NetworkNode.PeerHost, vn[*n-1].NetworkNode.PeerHost)
    setHandler(nodes[*n-1])
    setHandler(nodes[1])

    strmID := fmt.Sprintf("%vstrmID", peer.IDHexEncode(nodes[1].Identity))
    vn[*n-1].GetSubscriber(strmID)
    subReq := basicnet.SubReqMsg{StrmID: strmID}
    peers, _ := closestLocalPeers(vn[*n-1].NetworkNode.PeerHost.Peerstore(), subReq.StrmID)
    for _, p := range peers {
		//Don't send it back to the requesting peer
	       // glog.Infof("Got peer: %v", peer.IDHexEncode(p))
		if  p == vn[*n-1].NetworkNode.Identity {
			continue
		}

		if p == "" {
			glog.Errorf("Got empty peer from libp2p")
		}

		ns := vn[*n-1].NetworkNode.GetOutStream(p)
		if ns != nil {
			glog.Infof("sending message to %v",peer.IDHexEncode(p))
			if err := ns.SendMessage(basicnet.SubReqID, subReq); err != nil {
 //                       if err := ns.SendMessage(basicnet.StreamDataID, basicnet.StreamDataMsg{StrmID: strmID, Data: []byte("Hello")}); err != nil {
				//Question: Do we want to close the stream here?
				glog.Errorf("Error relaying subReq to %v: %v.", p, err)
				continue
			}

		} else {
			glog.Errorf("Cannot get stream for peer: %v", peer.IDHexEncode(p))
		}
   }
   // check for relayers
   for  i:= 0; i < *n ; i++ {
//         glog.Infof("no %v", vn[0].relayers)
   }

   select {}
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


func closestLocalPeers(ps peerstore.Peerstore, strmID string) ([]peer.ID, error) {
	targetPid, err := extractNodeID(strmID)
	if err != nil {
		glog.Errorf("Error extracting node id from streamID: %v", strmID)
		return nil, basicnet.ErrSubscriber
	}
	localPeers := ps.Peers()
	if len(localPeers) == 1 {
		glog.Errorf("No local peers")
		return nil, basicnet.ErrSubscriber
	}

	return kb.SortClosestPeers(localPeers, kb.ConvertPeerID(targetPid)), nil
}

func extractNodeID(strmOrManifestID string) (peer.ID, error) {
	if len(strmOrManifestID) < 68 {
		return "", basicnet.ErrProtocol
	}

	nid := strmOrManifestID[:68]
	return peer.IDHexDecode(nid)
}

func setHandler(n *basicnet.NetworkNode) {
	n.PeerHost.SetStreamHandler(basicnet.Protocol, func(stream net.Stream) {
		ws := basicnet.NewBasicInStream(stream)

		for {
			if err := streamHandler(ws, n); err != nil {
				glog.Errorf("Error handling stream: %v", err)
				// delete(n.NetworkNode.streams, stream.Conn().RemotePeer())
				stream.Close()
				return
			}
		}
	})
}

func streamHandler(ws *basicnet.BasicInStream, n *basicnet.NetworkNode) error {
	var subReq basicnet.SubReqMsg
	msg, err := ws.ReceiveMessage()
	if err != nil {
		glog.Errorf("Got error decoding msg: %v", err)
		return err
	}

	switch msg.Data.(type) {
		case basicnet.SubReqMsg:
			subReq,_= msg.Data.(basicnet.SubReqMsg)
			if err := n.GetOutStream(ws.Stream.Conn().RemotePeer()).SendMessage(basicnet.StreamDataID, basicnet.StreamDataMsg{StrmID: subReq.StrmID, Data: []byte("Hello from n1")}); err != nil {
				glog.Errorf("Error sending message from n1: %v", err)
	                }
	}
	glog.Infof("%v Recieved msg %v from %v", peer.IDHexEncode(ws.Stream.Conn().LocalPeer()), msg.Op, peer.IDHexEncode(ws.Stream.Conn().RemotePeer()))
	glog.Infof("Time since last message recieved: %v", time.Since(timer))
        time.Sleep(100 * time.Millisecond)
	return nil
}
