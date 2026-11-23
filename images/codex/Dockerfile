# The engine image you run, by digest.
ARG ENGINE_IMAGE

# Node.js and Codex's package tree are fetched and checked here, with a curl
# and an xz the engine image does not ship: only this platform's vendored
# Codex directory leaves this stage.
FROM ${ENGINE_IMAGE} AS codex
ARG TARGETARCH
# Crewlet's floor Node (NODE_FLOOR_VERSION and its SHA-256s in crewlet/crewlet's
# scripts/node-pin.env), reused rather than pinned again. It runs npm here and
# nothing at run time: Codex is a static musl binary.
ARG NODE_VERSION=22.23.5
ARG NODE_SHA256_AMD64=cdeb8670a0c81f51b4dc4ac61919f9b1ad917b304bf8221eb939570663d240f8
ARG NODE_SHA256_ARM64=2babb9eb5264860d540849f4b064a6fbdebcdc323657d9d382ce23d313fd5e09
USER root
RUN apt-get update && apt-get install -y --no-install-recommends curl xz-utils
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
WORKDIR /fetch
RUN case "${TARGETARCH}" in \
      amd64) platform=linux-x64 target=x86_64-unknown-linux-musl sha256=${NODE_SHA256_AMD64} ;; \
      arm64) platform=linux-arm64 target=aarch64-unknown-linux-musl sha256=${NODE_SHA256_ARM64} ;; \
      *) echo "no Node.js or Codex build for ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSLo node.tar.xz "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-${platform}.tar.xz"; \
    echo "${sha256}  node.tar.xz" | sha256sum -c -; \
    mkdir /opt/node; \
    tar -xJf node.tar.xz -C /opt/node --strip-components=1; \
    echo "${platform}/vendor/${target}" > /fetch/vendor
ENV PATH=/opt/node/bin:${PATH}
# package.json pins @openai/codex at 0.171.0, the one release Crewlet's codex
# profile is written for: its runner refuses an older one and continues a
# session only on that release. A bump needs the profile re-measured on the
# new release first. npm ci installs exactly the lock, every package checked
# against its integrity, and of the platform packages those of this machine's
# CPU. Codex has no install scripts.
WORKDIR /opt/codex-npm
COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund; \
    mkdir -p /out/opt /out/usr/local/bin; \
    cp -a "node_modules/@openai/codex-$(cat /fetch/vendor)" /out/opt/codex; \
    ln -s /opt/codex/bin/codex /out/usr/local/bin/codex

# Codex finds its rg and helpers through the codex-package.json beside its own
# executable, so the directory stays whole and PATH reaches it by a symlink.
FROM ${ENGINE_IMAGE}
COPY --from=codex --chown=0:0 /out/ /
