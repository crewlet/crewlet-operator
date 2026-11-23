# The engine image you run, by digest.
ARG ENGINE_IMAGE

# Nous Research's own image of the one release Crewlet's Hermes runner
# drives. Its /opt/hermes is a whole install, root-owned, with its own Python,
# Node.js and tools under /opt/hermes/tools, which runs offline on a
# read-only root as any user: an install.sh install instead serves only the
# account that ran it, preparing each other home's dependencies from the
# network at its first launch.
FROM nousresearch/hermes-agent:v0.23.3@sha256:3e9fe4c6ef36490f43aa0d0d80f9df64e9c92a576eee3042f1a97c8813c05355 AS hermes

FROM ${ENGINE_IMAGE}
USER root
# Hermes's bundled node links libatomic, which the engine image's Debian does
# not ship; the two images share a Debian release, so its package fits.
RUN apt-get update && apt-get install -y --no-install-recommends libatomic1 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=hermes --chown=0:0 /opt/hermes /opt/hermes
# The install's virtual environment runs /usr/local/bin/python3, a link into
# /opt/hermes/tools, as are node, npm, npx, rg, ffmpeg and ffprobe there.
COPY --from=hermes --chown=0:0 /usr/local/bin/ /usr/local/bin/
RUN ln -s /opt/hermes/bin/hermes /usr/local/bin/hermes
USER crewlet
