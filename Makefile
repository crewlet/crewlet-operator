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

.PHONY: all
all: build

##@ General

# Every target that carries a `## description` is listed by `make help`,
# grouped under the `##@` heading above it.

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

.PHONY: tidy
tidy: ## Run go mod tidy in both modules.
	@for m in $(GO_MODULES); do (cd $$m && go mod tidy); done

# The race detector is on because a controller is concurrent by construction:
# a reconciler, its watches and the cache all run on their own goroutines.
# test/e2e is excluded; it needs a cluster and runs under test-e2e.
.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run the unit and envtest suites of both modules.
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" \
		go test -race $$(go list ./... | grep -v /test/e2e) -coverprofile cover.out
	cd api && go test -race ./... -coverprofile cover.out

KIND_CLUSTER ?= crewlet-operator-test-e2e

.PHONY: setup-test-e2e
setup-test-e2e: kind ## Create the kind cluster for the e2e suite unless it exists.
	@case "$$("$(KIND)" get clusters)" in \
		*"$(KIND_CLUSTER)"*) \
			echo "Kind cluster '$(KIND_CLUSTER)' already exists. Skipping creation." ;; \
		*) \
			echo "Creating Kind cluster '$(KIND_CLUSTER)'..."; \
			"$(KIND)" create cluster --name $(KIND_CLUSTER) ;; \
	esac

# The suite shells out to kind, kubectl and make, so the pinned tools go first
# on its PATH. kubectl's kuberc is off unless KUBECTL_KUBERC=true, so a local
# kubectl configuration cannot change what the suite sees.
.PHONY: test-e2e
test-e2e: setup-test-e2e manifests generate fmt vet kubectl ## Run the e2e suite against a kind cluster, then delete the cluster.
	PATH="$(LOCALBIN):$$PATH" KIND="$(KIND)" KIND_CLUSTER=$(KIND_CLUSTER) \
		go test -tags=e2e ./test/e2e/ -v -ginkgo.v
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

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build the manager binary into bin/manager.
	go build -o bin/manager ./cmd

.PHONY: run
run: manifests generate fmt vet ## Run the manager from your host against the current kubeconfig.
	go run ./cmd

.PHONY: docker-build
docker-build: ## Build the manager image for the local platform.
	$(CONTAINER_TOOL) build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push the manager image.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS are the architectures docker-buildx builds and pushes. The
# Dockerfile cross-compiles on the build platform, so no target here needs
# emulation; a builder with the docker-container driver is still required to
# push a multi-platform manifest list.
PLATFORMS ?= linux/amd64,linux/arm64
.PHONY: docker-buildx
docker-buildx: ## Build and push the manager image for every platform in PLATFORMS.
	- $(CONTAINER_TOOL) buildx create --name crewlet-operator-builder
	$(CONTAINER_TOOL) buildx use crewlet-operator-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} .
	- $(CONTAINER_TOOL) buildx rm crewlet-operator-builder

.PHONY: build-installer
build-installer: manifests generate kustomize ## Render CRDs and the manager deployment into dist/install.yaml.
	mkdir -p dist
	cd config/manager && "$(KUSTOMIZE)" edit set image controller=${IMG}
	"$(KUSTOMIZE)" build config/default > dist/install.yaml

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
	cd config/manager && "$(KUSTOMIZE)" edit set image controller=${IMG}
	"$(KUSTOMIZE)" build config/default | "$(KUBECTL)" apply --server-side -f -

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

KUBECTL_VERSION ?= v1.37.1
KIND_VERSION ?= v0.33.0
KUSTOMIZE_VERSION ?= v5.8.3
CONTROLLER_TOOLS_VERSION ?= v0.22.0
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

.PHONY: kind
kind: $(LOCALBIN) ## Download kind locally if necessary.
	$(call go-install-tool,$(KIND),sigs.k8s.io/kind,$(KIND_VERSION))

.PHONY: kustomize
kustomize: $(LOCALBIN) ## Download kustomize locally if necessary.
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

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
.PHONY: golangci-lint
golangci-lint: $(LOCALBIN) ## Build golangci-lint with the logcheck plugin locally if necessary.
	@grep -qx 'version: $(GOLANGCI_LINT_VERSION)' .custom-gcl.yml || { \
		echo "GOLANGCI_LINT_VERSION is $(GOLANGCI_LINT_VERSION) but .custom-gcl.yml names another version; set both."; \
		exit 1; \
	}
	@[ -f "$(GOLANGCI_LINT)-$(GOLANGCI_LINT_VERSION)" ] || { \
		set -e; \
		stock="$$(mktemp -d)"; \
		trap 'rm -rf "$$stock"' EXIT; \
		echo "Downloading github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)"; \
		GOBIN="$$stock" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
		echo "Building golangci-lint $(GOLANGCI_LINT_VERSION) with the plugins in .custom-gcl.yml"; \
		"$$stock/golangci-lint" custom --destination "$(LOCALBIN)" --name "golangci-lint-$(GOLANGCI_LINT_VERSION)"; \
	}
	@ln -sf "$(GOLANGCI_LINT)-$(GOLANGCI_LINT_VERSION)" "$(GOLANGCI_LINT)"

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
