# Running Tool Servers

An `MCPServer` is a tool server a company's agents call: a program the engine
starts beside itself, or a remote server it connects to. It is how an agent
acts anywhere outside the engine, its chat replies included under Mattermost
and Slack.

Each renders one entry of Crewlet's `mcp_servers`; Crewlet's
[Tools & MCP](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md#mcp-integration)
describes what the engine does with it. This guide covers what changes when
the engine runs in a pod: a server the engine starts has to be in its image,
and Crewlet's image carries none; and each integration names the server its
agents use.

## Stdio and HTTP

- **`transport: HTTP`** connects to a server that runs elsewhere, at `url`.
  Nothing goes into the image.
- **`transport: Stdio`**, the default, starts `command` with `args` as a
  process of the engine's own container, as the engine's user (uid 10001),
  and speaks MCP over its standard input and output. The process gets the
  engine's environment, the server's `env` on top of it, and, for a server
  with `shared: false`, the credentials of the seat it serves.

A server with `shared: false` is a template rather than a process: each seat
that gives it credentials in its `spec.mcp` gets a process of its own, on the
pod that holds the seat, which is how each agent acts as itself. Every engine
pod of a company runs the class's image, a [seat pool](topology.md)'s pods
too, so the image has to carry every server any seat may need.

## The image needs the server

Crewlet's image is Debian with `ca-certificates`, `git`, `tini` and the
engine: no Python, Node.js, `uv`, `npm` or `curl`. Its
[Dockerfile](https://github.com/crewlet/crewlet/blob/main/Dockerfile) says so
on purpose: a stdio server whose runtime is not there needs a derived image.

A `command` the image does not have never starts. The engine logs
`mcp_server_failed`, the seats run without that server's tools, and nothing
else is affected; the MCPServer still reports `Applied=True`, since the
engine runs the document that names it
([A tool server does not start](troubleshooting.md#a-tool-server-does-not-start)).
Under Mattermost and Slack the server is the agent's only way to post, so on
the plain image an agent receives every message, runs its turn and answers
none.

Crewlet's own examples start these servers through a package runner:
`uvx mcp-server-mattermost` for a Python server, and
`npm exec slack-mcp-server@latest` for Slack's, a Go binary npm delivers. The
runner downloads the server, and for `uvx` a Python, as it starts. In a pod
that does not work: the runner is not in the image, and in an image that had
it, everything it downloaded would land in `HOME`, an `emptyDir` emptied
whenever the pod starts. Every pod would download its servers again at every
start, need egress to PyPI or npm to start them at all, and run whichever
release was newest that day. Install every server when you build the image,
pinned, and name the installed command.

## Building the image

Build the image from the engine image you run, named by digest: the engine of
the operator's release, or the copy of Crewlet's `main` image that
[Installing](installing.md#engine-image) makes in your registry. This
Dockerfile installs the four stdio servers Crewlet names for its
integrations ([below](#each-integrations-server)); delete the lines of any
your company does not run.

```dockerfile
# The engine image you run, by digest, and uv, which installs the Python
# servers and is not kept.
ARG ENGINE_IMAGE
ARG UV_IMAGE=ghcr.io/astral-sh/uv:0.13.0@sha256:cdc6093146eb3ff6a40107b38f008b789e050e77ad87865e381d9917da55a168

FROM ${UV_IMAGE} AS uv

# The two Go servers are downloaded and checked here, with a curl the engine
# image does not ship: only the two binaries leave this stage.
FROM ${ENGINE_IMAGE} AS fetch
ARG TARGETARCH
# GitLab: glab, whose `glab mcp serve` is the server. The digests are the
# release's checksums.txt.
ARG GLAB_VERSION=1.122.0
ARG GLAB_SHA256_AMD64=242326668011a110ea6dcbe865cfbf3ab763f9909851c90ef0da23ab1f2f3808
ARG GLAB_SHA256_ARM64=78910c21b6b517c0936e413df96e59f942558a3a4e4109370a452761f7d0f253
# Slack: slack-mcp-server, a Go program npm delivers. The binary is taken from
# its platform package, checked against the registry's dist.integrity, so no
# Node.js is needed to run it.
ARG SLACK_MCP_SERVER_VERSION=1.3.0
ARG SLACK_MCP_SERVER_INTEGRITY_AMD64=sha512-ud5BqvPq6GE2wazMX+qq+PeSuhKRzLBhYhoDVyrShGe5bguxoE+/olffsnUMdFLKwLaLxs/DMPdHprRUVGqJzw==
ARG SLACK_MCP_SERVER_INTEGRITY_ARM64=sha512-7yLIg4u6tlhk1TIsrayUgqMvJdvZ+Wp80JHy83pAx0pYCyo7Inwt80QNWKem/xMFC8RSOH//YHrlAWhATFQa+w==
USER root
RUN apt-get update && apt-get install -y --no-install-recommends curl
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
WORKDIR /fetch
RUN case "${TARGETARCH}" in \
      amd64) sum=${GLAB_SHA256_AMD64} integrity=${SLACK_MCP_SERVER_INTEGRITY_AMD64} ;; \
      arm64) sum=${GLAB_SHA256_ARM64} integrity=${SLACK_MCP_SERVER_INTEGRITY_ARM64} ;; \
      *) echo "no digest for ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSLo glab.tar.gz "https://gitlab.com/gitlab-org/cli/-/releases/v${GLAB_VERSION}/downloads/glab_${GLAB_VERSION}_linux_${TARGETARCH}.tar.gz"; \
    echo "${sum}  glab.tar.gz" | sha256sum -c -; \
    tar -xzf glab.tar.gz --strip-components=1 bin/glab; \
    package=slack-mcp-server-linux-${TARGETARCH}; \
    curl -fsSLo slack.tgz "https://registry.npmjs.org/${package}/-/${package}-${SLACK_MCP_SERVER_VERSION}.tgz"; \
    test "sha512-$(openssl dgst -sha512 -binary slack.tgz | base64 -w0)" = "${integrity}"; \
    tar -xzf slack.tgz --strip-components=2 "package/bin/${package}"; \
    mv "${package}" slack-mcp-server

FROM ${ENGINE_IMAGE}
# Python 3.13, not 3.14 or 3.15: both servers accept any Python from 3.10,
# but at these releases their CI tests 3.10 to 3.13 only, and
# mcp-atlassian's own image runs 3.13.
ARG PYTHON_VERSION=3.13.16
ARG MCP_SERVER_MATTERMOST_VERSION=0.6.1
ARG MCP_ATLASSIAN_VERSION=0.23.1
# The servers' own dependencies are resolved as PyPI had them on this date,
# so a rebuild installs what was tested. Move it with the versions.
ARG RESOLVED_AT=2026-10-12T00:00:00Z
USER root
# The interpreter, its standard library compiled, and each server's
# environment go under /opt/uv, the servers' commands into /usr/local/bin.
# Compiled now, because the root filesystem is read-only at run time: nothing
# can write the bytecode then, so every start would compile it again.
RUN --mount=from=uv,source=/uv,target=/bin/uv \
    export UV_NO_CACHE=1 UV_PYTHON_INSTALL_DIR=/opt/uv/python \
      UV_TOOL_DIR=/opt/uv/tools UV_TOOL_BIN_DIR=/usr/local/bin \
      UV_EXCLUDE_NEWER="${RESOLVED_AT}"; \
    uv python install --no-bin --compile-bytecode "${PYTHON_VERSION}" \
    && uv tool install --managed-python --compile-bytecode --python "${PYTHON_VERSION}" \
      "mcp-server-mattermost==${MCP_SERVER_MATTERMOST_VERSION}" \
    && uv tool install --managed-python --compile-bytecode --python "${PYTHON_VERSION}" \
      "mcp-atlassian==${MCP_ATLASSIAN_VERSION}"
COPY --from=fetch /fetch/glab /fetch/slack-mcp-server /usr/local/bin/
# The image is how glab is updated. The engine hands a stdio server its own
# environment, so this reaches every glab it starts, which would otherwise ask
# gitlab.com for a newer release.
ENV GLAB_CHECK_UPDATE=false
USER crewlet
```

- **The Python servers** each get a virtual environment of their own from
  `uv`, on one Python that `uv` installs under `/opt/uv`; `uv` itself is
  mounted for that step and does not stay in the image. Python writes the
  bytecode of every module it imports beside the module, and on a read-only
  root it cannot, so without the two `--compile-bytecode` flags a server
  would compile its standard library and its dependencies again at every
  start, in every seat's process.
- **`RESOLVED_AT`** holds the servers' own dependencies, which their pins do
  not name, to what PyPI held on that date (`uv`'s `--exclude-newer`): a
  rebuild next month installs the set that was tested, not whatever was
  released since. A version released after it cannot be resolved, so move
  the date with the versions.
- **The Go servers** need no runtime: GitLab's release archive is checked
  against its published SHA-256, and Slack's binary is taken out of the npm
  package `npm exec` would have run, checked against the integrity npm's
  registry records for it.

Build it from the engine image,

```bash
docker build --build-arg ENGINE_IMAGE=<your registry>/crewlet@sha256:<digest> \
  -t <your registry>/crewlet-tool-servers:<crewlet commit> .
```

check that each server starts in it as the engine's pod would start it, as
uid 10001 on a read-only root, with an empty `HOME` and `/tmp`, no
capabilities and no network, and answers what the engine first asks a server:
MCP's `initialize`, then `tools/list`, which has to name a tool the
integration's agents act with. The client is the image's own `bash`, so your
machine needs nothing but `docker`, and the settings are placeholders, since
nothing is reached:

```bash
image=<your registry>/crewlet-tool-servers:<crewlet commit>
pod=(--rm --read-only --network none --user 10001:10001 --cap-drop ALL
  --tmpfs /tmp --tmpfs /home/crewlet:uid=10001,gid=10001 -e HOME=/home/crewlet)
# lists TOOL COMMAND [ARG...]: COMMAND answers MCP on stdio and lists TOOL.
lists() {
  docker run -i "${pod[@]}" --entrypoint bash "$image" -s "$@" <<'CHECK'
tool=$1; shift
mkfifo /tmp/requests /tmp/answers
"$@" </tmp/requests >/tmp/answers 2>/tmp/stderr &
exec 3>/tmp/requests 4</tmp/answers
trap '' PIPE
printf '%s\n' >&3 \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"check","version":"1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
while IFS= read -r -t 60 line <&4 && [[ $line != *'"id":2'* ]]; do :; done
exec 3>&-
wait
if [[ $line == *"\"name\":\"$tool\""* ]]; then echo "lists $tool"
else echo "lists no $tool:" >&2; tail -n 20 /tmp/stderr >&2; exit 1; fi
CHECK
}
lists post_message env MATTERMOST_URL=https://chat.example.invalid \
  MATTERMOST_TOKEN=placeholder /usr/local/bin/mcp-server-mattermost
lists jira_add_comment env JIRA_URL=https://jira.example.invalid \
  JIRA_USERNAME=check JIRA_API_TOKEN=placeholder /usr/local/bin/mcp-atlassian
lists glab_mr_note /usr/local/bin/glab mcp serve
docker run "${pod[@]}" --entrypoint /usr/local/bin/slack-mcp-server "$image" --help 2>/dev/null &&
  echo "runs slack-mcp-server"
```

A failing check prints the last lines the server wrote to its standard error,
as the engine's `server_stderr_tail` does. The requests go in through a pipe
kept open until the answer is read: a server that sees the end of its input
stops, and may stop before it answers what it read. `--help` would prove much
less: a Python server parses its arguments before it imports its own code, so
its `--help` succeeds with none of its dependencies installed.
`slack-mcp-server` gets only `--help`, since it checks its token with Slack as
it starts, which needs the network: it shows that the binary runs in the
image, and no more.

Then name the image in the class; every company of the class runs it:

```yaml
# EngineClass with-tool-servers, excerpt
spec:
  engine:
    image: <your registry>/crewlet-tool-servers@sha256:<digest>   # crewlet <commit>
```

The four add about 480 MB to the image. Each running server is a process of
the engine's container, charged to its memory: started and asked for their
tools, `mcp-server-mattermost` held about 95 MB, `mcp-atlassian` 130 MB and
`glab` 50 MB. A server with `shared: false` runs once for every seat that
gives it credentials, on the pod that holds the seat, so size the class's
`data.resources` (and `seatPools.defaults.resources`) for those processes.

### One image with a coding CLI

A class names one engine image, so a company that also runs a seat on a
coding CLI needs one image with both. Build one on the other: build the image
of [The image needs the CLI](cli-agents.md#the-image-needs-the-cli) and push
it, then build this Dockerfile with `ENGINE_IMAGE` naming it,

```bash
docker build --build-arg ENGINE_IMAGE=<your registry>/crewlet-claude-code@sha256:<digest> \
  -t <your registry>/crewlet-agents:<crewlet commit> .
```

run the checks above against the result, and name it in the class. Both
Dockerfiles take the image they extend as `ENGINE_IMAGE`, so neither is
edited. Pasting one into the other does not work: the second file's `ARG`s
land inside the first one's last stage, where the second's `FROM` lines cannot
see them, and a build produces only its last stage, so the paste either fails
on a blank base image or leaves out the first file's additions.

## Each integration's server

Which server each integration's agents use is Crewlet's choice, made in its
own pages; the object below is that server as an `MCPServer`, named as
Crewlet looks for it.

| Integration | Tool server | Runtime | In the image |
|---|---|---|---|
| Mattermost | [`mcp-server-mattermost`](https://pypi.org/project/mcp-server-mattermost/), from PyPI | Python | `/usr/local/bin/mcp-server-mattermost` |
| Jira, Confluence, an Atlassian organization | [`mcp-atlassian`](https://pypi.org/project/mcp-atlassian/), from PyPI, one server for both products | Python | `/usr/local/bin/mcp-atlassian` |
| Slack | [`slack-mcp-server`](https://www.npmjs.com/package/slack-mcp-server), from npm | none: a Go binary | `/usr/local/bin/slack-mcp-server` |
| GitLab | [`glab`](https://gitlab.com/gitlab-org/cli), GitLab's CLI, as `glab mcp serve` | none: a Go binary | `/usr/local/bin/glab` |
| GitHub | GitHub's remote server, `https://api.githubcopilot.com/mcp/` | none: `transport: HTTP` | nothing |
| Datadog | none named ([below](#datadog)) | — | — |

Every `env` entry of an `MCPServer` is read from a Secret, a setting that is
not secret too: Crewlet treats a server's whole environment as credentials
([A tool server's token](credentials.md#a-tool-servers-token)). The servers
below read their settings from one Secret:

```bash
kubectl create secret generic acme-tool-servers -n acme \
  --from-literal=mattermostURL=https://chat.acme.example \
  --from-literal=jiraURL=https://jira.acme.example \
  --from-literal=confluenceURL=https://wiki.acme.example \
  --from-literal=slackAddMessageTool=true
```

Each seat gives its own credentials for the server in its `spec.mcp`, as
[Connecting Integrations](integrations.md) shows for each vendor.

### Mattermost

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: MCPServer
metadata:
  name: mattermost
spec:
  companyRef: {name: acme}
  shared: false                         # one per seat, on the seat's own bot token
  command: /usr/local/bin/mcp-server-mattermost
  env:
    - name: MATTERMOST_URL              # the address the engine reaches Mattermost at
      valueFrom:
        secretKeyRef: {name: acme-tool-servers, key: mattermostURL}
  toolPrefix: mattermost_
```

The prefix makes its posting tool `mattermost_post_message`, the name
Crewlet's [mentions skill](https://github.com/crewlet/crewlet/blob/main/examples/tool-skills/platform-mentions.md)
triggers on. Each seat gives it `MATTERMOST_TOKEN`, the bot token of its
`spec.integrations.mattermost`
([Mattermost](integrations.md#mattermost); Crewlet's
[Mattermost](https://github.com/crewlet/crewlet/blob/main/docs/integrations/mattermost.md#configure-in-yaml)).

### Jira and Confluence

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: MCPServer
metadata:
  name: atlassian                       # where Crewlet reads a seat's Atlassian account
spec:
  companyRef: {name: acme}
  shared: false
  command: /usr/local/bin/mcp-atlassian
  env:
    - name: JIRA_URL
      valueFrom:
        secretKeyRef: {name: acme-tool-servers, key: jiraURL}
    - name: CONFLUENCE_URL
      valueFrom:
        secretKeyRef: {name: acme-tool-servers, key: confluenceURL}
```

One server serves both products, so set the URL of each the company uses: the
engine does not derive them from the Integrations. Each seat gives it its
Atlassian account ([Jira and Confluence](integrations.md#jira-and-confluence);
Crewlet's [Jira](https://github.com/crewlet/crewlet/blob/main/docs/integrations/jira.md#mcp-server-agents-control-jira)).

### Slack

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: MCPServer
metadata:
  name: slack
spec:
  companyRef: {name: acme}
  shared: false
  command: /usr/local/bin/slack-mcp-server
  args: [--transport, stdio]
  env:
    - name: SLACK_MCP_ADD_MESSAGE_TOOL  # without it, the server offers no tool that posts
      valueFrom:
        secretKeyRef: {name: acme-tool-servers, key: slackAddMessageTool}
  toolPrefix: slack_
```

`slack-mcp-server` registers `conversations_add_message`, its only tool that
posts, when `SLACK_MCP_ADD_MESSAGE_TOOL` is set, or when
`SLACK_MCP_ENABLED_TOOLS`, the list of tools it offers, names it; a list that
leaves it out hides it whatever `SLACK_MCP_ADD_MESSAGE_TOOL` says. `true` lets
it post anywhere the bot is, and a list of channel IDs limits it to those. Its
reaction tools wait on `SLACK_MCP_REACTION_TOOL` the same way
([its settings](https://github.com/korotovsky/slack-mcp-server#environment-variables-quick-reference)).
Each seat gives it the bot token of its Slack app, the same key its
`spec.integrations.slack` names:

```yaml
spec:                       # Seat lead
  integrations:
    slack:
      credentialsSecretRef: {name: lead-slack}    # keys botToken, signingSecret
  mcp:
    - name: slack
      env:
        - name: SLACK_MCP_XOXB_TOKEN
          valueFrom:
            secretKeyRef: {name: lead-slack, key: botToken}
```

### GitLab

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: MCPServer
metadata:
  name: gitlab
spec:
  companyRef: {name: acme}
  shared: false
  command: /usr/local/bin/glab
  args: [mcp, serve]
```

Each seat gives it `GITLAB_TOKEN` ([GitLab](integrations.md#gitlab)); on a
self-managed instance, give it `GITLAB_HOST` as well, in the server's `env`.
GitLab marks `glab mcp serve` experimental, liable to change or go away, so
read `glab`'s release notes before you move its pin (Crewlet's
[GitLab](https://github.com/crewlet/crewlet/blob/main/docs/integrations/gitlab.md#mcp-tool-server)).

### GitHub

```yaml
apiVersion: crewlet.ai/v1alpha1
kind: MCPServer
metadata:
  name: github
spec:
  companyRef: {name: acme}
  transport: HTTP
  shared: false
  url: https://api.githubcopilot.com/mcp/
```

A remote server: nothing goes into the image, and each seat sends its own
`Authorization` header ([A tool server's token](credentials.md#a-tool-servers-token);
Crewlet's [GitHub](https://github.com/crewlet/crewlet/blob/main/docs/integrations/github.md#mcp-tools)).

### Datadog

Crewlet names no Datadog tool server. Its provisioning mints each seat's
application key into the credentials the seat gives the `MCPServer` named
`datadog`, under `DD_APP_KEY`, `DATADOG_APP_KEY` or `app_key`
([Datadog](integrations.md#datadog)): choose a server that reads its key
from one of those, name it `datadog`, and, if it is a stdio server, install
it in the image as above.

## See also

- [Connecting Integrations](integrations.md) — each vendor's Integration and the seats' credentials for its server
- [Running Seats on a Coding CLI](cli-agents.md) — the other program a derived engine image carries
- [Crewlet's Tools & MCP](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md) — shared and per-seat servers, and what a server that fails costs
