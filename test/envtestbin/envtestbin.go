// Package envtestbin finds the Kubernetes test binaries (kube-apiserver and
// etcd) that `make setup-envtest` installs under bin/k8s, for an envtest run
// started outside make (an IDE, a plain go test), where KUBEBUILDER_ASSETS is
// unset.
package envtestbin

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"k8s.io/apimachinery/pkg/util/version"
)

// Dir returns the directory under base (the repository's bin/k8s) that holds
// the newest Kubernetes release's binaries for this platform, or "" when base
// holds none.
//
// It picks the newest rather than the first: setup-envtest leaves the binaries
// of a release it installed in place when ENVTEST_K8S_VERSION moves, so after
// a bump base holds both, and the Makefile always pins the newest; base also
// holds ENVTEST_K8S_OLDEST's, which make runs test/crd against a second time.
// Directory order is lexical, which puts 1.36.4 before 1.37.0 and 1.9.0 after
// both.
func Dir(base string) (string, error) {
	entries, err := os.ReadDir(base)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading envtest binaries: %w", err)
	}

	// setup-envtest names each directory <version>-<os>-<arch>.
	suffix := "-" + runtime.GOOS + "-" + runtime.GOARCH
	var newest *version.Version
	var dir string
	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name(), suffix)
		if !entry.IsDir() || !ok {
			continue
		}
		v, err := version.ParseSemantic(name)
		if err != nil {
			continue
		}
		if newest == nil || v.GreaterThan(newest) {
			newest, dir = v, filepath.Join(base, entry.Name())
		}
	}
	return dir, nil
}
