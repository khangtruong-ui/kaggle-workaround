package server

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/khangtruong-ui/kaggle-workaround/pkg/protocol"
)

// ServerConfig holds the relay server configuration.
type ServerConfig struct {
	Addr            string
	Secret          string
	HandshakePeriod time.Duration
	IdleTimeout     time.Duration
}

// Server is the central Machine B relay coordinating agents and controllers.
type Server struct {
	config   ServerConfig
	registry *Registry
	listener net.Listener
	quit     chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex
	running  bool
}

// NewServer initializes a new relay Server.
func NewServer(cfg ServerConfig) *Server {
	if cfg.HandshakePeriod == 0 {
		cfg.HandshakePeriod = 10 * time.Second
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 90 * time.Second
	}
	return &Server{
		config:   cfg,
		registry: NewRegistry(),
		quit:     make(chan struct{}),
	}
}

// Registry returns the server's registry instance.
func (s *Server) Registry() *Registry {
	return s.registry
}

// Start binds to the configured address and begins accepting connections.
func (s *Server) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.New("server already running")
	}

	ln, err := net.Listen("tcp", s.config.Addr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("listen on %s failed: %w", s.config.Addr, err)
	}

	s.listener = ln
	s.running = true
	s.mu.Unlock()

	log.Printf("[Server] Relay listening on %s (single-port multiplexing)", ln.Addr().String())
	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// Addr returns the bound network address.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Stop gracefully shuts down the server and disconnects active streams.
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	close(s.quit)
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.mu.Unlock()

	s.wg.Wait()
	log.Printf("[Server] Relay shutdown complete")
	return nil
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
				log.Printf("[Server] Accept error: %v", err)
				continue
			}
		}

		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			s.handleConnection(c)
		}(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	stream := protocol.NewFramedStream(conn)
	defer stream.Close()

	// Enforce handshake timeout
	_ = stream.SetReadDeadline(time.Now().Add(s.config.HandshakePeriod))
	pkt, err := stream.ReadPacket()
	if err != nil {
		log.Printf("[Server] Handshake read failed from %s: %v", conn.RemoteAddr(), err)
		return
	}
	_ = stream.SetReadDeadline(time.Time{}) // clear deadline

	if pkt.Type != protocol.TypeHandshake {
		log.Printf("[Server] First packet from %s was not Handshake (got %v)", conn.RemoteAddr(), pkt.Type)
		_ = stream.SendJSON(protocol.TypeHandshakeAck, protocol.HandshakeAckPayload{
			Success: false,
			Error:   "expected handshake packet",
		})
		return
	}

	var hs protocol.HandshakePayload
	if err := pkt.UnmarshalJSON(&hs); err != nil {
		log.Printf("[Server] Invalid handshake JSON from %s: %v", conn.RemoteAddr(), err)
		_ = stream.SendJSON(protocol.TypeHandshakeAck, protocol.HandshakeAckPayload{
			Success: false,
			Error:   "malformed handshake payload",
		})
		return
	}

	// Validate secret token if configured
	if s.config.Secret != "" {
		if subtle.ConstantTimeCompare([]byte(hs.Token), []byte(s.config.Secret)) != 1 {
			log.Printf("[Server] Unauthorized handshake attempt from %s", conn.RemoteAddr())
			_ = stream.SendJSON(protocol.TypeHandshakeAck, protocol.HandshakeAckPayload{
				Success: false,
				Error:   "unauthorized: invalid secret token",
			})
			return
		}
	}

	switch hs.Role {
	case "agent":
		s.serveAgent(stream, hs)
	case "controller":
		s.serveController(stream, hs)
	default:
		log.Printf("[Server] Unknown role '%s' from %s", hs.Role, conn.RemoteAddr())
		_ = stream.SendJSON(protocol.TypeHandshakeAck, protocol.HandshakeAckPayload{
			Success: false,
			Error:   fmt.Sprintf("unknown role '%s'", hs.Role),
		})
	}
}

func (s *Server) serveAgent(stream protocol.Stream, hs protocol.HandshakePayload) {
	agentID := hs.ID
	if agentID == "" {
		agentID = GenerateID("agent")
	}
	agentName := hs.Name
	if agentName == "" {
		agentName = agentID
	}

	agent := &AgentSession{
		ID:            agentID,
		Name:          agentName,
		Hostname:      hs.Hostname,
		OS:            hs.OS,
		RemoteAddr:    stream.RemoteAddr().String(),
		ConnectedAt:   time.Now(),
		LastHeartbeat: time.Now(),
		Stream:        stream,
		Metadata:      hs.Metadata,
	}

	s.registry.RegisterAgent(agent)
	defer s.registry.UnregisterAgent(agentID)

	err := stream.SendJSON(protocol.TypeHandshakeAck, protocol.HandshakeAckPayload{
		Success:       true,
		AssignedID:    agentID,
		ServerVersion: "1.0.0",
	})
	if err != nil {
		log.Printf("[Server] Failed to send handshake ack to agent %s: %v", agentID, err)
		return
	}

	log.Printf("[Server] Agent connected: %s (name: %s, host: %s, addr: %s)", agentID, agentName, hs.Hostname, agent.RemoteAddr)

	// Read loop for Agent
	for {
		pkt, err := stream.ReadPacket()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				log.Printf("[Server] Agent %s read error: %v", agentID, err)
			}
			break
		}

		switch pkt.Type {
		case protocol.TypeHeartbeat:
			agent.mu.Lock()
			agent.LastHeartbeat = time.Now()
			agent.mu.Unlock()
			_ = stream.SendJSON(protocol.TypeHeartbeatAck, map[string]string{"status": "ok"})

		case protocol.TypeData:
			agent.mu.RLock()
			ctrl := agent.AttachedController
			agent.mu.RUnlock()

			if ctrl != nil {
				if err := ctrl.Stream.WritePacket(pkt); err != nil {
					log.Printf("[Server] Forward data from agent %s to ctrl %s failed: %v", agentID, ctrl.ID, err)
				}
			}

		case protocol.TypeDetach:
			var detach protocol.DetachPayload
			_ = pkt.UnmarshalJSON(&detach)
			log.Printf("[Server] Agent %s requested detach (reason: %s)", agentID, detach.Reason)
			_ = s.registry.DetachByAgent(agent, detach.Reason)

		case protocol.TypeError:
			var errPayload protocol.ErrorPayload
			_ = pkt.UnmarshalJSON(&errPayload)
			log.Printf("[Server] Agent %s reported error [%s]: %s", agentID, errPayload.Code, errPayload.Message)
			agent.mu.RLock()
			ctrl := agent.AttachedController
			agent.mu.RUnlock()
			if ctrl != nil {
				_ = ctrl.Stream.WritePacket(pkt)
			}
		}
	}

	log.Printf("[Server] Agent disconnected: %s (%s)", agentID, agentName)
}

func (s *Server) serveController(stream protocol.Stream, hs protocol.HandshakePayload) {
	ctrlID := GenerateID("ctrl")
	ctrl := &ControllerSession{
		ID:            ctrlID,
		RemoteAddr:    stream.RemoteAddr().String(),
		ConnectedAt:   time.Now(),
		LastHeartbeat: time.Now(),
		Stream:        stream,
	}

	s.registry.RegisterController(ctrl)
	defer s.registry.UnregisterController(ctrlID)

	err := stream.SendJSON(protocol.TypeHandshakeAck, protocol.HandshakeAckPayload{
		Success:       true,
		AssignedID:    ctrlID,
		ServerVersion: "1.0.0",
	})
	if err != nil {
		log.Printf("[Server] Failed to send handshake ack to controller %s: %v", ctrlID, err)
		return
	}

	log.Printf("[Server] Controller connected: %s (addr: %s)", ctrlID, ctrl.RemoteAddr)

	// Read loop for Controller
	for {
		pkt, err := stream.ReadPacket()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				log.Printf("[Server] Controller %s read error: %v", ctrlID, err)
			}
			break
		}

		switch pkt.Type {
		case protocol.TypeListAgents:
			agents := s.registry.ListAgents()
			_ = stream.SendJSON(protocol.TypeAgentList, protocol.AgentListPayload{Agents: agents})

		case protocol.TypeAttach:
			var attach protocol.AttachPayload
			if err := pkt.UnmarshalJSON(&attach); err != nil {
				_ = stream.SendJSON(protocol.TypeAttachAck, protocol.AttachAckPayload{
					Success: false,
					Error:   "invalid attach request format",
				})
				continue
			}

			err := s.registry.Attach(ctrl, attach.AgentID, attach.Rows, attach.Cols)
			if err != nil {
				_ = stream.SendJSON(protocol.TypeAttachAck, protocol.AttachAckPayload{
					Success: false,
					Error:   err.Error(),
					AgentID: attach.AgentID,
				})
				continue
			}

			agent := s.registry.GetAgent(attach.AgentID)
			agentName := attach.AgentID
			if agent != nil {
				agentName = agent.Name
			}

			_ = stream.SendJSON(protocol.TypeAttachAck, protocol.AttachAckPayload{
				Success:   true,
				AgentID:   attach.AgentID,
				AgentName: agentName,
			})
			log.Printf("[Server] Controller %s attached to Agent %s", ctrlID, attach.AgentID)

		case protocol.TypeData:
			ctrl.mu.RLock()
			agent := ctrl.AttachedAgent
			ctrl.mu.RUnlock()

			if agent != nil {
				if err := agent.Stream.WritePacket(pkt); err != nil {
					log.Printf("[Server] Forward data from ctrl %s to agent %s failed: %v", ctrlID, agent.ID, err)
				}
			}

		case protocol.TypeResize:
			ctrl.mu.RLock()
			agent := ctrl.AttachedAgent
			ctrl.mu.RUnlock()

			if agent != nil {
				_ = agent.Stream.WritePacket(pkt)
			}

		case protocol.TypeDetach:
			var detach protocol.DetachPayload
			_ = pkt.UnmarshalJSON(&detach)
			log.Printf("[Server] Controller %s requested detach (reason: %s)", ctrlID, detach.Reason)
			_ = s.registry.Detach(ctrl, detach.Reason)
			_ = stream.SendJSON(protocol.TypeDetach, protocol.DetachPayload{Reason: "detached"})

		case protocol.TypeHeartbeat:
			ctrl.mu.Lock()
			ctrl.LastHeartbeat = time.Now()
			ctrl.mu.Unlock()
			_ = stream.SendJSON(protocol.TypeHeartbeatAck, map[string]string{"status": "ok"})
		}
	}

	log.Printf("[Server] Controller disconnected: %s", ctrlID)
}
