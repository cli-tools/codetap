# CodeTap

CodeTap bridges VS Code on the host with containers, VMs, and remote machines. It downloads and runs VS Code Server inside the target environment, exposes the session over a Unix socket in `/dev/shm/codetap/`, and pairs with a companion VS Code extension that discovers sessions automatically and connects with one click.

**The problem it solves:** Getting a full VS Code remote development session into a container typically requires the Dev Containers extension, SSH access, or baking VS Code Server into images. CodeTap eliminates all of that — drop a single static binary into any Linux environment and get a working VS Code connection in seconds.

**Key properties:**

- Single static binary, zero dependencies (no CGO, no libc, stdlib-only Go)
- Two connection modes: shared `/dev/shm` (simplest) or stdio relay (works anywhere)
- Auto-downloads and caches VS Code Server — containers need nothing pre-installed
- Works with Docker, Podman, SSH, kubectl, or any bidirectional stdio transport
- Cross-compiled for Linux amd64 and arm64

## Overview

```
┌─────────────────────┐       /dev/shm/codetap/
│  VS Code (host)     │◄──────  session.sock
│  + CodeTap ext      │         session.json
└─────────────────────┘         session.token
                                    ▲
                                    │ (ipc=host or stdio relay)
                             ┌──────┴──────┐
                             │  Container   │
                             │  codetap run │
                             └──────────────┘
```

## Installation

Download the static binary for your architecture from [Releases](https://github.com/cli-tools/codetap/releases):

```sh
# amd64
curl -Lo /usr/local/bin/codetap https://github.com/cli-tools/codetap/releases/latest/download/codetap-linux-amd64
chmod +x /usr/local/bin/codetap

# arm64
curl -Lo /usr/local/bin/codetap https://github.com/cli-tools/codetap/releases/latest/download/codetap-linux-arm64
chmod +x /usr/local/bin/codetap
```

Install the VS Code extension from the `.vsix` file in Releases, or build it from source (see below).

## Usage

### Mode 1: Shared /dev/shm (--ipc=host)

The simplest mode. The container shares `/dev/shm` with the host, so VS Code can see the socket directly.

```sh
# Start a container with shared IPC
docker run --ipc=host -v /usr/local/bin/codetap:/usr/local/bin/codetap:ro \
  -it myimage bash

# Inside the container (auto-resolves latest VS Code Server)
codetap run --name myproject --folder /workspace
```

The VS Code extension discovers the session in `/dev/shm/codetap/` and offers to connect.

### Mode 2: Stdio relay (no --ipc=host needed)

For containers that can't share IPC, the stdio relay multiplexes VS Code Server traffic over stdin/stdout. No shared memory required.

```sh
# On the host — codetap creates the /dev/shm socket and spawns the remote command
codetap relay --name myproject -- \
  docker exec -i mycontainer \
  codetap run --stdio
```

This also works over SSH:

```sh
codetap relay --name remote-dev -- \
  ssh user@host \
  codetap run --stdio
```

Or with any transport that provides bidirectional stdin/stdout:

```sh
codetap relay --name k8s-pod -- \
  kubectl exec -i mypod -- \
  codetap run --stdio
```

### Listing sessions

```sh
codetap list
# NAME        COMMIT        FOLDER       PID    STATUS   STARTED
# myproject   abc123def456  /workspace   1234   alive    2024-01-15 10:30:00
```

### Cleaning stale sessions

```sh
codetap clean
```

Removes metadata for sessions whose sockets are no longer alive (e.g., after a container exit without graceful shutdown).

## Commands

| Command | Description |
|---------|-------------|
| `codetap` | Print help (also: `codetap help`, `codetap --help`) |
| `codetap run` | Start VS Code Server on a socket in /dev/shm/codetap/ |
| `codetap run --stdio` | Start VS Code Server and relay over stdin/stdout |
| `codetap list` | List all discovered sessions |
| `codetap clean` | Remove stale (dead) session entries |
| `codetap relay` | Host-side relay: creates /dev/shm socket and spawns remote command |

Running with no subcommand prints help. Passing flags without a subcommand defaults to `run` (e.g. `codetap --commit abc123`).

### Run flags

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `--name` | | hostname | Session name |
| `--commit` | `CODETAP_COMMIT` | auto-resolved | VS Code Server version, commit hash, or `latest` |
| `--folder` | | cwd | Workspace folder path |
| `--socket-dir` | `CODETAP_SOCKET_DIR` | `/dev/shm/codetap` | Socket directory |
| `--stdio` | | false | Use stdin/stdout relay mode |

### Relay flags

| Flag | Default | Description |
|------|---------|-------------|
| `--name` | hostname | Session name |
| `--commit` | | Commit hash for metadata |
| `--folder` | cwd | Workspace folder for metadata |
| `--socket-dir` | `/dev/shm/codetap` | Socket directory |

## Commit resolution

CodeTap automatically determines which VS Code Server version to download. The resolution order is:

1. `--commit` flag (hash like `abc123...`, version like `1.109.5`, or `latest`)
2. `CODETAP_COMMIT` environment variable
3. `~/.codetap/.commit` file
4. Local `code --version` output (if the VS Code CLI is on PATH)
5. Latest stable release from the Microsoft Update API

Running bare `codetap run` with network access downloads the latest stable server. To run offline, provide a commit via any of the first three methods.

## Storage

| Path | Purpose |
|------|---------|
| `~/.codetap/cache/` | Downloaded VS Code Server tarballs |
| `~/.codetap/repository/` | Extracted VS Code Server binaries |
| `~/.codetap/.commit` | Default commit hash |
| `/dev/shm/codetap/` | Runtime sockets, metadata, and tokens |

## VS Code Extension

The companion TypeScript extension (`extension/`) turns VS Code into a CodeTap client. It registers a `codetap` remote authority, polls the socket directory for `.json` metadata files, and presents discovered sessions in a sidebar tree view with live/dead status indicators. Connecting opens the remote folder over the Unix socket using VS Code's managed message-passing protocol — no port forwarding or SSH required.

> Note: `codetap` uses the VS Code resolver API proposal.
> One-time setup on stable VS Code:
> 1. Run `Preferences: Configure Runtime Arguments`.
> 2. Add this to `argv.json`:
>    ```json
>    {
>      "enable-proposed-api": ["codetap.codetap"]
>    }
>    ```
> 3. Restart VS Code.
> Flatpak `argv.json` path:
> `~/.var/app/com.visualstudio.code/config/Code/argv.json`

**Commands:** `codetap.connect` (open session), `codetap.refresh` (re-scan), `codetap.copyToken` (copy auth token to clipboard).

| Setting | Default | Description |
|---------|---------|-------------|
| `codetap.socketDir` | `/dev/shm/codetap` | Directory to scan for sessions |
| `codetap.pollInterval` | `3000` | Polling interval in milliseconds |

Build: `cd extension && npm ci && npm run compile && npm run package`

## Replacing devcontainers with Docker Compose + CodeTap

VS Code Dev Containers couple your container lifecycle to the IDE. Docker Compose + CodeTap gives you the same remote-development experience with plain Docker tooling: no Dev Containers extension, no `devcontainer.json`, and no IDE lock-in.

Use one of these two modes depending on whether shared IPC is allowed.

### Mode A: Shared IPC (`ipc: host`) — simplest

If your environment allows shared IPC, this is the cleanest workflow. Run CodeTap directly in the container with a fixed `--name`, and the host extension discovers it immediately.

Assumption: `codetap` is available in `PATH` inside the container.

```yaml
services:
  dev:
    image: registry.example.com/myteam/devimage:latest
    container_name: dev
    ipc: host
    volumes:
      - ..:/workspaces
    working_dir: /workspaces/myproject
    command: codetap run --name dev --folder /workspaces/myproject
```

```sh
docker compose up -d
# No relay command needed in shared IPC mode.
# VS Code discovers session name "dev" from /dev/shm/codetap on the host.
```

### Mode B: Isolated IPC (no `ipc: host`) — relay over stdio

When shared IPC is not available, keep the container running normally and bridge with `codetap relay` + `docker compose exec -T`.

Assumption: `codetap` is available in `PATH` on both host and container.
The host discoverable name comes from `codetap relay --name ...`. Folder/commit metadata are synced from the in-container `codetap run --stdio` process.
If host and container run different CodeTap versions and metadata sync is unavailable, pass `--folder` to `codetap relay` as a fallback.

```yaml
services:
  dev:
    image: registry.example.com/myteam/devimage:latest
    container_name: dev
    volumes:
      - ..:/workspaces
    working_dir: /workspaces/myproject
    command: sleep infinity
```

```sh
docker compose up -d

codetap relay --name dev -- \
  docker compose exec -T dev \
  codetap run --stdio --name dev --folder /workspaces/myproject
```

In this mode, the host-side `codetap relay` creates `/dev/shm/codetap/dev.sock` and forwards traffic to the in-container stdio session.

### Comparison with devcontainers

| | Dev Containers | Compose + CodeTap |
|---|---|---|
| IDE dependency | VS Code only | Any editor (VS Code via extension, others via socket) |
| Config files | `devcontainer.json` + Dockerfile | `compose.yaml` |
| Container lifecycle | Managed by IDE | Managed by `docker compose` |
| Shared IPC environments | N/A | Direct `codetap run` (no relay) |
| Isolated IPC environments | N/A | `codetap relay` over `docker compose exec -T` |
| Multi-service | Awkward | Native Compose workflow |
| CI reuse | Requires special tooling | Same `compose.yaml` works in CI |

## Building from source

### Prerequisites

- Go 1.22+ (at `/opt/go` or in PATH)
- Node.js 20+ and npm (for the extension)

### Go binary

```sh
cd codetap
make build        # Build for current platform
make build-all    # Cross-compile for amd64 + arm64
make test         # Run all tests
```

### VS Code extension

```sh
cd codetap/extension
npm ci
npm run compile
npm run package   # Creates codetap.vsix
```

## License

BSD Zero Clause (0BSD) — see [LICENSE](LICENSE).
