# Image the docker-*, deploy and build-installer targets build, push or deploy.
IMG ?= ghcr.io/crewlet/crewlet-operator:dev

# The repository holds two Go modules: the operator itself (.) and its API
# types (api/), which are a module of their own so that a consumer can import
# them without inheriting controller-runtime. Every Go target runs in both.
GO_MODULES := . api

# CONTAINER_TOOL builds and pushes images. Only docker is exercised by CI.
CONTAINER_TOOL ?= docker

# bash with pipefail, so a failure on the left of a pipe fails the recipe.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

##@ General

# Every target that carries a `## description` is listed by `make help`,
# grouped under the `##@` heading above it.

.PHONY: all
all: build ## Build the manager binary; the target a bare `make` runs.

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

# controller-gen runs in the module that owns the markers it reads: CRDs and
# deepcopy come from the types in api/, RBAC (and webhooks, once there are
# any) from the controllers in the operator module. One invocation cannot load
# both, because a pattern such as ./... stops at a nested module's boundary.
.PHONY: manifests
manifests: controller-gen ## Generate CustomResourceDefinitions (from api/) and the manager ClusterRole (from internal/).
	cd api && "$(CONTROLLER_GEN)" crd paths="./..." output:crd:artifacts:config=../config/crd/bases
	"$(CONTROLLER_GEN)" rbac:roleName=manager-role webhook paths="./..."

.PHONY: generate
generate: controller-gen ## Generate the DeepCopy methods of the API types.
	cd api && "$(CONTROLLER_GEN)" object paths="./..."

.PHONY: fmt
fmt: ## Run go fmt in both modules.
	@for m in $(GO_MODULES); do (cd $$m && go fmt ./...); done

.PHONY: vet
vet: ## Run go vet in both modules.
	@for m in $(GO_MODULES); do (cd $$m && go vet ./...); done

# The API reference is generated from the types' doc comments and markers, so
# a field's documentation is written once, on the field. --max-depth is raised
# from crd-ref-docs' default of 10 because EngineClass embeds corev1.Affinity,
# whose selector types sit deeper than that; the deeper walk adds nothing to
# the output (Kubernetes types link to their own reference) and keeps the run
# free of warnings.
.PHONY: api-docs
api-docs: crd-ref-docs ## Generate the API reference (docs/reference/api.md) from the API types.
	cd api && "$(CRD_REF_DOCS)" --source-path=. --config=../hack/crd-ref-docs/config.yaml \
		--renderer=markdown --output-path=../docs/reference/api.md --log-level=WARN --max-depth=12

.PHONY: tidy
tidy: ## Run go mod tidy in both modules.
	@for m in $(GO_MODULES); do (cd $$m && go mod tidy); done

# The Gateway API CRDs the controller suite installs into envtest: the
# standard channel of the sigs.k8s.io/gateway-api release go.mod requires,
# copied from the module cache, so the CRDs the tests run against and the Go
# types the operator writes are one release. The suite fails if they drift
# apart; run this after bumping the module.
GATEWAY_API_CRDS := internal/controller/testdata/gateway-api
.PHONY: gateway-api-crds
gateway-api-crds: ## Copy the Gateway API CRDs of the release go.mod requires into the controller suite's test data.
	@dir="$$(go list -m -f '{{.Dir}}' sigs.k8s.io/gateway-api)"; \
	rm -rf "$(GATEWAY_API_CRDS)"; \
	mkdir -p "$(GATEWAY_API_CRDS)"; \
	for kind in gatewayclasses gateways httproutes; do \
		install -m 0644 "$$dir/config/crd/standard/gateway.networking.k8s.io_$$kind.yaml" "$(GATEWAY_API_CRDS)/"; \
	done

# The race detector is on because a controller is concurrent by construction:
# a reconciler, its watches and the cache all run on their own goroutines.
# test/e2e is excluded; it needs a cluster and runs under test-e2e. Every suite
# that runs the real engine (engine, render, secretsync, controller, test/crd,
# test/docs) runs the crewlet binary `make crewlet` installed
# (internal/engine/enginetest), named explicitly so a CREWLET_BIN in the
# caller's environment cannot swap in another engine; test/chart renders the
# chart with the pinned helm.
.PHONY: test
test: manifests generate fmt vet setup-envtest crewlet helm ## Run the unit, envtest, engine and chart suites of both modules.
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" \
		CREWLET_BIN="$(CREWLET)" HELM="$(HELM)" \
		go test -race $$(go list ./... | grep -v /test/e2e) -coverprofile cover.out
	cd api && go test -race ./... -coverprofile cover.out

KIND_CLUSTER ?= crewlet-operator-test-e2e
# KIND_CONFIG optionally names a kind cluster configuration file, for a host
# that cannot run kind's default cluster (one whose kubelet needs settings of
# its own, for instance). Unset, the cluster is kind's default, as in CI.
KIND_CONFIG ?=

.PHONY: setup-test-e2e
setup-test-e2e: kind ## Create the kind cluster for the e2e suite unless it exists (KIND_CONFIG: optional cluster configuration).
	@case "$$("$(KIND)" get clusters)" in \
		*"$(KIND_CLUSTER)"*) \
			echo "Kind cluster '$(KIND_CLUSTER)' already exists. Skipping creation." ;; \
		*) \
			echo "Creating Kind cluster '$(KIND_CLUSTER)'..."; \
			"$(KIND)" create cluster --name $(KIND_CLUSTER) $(if $(KIND_CONFIG),--config "$(KIND_CONFIG)") ;; \
	esac

# The suite shells out to kind, kubectl and make, so the pinned tools go first
# on its PATH. kubectl's kuberc is off unless KUBECTL_KUBERC=true, so a local
# kubectl configuration cannot change what the suite sees. The suite loads two
# images into the cluster: the manager's, which it builds, and the engine's,
# CREWLET_IMG, which it pulls (make crewlet-image).
.PHONY: test-e2e
test-e2e: setup-test-e2e manifests generate fmt vet kubectl ## Run the e2e suite against a kind cluster, then delete the cluster (left in place when the suite fails; make cleanup-test-e2e).
	PATH="$(LOCALBIN):$$PATH" KIND="$(KIND)" KIND_CLUSTER=$(KIND_CLUSTER) CREWLET_IMG="$(CREWLET_IMG)" \
		go test -tags=e2e ./test/e2e/ -v -ginkgo.v -timeout 60m
	$(MAKE) cleanup-test-e2e

.PHONY: cleanup-test-e2e
cleanup-test-e2e: kind ## Delete the kind cluster used by the e2e suite.
	@"$(KIND)" delete cluster --name $(KIND_CLUSTER)

# golangci-lint stops at a nested module's boundary too, so it runs once per
# module; both runs read the one .golangci.yml at the repository root.
.PHONY: lint
lint: golangci-lint ## Run golangci-lint in both modules.
	@for m in $(GO_MODULES); do (cd $$m && "$(GOLANGCI_LINT)" run --config "$(CURDIR)/.golangci.yml"); done

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint in both modules and apply its fixes.
	@for m in $(GO_MODULES); do (cd $$m && "$(GOLANGCI_LINT)" run --config "$(CURDIR)/.golangci.yml" --fix); done

.PHONY: lint-config
lint-config: golangci-lint ## Verify the golangci-lint configuration.
	"$(GOLANGCI_LINT)" config verify --config "$(CURDIR)/.golangci.yml"

##@ Verification

# check is one target because it answers one question: would CI accept this
# change? Each step is a target of its own and runs in this order, cheapest
# first, with verify-generated ahead of everything that regenerates files
# (build and test run manifests and generate), since a step that rewrites the
# tree before the comparison would hide the drift it exists to catch.
.PHONY: check
check: ## Run every gate CI runs except e2e and the image: formatting, tidiness, generated files, vet, lint, chart, build, tests, sign-off.
	$(MAKE) fmt-check
	$(MAKE) tidy-check
	$(MAKE) verify-generated
	$(MAKE) vet
	$(MAKE) lint-config
	$(MAKE) lint
	$(MAKE) helm-lint
	$(MAKE) build
	$(MAKE) test
	$(MAKE) signoff-check
	$(MAKE) signoff-test

.PHONY: fmt-check
fmt-check: ## Fail if any Go file in either module is not gofmt-formatted.
	@unformatted="$$(gofmt -l $$(git ls-files --cached --others --exclude-standard -- '*.go'))"; \
	if [ -n "$$unformatted" ]; then \
		echo "These files are not gofmt-formatted; run: make fmt"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

.PHONY: tidy-check
tidy-check: ## Fail if go.mod or go.sum of either module is not tidy.
	@for m in $(GO_MODULES); do \
		(cd $$m && go mod tidy -diff) || { echo "$$m/go.mod is not tidy; the diff above is what make tidy writes."; exit 1; }; \
	done

# The paths whose content is generated: deepcopy (api/), CRDs and RBAC
# (config/), the chart templates (dist/chart) and the API reference
# (docs/reference).
GENERATED_PATHS := api config dist/chart docs/reference

# The tree is snapshotted into a throwaway index before regenerating, and the
# regenerated tree is compared with that snapshot rather than with HEAD, so the
# check answers "is the generated output in step with its sources?" on a
# working tree with uncommitted changes as well as on CI's clean checkout.
.PHONY: verify-generated
verify-generated: ## Fail if make manifests generate chart api-docs would change any generated file.
	@index="$$(mktemp)"; \
	trap 'rm -f "$$index"' EXIT; \
	cp "$$(git rev-parse --git-path index)" "$$index"; \
	GIT_INDEX_FILE="$$index" git add -A -- $(GENERATED_PATHS); \
	$(MAKE) --no-print-directory manifests generate chart api-docs; \
	new="$$(GIT_INDEX_FILE="$$index" git ls-files --others --exclude-standard -- $(GENERATED_PATHS))"; \
	if ! GIT_INDEX_FILE="$$index" git diff --exit-code -- $(GENERATED_PATHS) || [ -n "$$new" ]; then \
		[ -z "$$new" ] || printf 'New files:\n%s\n' "$$new"; \
		echo "Generated files were out of date and have been regenerated; review and commit them."; \
		exit 1; \
	fi

.PHONY: signoff-check
signoff-check: ## Fail if a commit this branch adds lacks a DCO Signed-off-by trailer.
	scripts/check-signoff.sh

.PHONY: signoff-test
signoff-test: ## Run the sign-off gate's own test suite.
	scripts/check-signoff_test.sh

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build the manager binary into bin/manager.
	go build -o bin/manager ./cmd

.PHONY: run
run: manifests generate fmt vet ## Run the manager from your host against the current kubeconfig.
	go run ./cmd --leader-elect=false

.PHONY: docker-build
docker-build: ## Build the manager image for the local platform.
	$(CONTAINER_TOOL) build -t ${IMG} .

# The engine image the e2e suite runs, pulled for this machine's platform: the
# one platform of it the suite loads into kind (test/utils), and so the only
# one a pull needs. crewlet's main image is pulled every time, since the tag
# moves with every build of crewlet's main. Another image the container tool
# already has is used as it is: a copy named by digest never changes, and an
# image built here (crewlet's `make image-context` and `docker buildx build
# --load`) is in no registry to pull from. The crewlet commit it was built
# from is printed from the image's own label, so the log names the engine the
# suite ran.
.PHONY: crewlet-image
crewlet-image: ## Pull the Crewlet engine image CREWLET_IMG for this platform, unless it is another image already here, and print its crewlet commit.
	@set -e; \
	if [ "$(CREWLET_IMG)" != "$(CREWLET_MAIN_IMG)" ] && "$(CONTAINER_TOOL)" image inspect "$(CREWLET_IMG)" >/dev/null 2>&1; then \
		echo "Crewlet engine image: $(CREWLET_IMG) is here already, not pulled"; \
	else \
		echo "Pulling $(CREWLET_IMG) for linux/$$(go env GOARCH)"; \
		"$(CONTAINER_TOOL)" pull --platform "linux/$$(go env GOARCH)" "$(CREWLET_IMG)"; \
	fi; \
	commit="$$("$(CONTAINER_TOOL)" image inspect \
		--format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$(CREWLET_IMG)")"; \
	echo "Crewlet engine image: $(CREWLET_IMG), built from crewlet commit $${commit:-unknown (it has no org.opencontainers.image.revision label)}"

.PHONY: docker-push
docker-push: ## Push the manager image.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS are the architectures docker-buildx builds and pushes. The
# Dockerfile cross-compiles on the build platform, so no target here needs
# emulation; a builder with the docker-container driver is still required to
# push a multi-platform manifest list.
#
# The builder is reused if a run that was killed left it behind, named with
# --builder rather than made the default with `buildx use` (which would change
# what every later `docker buildx` on the machine builds with), and removed by
# a trap once the build has finished, so its exit status is the build's own:
# a failed build or push fails the target.
PLATFORMS ?= linux/amd64,linux/arm64
BUILDX_BUILDER ?= crewlet-operator-builder
.PHONY: docker-buildx
docker-buildx: ## Build and push the manager image for every platform in PLATFORMS.
	"$(CONTAINER_TOOL)" buildx inspect $(BUILDX_BUILDER) >/dev/null 2>&1 || \
		"$(CONTAINER_TOOL)" buildx create --name $(BUILDX_BUILDER) --driver docker-container >/dev/null
	trap '"$(CONTAINER_TOOL)" buildx rm $(BUILDX_BUILDER)' EXIT; \
		"$(CONTAINER_TOOL)" buildx build --builder $(BUILDX_BUILDER) --push --platform=$(PLATFORMS) --tag ${IMG} .

# kustomize-build-img renders config/default with the manager image set to
# IMG. `kustomize edit set image` rewrites the kustomization it runs in, so it
# runs in a throwaway copy of config/: run in config/manager itself, every
# deploy with a local IMG (the e2e suite's, for one) left a tracked file
# modified, one commit away from shipping that image as the default and
# failing verify-generated in every clone. A copy rather than an overlay that
# names config/default as a resource, because kustomize refuses an absolute
# resource path and a relative one from a temporary directory is not portable.
define kustomize-build-img
tmp="$$(mktemp -d)"; \
trap 'rm -rf "$$tmp"' EXIT; \
cp -R config "$$tmp/config" && \
(cd "$$tmp/config/manager" && "$(KUSTOMIZE)" edit set image controller=${IMG}) && \
"$(KUSTOMIZE)" build "$$tmp/config/default"
endef

.PHONY: build-installer
build-installer: manifests generate kustomize ## Render CRDs and the manager deployment with image IMG into dist/install.yaml.
	mkdir -p dist
	@$(kustomize-build-img) > dist/install.yaml

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize kubectl ## Install the CRDs into the cluster of the current kubeconfig.
	"$(KUSTOMIZE)" build config/crd | "$(KUBECTL)" apply --server-side -f -

.PHONY: uninstall
uninstall: manifests kustomize kubectl ## Uninstall the CRDs; ignore-not-found=true tolerates missing ones.
	"$(KUSTOMIZE)" build config/crd | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize kubectl ## Deploy the manager (image IMG) to the cluster of the current kubeconfig.
	@$(kustomize-build-img) | "$(KUBECTL)" apply --server-side -f -

.PHONY: undeploy
undeploy: kustomize kubectl ## Remove the manager; ignore-not-found=true tolerates missing objects.
	"$(KUSTOMIZE)" build config/default | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -

##@ Dependencies

# Every tool is pinned to its newest release and installed into bin/ as
# <name>-<version>, with <name> a symlink to it. The tool targets are phony and
# re-check that symlink on every run, so bumping a version below installs the
# new one on the next make instead of whenever bin/ happens to look stale.
LOCALBIN ?= $(CURDIR)/bin
$(LOCALBIN):
	mkdir -p "$(LOCALBIN)"

KUBECTL ?= $(LOCALBIN)/kubectl
KIND ?= $(LOCALBIN)/kind
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
HELM ?= $(LOCALBIN)/helm
KUBEBUILDER ?= $(LOCALBIN)/kubebuilder
CRD_REF_DOCS ?= $(LOCALBIN)/crd-ref-docs
CREWLET ?= $(LOCALBIN)/crewlet

KUBECTL_VERSION ?= v1.37.1
HELM_VERSION ?= v4.3.0
KUBEBUILDER_VERSION ?= v4.16.0
CRD_REF_DOCS_VERSION ?= v0.3.0
KIND_VERSION ?= v0.33.0
KUSTOMIZE_VERSION ?= v5.8.3
CONTROLLER_TOOLS_VERSION ?= v0.22.0
# The Crewlet engine every suite runs: crewlet's main, as crewlet publishes it
# in this image, which the e2e suite runs and whose commit `make crewlet`
# installs for every other suite. Not a pin, unlike every version above: the
# operator is tested against what crewlet ships next, so a crewlet change that
# breaks it fails its next run, CI's daily one included (ADR-0002). The samples
# and the guides name the same image (enginetest.Image reads it here).
CREWLET_MAIN_IMG := ghcr.io/crewlet/crewlet:main
# CREWLET_IMG is the engine image the e2e suite runs, and whose commit `make
# crewlet` installs: crewlet's main unless you name another, such as a copy of
# an earlier main image or one built from a crewlet checkout.
CREWLET_IMG ?= $(CREWLET_MAIN_IMG)
# CREWLET_VERSION, when set, is the crewlet commit or module version `make
# crewlet` installs instead of CREWLET_IMG's, to run a given engine again: a
# failed run's log names the one it ran. The e2e suite runs CREWLET_IMG all the
# same; name an image of the same commit there.
CREWLET_VERSION ?=
# .custom-gcl.yml names the same version; the golangci-lint target refuses to
# build if the two disagree.
GOLANGCI_LINT_VERSION ?= v2.14.0

# setup-envtest is released with controller-runtime, so it follows go.mod.
ENVTEST_VERSION ?= $(shell v='$(call gomodver,sigs.k8s.io/controller-runtime)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_VERSION manually (controller-runtime replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v")

# ENVTEST_K8S_VERSION is the newest Kubernetes release that setup-envtest can
# download (`bin/setup-envtest list`), pinned to the patch rather than derived
# from k8s.io/api's minor so a test run never changes under a fixed tree.
ENVTEST_K8S_VERSION ?= 1.37.0

.PHONY: kubectl
kubectl: $(LOCALBIN) ## Download kubectl locally if necessary.
	@[ -f "$(KUBECTL)-$(KUBECTL_VERSION)" ] || { \
		set -e; \
		url="https://dl.k8s.io/release/$(KUBECTL_VERSION)/bin/$$(go env GOOS)/$$(go env GOARCH)/kubectl"; \
		echo "Downloading $$url"; \
		curl -fsSLo "$(KUBECTL)-$(KUBECTL_VERSION).tmp" "$$url"; \
		echo "$$(curl -fsSL "$$url.sha256")  $(KUBECTL)-$(KUBECTL_VERSION).tmp" | sha256sum --check --quiet; \
		chmod +x "$(KUBECTL)-$(KUBECTL_VERSION).tmp"; \
		mv "$(KUBECTL)-$(KUBECTL_VERSION).tmp" "$(KUBECTL)-$(KUBECTL_VERSION)"; \
	}
	@ln -sf "$(KUBECTL)-$(KUBECTL_VERSION)" "$(KUBECTL)"

.PHONY: helm
helm: $(LOCALBIN) ## Download helm locally if necessary.
	@[ -f "$(HELM)-$(HELM_VERSION)" ] || { \
		set -e; \
		tmp="$$(mktemp -d)"; \
		trap 'rm -rf "$$tmp"' EXIT; \
		archive="helm-$(HELM_VERSION)-$$(go env GOOS)-$$(go env GOARCH).tar.gz"; \
		echo "Downloading https://get.helm.sh/$$archive"; \
		curl -fsSLo "$$tmp/$$archive" "https://get.helm.sh/$$archive"; \
		(cd "$$tmp" && curl -fsSL "https://get.helm.sh/$$archive.sha256sum" | sha256sum --check --quiet); \
		tar -xzf "$$tmp/$$archive" -C "$$tmp"; \
		mv "$$tmp/$$(go env GOOS)-$$(go env GOARCH)/helm" "$(HELM)-$(HELM_VERSION)"; \
	}
	@ln -sf "$(HELM)-$(HELM_VERSION)" "$(HELM)"

.PHONY: kubebuilder
kubebuilder: $(LOCALBIN) ## Download kubebuilder locally if necessary.
	$(call go-install-tool,$(KUBEBUILDER),sigs.k8s.io/kubebuilder/v4,$(KUBEBUILDER_VERSION))

.PHONY: crd-ref-docs
crd-ref-docs: $(LOCALBIN) ## Download crd-ref-docs locally if necessary.
	$(call go-install-tool,$(CRD_REF_DOCS),github.com/elastic/crd-ref-docs,$(CRD_REF_DOCS_VERSION))

.PHONY: kind
kind: $(LOCALBIN) ## Download kind locally if necessary.
	$(call go-install-tool,$(KIND),sigs.k8s.io/kind,$(KIND_VERSION))

.PHONY: kustomize
kustomize: $(LOCALBIN) ## Download kustomize locally if necessary.
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

# The engine itself, for every suite that runs the real engine binary: built
# from the crewlet module at the commit CREWLET_IMG was built from, which
# test/enginecommit reads from the image's revision label in its registry, so
# these suites and the e2e suite run one engine. Never the module proxy's
# @main, which runs ahead of the image: crewlet builds it minutes after a
# merge, and not at all for a merge that starts no workflow or whose build
# fails. The commit is resolved to its module version, which names the binary,
# on every run: a moving engine has no version to compare with until it has
# been asked. Each new engine replaces the binaries of the ones before it,
# which take over 150 MB each and change daily: bin/ is one checkout's, so
# nothing else runs them. (Two checkouts sharing a LOCALBIN would share
# bin/crewlet too, and run one another's engine.) The binary is installed into
# a directory of its own first, so neither its name nor CREWLET's is assumed.
CREWLET_MODULE := github.com/crewlet/crewlet
.PHONY: crewlet
crewlet: $(LOCALBIN) ## Install the Crewlet engine every suite runs into bin/crewlet: CREWLET_IMG's commit, or CREWLET_VERSION.
	@set -e; \
	if [ -n "$(CREWLET_VERSION)" ]; then \
		ref='$(CREWLET_VERSION)'; \
		echo "Crewlet engine: CREWLET_VERSION=$$ref"; \
	else \
		ref="$$(go run ./test/enginecommit '$(CREWLET_IMG)')"; \
	fi; \
	version="$$(GOWORK=off GOFLAGS= go list -m -f '{{.Version}}' '$(CREWLET_MODULE)@'"$$ref")"; \
	echo "Crewlet engine: $(CREWLET_MODULE) $$version"; \
	if [ ! -f "$(CREWLET)-$$version" ]; then \
		echo "Installing $(CREWLET_MODULE)/cmd/crewlet@$$version"; \
		installed="$$(mktemp -d "$(LOCALBIN)/.crewlet.XXXXXX")"; \
		trap 'rm -rf "$$installed"' EXIT; \
		GOBIN="$$installed" go install "$(CREWLET_MODULE)/cmd/crewlet@$$version"; \
		rm -f "$(CREWLET)"-v*; \
		mv "$$installed/crewlet" "$(CREWLET)-$$version"; \
	fi; \
	ln -sf "$(CREWLET)-$$version" "$(CREWLET)"

.PHONY: controller-gen
controller-gen: $(LOCALBIN) ## Download controller-gen locally if necessary.
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the envtest binaries (kube-apiserver, etcd) for ENVTEST_K8S_VERSION.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@"$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path >/dev/null || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(LOCALBIN) ## Download setup-envtest locally if necessary.
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

# .custom-gcl.yml adds the logcheck plugin, which golangci-lint can only load
# by compiling it in: the stock binary is installed to a scratch directory and
# used once, to build the custom binary that bin/golangci-lint then points at.
# The binary's name carries a hash of .custom-gcl.yml, so a plugin change
# rebuilds it just as a version bump does.
GOLANGCI_LINT_BUILD = $(GOLANGCI_LINT)-$(GOLANGCI_LINT_VERSION)-$(shell sha256sum .custom-gcl.yml | cut -c1-12)
.PHONY: golangci-lint
golangci-lint: $(LOCALBIN) ## Build golangci-lint with the plugins in .custom-gcl.yml locally if necessary.
	@grep -qx 'version: $(GOLANGCI_LINT_VERSION)' .custom-gcl.yml || { \
		echo "GOLANGCI_LINT_VERSION is $(GOLANGCI_LINT_VERSION) but .custom-gcl.yml names another version; set both."; \
		exit 1; \
	}
	@[ -f "$(GOLANGCI_LINT_BUILD)" ] || { \
		set -e; \
		stock="$$(mktemp -d)"; \
		trap 'rm -rf "$$stock"' EXIT; \
		echo "Downloading github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)"; \
		GOBIN="$$stock" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
		echo "Building golangci-lint $(GOLANGCI_LINT_VERSION) with the plugins in .custom-gcl.yml"; \
		"$$stock/golangci-lint" custom --destination "$(LOCALBIN)" --name "$$(basename "$(GOLANGCI_LINT_BUILD)")"; \
	}
	@ln -sf "$(GOLANGCI_LINT_BUILD)" "$(GOLANGCI_LINT)"

# go-install-tool installs a Go tool into bin/ as <name>-<version> and points
# <name> at it, unless that version is already there.
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f "$(1)" ;\
GOBIN="$(LOCALBIN)" go install $${package} ;\
mv "$(LOCALBIN)/$$(basename "$(1)")" "$(1)-$(3)" ;\
} ;\
ln -sf "$(1)-$(3)" "$(1)"
endef

define gomodver
$(shell go list -m -f '{{if .Replace}}{{.Replace.Version}}{{else}}{{.Version}}{{end}}' $(1) 2>/dev/null)
endef

##@ Helm

# The chart in dist/chart is generated by Kubebuilder's helm/v2-alpha plugin
# from the kustomize output (dist/install.yaml). Its templates are regenerated
# every time; Chart.yaml, values.yaml, NOTES.txt and _helpers.tpl are written
# once and then curated by hand. A release packages it with the tag as both
# the chart version and the appVersion, which is the image tag it deploys.
HELM_NAMESPACE ?= crewlet-operator-system
HELM_RELEASE ?= crewlet-operator
HELM_CHART_DIR ?= dist/chart
HELM_EXTRA_ARGS ?=

# The plugin also writes .github/workflows/test-chart.yml whenever it is
# missing. Its checks (helm lint, an install on kind) live in ci.yml, so the
# copy is removed again rather than kept as a second, drifting workflow.
.PHONY: chart
chart: build-installer kubebuilder ## Regenerate the Helm chart templates in dist/chart from config/.
	"$(KUBEBUILDER)" edit --plugins=helm/v2-alpha
	rm -f .github/workflows/test-chart.yml

.PHONY: helm-lint
helm-lint: helm ## Lint the Helm chart and render it with its default values.
	"$(HELM)" lint --strict $(HELM_CHART_DIR)
	"$(HELM)" template $(HELM_RELEASE) $(HELM_CHART_DIR) --namespace $(HELM_NAMESPACE) >/dev/null

.PHONY: helm-deploy
helm-deploy: helm ## Install or upgrade the chart in the current cluster, deploying image IMG.
	IMG="$(IMG)"; "$(HELM)" upgrade --install $(HELM_RELEASE) $(HELM_CHART_DIR) \
		--namespace $(HELM_NAMESPACE) \
		--create-namespace \
		--set manager.image.repository=$${IMG%:*} \
		--set manager.image.tag=$${IMG##*:} \
		--wait \
		--timeout 5m \
		$(HELM_EXTRA_ARGS)

.PHONY: helm-uninstall
helm-uninstall: helm ## Uninstall the Helm release.
	"$(HELM)" uninstall $(HELM_RELEASE) --namespace $(HELM_NAMESPACE)

.PHONY: helm-status
helm-status: helm ## Show the Helm release's status.
	"$(HELM)" status $(HELM_RELEASE) --namespace $(HELM_NAMESPACE)

.PHONY: helm-history
helm-history: helm ## Show the Helm release's history.
	"$(HELM)" history $(HELM_RELEASE) --namespace $(HELM_NAMESPACE)

.PHONY: helm-rollback
helm-rollback: helm ## Roll the Helm release back to its previous revision.
	"$(HELM)" rollback $(HELM_RELEASE) --namespace $(HELM_NAMESPACE)
