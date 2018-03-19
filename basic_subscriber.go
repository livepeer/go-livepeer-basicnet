package basicnet

import (
	"context"
	"errors"
	"fmt"
	"time"

	inet "gx/ipfs/QmNa31VPzC561NWwRsJLE7nGYZYuuD2QfpK2b1q9BK54J1/go-libp2p-net"
	kb "gx/ipfs/QmSAFA8v42u4gpJNy1tb7vW3JiiXiaYDC2b845c2RnNSJL/go-libp2p-kbucket"
	ma "gx/ipfs/QmXY77cVe7rVRQXZZQRioukUM7aRW3BTcAgJe12MCtb3Ji/go-multiaddr"
	peer "gx/ipfs/QmXYjuNuxVzXKJCfWasQk1RqkhVLDM9jtUKhqc2WPQmFSB/go-libp2p-peer"

	"github.com/golang/glog"
	"github.com/livepeer/go-livepeer/common"
)

var SubscriberDataInsertTimeout = time.Second * 300
var InsertDataWaitTime = time.Second * 10
var ErrSubscriber = errors.New("ErrSubscriber")

//BasicSubscriber keeps track of
type BasicSubscriber struct {
	Network *BasicVideoNetwork
	// host    host.Host
	msgChan chan StreamDataMsg
	// networkStream *BasicStream
	StrmID       string
	UpstreamPeer peer.ID
	working      bool
	cancelWorker context.CancelFunc
}

func (s *BasicSubscriber) InsertData(sd *StreamDataMsg) error {
	go func(sd *StreamDataMsg) {
		if s.working {
			timer := time.NewTimer(InsertDataWaitTime)
			select {
			case s.msgChan <- *sd:
				// glog.V(4).Infof("Data segment %v for %v inserted. (%v)", sd.SeqNo, sd.StrmID, time.Since(start))
			case <-timer.C:
				glog.Errorf("Subscriber data insert timed out: %v", sd.StrmID)
			}
		}
	}(sd)
	return nil
}

//Subscribe kicks off a go routine that calls the gotData func for every new video chunk
func (s *BasicSubscriber) Subscribe(ctx context.Context, gotData func(seqNo uint64, data []byte, eof bool)) error {
	//Do we already have the broadcaster locally? If we do, just subscribe to it and listen.
	if b := s.Network.broadcasters[s.StrmID]; b != nil {
		localS := NewLocalOutStream(s)
		b.AddListeningStream("localSub", localS)

		ctxW, cancel := context.WithCancel(context.Background())
		s.cancelWorker = cancel
		s.working = true
		s.startWorker(ctxW, nil, gotData)
		return nil
	}

	//If we don't, send subscribe request, listen for response
	localPeers := s.Network.NetworkNode.GetPeers()
	if len(localPeers) == 1 {
		glog.Errorf("No local peers")
		return ErrSubscriber
	}
	targetPid, err := extractNodeID(s.StrmID)
	if err != nil {
		glog.Errorf("Error extracting node id from streamID: %v", s.StrmID)
		return ErrSubscriber
	}
	peers := kb.SortClosestPeers(localPeers, kb.ConvertPeerID(targetPid))

	for _, p := range peers {
		if p == s.Network.NetworkNode.ID() {
			continue
		}
		//Question: Where do we close the stream? If we only close on "Unsubscribe", we may leave some streams open...
		glog.V(5).Infof("New peer from kademlia: %v", peer.IDHexEncode(p))
		ns := s.Network.NetworkNode.GetOutStream(p)
		if ns != nil {
			//Send TranscodeSub
			glog.Infof("%v Sending TranscodeSub to %v", s.Network.NetworkNode.ID().String(), p.String())
			localNodeID := peer.IDB58Encode(s.Network.NetworkNode.ID())
			ipfs, err := ma.NewMultiaddr("/ipfs/" + localNodeID)
			if err != nil {
				glog.Errorf("Unable to create IPFS multiaddr for local node : %v", err)
				return err
			}
			maddrs := make([]ma.Multiaddr, len(s.Network.NetworkNode.Host().Addrs()))
			for i, v := range s.Network.NetworkNode.Host().Addrs() {
				maddrs[i] = v.Encapsulate(ipfs)
			}
			ts := TranscodeSubMsg{
				MultiAddrs: maddrs,
				StrmID:     s.StrmID,
			}
			var sig []byte
			sig, err = s.Network.NetworkNode.Sign(ts.BytesForSigning())
			if err != nil {
				glog.Errorf("Error signing TranscodeSubMsg: %v", err)
				return err
			}
			ts.Sig = sig
			s.Network.NetworkNode.Host().Network().Notify(s)
			if err = s.Network.sendMessageWithRetry(p, ns, TranscodeSubID, ts); err != nil {
				glog.Errorf("Error sending SubReq to %v: %v", peer.IDHexEncode(p), err)
			}
			ctxW, cancel := context.WithCancel(context.Background())
			s.cancelWorker = cancel
			s.working = true
			// s.networkStream = ns
			s.UpstreamPeer = p
			s.startWorker(ctxW, ns, gotData)
			return nil
		}
	}

	glog.Errorf("Cannot subscribe from any of the peers: %v", peers)
	return ErrNoClosePeers

	//Call gotData for every new piece of data
}

func (s *BasicSubscriber) startWorker(ctxW context.Context, ws *BasicOutStream, gotData func(seqNo uint64, data []byte, eof bool)) {
	//We expect DataStreamMsg to come back
	go func() {
		for {
			//Get message from the msgChan (inserted from the network by StreamDataMsg)
			//Call gotData(seqNo, data)
			//Question: What happens if the handler gets stuck?
			start := time.Now()
			select {
			case msg := <-s.msgChan:
				networkWaitTime := time.Since(start)
				go gotData(msg.SeqNo, msg.Data, false)
				glog.V(common.DEBUG).Infof("Subscriber worker inserted segment: %v - took %v in total, %v waiting for data", msg.SeqNo, time.Since(start), networkWaitTime)
			case <-ctxW.Done():
				// s.networkStream = nil
				s.working = false
				glog.Infof("Done with subscription, sending CancelSubMsg")
				//Send EOF
				go gotData(0, nil, true)
				if ws != nil {
					if err := s.Network.sendMessageWithRetry(ws.Stream.Conn().RemotePeer(), ws, CancelSubID, CancelSubMsg{StrmID: s.StrmID}); err != nil {
						glog.Errorf("Error sending CancelSubMsg during worker cancellation: %v", err)
					}
				}
				return
			}
		}
	}()
}

//Unsubscribe unsubscribes from the broadcast
func (s *BasicSubscriber) Unsubscribe() error {
	if s.cancelWorker != nil {
		s.cancelWorker()
	}

	//Remove self from local broadcaster listener pool if it's in there
	if b := s.Network.broadcasters[s.StrmID]; b != nil {
		delete(b.listeners, "localSub")
	}

	//Remove self from network
	delete(s.Network.subscribers, s.StrmID)
	s.Network.NetworkNode.Host().Network().StopNotify(s)

	return nil
}

func (s BasicSubscriber) String() string {
	return fmt.Sprintf("StreamID: %v, working: %v", s.StrmID, s.working)
}

func (s *BasicSubscriber) IsLive() bool {
	return s.working
}

// Notifiee
func (s *BasicSubscriber) HandleConnection(conn inet.Conn) {
}
func (s *BasicSubscriber) Listen(n inet.Network, m ma.Multiaddr) {
}
func (s *BasicSubscriber) ListenClose(n inet.Network, m ma.Multiaddr) {
}
func (s *BasicSubscriber) OpenedStream(n inet.Network, st inet.Stream) {
}
func (s *BasicSubscriber) ClosedStream(n inet.Network, st inet.Stream) {
}
func (s *BasicSubscriber) Connected(n inet.Network, conn inet.Conn) {
	glog.Infof("%v Connected; processing", conn.LocalPeer())
	broadcasterPid, err := extractNodeID(s.StrmID)
	if err != nil {
		glog.Errorf("%v Unable to extract NodeID from %v", conn.LocalPeer(), s.StrmID)
		return
	}
	if conn.RemotePeer() != broadcasterPid {
		glog.Infof("%v subscriber got a connection from a non-sub %v", conn.LocalPeer(), conn.RemotePeer())
		return
	}
	// check for duplicated cxns or subs?
	glog.Infof("%v Getting OutStream", conn.LocalPeer())
	go func() {
		ns := s.Network.NetworkNode.GetOutStream(conn.RemotePeer())
		if ns == nil {
			glog.Errorf("%v Unable to create an outstream with %v", conn.LocalPeer(), conn.RemotePeer())
			return
		}
		glog.Infof("%v Sending Message: SubReq", conn.LocalPeer())
		err = s.Network.sendMessageWithRetry(conn.RemotePeer(), ns, SubReqID, SubReqMsg{StrmID: s.StrmID})
		if err != nil {
			glog.Errorf("%v Unable to send SubReq to %v : %v", conn.LocalPeer(), conn.RemotePeer(), err)
			return
		}
		glog.Infof("%v Setting Upstream Peer", conn.LocalPeer())
		s.UpstreamPeer = conn.RemotePeer()
		glog.Infof("%v Subscriber got direct connection from %v", conn.LocalPeer(), conn.RemotePeer())
	}()
}
func (s *BasicSubscriber) Disconnected(n inet.Network, conn inet.Conn) {
	// Resend TranscodeSub periodically if necessary?
}
