package basicnet

import (
	"context"
        "flag"
	"fmt"
	peer "gx/ipfs/QmXYjuNuxVzXKJCfWasQk1RqkhVLDM9jtUKhqc2WPQmFSB/go-libp2p-peer"
	crypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"
	"sync"
	"testing"
	"time"
	"github.com/ericxtang/m3u8"
	"github.com/golang/glog"
)
var msgType string
var runIntegrationTests = flag.Bool("integration", false, "Run the integration tests ")

func init() {
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
go test -integration
*/
func runIntegrationTest(n int, networkTopo string, msgType string){
    fmt.Println("network topology:",networkTopo)
    nodes, vn := createNetwork(n, networkTopo)
    strmID := fmt.Sprintf("%vstrmID", peer.IDHexEncode(nodes[1].Identity))
    subtmp,_ :=vn[n-1].GetSubscriber(strmID)
    sub, _ := subtmp.(*BasicSubscriber)
    b1, _ := vn[1].GetBroadcaster(strmID)
    result := make(map[uint64][]byte)
    lock := &sync.Mutex{}
    switch  msgType {
	    // nodeN sends Sub to node1, node1 receives the Sub req and sends data to nodeN, nodeN recieve the data and send Cancel to node1
	    case "Sub","CancelSub": 
		    sub.Subscribe(context.Background(), func(seqNo uint64, data []byte, eof bool) {
		//		glog.Infof("Got response: %v, %v", seqNo, data)
				lock.Lock()
				result[seqNo] = data
				lock.Unlock()
		    })

		    if sub.cancelWorker == nil {
				glog.Errorf("Cancel function should be assigned")
		    }
		    if !sub.working {
				glog.Errorf("Subscriber should be working")
		    }

		    err := b1.Broadcast(100, []byte("test data"))
		    if err != nil {
				glog.Errorf("Error broadcasting: %v", err)
		    }

		    for start := time.Now(); time.Since(start) < 3*time.Second; {
				if len(result) == 1 {
					break
				} else {
					time.Sleep(time.Millisecond * 50)
				}
		    }
		    if len(result) != 1 {
				glog.Errorf("Expecting length of result to be 5, but got %v: %v", len(result), result)
		    }

		    for _, d := range result {
				if string(d) != "test data" {
					glog.Errorf("Expecting data to be 'test data', but got %v", d)
				}
		    }

		   time.Sleep(1000 * time.Millisecond)
		   if msgType == "CancelSub" {
			   sub.Unsubscribe()
	           }
	    //nodeN first subscribes to node1, node1 sends Finish to nodeN to finish the broadcasting
	    case  "Finish" :
                           sub.Subscribe(context.Background(), func(seqNo uint64, data []byte, eof bool) {
		//		glog.Infof("Got response: %v, %v", seqNo, data)
				lock.Lock()
				result[seqNo] = data
				lock.Unlock()
		           })


		           time.Sleep(1000 * time.Millisecond)
                           err := b1.Broadcast(100, []byte("test data"))
		           if err != nil {
				glog.Errorf("Error broadcasting: %v", err)
		           }


			   if len(vn[1].broadcasters) != 1 {
				                   glog.Errorf("Should be 1 broadcaster in n1")
			   }

			   if len(vn[1].broadcasters[strmID].listeners) != 1 {
				   glog.Errorf("Should be 1 listener in b1")
	                   }
		     //      glog.Infof("Listeners: %v",vn[1].broadcasters[strmID].listeners) 
			   for start := time.Now(); time.Since(start) < 3*time.Second; {
				if len(result) == 1 {
					break
				} else {
					time.Sleep(time.Millisecond * 50)
				}
		           }
		           err1 := b1.Finish()
			   if err1 != nil {
		                  glog.Errorf("Error when broadcasting Finish: %v", err1)
	                   }
            //NodeN sends TranscodeResponse to Node1
	    case  "TranscodeResponse":
		   go func() {
			   err := vn[n-1].SendTranscodeResponse(peer.IDHexEncode(nodes[1].Identity), fmt.Sprintf("%v:%v", strmID, 0), map[string]string{"strmid1": "P240p30fps4x3", "strmid2": "P360p30fps4x3"})
				if err != nil {
					glog.Errorf("Error sending transcode result: %v", err)
				}
		   }()
	           time.Sleep(1000 * time.Millisecond)		
	   //NodeN request MasterPlayList to Node1, node1 sends the MasterPlaylist to nodeN 
	   case "GetMasterPlaylist":
		mpl := m3u8.NewMasterPlaylist()
		pl, _ := m3u8.NewMediaPlaylist(10, 10)
		mpl.Append("test.m3u8", pl, m3u8.VariantParams{Bandwidth: 100000})
		strmID := fmt.Sprintf("%vba1637fd2531f50f9e8f99a37b48d7cfe12fa498ff6da8d6b63279b4632101d5e8b1c872c", peer.IDHexEncode(vn[1].NetworkNode.Identity))

		//node1 Updates Playlist
		if err := vn[1].UpdateMasterPlaylist(strmID, mpl); err != nil {
			glog.Errorf("Error updating master playlist")
		}

		//nodeN Gets Playlist
		mplc, err := vn[n-1].GetMasterPlaylist(vn[1].GetNodeID(), strmID)
		if err != nil {
			glog.Errorf("Error getting master playlist: %v", err)
		}
		select {
		case r := <-mplc:
			vars := r.Variants
		//	glog.Infof("got: %v", r)
			if len(vars) != 1 {
				glog.Errorf("Expecting 1 variants, but got: %v - %v", len(vars), r)
			}
		case <-time.After(time.Second * 3):
			glog.Infof("n1 mplMap: %v", vn[1].mplMap)
			glog.Errorf("Timed out")
		}

	   //NodeN sends GetNodeStatus to Node1, node1 sends the NodeStatus to NodeN
	   case "GetNodeStatus":
		//Add a manifest
		mpl := m3u8.NewMasterPlaylist()
		pl, _ := m3u8.NewMediaPlaylist(10, 10)
		mpl.Append("test.m3u8", pl, m3u8.VariantParams{Bandwidth: 100000})
		vn[1].UpdateMasterPlaylist("testStrm", mpl)
		sc, err := vn[n-1].GetNodeStatus(vn[1].GetNodeID())
		if err != nil {
			glog.Errorf("Error: %v", err)
		}
		status := <-sc
		if len(status.Manifests) != 1 {
			glog.Errorf("Expecting 1 manifest, but got %v", status.Manifests)
		}
	}
	  //check node status(subscribers,relayers..) 
	for i:=0; i<n; i++ {
		 glog.Infof("node%v subsribers: %v relayers: %v",i, vn[i].subscribers,vn[i].relayers)
	}
	time.Sleep(1000 * time.Millisecond)



}

func TestSendNetworkMsg(t *testing.T) {
    if !*runIntegrationTests {
	        t.Skip("To run this test, use: go test -integration")
    }
    glog.Infof("\n\nIntegration testing...")
    runIntegrationTest(5,"star","Sub")
    runIntegrationTest(10,"ring","Finish")
    runIntegrationTest(5,"star","TranscodeResponse")
//    runIntegrationTest(10,"ring","GetMasterPlaylist")
//    runIntegrationTest(5,"ring","GetNodeStatus")

}



