package basicnet

import (
	"context"
        "flag"
	"fmt"
	net "gx/ipfs/QmNa31VPzC561NWwRsJLE7nGYZYuuD2QfpK2b1q9BK54J1/go-libp2p-net"
	peer "gx/ipfs/QmXYjuNuxVzXKJCfWasQk1RqkhVLDM9jtUKhqc2WPQmFSB/go-libp2p-peer"
	crypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"
	"sync"
	"testing"
	"time"
	"github.com/ericxtang/m3u8"
	"github.com/golang/glog"
)
var nodeCount = flag.Int("nodeNum", 10, "number of nodes") 
var networkTopology string
var msgType string
var rc = make(chan map[string]string)
var finishResult  FinishStreamMsg


func init() {
     flag.StringVar(&networkTopology,"topo","star","network topology") 
     flag.StringVar(&msgType, "msg","Sub","message type")
     flag.Lookup("logtostderr").Value.Set("true")
     var logLevel string
     flag.StringVar(&logLevel, "logLevel", "3", "test")
     flag.Lookup("v").Value.Set(logLevel)
}

func createNetwork(n int, networkTopo string) ([]*NetworkNode, []*BasicVideoNetwork) {
    nodes := make([]*NetworkNode,n,n) 
    vn := make([]*BasicVideoNetwork, n,n)
    for  i:= 0; i < n ; i++ {
	priv, pub, _ := crypto.GenerateKeyPair(crypto.RSA, 2048)
	nodes[i], _ = NewNode(15000+i, priv, pub, &BasicNotifiee{})
	vn[i],_ = NewBasicVideoNetwork(nodes[i], "")
	if err := vn[i].SetupProtocol(); err != nil {
		glog.Errorf("Error creating node: %v", err)
	}
    }
    for  i:= 0; i < n ; i++ {
        switch networkTopo {
            case "ring":
		      connectHosts(vn[i].NetworkNode.PeerHost, vn[(i+1)%n].NetworkNode.PeerHost)
	    //default topology is star 
            default:
               if i > 0 {  
		       connectHosts(vn[i].NetworkNode.PeerHost, vn[0].NetworkNode.PeerHost)
	       }
        }
    }
    return nodes, vn 

}

/*
Create a network of N nodes(N>2) with 2 different topology options: star and ring, test sending and receiving different types of messages between random nodes and check each node's status after messages are sent
To run the test:
go test -run SendNetworkMsg -args  -topo ring  -msg TranscodeResponse  -nodeNum 5
*/

func TestSendNetworkMsg(t *testing.T) {
    fmt.Println("network topology:",networkTopology)
    nodes, vn := createNetwork(*nodeCount, networkTopology)
    strmID := fmt.Sprintf("%vstrmID", peer.IDHexEncode(nodes[1].Identity))
    subtmp,_ :=vn[*nodeCount-1].GetSubscriber(strmID)
    sub, _ := subtmp.(*BasicSubscriber)
  //  subReq := SubReqMsg{StrmID: strmID}
    b1, _ := vn[1].GetBroadcaster(strmID)
    var cancelMsg CancelSubMsg
    result := make(map[uint64][]byte)
    lock := &sync.Mutex{}
    switch  msgType {
	    // nodeN sends Sub to node1, node1 receives the Sub req and sends data to nodeN, nodeN recieve the data and send Cancel to node1
	    case "Sub","CancelSub": 
                    setHandler(nodes[1])
		    sub.Subscribe(context.Background(), func(seqNo uint64, data []byte, eof bool) {
		//		glog.Infof("Got response: %v, %v", seqNo, data)
				lock.Lock()
				result[seqNo] = data
				lock.Unlock()
		    })

		    if sub.cancelWorker == nil {
				t.Errorf("Cancel function should be assigned")
		    }
		    if !sub.working {
				t.Errorf("Subscriber should be working")
		    }

		    for start := time.Now(); time.Since(start) < 3*time.Second; {
				if len(result) == 5 {
					break
				} else {
					time.Sleep(time.Millisecond * 50)
				}
		    }
		    if len(result) != 5 {
				t.Errorf("Expecting length of result to be 5, but got %v: %v", len(result), result)
		    }

		    for _, d := range result {
				if string(d) != "test data" {
					t.Errorf("Expecting data to be 'test data', but got %v", d)
				}
		    }

		   time.Sleep(1000 * time.Millisecond)
		   if msgType == "CancelSub" {
			   sub.Unsubscribe()

			   for start := time.Now(); time.Since(start) < 1*time.Second; {
					if cancelMsg.StrmID != "" {			break
					} else {
						time.Sleep(50 * time.Millisecond)
					}
			   }
	           }
	    //nodeN first subscribes to node1, node1 sends Finish to nodeN to finish the broadcasting
	    case  "Finish" :
                           setHandler(nodes[*nodeCount-1])
                           sub.Subscribe(context.Background(), func(seqNo uint64, data []byte, eof bool) {
		//		glog.Infof("Got response: %v, %v", seqNo, data)
				lock.Lock()
				result[seqNo] = data
				lock.Unlock()
		           })


		           time.Sleep(1000 * time.Millisecond)
                           if len(vn[1].broadcasters) != 1 {
				                   t.Errorf("Should be 1 broadcaster in n1")
			   }

			   if len(vn[1].broadcasters[strmID].listeners) != 1 {
				   t.Errorf("Should be 1 listener in b1")
	                   }
		           glog.Infof("Listeners: %v",vn[1].broadcasters[strmID].listeners) 
			   err1 := b1.Finish()
			   if err1 != nil {
		                  t.Errorf("Error when broadcasting Finish: %v", err1)
	                   }
			   start := time.Now()
	                   for time.Since(start) < time.Second*5 {
		             if finishResult.StrmID == "" {
			       time.Sleep(100 * time.Millisecond)
		             } else {
			              break
		             }
	                   }
	                   if finishResult.StrmID != strmID {
		                t.Errorf("Expecting finishResult to have strmID: %v, but got %v", strmID, finishResult)
	                   }
            //NodeN sends TranscodeResponse to Node1
	    case  "TranscodeResponse":
                   setHandler(nodes[1])
		   go func() {
			   err := vn[*nodeCount-1].SendTranscodeResponse(peer.IDHexEncode(nodes[1].Identity), fmt.Sprintf("%v:%v", strmID, 0), map[string]string{"strmid1": "P240p30fps4x3", "strmid2": "P360p30fps4x3"})
				if err != nil {
					t.Errorf("Error sending transcode result: %v", err)
				}
		   }()
		   select {
			case r := <-rc:
				if r["strmid1"] != "P240p30fps4x3" {
					t.Errorf("Expecting %v, got %v", "P240p30fps4x3", r["strmid1"])
				}
				if r["strmid2"] != "P360p30fps4x3" {
					t.Errorf("Expecting %v, got %v", "P360p30fps4x3", r["strmid2"])
				}
			case <-time.After(time.Second * 5):
				t.Errorf("Timed out")
		   }
	   //NodeN request MasterPlayList to Node1, node1 sends the MasterPlaylist to nodeN 
	   case "GetMasterPlaylist":
                setHandler(nodes[1])
		mpl := m3u8.NewMasterPlaylist()
		pl, _ := m3u8.NewMediaPlaylist(10, 10)
		mpl.Append("test.m3u8", pl, m3u8.VariantParams{Bandwidth: 100000})
		strmID := fmt.Sprintf("%vba1637fd2531f50f9e8f99a37b48d7cfe12fa498ff6da8d6b63279b4632101d5e8b1c872c", peer.IDHexEncode(vn[1].NetworkNode.Identity))

		//node1 Updates Playlist
		if err := vn[1].UpdateMasterPlaylist(strmID, mpl); err != nil {
			t.Errorf("Error updating master playlist")
		}

		//nodeN Gets Playlist
		mplc, err := vn[*nodeCount-1].GetMasterPlaylist(vn[1].GetNodeID(), strmID)
		if err != nil {
			t.Errorf("Error getting master playlist: %v", err)
		}
		select {
		case r := <-mplc:
			vars := r.Variants
			glog.Infof("got: %v", r)
			if len(vars) != 1 {
				t.Errorf("Expecting 1 variants, but got: %v - %v", len(vars), r)
			}
		case <-time.After(time.Second * 3):
			glog.Infof("n1 mplMap: %v", vn[1].mplMap)
			t.Errorf("Timed out")
		}

	   //NodeN sends GetNodeStatus to Node1, node1 sends the NodeStatus to NodeN
	   case "GetNodeStatus":
                setHandler(nodes[1])
		//Add a manifest
		mpl := m3u8.NewMasterPlaylist()
		pl, _ := m3u8.NewMediaPlaylist(10, 10)
		mpl.Append("test.m3u8", pl, m3u8.VariantParams{Bandwidth: 100000})
		vn[1].UpdateMasterPlaylist("testStrm", mpl)
		sc, err := vn[*nodeCount-1].GetNodeStatus(vn[1].GetNodeID())
		if err != nil {
			t.Errorf("Error: %v", err)
		}
		status := <-sc
		if len(status.Manifests) != 1 {
			t.Errorf("Expecting 1 manifest, but got %v", status.Manifests)
		}
	}
	  //check node status(subscribers,relayers..) 
	for i:=0; i<*nodeCount; i++ {
		 glog.Infof("node%v subsribers: %v relayers: %v",i, vn[i].subscribers,vn[i].relayers)
	}

}


func setHandler(n *NetworkNode) {
	n.PeerHost.SetStreamHandler(Protocol, func(stream net.Stream) {
		ws := NewBasicInStream(stream)

		for {
			if err := eventHandler(ws, n); err != nil {
				glog.Errorf("Error handling stream: %v", err)
				// delete(n.NetworkNode.streams, stream.Conn().RemotePeer())
				stream.Close()
				return
			}
		}
	})
}

func eventHandler(ws *BasicInStream, n *NetworkNode) error {
	var subReq SubReqMsg
	var cancelMsg CancelSubMsg
        //rc := make(chan map[string]string)
	msg, err := ws.ReceiveMessage()
	if err != nil {
		glog.Errorf("Got error decoding msg: %v", err)
		return err
	}

	glog.Infof("%v Recieved msg %v from %v", peer.IDHexEncode(ws.Stream.Conn().LocalPeer()), msg.Op, peer.IDHexEncode(ws.Stream.Conn().RemotePeer()))
	switch msg.Data.(type) {
		case SubReqMsg:
			subReq,_= msg.Data.(SubReqMsg)
			//send data message to subsriber
                        for i := 0; i < 5; i++ {
					//TODO: Sleep here is needed, because we can't handle the messages fast enough.
					//I think we need to re-organize our code to kick off goroutines / workers instead of handling everything in a for loop.
					time.Sleep(time.Millisecond * 100)
					if err := n.GetOutStream(ws.Stream.Conn().RemotePeer()).SendMessage(StreamDataID, StreamDataMsg{SeqNo: uint64(i),StrmID: subReq.StrmID, Data: []byte("test data")}); err != nil {
					         glog.Errorf("Error sending message from n1: %v", err)
	                                }
			}
			//handle broadcaster logic
			handleSubReq(n.Network, subReq, ws.Stream.Conn().RemotePeer())
                case CancelSubMsg:
				cancelMsg, _ = msg.Data.(CancelSubMsg)
				glog.Infof("Got CancelMsg %v", cancelMsg)
                case TranscodeResponseMsg:
		       _, ok := msg.Data.(TranscodeResponseMsg)
		       if !ok {
			       glog.Errorf("Cannot convert TranscodeResponseMsg: %v", msg.Data)
			       return ErrProtocol
		       }
	        //      glog.Infof("n1 got Msg: %v", msg)
		      rc <- msg.Data.(TranscodeResponseMsg).Result
		case GetMasterPlaylistReqMsg:
		//Get the local master playlist from a broadcaster and send it back
			mplr, ok := msg.Data.(GetMasterPlaylistReqMsg)
			if !ok {
				glog.Errorf("Cannot convert GetMasterPlaylistReqMsg: %v", msg.Data)
				return ErrProtocol
			}
			return handleGetMasterPlaylistReq(n.Network, ws.Stream.Conn().RemotePeer(), mplr)
                case NodeStatusReqMsg:
			nsr, ok := msg.Data.(NodeStatusReqMsg)
			if !ok {
				glog.Errorf("Cannot convert NodeStatusReqMsg: %v", msg)
				return ErrProtocol
			}
			return handleNodeStatusReqMsg(n.Network, ws.Stream.Conn().RemotePeer(), nsr)
                case FinishStreamMsg:
			finishResult, _ = msg.Data.(FinishStreamMsg)
			glog.Infof("Got FinishMsg %v", finishResult)
	        }
	return nil
}
