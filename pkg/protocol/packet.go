package protocol

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// MaxPayloadSize sets a hard limit on packet size (4 MB) to prevent memory attacks.
const MaxPayloadSize = 4 * 1024 * 1024

// PacketType defines the type of message on the wire.
type PacketType byte

const (
	TypeHandshake    PacketType = 0x01
	TypeHandshakeAck PacketType = 0x02
	TypeHeartbeat    PacketType = 0x03
	TypeHeartbeatAck PacketType = 0x04
	TypeListAgents   PacketType = 0x05
	TypeAgentList    PacketType = 0x06
	TypeAttach       PacketType = 0x07
	TypeAttachAck    PacketType = 0x08
	TypeData         PacketType = 0x09
	TypeResize       PacketType = 0x0A
	TypeDetach       PacketType = 0x0B
	TypeError        PacketType = 0x0C
)

func (t PacketType) String() string {
	switch t {
	case TypeHandshake:
		return "Handshake"
	case TypeHandshakeAck:
		return "HandshakeAck"
	case TypeHeartbeat:
		return "Heartbeat"
	case TypeHeartbeatAck:
		return "HeartbeatAck"
	case TypeListAgents:
		return "ListAgents"
	case TypeAgentList:
		return "AgentList"
	case TypeAttach:
		return "Attach"
	case TypeAttachAck:
		return "AttachAck"
	case TypeData:
		return "Data"
	case TypeResize:
		return "Resize"
	case TypeDetach:
		return "Detach"
	case TypeError:
		return "Error"
	default:
		return fmt.Sprintf("Unknown(0x%02x)", byte(t))
	}
}

// Packet is the raw unit of network exchange.
type Packet struct {
	Type    PacketType
	Payload []byte
}

// HandshakePayload represents initial identification and authentication.
type HandshakePayload struct {
	Role     string            `json:"role"` // "agent" or "controller"
	Token    string            `json:"token"`
	ID       string            `json:"id,omitempty"`
	Name     string            `json:"name,omitempty"`
	Hostname string            `json:"hostname,omitempty"`
	OS       string            `json:"os,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Version  string            `json:"version,omitempty"`
}

// HandshakeAckPayload responds to a Handshake request.
type HandshakeAckPayload struct {
	Success       bool   `json:"success"`
	Error         string `json:"error,omitempty"`
	AssignedID    string `json:"assigned_id,omitempty"`
	ServerVersion string `json:"server_version,omitempty"`
}

// AgentInfo describes a connected agent available for control.
type AgentInfo struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Hostname    string            `json:"hostname"`
	RemoteAddr  string            `json:"remote_addr"`
	ConnectedAt time.Time         `json:"connected_at"`
	Attached    bool              `json:"attached"`
	AttachedBy  string            `json:"attached_by,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// AgentListPayload is the response to TypeListAgents.
type AgentListPayload struct {
	Agents []AgentInfo `json:"agents"`
}

// AttachPayload requests terminal attachment to a target agent.
type AttachPayload struct {
	AgentID string `json:"agent_id"`
	Rows    uint16 `json:"rows"`
	Cols    uint16 `json:"cols"`
}

// AttachAckPayload acknowledges an attach attempt.
type AttachAckPayload struct {
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	AgentName string `json:"agent_name,omitempty"`
}

// ResizePayload notifies terminal window dimension changes.
type ResizePayload struct {
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

// DetachPayload informs of terminal detachment.
type DetachPayload struct {
	Reason  string `json:"reason,omitempty"`
	AgentID string `json:"agent_id,omitempty"`
}

// ErrorPayload describes errors encountered.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// NewPacket creates a new packet with raw bytes.
func NewPacket(t PacketType, payload []byte) *Packet {
	return &Packet{
		Type:    t,
		Payload: payload,
	}
}

// NewJSONPacket creates a packet serialized from a Go structure.
func NewJSONPacket(t PacketType, v any) (*Packet, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal json packet (%v): %w", t, err)
	}
	return &Packet{
		Type:    t,
		Payload: data,
	}, nil
}

// UnmarshalJSON unmarshals packet payload into target v.
func (p *Packet) UnmarshalJSON(v any) error {
	if p == nil || len(p.Payload) == 0 {
		return errors.New("empty packet payload")
	}
	return json.Unmarshal(p.Payload, v)
}

// Encode writes a packet to an io.Writer.
// Wire format: [4 bytes BigEndian PayloadLength] [1 byte Type] [Payload bytes]
func (p *Packet) Encode(w io.Writer) error {
	if len(p.Payload) > MaxPayloadSize {
		return fmt.Errorf("payload size %d exceeds maximum allowed %d", len(p.Payload), MaxPayloadSize)
	}
	header := make([]byte, 5)
	binary.BigEndian.PutUint32(header[0:4], uint32(len(p.Payload)))
	header[4] = byte(p.Type)

	if _, err := w.Write(header); err != nil {
		return err
	}
	if len(p.Payload) > 0 {
		if _, err := w.Write(p.Payload); err != nil {
			return err
		}
	}
	return nil
}

// DecodePacket reads a packet from an io.Reader.
func DecodePacket(r io.Reader) (*Packet, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	payloadLen := binary.BigEndian.Uint32(header[0:4])
	if payloadLen > MaxPayloadSize {
		return nil, fmt.Errorf("packet payload size %d exceeds limit of %d", payloadLen, MaxPayloadSize)
	}

	pktType := PacketType(header[4])
	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
	}

	return &Packet{
		Type:    pktType,
		Payload: payload,
	}, nil
}
