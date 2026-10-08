// Package utils holds the helpers the e2e suite uses to drive make, kubectl
// and kind from Go.
package utils

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/onsi/ginkgo/v2"
)

const (
	defaultKindBinary  = "kind"
	defaultKindCluster = "kind"
)

// Run executes cmd from the project root and returns its combined output.
func Run(cmd *exec.Cmd) (string, error) {
	dir, err := GetProjectDir()
	if err != nil {
		return "", err
	}
	cmd.Dir = dir

	command := strings.Join(cmd.Args, " ")
	_, _ = fmt.Fprintf(ginkgo.GinkgoWriter, "running: %q\n", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%q failed with error %q: %w", command, string(output), err)
	}

	return string(output), nil
}

// LoadImageToKindClusterWithName loads a local image into the kind cluster
// named by KIND_CLUSTER, using the kind binary named by KIND.
func LoadImageToKindClusterWithName(name string) error {
	cluster := defaultKindCluster
	if v, ok := os.LookupEnv("KIND_CLUSTER"); ok {
		cluster = v
	}
	kindBinary := defaultKindBinary
	if v, ok := os.LookupEnv("KIND"); ok {
		kindBinary = v
	}
	cmd := exec.Command(kindBinary, "load", "docker-image", name, "--name", cluster)
	_, err := Run(cmd)
	return err
}

// GetNonEmptyLines splits command output into lines, dropping empty ones.
func GetNonEmptyLines(output string) []string {
	var res []string
	for element := range strings.SplitSeq(output, "\n") {
		if element != "" {
			res = append(res, element)
		}
	}
	return res
}

// GetProjectDir returns the repository root, which is where the suite's
// make and kubectl invocations expect to run.
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}
	return strings.ReplaceAll(wd, "/test/e2e", ""), nil
}
