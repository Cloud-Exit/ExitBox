```
  _____      _ _   ____
 | ____|_  _(_) |_| __ )  _____  __
 |  _| \ \/ / | __|  _ \ / _ \ \/ /
 | |___ >  <| | |_| |_) | (_) >  <
 |_____/_/\_\_|\__|____/ \___/_/\_\
              By Cloud Exit / https://cloud-exit.com
```

# ExitBox

[![CI](https://github.com/Cloud-Exit/ExitBox/actions/workflows/test.yml/badge.svg)](https://github.com/Cloud-Exit/ExitBox/actions/workflows/test.yml)
[![Coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/Cloud-Exit/ExitBox/main/.github/badges/coverage.json)](https://github.com/Cloud-Exit/ExitBox/actions/workflows/test.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/cloud-exit/exitbox)](https://goreportcard.com/report/github.com/cloud-exit/exitbox)
[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Release](https://img.shields.io/github/v/release/Cloud-Exit/ExitBox)](https://github.com/Cloud-Exit/ExitBox/releases/latest)

**Multi-Agent Container Sandbox** by [Cloud Exit](https://cloud-exit.com)

Run AI coding assistants (Claude, Codex, OpenCode) in isolated containers with defense-in-depth security.


## Getting Started

Install ExitBox and run the interactive setup wizard to configure your environment:

```bash
# Install (Linux/macOS)
curl -fsSL https://raw.githubusercontent.com/cloud-exit/exitbox/main/scripts/install.sh | sh

# Run the setup wizard
exitbox setup
```

The setup wizard guides you through:
1. **Developer role** — Frontend, Backend, Fullstack, DevOps, Data Science, Mobile, Embedded, or Security
2. **Languages** — Pre-selected based on your role, customize as needed
3. **Tool categories** — Build tools, databases, networking, DevOps, security, and more
4. **Extra packages** — Search and add Alpine packages beyond the tool categories
5. **Workspace name** — Named context for isolated agent configs (e.g. personal, work)
6. **Credentials** — Import credentials from host or copy from an existing workspace
7. **Agents** — Choose which AI assistants to enable (Claude, Codex, OpenCode)
8. **Settings** — Auto-update, status bar, default workspace, firewall, auto-resume, env passing, read-only
9. **Firewall** — Edit the domain allowlist categories interactively
10. **Review** — Confirm your selections before saving

The wizard generates a tailored `config.yaml` and `allowlist.yaml` with your preferences. You can re-run it at any time with `exitbox setup`.

## Features

### Security

The project's security posture is rated **High / Robust**, employing a "Defense in Depth" strategy:

1.  **DNS Isolation (The "Moat")**: Containers cannot resolve external domain names directly. This forces all traffic through the proxy, as the container "knows" nothing of the outside internet.
2.  **Mandatory Proxy Usage**: Since direct DNS fails, tools are forced to use the configured `http_proxy`. Bypassing these variables results in immediate connection failure.
3.  **Proxy Access Control**: The Squid proxy actively inspects destinations, enforcing a strict allow/deny policy (returning `403 Forbidden` for blocked domains).
4.  **Capability Restrictions**: `CAP_NET_RAW` and other capabilities are dropped, preventing raw socket creation and network enumeration attacks (e.g., `ping` is disabled).

**Core Features:**
- **Rootless Containers**: Runs without host root privileges using Podman's user namespaces
- **Alpine Base Image**: Minimal Alpine Linux base (~5 MB) with a managed tool list
- **Supply-Chain Hardened Installs**: Claude Code is installed via direct binary download with SHA-256 checksum verification against Anthropic's signed manifest — no `curl | bash`
- **Squid Proxy Firewall**: Proxy-based destination filtering with explicit allowlist rules
- **Hard Egress Isolation**: Agent containers run on an internal-only network and can exit only via Squid
- **No Privilege Escalation**: `--security-opt=no-new-privileges:true` enforced
- **Capability Dropping**: `--cap-drop=ALL` removes all Linux capabilities
- **Resource Limits**: Default 8GB RAM / 4 CPUs to prevent DoS
- **Secure Defaults**: No automatic mounting of sensitive SSH keys or AWS credentials

### Sandbox-Aware Agents

ExitBox automatically injects sandbox instructions into each agent on container start. This tells the agent it is running inside a restricted container so it won't attempt actions that can't work (e.g., running `docker`, `podman`, or managing infrastructure).

Instructions are written to each agent's native global instructions file:

| Agent    | Instructions file                        |
|:---------|:-----------------------------------------|
| Claude   | `~/.claude/CLAUDE.md`                    |
| Codex    | `~/.codex/AGENTS.md`                     |
| OpenCode | `~/.config/opencode/AGENTS.md`           |

If the file already exists (e.g., from your own global instructions), ExitBox appends the sandbox notice once. The instructions inform the agent about network restrictions, dropped capabilities, and the read-only nature of the environment so it can focus on writing and debugging code within `/workspace`.

### Containers
- **Podman-First**: Optimized for Podman (rootless, daemonless) with Docker fallback
- **Multi-Agent Support**: Run Claude Code, OpenAI Codex, or OpenCode
- **Project Isolation**: Each project gets its own containerized environment
- **Development Profiles**: Pre-configured environments for Rust, Python, Go, Node.js, and more
- **Custom Tools**: Add Alpine packages to any image via `-t` flag or `config.yaml`

### Named Resumable Sessions

When agents like Claude Code and Codex exit, they display a resume token (e.g. `claude --resume <id>`). ExitBox captures this token and can pass it on the next run, so you seamlessly resume where you left off.

- **Named sessions** — set a session name explicitly with `--name "<session>"`; if omitted, ExitBox auto-generates one using local time (`YYYY-MM-DD HH:MM:SS`)
- **Named session resume** — when `--name "<session>"` is provided, ExitBox resumes that named session automatically if it exists (use `--no-resume` to force fresh)
- **Disabled by default** — enable via "Auto-resume sessions" in `exitbox setup` or set `auto_resume: true` in `config.yaml`
- **Always shown at exit** — ExitBox always prints a resume command after a session ends (e.g. `exitbox run codex --name "2026-02-11 14:51:02" --resume`)
- **Workspace-aware** — resume commands include `--workspace` when running a non-default workspace
- **Disable per-session** with `--no-resume` to start a fresh session
- Resume tokens are stored per-workspace, per-agent, per-project, and per-session at `~/.config/exitbox/profiles/global/<workspace>/<agent>/projects/<project_key>/sessions/<session_key>/.resume-token`

### Usability
- **Cross-Platform**: Native binaries for Linux, macOS, and Windows
- **Setup Wizard**: Interactive TUI that configures your environment based on your developer role
- **YAML Config**: Clean, readable configuration with `config.yaml` and `allowlist.yaml`
- **Config Import**: All platforms use managed config (import-only). Use `exitbox import <agent>` to seed host config. Use `exitbox import <agent> --workspace <name>` to import into a specific workspace.
- **Simple Commands**: Just run `exitbox run claude` to get started
- **Shell Completion**: Tab-completion for bash, zsh, and fish via `exitbox completion`
- **CLI Shorthands**: All flags have single-letter aliases (`-f`, `-r`, `-t`, `-a`, etc.)
- **Session Allow-URLs**: Temporarily allow extra domains with `-a` — no config file edits, no restarts
- **Runtime Domain Requests**: Agents can request domain access at runtime via `exitbox-allow` inside the container

## Supported Agents

| Agent       | Description                  | Host Requirement |
|:------------|:-----------------------------|:-----------------|
| `claude`    | Anthropic's Claude Code CLI  | None (installed in container) |
| `codex`     | OpenAI's Codex CLI           | None (downloaded)|
| `opencode`  | OpenCode AI assistant        | None (binary download)  |

All agents are installed inside the container. Existing host config (`~/.claude`, etc.) is imported once into managed storage on first run. Use `exitbox import <agent>` (or `exitbox import all`) to re-seed from host config. Use `--workspace` to target a specific workspace.

## Installation

### Prerequisites

- **Podman** (recommended) or **Docker** — at least one is required; Podman is preferred for its rootless, daemonless design
- For Windows: **Docker Desktop** provides the Docker CLI that ExitBox uses

### Quick Install (Linux / macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/cloud-exit/exitbox/main/scripts/install.sh | sh
```

This downloads the latest release binary, verifies its SHA-256 checksum, and installs to `~/.local/bin/`.

### Linux

```bash
# Install Podman (recommended) or Docker
sudo apt update && sudo apt install -y podman   # Ubuntu/Debian
# OR: install Docker - see https://docs.docker.com/engine/install/

# Install exitbox
curl -fsSL https://raw.githubusercontent.com/cloud-exit/exitbox/main/scripts/install.sh | sh

# Run the setup wizard
exitbox setup

# Run an agent
exitbox run claude
```

### macOS

```bash
# Install Podman (recommended) or Docker
brew install podman
podman machine init && podman machine start
# OR: brew install --cask docker

# Install exitbox
curl -fsSL https://raw.githubusercontent.com/cloud-exit/exitbox/main/scripts/install.sh | sh

# Run the setup wizard
exitbox setup
```

### Windows

ExitBox runs natively on Windows with Docker Desktop.

1. Install [Docker Desktop for Windows](https://docs.docker.com/desktop/install/windows-install/)
2. Download the latest `exitbox-windows-amd64.exe` from [Releases](https://github.com/cloud-exit/exitbox/releases)
3. Rename to `exitbox.exe` and place in a directory on your `PATH` (e.g., `C:\Users\<you>\AppData\Local\bin\`)
4. Run the setup wizard:

```powershell
exitbox setup
```

### Windows (WSL2)

Alternatively, use ExitBox inside WSL2 for a Linux-native experience:

```powershell
# In PowerShell as Administrator
wsl --install -d Ubuntu
```

Then in WSL2:
```bash
sudo apt update && sudo apt install -y podman
curl -fsSL https://raw.githubusercontent.com/cloud-exit/exitbox/main/scripts/install.sh | sh
exitbox setup
```

### Build from Source

```bash
git clone https://github.com/Cloud-Exit/exitbox.git
cd exitbox
make build       # builds ./exitbox
make install     # installs to ~/.local/bin/exitbox
```

## Quick Start

```bash
# Run the setup wizard (first time)
exitbox setup

# Navigate to your project
cd /path/to/your/project

# Run an agent (builds image automatically on first run)
exitbox run claude

# Or run other agents
exitbox run codex
exitbox run opencode
```

ExitBox automatically:
- Builds the container image if needed
- Imports your existing config (`~/.claude`, `~/.codex`, etc.) on first run
- Mounts your project directory
- Sets up the network firewall (Squid proxy)

## Commands

### Setup

```bash
exitbox setup             # Run the interactive setup wizard (recommended first step)
```

### Running Agents

```bash
exitbox run claude [args]     # Run Claude Code
exitbox run codex [args]      # Run Codex
exitbox run opencode [args]   # Run OpenCode
```

### Management

```bash
exitbox list              # List available agents and build status
exitbox enable <agent>    # Enable an agent
exitbox disable <agent>   # Disable an agent
exitbox rebuild <agent>   # Force rebuild of agent image
exitbox rebuild all       # Rebuild all enabled agents
exitbox uninstall <agent> # Remove agent images and config
exitbox aliases           # Print shell aliases for ~/.bashrc
exitbox info              # Show system information
```

### Workspace Management

Workspaces are named contexts (e.g. `personal`, `work`, `client-a`) that provide isolated agent configurations, credentials, and development stacks. Each workspace stores its own agent config directories, so API keys and conversation history are kept separate.

```bash
exitbox workspaces list                    # List all workspaces
exitbox workspaces add <name>              # Create a new workspace (interactive)
exitbox workspaces remove <name>           # Delete a workspace
exitbox workspaces use <name>              # Set the active workspace
exitbox workspaces default [name]          # Get or set the default workspace
exitbox workspaces status                  # Show workspace resolution chain
```

### Session Management

Named sessions are stored per-project. You can list and remove them from the CLI:

```bash
exitbox sessions list                          # List saved sessions for current project/workspace
exitbox sessions list --agent codex            # Filter by agent
exitbox sessions list -w work                  # Inspect another workspace
exitbox sessions rm "2026-02-11 14:51:02"      # Remove one named session
exitbox sessions rm "my-session" --agent claude # Remove for a specific agent only
```

Shell completion:
- `exitbox sessions rm <Tab>` suggests saved session names for the current project
- `exitbox run <agent> --resume <Tab>` suggests saved session names for that agent

#### How Workspaces Work

- **Isolated credentials**: Each workspace has its own agent config directory at `~/.config/exitbox/profiles/global/<workspace>/<agent>/`. API keys, auth tokens, and conversation history are not shared between workspaces.
- **Development stacks**: Each workspace can have its own set of development profiles (languages/tools). The setup wizard or `exitbox workspaces add` lets you pick the stack for each workspace.
- **Per-project auto-detection**: Workspaces can be scoped to a directory. When you run an agent from that directory, ExitBox automatically uses the matching workspace.
- **Default workspace**: Set via `exitbox setup` or `exitbox workspaces default`. Used when no directory-scoped workspace matches.
- **Credential import**: When creating a workspace, you can import credentials from the host or copy them from an existing workspace. You can also import later with `exitbox import <agent> --workspace <name>`.

#### Workspace Resolution Order

1. **CLI flag**: `exitbox run -w work claude` — explicit override for this session
2. **Directory-scoped**: If the current directory matches a workspace's `directory` field in `config.yaml`
3. **Default workspace**: The workspace set as default in settings
4. **Active workspace**: The last-used workspace from `config.yaml`
5. **Fallback**: `default`

#### In-Container Menus

Inside a running agent session, ExitBox provides two dedicated `fzf` menus:

- **Ctrl+Alt+P** — workspace menu (save current session, then switch workspace)
- **Ctrl+Alt+S** — session menu (switch to another named session or start a new timestamp session)

**Note**: Credentials are bind-mounted at container start for security isolation. If you switch to a workspace that wasn't mounted, ExitBox warns you that credentials for that workspace aren't available and suggests re-running with the `--workspace` flag.

#### Workspace Examples

```bash
# Create workspaces for different contexts
exitbox workspaces add work
exitbox workspaces add personal

# Run claude in a specific workspace
exitbox run -w work claude
exitbox run -w personal claude

# Set the default workspace
exitbox workspaces default work

# Now "exitbox run claude" uses the "work" workspace by default
exitbox run claude
```

#### Shell Aliases

Generate recommended shell aliases for quick access:

```bash
exitbox aliases
```

Or add custom aliases to your `~/.bashrc` or `~/.zshrc`:

```bash
alias claude-work="exitbox run -w work claude"
alias claude-personal="exitbox run -w personal claude"
alias codex-work="exitbox run -w work codex"
```

### Utilities

```bash
exitbox info              # Show system information
exitbox logs <agent>      # Show latest agent log file
exitbox clean             # Clean unused container resources
exitbox clean all         # Remove all exitbox images
exitbox projects          # List known projects
```

### Shell Completion

ExitBox provides tab-completion for bash, zsh, and fish:

```bash
# Zsh (add to ~/.zshrc)
eval "$(exitbox completion zsh)"

# Bash (add to ~/.bashrc)
eval "$(exitbox completion bash)"

# Fish
exitbox completion fish > ~/.config/fish/completions/exitbox.fish
```

For faster shell startup, generate a file instead of using `eval`:

```bash
# Zsh
exitbox completion zsh > ~/.zfunc/_exitbox
# then add to ~/.zshrc (before compinit): fpath=(~/.zfunc $fpath)

# Bash
exitbox completion bash > ~/.local/share/bash-completion/completions/exitbox
```

### Options

```bash
exitbox run -f claude              # Disable network firewall *DANGEROUS*
exitbox run -r claude              # Mount workspace as read-only (safety)
exitbox run -v claude              # Enable verbose output
exitbox run -n claude              # Don't pass host environment variables
exitbox run -n -e MY_KEY=val claude  # Only pass specific env vars
exitbox run -i /tmp/foo claude     # Mount /tmp/foo into /workspace/foo
exitbox run -t nodejs,go claude    # Add Alpine packages to image (persisted)
exitbox run -a api.example.com claude  # Allow extra domains for this session
exitbox run -u claude              # Check for and apply agent updates
exitbox run --no-resume claude     # Start a fresh session (don't resume previous)
exitbox run --name "my-session" claude   # No --resume needed; resumes if session exists
exitbox run --resume "my-session" claude # Resume by named session (or by session id)
exitbox run -w work claude         # Use a specific workspace for this session
```

All flags have long forms: `-f`/`--no-firewall`, `-r`/`--read-only`, `-v`/`--verbose`, `-n`/`--no-env`, `--resume [SESSION|TOKEN]`, `--no-resume`, `--name`, `-i`/`--include-dir`, `-t`/`--tools`, `-a`/`--allow-urls`, `-u`/`--update`, `-w`/`--workspace`.

## Available Profiles

Profiles are pre-configured development environments. The setup wizard suggests profiles based on your developer role, or you can add them manually.

| Profile       | Description                              |
|:--------------|:-----------------------------------------|
| `base`        | Base development tools                   |
| `build-tools` | Build toolchain helpers                  |
| `shell`       | Shell and file transfer utilities        |
| `networking`  | Network diagnostics and tooling          |
| `c`           | C/C++ toolchain (gcc, make, cmake)       |
| `node`        | Node.js runtime with npm and JS tooling  |
| `python`      | Python 3 with pip                        |
| `rust`        | Rust toolchain with cargo                |
| `go`          | Go runtime (arch-aware, checksum verified) |
| `java`        | OpenJDK with Maven and Gradle            |
| `ruby`        | Ruby with bundler                        |
| `php`         | PHP with composer                        |
| `database`    | Database CLI clients                     |
| `devops`      | Docker CLI / kubectl / helm / opentofu / kind |
| `web`         | Web server/testing tools                 |
| `security`    | Security diagnostics tools               |
| `flutter`     | Flutter SDK                              |

## Configuration

ExitBox uses YAML configuration files stored in `~/.config/exitbox/` (Linux/macOS) or `%APPDATA%\exitbox\` (Windows).

### Setup Wizard

The recommended way to configure ExitBox is through the setup wizard:

```bash
exitbox setup
```

The wizard generates `config.yaml` and `allowlist.yaml` tailored to your developer role. It walks you through up to 10 steps: roles, languages, tools, packages, workspace name, credentials, agents, settings, firewall (domain allowlist), and review. Some steps are shown conditionally (e.g., credentials only when other workspaces exist). Re-run it at any time to reconfigure.

### config.yaml

The main configuration file controls which agents are enabled, extra packages, and default settings:

```yaml
version: 1
roles:
  - backend
  - devops

workspaces:
  active: default
  items:
    - name: default
      development:
        - go
        - python
    - name: work
      development:
        - node
        - python

agents:
  claude:
    enabled: true
  codex:
    enabled: false
  opencode:
    enabled: true

tools:
  user:
    - postgresql-client
    - redis

settings:
  auto_update: false
  status_bar: true            # Show "ExitBox <version> - <agent>" bar at top of terminal
  default_workspace: default  # Workspace used when no directory match is found
  default_flags:
    no_firewall: false        # Set true to disable firewall by default
    read_only: false          # Set true to mount workspace as read-only by default
    no_env: false             # Set true to not pass host env vars by default
    auto_resume: false        # Set true to auto-resume agent sessions
```

**Settings reference:**
- `status_bar` — Thin status bar at the top of the terminal showing agent, workspace, and version. Enabled by default.
- `auto_resume` — Automatically resume the last agent conversation on next run. Disabled by default. Enable in `exitbox setup` or set to `true`. Disable per-session with `--no-resume`.

### allowlist.yaml

The network allowlist is organized by category for readability:

```yaml
version: 1
ai_providers:
  - anthropic.com
  - claude.ai
  - openai.com
  # ...

development:
  - github.com
  - npmjs.org
  - pypi.org
  # ...

cloud_services:
  - googleapis.com
  - amazonaws.com
  - azure.com

custom:
  - mycompany.com
```

### Custom Tools

Add extra Alpine packages to your container images:

1. **CLI flag** (persisted automatically):
   ```bash
   exitbox run -t nodejs,python3-dev claude
   ```

2. **config.yaml**:
   ```yaml
   tools:
     user:
       - nodejs
       - python3-dev
   ```

The image rebuilds automatically when tools change.

### Resource Limits

ExitBox enforces default resource limits to prevent runaway agents:
- **Memory**: 8GB
- **CPU**: 4 vCPUs

### What Gets Mounted

ExitBox uses **managed config** (import-only) with per-workspace isolation. On first run, host config is copied into the active workspace's managed directory. Host originals are never modified. Use `exitbox import <agent>` to re-seed from host config at any time, optionally with `--workspace <name>` to target a specific workspace.

All managed paths follow the pattern `~/.config/exitbox/profiles/global/<workspace>/<agent>/`. For example, with workspace `default` and agent `claude`:

| Agent    | Managed Path (under workspace agent dir)    | Container Path                    |
|:---------|:--------------------------------------------|:----------------------------------|
| Claude   | `.claude/`                                   | `/home/user/.claude`              |
| Claude   | `.claude.json`                               | `/home/user/.claude.json`         |
| Claude   | `.config/`                                   | `/home/user/.config`              |
| Codex    | `.codex/`                                    | `/home/user/.codex`               |
| Codex    | `.config/codex/`                             | `/home/user/.config/codex`        |
| OpenCode | `.opencode/`                                 | `/home/user/.opencode`            |
| OpenCode | `.config/opencode/`                          | `/home/user/.config/opencode`     |
| OpenCode | `.local/share/opencode/`                     | `/home/user/.local/share/opencode` |
| OpenCode | `.local/state/`                              | `/home/user/.local/state`         |
| OpenCode | `.cache/opencode/`                           | `/home/user/.cache/opencode`      |

Your project directory is mounted at `/workspace`.

When Codex is enabled, ExitBox publishes callback port `1455` on the shared `exitbox-squid` container and relays it to the active Codex container, so OrbStack/private-networking callback flows work reliably.

**Note:** SSH keys (`~/.ssh`) and AWS credentials (`~/.aws`) are **NOT** mounted by default for security.

### Environment Variables

| Variable              | Description                          |
|:----------------------|:-------------------------------------|
| `VERBOSE`             | Enable verbose output                |
| `CONTAINER_RUNTIME`   | Force runtime (`podman` or `docker`) |
| `EXITBOX_NO_FIREWALL`| Disable firewall (`true`)            |
| `EXITBOX_SQUID_DNS`  | Squid DNS servers (comma/space list, default: `1.1.1.1,8.8.8.8`) |
| `EXITBOX_SQUID_DNS_SEARCH` | Squid DNS search domains (default: `.` to disable inherited search suffixes) |

## Architecture

### Alpine Base Image

All agent images are built on **Alpine Linux**. The base package list is embedded in the binary and shared by all image builds.

Alpine was chosen for:
- **Small image size**: ~5 MB base vs ~80 MB for Debian slim
- **musl libc**: Matches the native binaries shipped by Claude Code, git-delta, and yq
- **Consistent package manager**: `apk` is used everywhere — base image, profiles, and user tools

### 3-Layer Image Hierarchy

```
base image (Alpine + tools)
  └── core image (agent-specific install)
        └── project image (development profiles layered on)
```

Each layer uses label-based caching (`exitbox.version`, `exitbox.agent.version`, `exitbox.tools.hash`, `exitbox.profiles.hash`) so rebuilds are fast and incremental.

### Supply-Chain Hardened Agent Installs

Claude Code is installed via **direct binary download with SHA-256 checksum verification** against Anthropic's signed manifest — no `curl | bash`. The download URL is auto-discovered from the official installer if the hardcoded endpoint ever changes. The build aborts on any checksum mismatch.

## Network Firewall

ExitBox uses a **Squid Proxy** container to enforce strict destination allowlisting:

1. **Hard egress control**: Agent containers run on an internal-only network with no direct internet route.
2. **Proxy path**: Squid is dual-homed (internal + egress networks), so outbound traffic must traverse Squid.
3. **Allowlist**: Only destinations listed in `allowlist.yaml` are permitted through the proxy.
4. **Fail closed**: Missing or empty allowlist blocks all outbound destinations.

### Configuring the Allowlist

Edit `~/.config/exitbox/allowlist.yaml` and add domains to the `custom` list:

```yaml
custom:
  - mycompany.com
  - api.internal.example.com
```

Domain formats:
- `example.com` allows `example.com` and its subdomains
- `api.example.com` allows only that host scope (and deeper subdomains)
- `*.example.com` is accepted as wildcard syntax
- `8.8.8.8` allows a specific IPv4 destination
- `2606:4700:4700::1111` allows a specific IPv6 destination

### Temporary Domain Access

Allow extra domains for a single session without editing the allowlist:

```bash
exitbox run -a api.example.com,cdn.example.com claude
```

The domains are merged into the Squid config and applied via **hot-reload** (`squid -k reconfigure`) — no proxy restart, no container restart, no connection drop. These domains do not persist across sessions.

### Runtime Domain Requests

When an agent needs access to a domain not in the allowlist, it (or the user) can request it at runtime from inside the container:

```bash
exitbox-allow registry.npmjs.org
```

This connects to the host via a Unix socket IPC channel. The host user is prompted on their terminal to approve or deny the request. Approved domains are added to the Squid config and hot-reloaded immediately — no container restart needed.

- Requires firewall mode (not available with `--no-firewall`)
- The host prompt appears on `/dev/tty`, so it works even while the agent is running
- Agents are informed about `exitbox-allow` via the sandbox instructions injected at container start

### Disabling the Firewall

```bash
exitbox run --no-firewall claude   # *DANGEROUS* - disables all network restrictions
```

## Why Podman?

| Feature              | Podman           | Docker               |
|:---------------------|:-----------------|:---------------------|
| Rootless by default  | Yes              | No (requires group)  |
| Daemonless           | Yes              | No (requires daemon) |
| Security             | Better isolation | Requires daemon root |

On Windows, Docker Desktop is the primary supported runtime.

## Troubleshooting

### Podman: "cannot find UID/GID for user"

```bash
sudo usermod --add-subuids 100000-165535 --add-subgids 100000-165535 $USER
podman system migrate
```

### Podman on macOS: "Cannot connect to Podman"

```bash
podman machine start
```

### Docker: Permission Denied

```bash
sudo usermod -aG docker $USER
newgrp docker
```

### Windows: Docker Desktop not detected

Ensure Docker Desktop is running and the `docker` CLI is on your `PATH`. You can verify with:

```powershell
docker info
```

### Re-running the Setup Wizard

If your needs change or you want to reconfigure:

```bash
exitbox setup
```

## Uninstallation

```bash
# Remove the binary
rm -f ~/.local/bin/exitbox

# Remove configuration and data
rm -rf ~/.config/exitbox ~/.cache/exitbox ~/.local/share/exitbox

# Remove container images
podman images | grep exitbox | awk '{print $3}' | xargs podman rmi -f
# OR for Docker:
docker images | grep exitbox | awk '{print $3}' | xargs docker rmi -f
```

On Windows, delete `exitbox.exe` and remove `%APPDATA%\exitbox\`.

## License

AGPL-3.0 - see [LICENSE](LICENSE) file.

ExitBox is open-source software licensed under the GNU Affero General Public License v3.0. Commercial licensing is available from [Cloud Exit](https://cloud-exit.com) for organizations that require proprietary usage terms.

## Contributing

Contributions welcome via pull requests and issues.
