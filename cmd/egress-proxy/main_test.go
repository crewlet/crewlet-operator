package main

import (
	"flag"
	"testing"
	"time"
)

// TestFlagsBindOnTheDefaultFlagSet registers the command's flags where main
// does, beside what the packages it imports register there (controller-
// runtime's --kubeconfig among them): a flag defined twice panics at start,
// before any other test of the proxy would see it.
func TestFlagsBindOnTheDefaultFlagSet(t *testing.T) {
	o := bindFlags(flag.CommandLine)
	err := flag.CommandLine.Parse([]string{
		"--config=/c.yaml", "--signing-key-file=/k", "--listen=:1", "--health-listen=:2",
		"--dial-timeout=3s", "--shutdown-timeout=4s", "--kubeconfig=/kc", "--zap-devel",
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.configPath != "/c.yaml" || o.keyPath != "/k" || o.listen != ":1" || o.healthListen != ":2" ||
		o.dialTimeout != 3*time.Second || o.shutdownTimeout != 4*time.Second || !o.zap.Development {
		t.Errorf("parsed %+v", o)
	}
	if f := flag.CommandLine.Lookup("kubeconfig"); f == nil || f.Value.String() != "/kc" {
		t.Errorf("--kubeconfig = %v, want controller-runtime's flag holding /kc", f)
	}
}
