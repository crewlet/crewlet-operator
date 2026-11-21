package egressproxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if _, err := NewKeyFile(path, 0); err == nil {
		t.Fatal("a missing key file was accepted")
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewKeyFile(path, 0); err == nil {
		t.Fatal("an empty key file was accepted")
	}
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	k, err := NewKeyFile(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	// The trailing newline is part of the key, as the operator reads it.
	if got, _ := k.Key(); string(got) != "one\n" {
		t.Fatalf("key = %q", got)
	}
	if err := os.WriteFile(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := k.Key(); string(got) != "two" {
		t.Fatalf("rotated key = %q", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got, err := k.Key(); err != nil || string(got) != "two" {
		t.Fatalf("after the file went away: %q, %v; want the last good key", got, err)
	}
}
