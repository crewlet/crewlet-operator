# The operator's container image: a static manager binary on distroless.
#
# Both base images are pinned by digest, with the tag beside it for the reader
# and for Dependabot, which bumps the tag and the digest together.

FROM --platform=$BUILDPLATFORM golang:1.27.2-trixie@sha256:e58d6f83b3416618d8bcac2b3dde1b7f7e3c4a77d25e88637f8bbae81536c48d AS builder
ARG TARGETOS
ARG TARGETARCH

# The image's Go is the go.mod `go` line, and the image's own
# GOTOOLCHAIN=local is left alone: a go.mod that asks for a newer Go than
# this tag fails the build at once, which says to move the tag here, rather
# than fetching a toolchain the pin above does not name.

WORKDIR /workspace

# Module manifests first, so the dependency download is its own layer and
# survives source-only changes. The API module's manifests come too: go.mod
# replaces github.com/crewlet/crewlet-operator/api with ./api, and the go
# command reads that module's go.mod to resolve the graph.
COPY go.mod go.sum ./
COPY api/go.mod api/go.sum api/
RUN go mod download

# The rest of the source; .dockerignore keeps it to Go files.
COPY . .

# Cross-compiled on the build platform ($BUILDPLATFORM above) rather than
# emulated: CGO is off, so Go needs nothing from the target architecture, and
# a multi-arch build under QEMU would be several times slower.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags='-s -w' -o manager ./cmd

FROM gcr.io/distroless/static-debian13:nonroot@sha256:d62067879de890a1def9da0e7766b7d20810a5e5555cef8506b752437d7a3e88
WORKDIR /
COPY --from=builder /workspace/manager .
# The distroless nonroot user, numerically, so a restricted Pod Security
# admission check (runAsNonRoot) can verify it without resolving a name.
USER 65532:65532

ENTRYPOINT ["/manager"]
