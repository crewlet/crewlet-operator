# The engine image you run, by digest.
ARG ENGINE_IMAGE

# Node.js and Copilot's package tree are fetched and checked here, with a curl
# and an xz the engine image does not ship: only the binary of this platform's
# package leaves this stage.
FROM ${ENGINE_IMAGE} AS copilot
ARG TARGETARCH
# Crewlet's floor Node (NODE_FLOOR_VERSION and its SHA-256s in crewlet/crewlet's
# scripts/node-pin.env), reused rather than pinned again. It runs npm here and
# nothing at run time: the binary embeds its own Node.js.
ARG NODE_VERSION=22.23.5
ARG NODE_SHA256_AMD64=cdeb8670a0c81f51b4dc4ac61919f9b1ad917b304bf8221eb939570663d240f8
ARG NODE_SHA256_ARM64=2babb9eb5264860d540849f4b064a6fbdebcdc323657d9d382ce23d313fd5e09
USER root
RUN apt-get update && apt-get install -y --no-install-recommends curl xz-utils
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
WORKDIR /fetch
RUN case "${TARGETARCH}" in \
      amd64) platform=linux-x64 sha256=${NODE_SHA256_AMD64} ;; \
      arm64) platform=linux-arm64 sha256=${NODE_SHA256_ARM64} ;; \
      *) echo "no Node.js or Copilot CLI build for ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSLo node.tar.xz "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-${platform}.tar.xz"; \
    echo "${sha256}  node.tar.xz" | sha256sum -c -; \
    mkdir /opt/node; \
    tar -xJf node.tar.xz -C /opt/node --strip-components=1; \
    echo "${platform}" > /fetch/platform
ENV PATH=/opt/node/bin:${PATH}
# npm ci installs exactly the lock, every package checked against its
# integrity, and of the platform packages those of this machine's CPU; the
# glibc one's binary is what the image runs. Copilot has no install scripts.
WORKDIR /opt/copilot
COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund; \
    cp "node_modules/@github/copilot-$(cat /fetch/platform)/copilot" /fetch/copilot

FROM ${ENGINE_IMAGE}
COPY --from=copilot --chown=0:0 /fetch/copilot /usr/local/bin/copilot
