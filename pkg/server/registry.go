package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/khangtruong-ui/kaggle-workaround/pkg/protocol"
)

// AgentSession represents a connected target machine (Machine C).
type AgentSession struct {
	ID                 string
	Name               string
	Hostname           string
	OS                 string
	RemoteAddr         string
	ConnectedAt        time.Time
	LastHeartbeat      time.Time
	Stream             protocol.Stream
	Metadata           map[string]string
	AttachedController *ControllerSession
	mu                 sync.RWMutex
}

// ControllerSession represents a connected operator terminal (Machine A).
type ControllerSession struct {
	ID            string
	RemoteAddr    string
	ConnectedAt   time.Time
	LastHeartbeat time.Time
	Stream        protocol.Stream
	AttachedAgent *AgentSession
	mu            sync.RWMutex
}

// Registry manages all active agents and controllers connected to Machine B.
type Registry struct {
	mu          sync.RWMutex
	agents      map[string]*AgentSession
	controllers map[string]*ControllerSession
}

// NewRegistry creates a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		agents:      make(map[string]*AgentSession),
		controllers: make(map[string]*ControllerSession),
	}
}

// GenerateID produces a random secure hexadecimal ID.
func GenerateID(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(b))
}

// RegisterAgent registers an agent into the registry.
func (r *Registry) RegisterAgent(agent *AgentSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[agent.ID] = agent
}

// UnregisterAgent removes an agent and detaches any attached controller.
func (r *Registry) UnregisterAgent(agentID string) *AgentSession {
	r.mu.Lock()
	agent, exists := r.agents[agentID]
	if exists {
		delete(r.agents, agentID)
	}
	r.mu.Unlock()

	if agent != nil {
		agent.mu.Lock()
		ctrl := agent.AttachedController
		agent.AttachedController = nil
		agent.mu.Unlock()

		if ctrl != nil {
			ctrl.mu.Lock()
			if ctrl.AttachedAgent != nil && ctrl.AttachedAgent.ID == agentID {
				ctrl.AttachedAgent = nil
			}
			ctrl.mu.Unlock()
			// Notify controller that agent disconnected
			_ = ctrl.Stream.SendJSON(protocol.TypeError, protocol.ErrorPayload{
				Code:    "AGENT_DISCONNECTED",
				Message: fmt.Sprintf("Agent %s (%s) has disconnected", agent.Name, agent.ID),
			})
			_ = ctrl.Stream.SendJSON(protocol.TypeDetach, protocol.DetachPayload{
				Reason:  "agent disconnected",
				AgentID: agentID,
			})
		}
	}

	return agent
}

// GetAgent retrieves an agent by ID.
func (r *Registry) GetAgent(agentID string) *AgentSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agents[agentID]
}

// ListAgents returns information about all connected agents.
func (r *Registry) ListAgents() []protocol.AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]protocol.AgentInfo, 0, len(r.agents))
	for _, a := range r.agents {
		a.mu.RLock()
		attached := a.AttachedController != nil
		attachedBy := ""
		if attached {
			attachedBy = a.AttachedController.RemoteAddr
		}

		// Clone metadata map
		meta := make(map[string]string)
		for k, v := range a.Metadata {
			meta[k] = v
		}

		info := protocol.AgentInfo{
			ID:          a.ID,
			Name:        a.Name,
			Hostname:    a.Hostname,
			RemoteAddr:  a.RemoteAddr,
			ConnectedAt: a.ConnectedAt,
			Attached:    attached,
			AttachedBy:  attachedBy,
			Metadata:    meta,
		}
		a.mu.RUnlock()
		list = append(list, info)
	}
	return list
}

// RegisterController registers a controller into the registry.
func (r *Registry) RegisterController(ctrl *ControllerSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.controllers[ctrl.ID] = ctrl
}

// UnregisterController removes a controller and detaches from any agent.
func (r *Registry) UnregisterController(ctrlID string) *ControllerSession {
	r.mu.Lock()
	ctrl, exists := r.controllers[ctrlID]
	if exists {
		delete(r.controllers, ctrlID)
	}
	r.mu.Unlock()

	if ctrl != nil {
		ctrl.mu.Lock()
		agent := ctrl.AttachedAgent
		ctrl.AttachedAgent = nil
		ctrl.mu.Unlock()

		if agent != nil {
			agent.mu.Lock()
			if agent.AttachedController != nil && agent.AttachedController.ID == ctrlID {
				agent.AttachedController = nil
			}
			agent.mu.Unlock()
			// Notify agent that controller detached
			_ = agent.Stream.SendJSON(protocol.TypeDetach, protocol.DetachPayload{
				Reason: "controller disconnected",
			})
		}
	}

	return ctrl
}

// Attach pairs a controller with an agent.
func (r *Registry) Attach(ctrl *ControllerSession, agentID string, rows, cols uint16) error {
	r.mu.RLock()
	agent, exists := r.agents[agentID]
	r.mu.RUnlock()

	if !exists {
		return fmt.Errorf("agent '%s' not found", agentID)
	}

	agent.mu.Lock()
	defer agent.mu.Unlock()

	if agent.AttachedController != nil && agent.AttachedController.ID != ctrl.ID {
		return fmt.Errorf("agent '%s' is already in use by %s", agent.Name, agent.AttachedController.RemoteAddr)
	}

	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	// If controller was attached to another agent, detach first
	if ctrl.AttachedAgent != nil && ctrl.AttachedAgent.ID != agentID {
		prevAgent := ctrl.AttachedAgent
		prevAgent.mu.Lock()
		prevAgent.AttachedController = nil
		prevAgent.mu.Unlock()
		_ = prevAgent.Stream.SendJSON(protocol.TypeDetach, protocol.DetachPayload{Reason: "controller switched agent"})
	}

	agent.AttachedController = ctrl
	ctrl.AttachedAgent = agent

	// Forward attach message to agent
	err := agent.Stream.SendJSON(protocol.TypeAttach, protocol.AttachPayload{
		AgentID: agent.ID,
		Rows:    rows,
		Cols:    cols,
	})
	if err != nil {
		agent.AttachedController = nil
		ctrl.AttachedAgent = nil
		return fmt.Errorf("failed to notify agent of attach: %w", err)
	}

	return nil
}

// Detach unpairs a controller from its attached agent.
func (r *Registry) Detach(ctrl *ControllerSession, reason string) error {
	ctrl.mu.Lock()
	agent := ctrl.AttachedAgent
	ctrl.AttachedAgent = nil
	ctrl.mu.Unlock()

	if agent == nil {
		return nil
	}

	agent.mu.Lock()
	if agent.AttachedController != nil && agent.AttachedController.ID == ctrl.ID {
		agent.AttachedController = nil
	}
	agent.mu.Unlock()

	_ = agent.Stream.SendJSON(protocol.TypeDetach, protocol.DetachPayload{
		Reason: reason,
	})

	return nil
}

// DetachByAgent unpairs an agent from its attached controller.
func (r *Registry) DetachByAgent(agent *AgentSession, reason string) error {
	agent.mu.Lock()
	ctrl := agent.AttachedController
	agent.AttachedController = nil
	agent.mu.Unlock()

	if ctrl == nil {
		return nil
	}

	ctrl.mu.Lock()
	if ctrl.AttachedAgent != nil && ctrl.AttachedAgent.ID == agent.ID {
		ctrl.AttachedAgent = nil
	}
	ctrl.mu.Unlock()

	_ = ctrl.Stream.SendJSON(protocol.TypeDetach, protocol.DetachPayload{
		Reason:  reason,
		AgentID: agent.ID,
	})

	return nil
}
