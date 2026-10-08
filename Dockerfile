# The operator's container image: a static manager binary on distroless.
#
# Both base images are pinned by digest, with the tag beside it for the reader
# and for Dependabot, which bumps the tag and the digest together.

# golang:1.27.1 is held back on purpose: go1.27.2 is the newest Go release,
# but no golang:1.27.2 image has been published yet. GOTOOLCHAIN=auto below
# makes this image fetch the go1.27.2 toolchain that go.mod's `go` line
# names (checksum-verified against sum.golang.org), so the binary is still
# built with the newest Go. Move the tag when the image appears; auto is
# then a no-op.
FROM --platform=$BUILDPLATFORM golang:1.27.1-trixie@sha256:8f58fd67ea075142d947a60e0caa4317746a55118d312f027793d382c7741734 AS builder
ARG TARGETOS
ARG TARGETARCH

# The official golang images set GOTOOLCHAIN=local, which would refuse a
# go.mod asking for a newer Go than the image carries.
ENV GOTOOLCHAIN=auto

WORKDIR /workspace

# Module manifests first, so the dependency download is its own layer and
# survives source-only changes.
COPY go.mod go.sum ./
RUN go mod download

# The rest of the source; .dockerignore keeps it to Go files.
COPY . .

# Cross-compiled on the build platform ($BUILDPLATFORM above) rather than
# emulated: CGO is off, so Go needs nothing from the target architecture, and
# a multi-arch build under QEMU would be several times slower.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags='-s -w' -o manager ./cmd

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
WORKDIR /
COPY --from=builder /workspace/manager .
# The distroless nonroot user, numerically, so a restricted Pod Security
# admission check (runAsNonRoot) can verify it without resolving a name.
USER 65532:65532

ENTRYPOINT ["/manager"]
