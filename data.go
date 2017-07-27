package net

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
)

//Standard Profiles:
//1080p_60fps: 9000kbps
//1080p_30fps: 6000kbps
//720p_60fps: 6000kbps
//720p_30fps: 4000kbps
//480p_30fps: 2000kbps
//360p_30fps: 1000kbps
//240p_30fps: 700kbps
//144p_30fps: 400kbps
type VideoProfile struct {
	Name        string
	Bitrate     string
	Framerate   uint
	AspectRatio string
	Resolution  string
}

//Some sample video profiles
var (
	P_720P_60FPS_16_9 = VideoProfile{Name: "P720P60FPS169", Bitrate: "6000k", Framerate: 60, AspectRatio: "16:9", Resolution: "1280:720"}
	P_720P_30FPS_16_9 = VideoProfile{Name: "P720P30FPS169", Bitrate: "4000k", Framerate: 30, AspectRatio: "16:9", Resolution: "1280:720"}
	P_720P_30FPS_4_3  = VideoProfile{Name: "P720P30FPS43", Bitrate: "4000k", Framerate: 30, AspectRatio: "4:3", Resolution: "960:720"}
	P_360P_30FPS_16_9 = VideoProfile{Name: "P360P30FPS169", Bitrate: "1000k", Framerate: 30, AspectRatio: "16:9", Resolution: "640:360"}
	P_360P_30FPS_4_3  = VideoProfile{Name: "P360P30FPS43", Bitrate: "1000k", Framerate: 30, AspectRatio: "4:3", Resolution: "480:360"}
	P_240P_30FPS_16_9 = VideoProfile{Name: "P240P30FPS169", Bitrate: "700k", Framerate: 30, AspectRatio: "16:9", Resolution: "426:240"}
	P_240P_30FPS_4_3  = VideoProfile{Name: "P240P30FPS43", Bitrate: "700k", Framerate: 30, AspectRatio: "4:3", Resolution: "320:240"}
	P_144P_30FPS_16_9 = VideoProfile{Name: "P144P30FPS169", Bitrate: "400k", Framerate: 30, AspectRatio: "16:9", Resolution: "256:144"}
)

var VideoProfileLookup = map[string]VideoProfile{
	"P720P60FPS169": P_720P_60FPS_16_9,
	"P720P30FPS169": P_720P_30FPS_16_9,
	"P720P30FPS43":  P_720P_30FPS_4_3,
	"P360P30FPS169": P_360P_30FPS_16_9,
	"P360P30FPS43":  P_360P_30FPS_4_3,
	"P240P30FPS169": P_240P_30FPS_16_9,
	"P240P30FPS43":  P_240P_30FPS_4_3,
	"P144P30FPS169": P_144P_30FPS_16_9,
}

type TranscodeConfig struct {
	StrmID              string
	Profiles            []VideoProfile
	PerformOnchainClaim bool
	JobID               *big.Int
}

type Opcode uint8

const (
	StreamDataID Opcode = iota
	FinishStreamID
	SubReqID
	CancelSubID
	TranscodeResultID
	SimpleString
)

type Msg struct {
	Op   Opcode
	Data interface{}
}

type msgAux struct {
	Op   Opcode
	Data []byte
}

type SubReqMsg struct {
	StrmID string
	// SubNodeID string
	//TODO: Add Signature
}

type CancelSubMsg struct {
	StrmID string
}

type FinishStreamMsg struct {
	StrmID string
}

type StreamDataMsg struct {
	SeqNo  uint64
	StrmID string
	Data   []byte
}

type TranscodeResultMsg struct {
	//map of streamid -> video description
	StrmID string
	Result map[string]string
}

func (m Msg) MarshalJSON() ([]byte, error) {
	// Encode m.Data into a gob
	var b bytes.Buffer
	enc := gob.NewEncoder(&b)
	switch m.Data.(type) {
	case SubReqMsg:
		gob.Register(SubReqMsg{})
		err := enc.Encode(m.Data.(SubReqMsg))
		if err != nil {
			return nil, fmt.Errorf("Failed to marshal Handshake: %v", err)
		}
	case CancelSubMsg:
		gob.Register(CancelSubMsg{})
		err := enc.Encode(m.Data.(CancelSubMsg))
		if err != nil {
			return nil, fmt.Errorf("Failed to marshal CancelSubMsg: %v", err)
		}
	case StreamDataMsg:
		gob.Register(StreamDataMsg{})
		err := enc.Encode(m.Data.(StreamDataMsg))
		if err != nil {
			return nil, fmt.Errorf("Failed to marshal StreamDataMsg: %v", err)
		}
	case FinishStreamMsg:
		gob.Register(FinishStreamMsg{})
		err := enc.Encode(m.Data.(FinishStreamMsg))
		if err != nil {
			return nil, fmt.Errorf("Failed to marshal FinishStreamMsg: %v", err)
		}
	case TranscodeResultMsg:
		gob.Register(TranscodeResultMsg{})
		err := enc.Encode(m.Data.(TranscodeResultMsg))
		if err != nil {
			return nil, fmt.Errorf("Failed to marshal TranscodeResultMsg: %v", err)
		}
	case string:
		err := enc.Encode(m.Data)
		if err != nil {
			return nil, fmt.Errorf("Failed to marshal string: %v", err)
		}
	default:
		return nil, errors.New("failed to marshal message data")
	}

	// build an aux and marshal using built-in json
	aux := msgAux{Op: m.Op, Data: b.Bytes()}
	return json.Marshal(aux)
}

func (m *Msg) UnmarshalJSON(b []byte) error {
	// Use builtin json to unmarshall into aux
	var aux msgAux
	json.Unmarshal(b, &aux)

	// The Op field in aux is already what we want for m.Op
	m.Op = aux.Op

	// decode the gob in aux.Data and put it in m.Data
	dec := gob.NewDecoder(bytes.NewBuffer(aux.Data))
	switch aux.Op {
	case SubReqID:
		var sr SubReqMsg
		err := dec.Decode(&sr)
		if err != nil {
			return errors.New("failed to decode handshake")
		}
		m.Data = sr
	case CancelSubID:
		var cs CancelSubMsg
		err := dec.Decode(&cs)
		if err != nil {
			return errors.New("failed to decode CancelSubMsg")
		}
		m.Data = cs
	case StreamDataID:
		var sd StreamDataMsg
		err := dec.Decode(&sd)
		if err != nil {
			return errors.New("failed to decode StreamDataMsg")
		}
		m.Data = sd
	case FinishStreamID:
		var fs FinishStreamMsg
		err := dec.Decode(&fs)
		if err != nil {
			return errors.New("failed to decode FinishStreamMsg")
		}
		m.Data = fs
	case TranscodeResultID:
		var tr TranscodeResultMsg
		err := dec.Decode(&tr)
		if err != nil {
			return errors.New("failed to decode TranscodeResultMsg")
		}
		m.Data = tr
	case SimpleString:
		var str string
		err := dec.Decode(&str)
		if err != nil {
			return errors.New("Failed to decode string msg")
		}
		m.Data = str

	default:
		return errors.New("failed to decode message data")
	}

	return nil
}
