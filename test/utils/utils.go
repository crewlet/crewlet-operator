// Package utils holds the helpers the e2e suite uses to drive make, kubectl
// and kind from Go.
package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
// named by KIND_CLUSTER, using the kind binary named by KIND: this machine's
// platform of it alone, saved by docker into an archive kind imports.
//
// Not `kind load docker-image`, which saves the image whole and imports every
// platform it lists: on Docker's containerd image store a pulled image keeps
// its registry's index of platforms, of which only the pulled one's content is
// here, so the import fails on the first platform it cannot find. Saved for
// one platform, the archive holds that platform's image and nothing it lacks,
// on either image store, and the node runs it under the same name. docker
// save takes --platform from Docker 28 on, which the suite therefore needs.
func LoadImageToKindClusterWithName(name string) error {
	cluster := defaultKindCluster
	if v, ok := os.LookupEnv("KIND_CLUSTER"); ok {
		cluster = v
	}
	kindBinary := defaultKindBinary
	if v, ok := os.LookupEnv("KIND"); ok {
		kindBinary = v
	}
	dir, err := os.MkdirTemp("", "kind-image-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	archive := filepath.Join(dir, "image.tar")
	cmd := exec.Command("docker", "save", "--platform", "linux/"+runtime.GOARCH, "--output", archive, name)
	if _, err := Run(cmd); err != nil {
		return err
	}
	cmd = exec.Command(kindBinary, "load", "image-archive", archive, "--name", cluster)
	_, err = Run(cmd)
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
