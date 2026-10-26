package protocol

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestPacketEncodeDecode(t *testing.T) {
	tests := []struct {
		name    string
		pType   PacketType
		payload []byte
	}{
		{
			name:    "empty payload",
			pType:   TypeHeartbeat,
			payload: nil,
		},
		{
			name:    "small data payload",
			pType:   TypeData,
			payload: []byte("ls -la\n"),
		},
		{
			name:    "binary control character payload (Ctrl+C 0x03)",
			pType:   TypeData,
			payload: []byte{0x03},
		},
		{
			name:    "large payload",
			pType:   TypeData,
			payload: bytes.Repeat([]byte("A"), 64*1024),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			pkt := NewPacket(tc.pType, tc.payload)

			if err := pkt.Encode(&buf); err != nil {
				t.Fatalf("encode failed: %v", err)
			}

			decoded, err := DecodePacket(&buf)
			if err != nil {
				t.Fatalf("decode failed: %v", err)
			}

			if decoded.Type != tc.pType {
				t.Errorf("expected type %v, got %v", tc.pType, decoded.Type)
			}

			if !bytes.Equal(decoded.Payload, tc.payload) && !(len(decoded.Payload) == 0 && len(tc.payload) == 0) {
				t.Errorf("payload mismatch")
			}
		})
	}
}

func TestJSONPacket(t *testing.T) {
	hs := HandshakePayload{
		Role:     "agent",
		Token:    "secret-123",
		ID:       "kaggle-1",
		Hostname: "worker-node",
		Metadata: map[string]string{"gpu": "T4"},
	}

	pkt, err := NewJSONPacket(TypeHandshake, hs)
	if err != nil {
		t.Fatalf("failed to create JSON packet: %v", err)
	}

	var buf bytes.Buffer
	if err := pkt.Encode(&buf); err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := DecodePacket(&buf)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	var result HandshakePayload
	if err := decoded.UnmarshalJSON(&result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if result.Role != hs.Role || result.Token != hs.Token || result.ID != hs.ID || result.Metadata["gpu"] != "T4" {
		t.Errorf("deserialized struct mismatch: %+v", result)
	}
}

func TestMaxPayloadSizeExceeded(t *testing.T) {
	// Attempt to encode payload larger than limit
	oversized := make([]byte, MaxPayloadSize+1)
	pkt := NewPacket(TypeData, oversized)

	var buf bytes.Buffer
	if err := pkt.Encode(&buf); err == nil {
		t.Fatal("expected error on oversized payload, got nil")
	}
}

func TestTruncatedPacketDecode(t *testing.T) {
	// 5-byte header indicating 100 bytes payload, but buffer only has 10 bytes
	header := []byte{0x00, 0x00, 0x00, 0x64, byte(TypeData), 0x01, 0x02, 0x03}
	buf := bytes.NewReader(header)

	_, err := DecodePacket(buf)
	if err != io.ErrUnexpectedEOF && err != io.EOF {
		t.Fatalf("expected EOF error, got %v", err)
	}
}

func TestFramedStreamConcurrentWrites(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	serverStream := NewFramedStream(serverConn)
	clientStream := NewFramedStream(clientConn)

	const numWriters = 10
	const msgsPerWriter = 20
	var wg sync.WaitGroup

	// Reader goroutine
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		count := 0
		for count < numWriters*msgsPerWriter {
			pkt, err := serverStream.ReadPacket()
			if err != nil {
				return
			}
			if pkt.Type == TypeData {
				count++
			}
		}
	}()

	// Concurrent writers
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < msgsPerWriter; j++ {
				data := []byte{byte(workerID), byte(j)}
				if err := clientStream.SendData(data); err != nil {
					return
				}
			}
		}(i)
	}

	wg.Wait()

	select {
	case <-readDone:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for concurrent reads")
	}
}
