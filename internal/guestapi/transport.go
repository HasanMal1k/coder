package guestapi

import (
	"io"
	"time"

	"github.com/hashicorp/yamux"
)

// HeaderInstanceID is the request header guestd sends, alongside
// "Authorization: Bearer <guest token>", to say which instance it is.
const HeaderInstanceID = "Coder-Instance-Id"

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
