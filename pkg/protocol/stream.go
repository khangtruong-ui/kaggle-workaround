package protocol

import (
	"bufio"
	"fmt"
	"net"
	"sync"
	"time"
)

// Stream is an abstraction for a bidirectional framed stream over a connection.
type Stream interface {
	ReadPacket() (*Packet, error)
	WritePacket(p *Packet) error
	SendJSON(t PacketType, v any) error
	SendData(data []byte) error
	Close() error
	RemoteAddr() net.Addr
	SetDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

// FramedStream implements Stream over a net.Conn with thread-safe writes.
type FramedStream struct {
	conn      net.Conn
	reader    *bufio.Reader
	writeMu   sync.Mutex
	closeOnce sync.Once
	closed    bool
}

// NewFramedStream creates a new FramedStream.
func NewFramedStream(conn net.Conn) *FramedStream {
	return &FramedStream{
		conn:   conn,
		reader: bufio.NewReaderSize(conn, 64*1024),
	}
}

// ReadPacket decodes the next packet from the stream.
func (s *FramedStream) ReadPacket() (*Packet, error) {
	return DecodePacket(s.reader)
}

// WritePacket encodes and writes a packet to the stream safely under mutex.
func (s *FramedStream) WritePacket(p *Packet) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if s.closed {
		return net.ErrClosed
	}

	return p.Encode(s.conn)
}

// SendJSON marshals and sends a JSON packet.
func (s *FramedStream) SendJSON(t PacketType, v any) error {
	pkt, err := NewJSONPacket(t, v)
	if err != nil {
		return fmt.Errorf("failed to create JSON packet: %w", err)
	}
	return s.WritePacket(pkt)
}

// SendData writes a raw data packet.
func (s *FramedStream) SendData(data []byte) error {
	return s.WritePacket(NewPacket(TypeData, data))
}

// Close closes the underlying network connection.
func (s *FramedStream) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.writeMu.Lock()
		s.closed = true
		s.writeMu.Unlock()
		err = s.conn.Close()
	})
	return err
}

// RemoteAddr returns the remote network address.
func (s *FramedStream) RemoteAddr() net.Addr {
	return s.conn.RemoteAddr()
}

// SetDeadline sets the read and write deadlines.
func (s *FramedStream) SetDeadline(t time.Time) error {
	return s.conn.SetDeadline(t)
}

// SetReadDeadline sets the read deadline.
func (s *FramedStream) SetReadDeadline(t time.Time) error {
	return s.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline.
func (s *FramedStream) SetWriteDeadline(t time.Time) error {
	return s.conn.SetWriteDeadline(t)
}
