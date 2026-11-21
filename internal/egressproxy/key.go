package egressproxy

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// KeyFile is the signing key, read from a mounted Secret and read again at
// most every refresh interval, so that a rotated key takes effect without a
// restart (the kubelet replaces the file in place).
type KeyFile struct {
	path    string
	refresh time.Duration

	mu     sync.Mutex
	key    []byte
	readAt time.Time
}

// NewKeyFile reads the key once, so that a missing or empty key fails the
// start rather than every connection.
func NewKeyFile(path string, refresh time.Duration) (*KeyFile, error) {
	k := &KeyFile{path: path, refresh: refresh}
	if _, err := k.Key(); err != nil {
		return nil, err
	}
	return k, nil
}

// Key returns the current key. A failed re-read keeps the last good key.
func (k *KeyFile) Key() ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.key != nil && time.Since(k.readAt) < k.refresh {
		return k.key, nil
	}
	raw, err := os.ReadFile(k.path)
	k.readAt = time.Now()
	// The bytes are used as they are, a trailing newline included: the
	// operator mints with the Secret's value unchanged, and the two must agree.
	if err == nil && len(raw) == 0 {
		err = fmt.Errorf("signing key file %s is empty", k.path)
	}
	if err != nil {
		if k.key != nil {
			return k.key, nil
		}
		return nil, fmt.Errorf("reading the signing key: %w", err)
	}
	k.key = raw
	return k.key, nil
}
