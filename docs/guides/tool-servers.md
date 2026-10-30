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
  and speaks MCP over its standard input and output. The process does not
  get the engine's environment, only the part Crewlet hands a tool server:
  `PATH`, the locale, the TLS and proxy variables and the engine user's
  `HOME`, among a few others
  ([Crewlet's list](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md#what-a-stdio-servers-environment-is)).
  The server's `env` goes on top of that, and, for a server with
  `shared: false`, the values of the seat it serves on top again.

A variable the image sets with `ENV`, or the class's `podTemplate.spec.env`
or the Company's `runtime.env` sets on the pod, reaches a stdio server only
when that list names it, as it names the proxy variables. Give the server
every other variable it reads in its `env`
([Settings and credentials](#settings-and-credentials)): one that relies on
anything else of the engine's starts without it, and fails or behaves as
though it were unset.

A server with `shared: false` is a template rather than a process: each agent
seat that names it in `spec.mcp`, its own or its unit's, gets a process of its
own, on the pod that holds the seat, which is how each agent acts as itself.
Crewlet starts that process for an entry whatever it holds, a setting alone
too, and one started without the seat's credentials has no identity of the
seat's to act as. So a setting every seat shares goes in the server's own
`env` rather than in a Unit's `spec.mcp`, which starts a process for every
agent seat directly in the unit. Every engine pod of a company runs the
class's image, a [seat pool](topology.md)'s pods too, so the image has to
carry every server any seat may need.

## Settings and credentials

A server's `env` (an HTTP server's `headers`) and the values a Seat or a Unit
gives it in `spec.mcp` take two kinds of entry: a credential, read from a
Secret with `valueFrom`, and a setting that is no credential, such as an
address, a switch or a path, written out as `value`:

```yaml
spec:                       # MCPServer atlassian
  env:
    - name: JIRA_URL                    # a setting
      value: https://jira.acme.example
---
spec:                       # Seat lead
  mcp:
    - name: atlassian
      env:
        - name: JIRA_API_TOKEN          # a credential
          valueFrom:
            secretKeyRef: {name: lead-jira, key: token}
```

Crewlet cannot tell the two apart: it treats a server's whole environment,
and every value a seat gives it, as credentials, and masks each on every
read ([A tool server's token](credentials.md#a-tool-servers-token)). The
name can, and the API reads it as Crewlet reads a credential's name: a
`value` is refused under a name one of whose parts, cut at every character
that is not a letter or a digit, is, in any case, `KEY`, `KEYS`, `APIKEY`,
`ACCESSKEY`, `SECRETKEY`, `TOKEN`, `SECRET`, `PASSWORD`, `PASSWD`,
`CREDENTIAL`, `CREDENTIALS`, `AUTH`, `OAUTH` or `PAT`, and under the
`Authorization`, `Proxy-Authorization` and `Cookie` headers. So
`MATTERMOST_TOKEN`, `DD_APP_KEY` and `X-Api-Key` come from Secrets, and
`MATTERMOST_URL`, `GITLAB_HOST` or `SLACK_MCP_ADD_MESSAGE_TOOL` may be written
out. The rule keeps a token from being typed where a setting goes; it cannot
keep every secret out, and a credential under a name that says nothing, such
as a database address with a password in it, is still yours to put in a
Secret. A `value` lands in the company document as written, where anyone who
can read the object reads it. It may not hold a `${NAME}` reference: the
engine resolves one from its own secret store and then from its own
environment, which is how Crewlet passes one of the engine's own variables
on to a server, and the API does not offer that, since both hold credentials
that are not the object's to name. So a variable beyond what Crewlet's
allowlist hands every server is written out here or read from a Secret,
never named from the engine's secret store or environment.

A setting is the server's when every seat runs with it, and a seat's when
each seat needs its own: Slack's channel cache is one, a path no two seats
may share ([below](#slack)).

### A key that may go without its value

Crewlet starts a server only once every value it is started with holds
something: its own `env` or `headers`, and what a Seat or its Unit gives it.
A value read from a Secret that does not exist, holds no such key or holds an
empty value keeps the server from starting, shared or a seat's, rather than
handing it an empty token it would refuse or act as nobody with. A value the
server can run without, such as the key Context7's server takes for higher
rate limits and serves without, is listed by its key in `optionalKeys`:

```yaml
spec:                       # MCPServer context7
  transport: HTTP
  url: https://mcp.context7.com/mcp
  headers:
    - name: Authorization             # Bearer and the key
      valueFrom:
        secretKeyRef: {name: context7, key: authorization}
  optionalKeys: [Authorization]
```

Under a listed key, a value whose Secret or key does not exist, or that holds
an empty value, is left out whole: the server starts without the variable, or
its requests go without the header, as Kubernetes starts a container without
an `optional` `secretKeyRef`. Nothing holds the company's document, and the
object that gives the value reports `ResolvedRefs=True`, its message naming
each value it goes without and the list that lets it: the MCPServer for its
own `env` or `headers`, a Seat or a Unit for what its `spec.mcp` gives. When
the Secret appears, the operator pushes its value and applies the document
again, as it does for a rotated credential
([Rotating a credential](credentials.md#rotating-a-credential)), and the
engine starts the server with it. When it is deleted, the operator takes the
value out of the engine's secret store and has the engine read the store
again: by applying the next document, or, while that document is held for
something else, by activating the revision the engine runs again, unchanged,
so the server is not started again with a deleted Secret's value. That is the
operator's under the `Enforce` policy, under which it keeps the store in step
with the Secrets ([Management policies](../concepts/applying.md#management-policies)),
and it holds where the revision the engine runs goes without the value too.
A revision that reads the key as required, as the one from before you listed
the key does, or another document somebody else activated, whose reading the
operator does not know, keeps the value it holds until the next document
runs, as the last good revision keeps every value it requires; the Company's
`SecretsSynced`, the object's `ResolvedRefs` message and a `SecretKept` event
say so (a document the render holds is told by the event alone). Once no
engine pod runs, or would start on, a revision that requires it, the operator
deletes the value and activates the new revision again without it, so that
revision may start the server with the deleted Secret's value in between. A
pod that refused the next document keeps running the revision before it, which
keeps the value while it does; only a data pod starts on that revision again
if it restarts, while a seat pod starts on nothing and tries the fleet's
revision again, and a pod that runs no company and would start on none holds
nothing back. A pod whose revision the operator cannot learn keeps every
value, as a data pod does while it is down or cut off from the engine's fleet
([Applying a document](../concepts/applying.md#what-one-reconcile-does)). A
Secret key that is also read where it is required, by this server or anything
else, is not one the company goes without: its deletion holds the document
(`SecretNotFound`), and the revision the engine runs keeps the value until
the next one applies if it requires it too, and goes without it if it lets it
go.

- **The list covers every value the server is started with**: its own `env`
  (a Stdio server, by the variable's name, as written) or `headers` (an HTTP
  server, a header in any case), and every Seat's and Unit's `spec.mcp` entry
  for it, which may give a Stdio server a key outside a variable's grammar. A
  seat's value for a key replaces the server's own whole, so one that goes
  missing leaves the key out rather than handing the server's own value back.
  It is the server's settings the list is for: a seat's own credential is the
  identity its instance acts as, and one started without it acts as nobody.
- **A provisioned value is waited for whatever the list says**: an
  Integration mints it for the seat, and the server starts once it has. One
  nothing mints, while no Integration of the company has
  `provisioning: Engine`, is left out.
- **An entry must leave something out.** The API refuses one spelling an
  HTTP server's header twice, in two cases, and a key no Seat or Unit could
  write. One under which no value is read from a Secret, or provisioned, by
  the server or by any Seat or Unit for it, every value there being a literal
  `value`, which is never missing, or there being none, holds the company's
  document: the MCPServer reports `ResolvedRefs=False OptionalKeyUnused`,
  naming the objects that write the literals, since Crewlet refuses it. A
  change to a Seat or a Unit can make it so: deleting the only Seat that
  gives the key from a Secret holds the document, the deletion included,
  until the entry is removed or another value is.
- **A Seat and its Unit spell a header alike.** Crewlet lays a Unit's
  `spec.mcp` under each agent member's own by the name as written, so a
  member's `authorization` beside its Unit's `Authorization` would send
  either value; the Seat reports `Accepted=False Conflict`, and the document
  is held, as Crewlet refuses it.
- **A required value that holds nothing is reported.** Where the key is not
  listed, a value read from a Secret key that holds an empty value does not
  hold the document: the engine applies it and does not start what the value
  is given to. The object that gives the value reports `ResolvedRefs=False
  SecretValueEmpty`, with a Warning event, naming the Secret, the key, the
  variable and what does not start; it says so only of a value the engine
  would start something with, so a server's own value that every seat
  granted the server replaces is not one. A missing Secret or key holds the
  document, as anywhere else (`SecretNotFound`).

A Seat's `spec.sandbox` and a setup step take an `optionalKeys` of their own
for a seat's executor run's environment and files
([A value a run may go without](cli-agents.md#a-value-a-run-may-go-without)).
Crewlet's
[A key that may go without its value](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md#a-key-that-may-go-without-its-value)
has the engine's side.

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

Crewlet's own docs and examples start these servers through a package runner:
`uvx` for a Python server such as `mcp-server-mattermost`, and `npm exec` for
`slack-mcp-server`, a Go binary npm delivers. The runner downloads the server,
and for `uvx` a Python, as it starts. In a pod that does not work: the runner
is not in the image, and in an image that had it, everything it downloaded
would land in `HOME`, an `emptyDir` emptied whenever the pod starts. Every pod
would download its servers again at every start and need egress to PyPI or
npm to start them at all. Even with the server pinned, each start would also
resolve again every dependency the package leaves unbounded, so a pod could
start a different set from the one you tested. Install every server when you
build the image, pinned, and name the installed command.

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
ARG UV_IMAGE=ghcr.io/astral-sh/uv:0.13.1@sha256:4a1f3666816dafadd20ceb71eac9a7d56d7ef30279f7d64048519ab901ada336

FROM ${UV_IMAGE} AS uv

# The two Go servers are downloaded and checked here, with a curl the engine
# image does not ship: only the two binaries leave this stage.
FROM ${ENGINE_IMAGE} AS fetch
ARG TARGETARCH
# GitLab: glab, whose `glab mcp serve` is the server. The digests are the
# release's checksums.txt.
ARG GLAB_VERSION=1.123.0
ARG GLAB_SHA256_AMD64=9c9ac4c2b74ec8709dec4819a4041050f3680c6aaa18f0ed30434081f4fe9bef
ARG GLAB_SHA256_ARM64=1f2d0a885b28d08de341a597b2eb7204ce03d4e67b008efecab9951e4b014f0c
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
ARG RESOLVED_AT=2026-10-19T00:00:00Z
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

check that each server starts in it as the engine's pod would start it: as
uid 10001 on a read-only root, with an empty `HOME` and `/tmp`, no
capabilities and no network, and with no environment but `PATH`, `HOME`
and the values it needs to start, as its `MCPServer` and a seat would give
them. That is stricter than the pod, which also hands a server each variable
on Crewlet's allowlist that the image or the pod sets
([Stdio and HTTP](#stdio-and-http)). And check
that it answers what the engine first asks a server: MCP's `initialize`,
then `tools/list`, which has to name a tool the integration's agents act
with. The client is the image's own `bash`, so your machine needs nothing but
`docker`, and the settings are placeholders, since nothing is reached:

```bash
image=<your registry>/crewlet-tool-servers:<crewlet commit>
pod=(--rm --read-only --network none --user 10001:10001 --cap-drop ALL
  --tmpfs /tmp --tmpfs /home/crewlet:uid=10001,gid=10001 -e HOME=/home/crewlet)
# lists [-q] TOOL [NAME=VALUE...] COMMAND [ARG...]: COMMAND, started with
# PATH, HOME and each NAME=VALUE and nothing else, answers MCP on stdio and
# lists TOOL; with -q, it also writes nothing to its standard error.
lists() {
  docker run -i "${pod[@]}" --entrypoint bash "$image" -s -- "$@" <<'CHECK'
quiet=; if [[ $1 == -q ]]; then quiet=1; shift; fi
tool=$1; shift
mkfifo /tmp/requests /tmp/answers
env -i PATH="$PATH" HOME="$HOME" "$@" </tmp/requests >/tmp/answers 2>/tmp/stderr &
exec 3>/tmp/requests 4</tmp/answers
trap '' PIPE
printf '%s\n' >&3 \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"check","version":"1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
while IFS= read -r -t 60 line <&4 && [[ $line != *'"id":2'* ]]; do :; done
exec 3>&-
wait
if [[ $line != *"\"name\":\"$tool\""* ]]; then
  echo "lists no $tool:" >&2; tail -n 20 /tmp/stderr >&2; exit 1
elif [[ $quiet && -s /tmp/stderr ]]; then
  echo "lists $tool, and wrote:" >&2; cat /tmp/stderr >&2; exit 1
fi
echo "lists $tool"
CHECK
}
lists post_message MATTERMOST_URL=https://chat.example.invalid \
  MATTERMOST_TOKEN=placeholder /usr/local/bin/mcp-server-mattermost
lists jira_add_comment JIRA_URL=https://jira.example.invalid \
  JIRA_USERNAME=check JIRA_API_TOKEN=placeholder /usr/local/bin/mcp-atlassian
lists -q glab_mr_note GLAB_CHECK_UPDATE=false GLAB_SHOW_WHATS_NEW=false \
  GLAB_SEND_TELEMETRY=false /usr/local/bin/glab mcp serve
docker run "${pod[@]}" --entrypoint /usr/local/bin/slack-mcp-server "$image" --help 2>/dev/null &&
  echo "runs slack-mcp-server"
```

A failing check prints the last lines the server wrote to its standard error,
as the engine's `server_stderr_tail` does. Each line hands its server the
values it needs to start, as the `MCPServer` below and a seat would give
them, and `env -i` starts the server with nothing else but `PATH` and
`HOME`: a server that relied on a variable of the image's `ENV` that
Crewlet's allowlist does not name, or on one Docker sets, would pass a check
that handed it the container's environment and fail in the pod. `glab`'s
line is `-q` because `glab` puts whatever a tool's command writes to its
standard error into the agent's answer ([GitLab](#gitlab)), and as
`glab mcp serve` itself stops, it writes to its own standard error what a
command would add to an answer: without `GLAB_CHECK_UPDATE` the update check
that cannot reach gitlab.com, without `GLAB_SHOW_WHATS_NEW`
`What's new in glab`, so the check fails. Without `GLAB_SEND_TELEMETRY` it
writes nothing there: what that switch stops is a request to the GitLab
instance, which a check with no network cannot see. The requests go in
through a pipe kept open until the answer is read: a server that sees the
end of its input stops, and may stop before it answers what it read.
`--help` would prove much less: a Python server parses its arguments before
it imports its own code, so its `--help` succeeds with none of its
dependencies installed. `slack-mcp-server` gets only `--help`, since it
checks its token with Slack as it starts, which needs the network: it shows
that the binary runs in the image, and no more.

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
`glab` 50 MB. A server with `shared: false` runs once for every agent seat
that names it in `spec.mcp`, its own or its unit's, on the pod that holds the
seat, so size the class's
`data.resources` (and `seatPools.defaults.resources`) for those processes.

### One image with a coding CLI

A class names one engine image, so a company that also runs a seat on a
coding CLI needs one image with both. Build one on the other: build the image
of [The image needs the CLI](cli-agents.md#the-image-needs-the-cli) for each
CLI the company runs (Claude Code's, OpenCode's, or one built on the other)
and push it, then build this Dockerfile with `ENGINE_IMAGE` naming it,

```bash
docker build --build-arg ENGINE_IMAGE=<your registry>/crewlet-cli@sha256:<digest> \
  -t <your registry>/crewlet-agents:<crewlet commit> .
```

run the checks above against the result, and [OpenCode's](cli-agents.md#opencode)
too where OpenCode is in it, and name it in the class. Each Dockerfile takes
the image it extends as `ENGINE_IMAGE`, so none is edited. Pasting one into
another does not work: the second file's `ARG`s land inside the first one's
last stage, where the second's `FROM` lines cannot see them, and a build
produces only its last stage, so the paste either fails on a blank base image
or leaves out the first file's additions.

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

The servers below write their settings out as values
([Settings and credentials](#settings-and-credentials)). Each seat's own
credential reaches its instance of a server one of two ways, as
[Connecting Integrations](integrations.md) shows for each vendor: where the
engine provisions the vendor (GitLab or Mattermost under `provisioning:
Engine`, Datadog, an Atlassian organization) the engine hands the server the
credential it minted for the seat, and the Seat names none; everywhere else
the seat gives its own Secret in its `spec.mcp`.

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
      value: https://chat.acme.example
  toolPrefix: mattermost_
```

`MATTERMOST_URL` takes the Mattermost Integration's `url`, the address the
engine reaches the server at, and the same rule holds for it
([Mattermost](integrations.md#mattermost)): Mattermost has to answer its API
there without a redirect, on the scheme the address is served on and at the
Site URL's path. `mcp-server-mattermost` sends every call to that address
with `httpx`, which follows no redirect unless told to, and it never tells
it, so at an address Mattermost redirects no agent can post.

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
      value: https://jira.acme.example
    - name: CONFLUENCE_URL
      value: https://wiki.acme.example
```

One server serves both products, so set the URL of each the company uses: the
engine does not derive them from the Integrations. Each seat gives it its
Atlassian account ([Jira and Confluence](integrations.md#jira-and-confluence);
Crewlet's [Jira](https://github.com/crewlet/crewlet/blob/main/docs/integrations/jira.md#mcp-server-agents-control-jira)).

Behind an egress proxy, `mcp-atlassian` takes the pod's `HTTPS_PROXY`,
which the engine hands it, spelled in capitals: signed in with a personal
access token it reads no `https_proxy`. A proxy for one product alone goes in
the server's `env`, as `JIRA_HTTPS_PROXY` or `CONFLUENCE_HTTPS_PROXY`, and so
does a proxy auto-config file, `ATLASSIAN_PROXY_WPAD_ENABLE` and
`ATLASSIAN_PROXY_WPAD_URL`, which also needs the server installed with its
`wpad` extra (`"mcp-atlassian[wpad]==${MCP_ATLASSIAN_VERSION}"` in the
Dockerfile above): the engine hands it none of those from the pod.
`mcp-atlassian` reads that file only when no `HTTP_PROXY`, `HTTPS_PROXY` or
`SOCKS_PROXY` reaches it, the pod's included, and otherwise ignores it
without a word. So where the pod sets a proxy, give `JIRA_HTTP_PROXY` and
`JIRA_HTTPS_PROXY`, and `CONFLUENCE_HTTP_PROXY` and `CONFLUENCE_HTTPS_PROXY`,
an empty `value` in the server's `env` beside the two settings: it takes a
product's variable, empty too, in place of the pod's, and the engine names
the four in its `empty_env_vars` warning, which here is expected
([its source at 0.23.1](https://github.com/sooperset/mcp-atlassian/blob/v0.23.1/src/mcp_atlassian/utils/proxy.py)).

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
      value: "true"
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
`spec.integrations.slack` names, and a channel cache of its own:

```yaml
spec:                       # Seat lead
  integrations:
    slack:
      enabled: true
      credentialsSecretRef: {name: lead-slack}    # keys botToken, signingSecret
  mcp:
    - name: slack
      env:
        - name: SLACK_MCP_XOXB_TOKEN
          valueFrom:
            secretKeyRef: {name: lead-slack, key: botToken}
        - name: SLACK_MCP_CHANNELS_CACHE          # this seat's own: no other seat names it
          value: /home/crewlet/.cache/slack-mcp-server/lead-channels.json
```

**Each seat names a channel cache of its own.** `slack-mcp-server` keeps the
channels its bot can see, the private channels and direct messages it is in
among them, in a file, and unless `SLACK_MCP_CHANNELS_CACHE` names one it
names that file after the Slack workspace alone,
`<team id>_channels_cache_v2.json` under `$HOME/.cache/slack-mcp-server`, and
reads it back for 24 hours before it asks Slack again
([its source at 1.3.0](https://github.com/korotovsky/slack-mcp-server/blob/v1.3.0/pkg/provider/api.go)).
Every seat's server on one engine pod runs as the engine's user, with one
`HOME`, so the seats whose bots are in one workspace share that file, and
whichever wrote it last decides what `channels_list` tells the others: that
bot's private channels and direct messages, named to seats whose bots are not
in them, and none of their own. So the path is each Seat's own, and never a
Unit's or the server's `env`, which every seat they reach shares. It is a
setting, not a credential, so it is a `value`
([Settings and credentials](#settings-and-credentials)). The server makes
`$HOME/.cache/slack-mcp-server` itself as it starts, for the other cache it
keeps there, so a file in it is written; `HOME` is emptied when the pod
starts, so a new pod's server asks Slack again, for its own bot alone.

**Behind an egress proxy, give it `SLACK_MCP_PROXY`.** `slack-mcp-server`
reads no `HTTPS_PROXY`: it makes its own connections, through the proxy
`SLACK_MCP_PROXY` names and otherwise none, so the pod's proxy, which the
engine hands it, does not reach its calls, and it cannot reach Slack. Set it
in the server's `env`, as a `value` unless the address holds a password. A
CA it trusts beside the system's, such as a proxy's that reads the traffic,
goes in `SLACK_MCP_SERVER_CA`, the path of a file in the image
([its source at 1.3.0](https://github.com/korotovsky/slack-mcp-server/blob/v1.3.0/pkg/transport/transport.go)).

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
  env:
    - name: GLAB_CHECK_UPDATE           # no update notice (or failed check) in an answer
      value: "false"
    - name: GLAB_SHOW_WHATS_NEW         # no "What's new" banner after an upgrade
      value: "false"
    - name: GLAB_SEND_TELEMETRY         # no usage report to the instance per call
      value: "false"
```

Each seat's instance gets the seat's GitLab token: from the engine under
`provisioning: Engine`, and from the Seat's `spec.mcp`, under `GITLAB_TOKEN`,
under `External` ([GitLab](integrations.md#gitlab)). On a self-managed
instance, give the server `GITLAB_HOST` as well, in its `env`,
and, when that instance serves plain HTTP, `GLAB_API_PROTOCOL` with the
value `http` beside it: `glab` takes the API's scheme from that setting,
`https` unless it says otherwise, never from the address in `GITLAB_HOST`.
GitLab marks `glab mcp serve` experimental, liable to change or go away, so
read `glab`'s release notes before you move its pin (Crewlet's
[GitLab](https://github.com/crewlet/crewlet/blob/main/docs/integrations/gitlab.md#mcp-tool-server)).

**`GLAB_CHECK_UPDATE` and `GLAB_SHOW_WHATS_NEW` keep `glab`'s own notices
out of an agent's answers, and `GLAB_SEND_TELEMETRY` keeps its usage reports
off the instance.**
`glab mcp serve` runs every tool call as a `glab` command of its own and
answers with everything that command wrote, its standard error as well as
its output, and after a command `glab` writes two notices to its standard
error unasked. Once a day, by a timestamp it keeps under `HOME`, which a new
pod starts without, it asks gitlab.com for a newer release and writes
`A new version of glab is available`, or, where gitlab.com cannot be reached,
`ERROR: failed checking for glab updates`, once the call has waited out the
connection; `GLAB_CHECK_UPDATE` turns that off. And the first command it runs
in a `HOME` it has not run in, which is every new pod's, and the first after
an upgrade, write `What's new in glab` and `Run: glab whatsnew`, which only
`GLAB_SHOW_WHATS_NEW` turns off. Either lands after the JSON the agent asked
for, so the answer is no longer that JSON. `glab` also reports every command
it runs to the GitLab instance, under the seat's token, and waits up to two
seconds for the instance to answer, so against a slow instance each tool call
takes two seconds longer, and so does the server's stop, which then runs
into the two seconds the engine allows a server before it signals it;
`GLAB_SEND_TELEMETRY` turns that off, and leaving it out lets GitLab's usage
statistics count your agents' commands. The three go in the server's `env`,
which every seat's process starts with: the engine hands a stdio server none
of the image's `ENV` but what Crewlet's allowlist names, and it names no
`GLAB_` variable ([Stdio and HTTP](#stdio-and-http)), so an `ENV` in the
Dockerfile above would reach a shell in the container and no `glab` the
engine starts. Spell each `"false"`: `glab` reads two of them as Go reads a
boolean, and a value it cannot read, such as `"no"` or `"off"`, puts a
warning of its own into every answer.

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

A remote server: nothing goes into the image. The engine sends each seat's
instance an `Authorization` header with the installation token it issues for
the seat's own GitHub App, narrowed to the seat's `permissions` and `repos`
([Slack and GitHub](integrations.md#slack-and-github); Crewlet's
[GitHub](https://github.com/crewlet/crewlet/blob/main/docs/integrations/github.md#mcp-tools)).

### Datadog

Crewlet names no Datadog tool server. The engine hands each seat's instance
of the per-seat `MCPServer` named `datadog` the application key it minted
for the seat, as `DD_APPLICATION_KEY` ([Datadog](integrations.md#datadog)):
Datadog's remote server (`https://mcp.datadoghq.com/v1/mcp` on US1, over
`transport: HTTP`) reads it as a header, beside the organization's API key in
the server's own `DD_API_KEY` header. Name the server `datadog`, give it
`shared: false`, and, for a stdio server, install it in the image as above.

## See also

- [Connecting Integrations](integrations.md) — each vendor's Integration and the seats' credentials for its server
- [Running Seats on a Coding CLI](cli-agents.md) — the other program a derived engine image carries
- [Crewlet's Tools & MCP](https://github.com/crewlet/crewlet/blob/main/docs/guides/tools-and-mcp.md) — shared and per-seat servers, and what a server that fails costs
