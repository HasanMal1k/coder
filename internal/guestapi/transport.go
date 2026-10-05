package guestapi

import (
	"io"
	"time"

	"github.com/hashicorp/yamux"
)

// HeaderInstanceID is the request header guestd sends, alongside
// "Authorization: Bearer <guest token>", to say which instance it is.
const HeaderInstanceID = "Coder-Instance-Id"

// IDEPort is the loopback port the editor (code-server) listens on in every
// machine.
const IDEPort = 13337

// AgentPort is the loopback port the coding agent (OpenCode) listens on in
// every machine.
const AgentPort = 4096

// ModelPath is where on the control plane machines find the model their
// coding agent uses, as an OpenAI-compatible API.
const ModelPath = "/llm/v1"

// Where on the control plane machines find the record of their owner's
// other machines and agents: as files to keep a copy of, and as an MCP
// server for the coding agent to ask.
const (
	TeamPath = "/guest/team"
	MCPPath  = "/mcp"
)

const (
	// ServicesHeader is sent by a machine when it asks for its record: a
	// JSON object of the services it has started, by name, with the
	// revision of each. It is how the control plane knows a program it
	// asked for is the one now running, and not the one it replaced.
	ServicesHeader = "X-Coder-Services"

	// ServiceRevisionEnv is set in a service's environment to the revision
	// it was started for.
	ServiceRevisionEnv = "CODER_SERVICE_REVISION"
)

// Environment variables the control plane sets on a machine for guestd.
const (
	EnvInstanceID = "CODER_INSTANCE_ID"
	EnvGuestToken = "CODER_GUEST_TOKEN"
	EnvConnectURL = "CODER_CONNECT_URL"
)

// MuxConfig is the yamux configuration both ends of the tunnel use. Keepalives
// are what detect a machine that vanished without closing its connection.
func MuxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = 15 * time.Second
	cfg.ConnectionWriteTimeout = 10 * time.Second
	cfg.LogOutput = io.Discard

	return cfg
}
