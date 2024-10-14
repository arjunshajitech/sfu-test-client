package main

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
	"log"
	"net/url"
	"strconv"
	"time"
)

type Message struct {
	PeerType string      `json:"peerType"`
	Type     string      `json:"type"`
	Data     interface{} `json:"data"`
}

func StartTest(meetingId string, noOfParticipants int) {

	for i := range noOfParticipants {
		name := "Name-" + strconv.Itoa(i)

		token, _ := createToken(meetingId, name)
		addr := "192.168.1.5:8080"
		path := "/websocket"
		query := "auth=" + token

		u := url.URL{Scheme: "ws", Host: addr, Path: path, RawQuery: query}

		connection, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
		if err != nil {
			log.Fatal("Error on socket connect", err)
		}

		log.Println("Connected to socket with meetingId : ", meetingId, " and participantName : ", name)

		handleConnection(connection)
	}
}

func handleConnection(connection *websocket.Conn) {

	mainChannel := make(chan string, 10)
	go handleMainChannel(mainChannel, connection)

	senderPeer, _ := getPeerConnection()
	receiverPeer, _ := getPeerConnection()

	senderPeer.OnConnectionStateChange(func(p webrtc.PeerConnectionState) {
		switch p {
		case webrtc.PeerConnectionStateFailed:
			log.Println("Sender Peer Connection State Failed")
		case webrtc.PeerConnectionStateClosed:
			log.Println("Sender Peer Connection State Closed")
		case webrtc.PeerConnectionStateDisconnected:
			log.Println("Sender Peer Connection State Disconnected")
		case webrtc.PeerConnectionStateConnected:
			log.Println("Sender Peer Connection State Connected")
		default:
		}
	})

	senderPeer.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		switch state {
		case webrtc.ICEConnectionStateConnected:
			log.Println("Sender Peer ICE Connection State Connected")
		default:
		}
	})

	receiverPeer.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		switch state {
		case webrtc.ICEConnectionStateConnected:
			log.Println("Receiver Peer ICE Connection State Connected")
		default:
		}
	})

	receiverPeer.OnConnectionStateChange(func(p webrtc.PeerConnectionState) {
		switch p {
		case webrtc.PeerConnectionStateFailed:
			log.Println("Receiver Peer Connection State Failed")
		case webrtc.PeerConnectionStateClosed:
			log.Println("Receiver Peer Connection State Closed")
		case webrtc.PeerConnectionStateDisconnected:
			log.Println("Receiver Peer Connection State Disconnected")
		case webrtc.PeerConnectionStateConnected:
			log.Println("Receiver Peer Connection State Connected")
		default:
		}
	})

	go func() {
		for {
			_, message, err := connection.ReadMessage()
			if err != nil {
				log.Println("Error on socket read message", err)
				return
			}

			data := &Message{}
			err = json.Unmarshal(message, data)
			if err != nil {
				log.Println("Error on socket unmarshal", err)
			}

			switch data.Type {
			case "offer":
				{
					handleOffer(receiverPeer, data, mainChannel)
				}
			case "answer":
				{
					handleAnswer(senderPeer, data)
				}
			}
		}
	}()

	initialMessage(senderPeer, mainChannel)

}

func handleMainChannel(channel chan string, connection *websocket.Conn) {
	for {
		select {
		case message := <-channel:
			{
				w := &Message{}
				err := json.Unmarshal([]byte(message), &w)
				if err != nil {
					return
				}
				err = connection.WriteJSON(w)
				if err != nil {
					return
				}
			}
		}
	}
}

func initialMessage(senderPeer *webrtc.PeerConnection, mainChannel chan string) {
	transceiverInitSendOnly := webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly}

	fakeAudioTrackLocal, _ := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio_"+uuid.NewString(), uuid.NewString())
	_, err := senderPeer.AddTransceiverFromTrack(fakeAudioTrackLocal, transceiverInitSendOnly)

	fakeVideoTrackLocal, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video_"+uuid.NewString(), uuid.NewString())
	_, err = senderPeer.AddTransceiverFromTrack(fakeVideoTrackLocal, transceiverInitSendOnly)

	// Video: running at ~30 fps (33ms per frame)
	go func() {
		ticker := time.NewTicker(time.Millisecond * 33) // 30fps
		defer ticker.Stop()

		sequenceNumber := uint16(0)
		ssrc := uint32(12345)
		timestamp := uint32(0)

		for range ticker.C {
			rtpHeader := createRTPHeader(96, sequenceNumber, timestamp, ssrc)
			videoPayload := []byte{0x00, 0x00, 0x00}
			rtpPacket := append(rtpHeader, videoPayload...)

			_, err := fakeVideoTrackLocal.Write(rtpPacket)
			if err != nil {
				fmt.Println("Error writing video sample:", err)
				return
			}

			sequenceNumber++
			timestamp += 3000 // Simulate timestamp increment for 30fps video
		}
	}()

	// Audio: running at ~50 fps (20ms per sample)
	go func() {
		ticker := time.NewTicker(time.Millisecond * 20)
		defer ticker.Stop()

		sequenceNumber := uint16(0)
		ssrc := uint32(67890)
		timestamp := uint32(0)

		for range ticker.C {
			// Create an RTP header for audio
			rtpHeader := createRTPHeader(97, sequenceNumber, timestamp, ssrc)
			audioPayload := []byte{0xF8, 0xFF, 0xFE} // Simulated audio payload
			rtpPacket := append(rtpHeader, audioPayload...)

			_, err := fakeAudioTrackLocal.Write(rtpPacket)
			if err != nil {
				fmt.Println("Error writing audio sample:", err)
				return
			}

			sequenceNumber++
			timestamp += 160 // Simulate timestamp increment for 20ms audio
		}
	}()

	offerSDP, _ := senderPeer.CreateOffer(nil)
	_ = senderPeer.SetLocalDescription(offerSDP)

	offerData, err := json.Marshal(offerSDP)
	if err != nil {
		log.Println("Error on offer marshal", err)
		return
	}

	message := Message{
		PeerType: "sender",
		Type:     "offer",
		Data:     string(offerData),
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		log.Println("Error on offer message marshal", err)
		return
	}

	mainChannel <- string(messageBytes)
}

// Function to create a basic RTP header with dynamic payload type (96 for video, 97 for audio)
func createRTPHeader(payloadType uint8, sequenceNumber uint16, timestamp uint32, ssrc uint32) []byte {
	return []byte{
		0x80,                      // Version 2, no padding, no extensions, 0 CSRC
		payloadType,               // Payload type (dynamic for testing, 96 for video, 97 for audio)
		byte(sequenceNumber >> 8), // Sequence number (high byte)
		byte(sequenceNumber),      // Sequence number (low byte)
		byte(timestamp >> 24),     // Timestamp (high byte)
		byte(timestamp >> 16),     // Timestamp (next byte)
		byte(timestamp >> 8),      // Timestamp (next byte)
		byte(timestamp),           // Timestamp (low byte)
		byte(ssrc >> 24),          // SSRC (high byte)
		byte(ssrc >> 16),          // SSRC (next byte)
		byte(ssrc >> 8),           // SSRC (next byte)
		byte(ssrc),                // SSRC (low byte)
	}
}

func getPeerConnection() (*webrtc.PeerConnection, error) {
	mediaEngine := &webrtc.MediaEngine{}
	_ = mediaEngine.RegisterDefaultCodecs()

	interceptorRegistry := &interceptor.Registry{}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(interceptorRegistry))

	return api.NewPeerConnection(webrtc.Configuration{})
}

func handleOffer(receiverPeer *webrtc.PeerConnection, data *Message, mainChannel chan string) {

	receiverPeer.OnTrack(func(t *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Println("Track Received")
	})

	jsonData, err := json.Marshal(data.Data)
	if err != nil {
		log.Println("Error marshalling map data to JSON:", err)
		return
	}
	dataStr := string(jsonData)

	var offer webrtc.SessionDescription
	err = json.Unmarshal([]byte(dataStr), &offer)
	if err != nil {
		log.Println("Error on socket unmarshal", err)
		return
	}
	err = receiverPeer.SetRemoteDescription(offer)
	if err != nil {
		log.Println("Error on socket unmarshal", err)
		return
	}
	answer, err := receiverPeer.CreateAnswer(nil)
	if err != nil {
		log.Println("Error on socket unmarshal", err)
		return
	}

	err = receiverPeer.SetLocalDescription(answer)
	if err != nil {
		log.Println("Error on socket unmarshal", err)
		return
	}

	answerData, err := json.Marshal(answer)
	if err != nil {
		log.Println("Error on offer marshal", err)
		return
	}

	answerMessage := Message{
		PeerType: "receiver",
		Type:     "answer",
		Data:     string(answerData),
	}

	answerStr, _ := json.Marshal(&answerMessage)
	mainChannel <- string(answerStr)
}

func handleAnswer(senderPeer *webrtc.PeerConnection, data *Message) {
	var answer webrtc.SessionDescription

	jsonData, err := json.Marshal(data.Data)
	if err != nil {
		log.Println("Error marshalling map data to JSON:", err)
		return
	}
	dataStr := string(jsonData)

	err = json.Unmarshal([]byte(dataStr), &answer)
	if err != nil {
		log.Println("Error on socket unmarshal", err)
		return
	}
	err = senderPeer.SetRemoteDescription(answer)
	if err != nil {
		log.Println("Error on socket set remote description", err)
		return
	}
}
