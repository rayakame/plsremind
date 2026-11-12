// Package gateway is the WebSocket side of the hub: clients connect, say what
// they want to hear about, and are told about every change without asking.
// The protocol follows Discord's gateway.
package gateway

import "encoding/json"

// Opcodes. The numbers are Discord's.
const (
	OpDispatch       = 0
	OpHeartbeat      = 1
	OpIdentify       = 2
	OpResume         = 6
	OpReconnect      = 7
	OpInvalidSession = 9
	OpHello          = 10
	OpHeartbeatACK   = 11
)

// Close codes.
const (
	CloseUnknownOp            = 4001
	CloseDecode               = 4002
	CloseNotAuthenticated     = 4003
	CloseAuthFailed           = 4004
	CloseAlreadyAuthenticated = 4005
	CloseInvalidSeq           = 4007
	CloseRateLimited          = 4008
	CloseTimeout              = 4009
	CloseInvalidIntents       = 4013
	CloseDisallowedIntents    = 4014
)

// Event names of dispatches.
const (
	EventReady   = "READY"
	EventResumed = "RESUMED"
)

type inbound struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
}

type outbound struct {
	Op int     `json:"op"`
	D  any     `json:"d"`
	S  *int64  `json:"s"`
	T  *string `json:"t"`
}

func encode(op int, d any, s *int64, t *string) []byte {
	raw, err := json.Marshal(outbound{Op: op, D: d, S: s, T: t})
	if err != nil {
		// Payloads are built from our own types; failing to encode one is a bug.
		panic(err)
	}
	return raw
}

type identify struct {
	Token   string   `json:"token"`
	Intents []string `json:"intents"`
}

type resume struct {
	Token     string `json:"token"`
	SessionID string `json:"session_id"`
	Seq       int64  `json:"seq"`
}

type readyPrincipal struct {
	Kind       string           `json:"kind"`
	UserID     *string          `json:"user_id,omitempty"`
	Connection *readyConnection `json:"connection,omitempty"`
}

type readyConnection struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

type ready struct {
	SessionID string         `json:"session_id"`
	Intents   []string       `json:"intents"`
	Principal readyPrincipal `json:"principal"`
	Users     []string       `json:"users"`
}
