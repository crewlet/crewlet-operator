package egressproxy

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

// Entry is one connection's access log line. It names who connected, where
// to, what the proxy decided and how much passed: the record an abuse report
// about an address of the tier's is answered from. It never holds the
// credential. Which seat run made the connection is the engine's to stamp
// on a per-run credential; the pod and seat pool are what the proxy knows.
type Entry struct {
	Time       time.Time `json:"time"`
	Decision   string    `json:"decision"`
	Reason     string    `json:"reason"`
	Tenant     string    `json:"tenant"`
	Namespace  string    `json:"namespace"`
	Company    string    `json:"company"`
	Pod        string    `json:"pod"`
	Component  string    `json:"component"`
	SeatPool   string    `json:"seatPool"`
	TrustTier  string    `json:"trustTier"`
	Method     string    `json:"method"`
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	IP         string    `json:"ip"`
	BytesUp    int64     `json:"bytesUp"`
	BytesDown  int64     `json:"bytesDown"`
	DurationMs int64     `json:"durationMs"`
}

// The decisions an Entry records.
const (
	DecisionAllowed = "allowed"
	DecisionDenied  = "denied"
)

// AccessLog writes Entries as JSON lines, one write per line so that lines
// from concurrent connections never interleave.
type AccessLog struct {
	mu  sync.Mutex
	enc *json.Encoder
}

// NewAccessLog writes to w.
func NewAccessLog(w io.Writer) *AccessLog {
	return &AccessLog{enc: json.NewEncoder(w)}
}

// Write logs e. A failed write is dropped: the connection it describes has
// already been served or refused.
func (l *AccessLog) Write(e *Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.enc.Encode(e)
}
