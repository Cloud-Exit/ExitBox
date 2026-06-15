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

Run AI coding assistants (Claude, Codex, Copilot, Cursor, Kimi, OpenCode, Qwen) in isolated containers with defense-in-depth security.

## Getting Started

```bash
# Install (Linux/macOS) — download the latest release binary
# See Installation section below for full instructions
mkdir -p ~/.local/bin
curl -fsSL https://github.com/Cloud-Exit/ExitBox/releases/latest/download/exitbox-$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') -o ~/.local/bin/exitbox
chmod +x ~/.local/bin/exitbox

# Run the setup wizard (first time)
exitbox setup

# Navigate to your project
cd /path/to/your/project

# Run an agent (builds image automatically on first run)
exitbox run claude

# Or run other agents
exitbox run codex
exitbox run copilot
exitbox run cursor
exitbox run kimi
exitbox run opencode
exitbox run qwen
```

ExitBox automatically:
- Builds the container image if needed
- Imports your existing config (`~/.claude`, `~/.codex`, etc.) on first run
- Mounts your project directory
- Sets up the network firewall (Squid proxy)

## Features

- **Rootless Containers** — runs without host root privileges using Podman's user namespaces (Docker fallback supported)
- **Squid Proxy Firewall** — strict domain allowlisting with hard egress isolation; agents can only reach approved destinations
- **Runtime Domain Requests** — agents request access to new domains at runtime via `exitbox-allow`; host user approves via popup
- **Encrypted Vault** — AES-256 + Argon2id encrypted secret storage with per-access approval popups; agents can read and write secrets from inside the container
- **Sandbox-Aware Agents** — automatic instruction injection tells agents about container restrictions, vault usage, and security rules
- **Named Resumable Sessions** — save and resume agent conversations by name across container restarts
- **Multi-Agent Support** — run Claude Code, OpenAI Codex, GitHub Copilot CLI, Cursor CLI, Kimi Code CLI, OpenCode, or Qwen Code in the same isolated environment
- **Workspace Isolation** — named contexts (personal, work, client) with separate credentials, tools, and vault per workspace
- **Codex Account Switching** — save and switch between multiple Codex logins inside a workspace with `exitbox codex accounts`
- **IDE Integration** — Unix socket relay connects host editors (VS Code, Cursor, Windsurf) to agents inside the container for go-to-definition, diagnostics, and code actions
- **Full Git Support** — optional mode that mounts host `.gitconfig` and SSH agent for seamless git operations inside the container
- **GitHub CLI Authentication** — pre-flight vault import for `GITHUB_TOKEN` with automatic in-container export so `gh` and HTTPS git work transparently
- **RTK Token Optimizer (Experimental)** — optional [rtk](https://github.com/rtk-ai/rtk) integration reduces CLI output token consumption by 60-90%
- **External Tools** — configure third-party tools (GitHub CLI, etc.) via the setup wizard; packages are auto-installed at image build time
- **Supply-Chain Hardened Installs** — Claude Code and OpenCode installed via direct download with SHA-256 checksum verification
- **Alpine Base Image** — minimal ~5 MB base with 3-layer image hierarchy and incremental rebuilds
- **Setup Wizard** — interactive TUI that configures roles, languages, tools, agents, firewall, and vault in one pass
- **Cross-Platform** — native binaries for Linux, macOS, and Windows

---

### Security

The project's security posture is rated **High / Robust**, employing a "Defense in Depth" strategy:

1.  **DNS Isolation (The "Moat")**: Containers cannot resolve external domain names directly. This forces all traffic through the proxy, as the container "knows" nothing of the outside internet.
2.  **Mandatory Proxy Usage**: Since direct DNS fails, tools are forced to use the configured `http_proxy`. Bypassing these variables results in immediate connection failure.
3.  **Proxy Access Control**: The Squid proxy actively inspects destinations, enforcing a strict allow/deny policy (returning `403 Forbidden` for blocked domains).
4.  **Capability Restrictions**: `CAP_NET_RAW` and other capabilities are dropped, preventing raw socket creation and network enumeration attacks (e.g., `ping` is disabled).

Additional hardening:
- **No Privilege Escalation**: `--security-opt=no-new-privileges:true` enforced
- **Capability Dropping**: `--cap-drop=ALL` removes all Linux capabilities
- **Resource Limits**: Default 8GB RAM / 4 CPUs to prevent DoS
- **Secure Defaults**: SSH keys (`~/.ssh`) and AWS credentials (`~/.aws`) are NOT mounted by default

### Sandbox-Aware Agents

ExitBox automatically injects sandbox instructions into each agent on container start. This tells the agent it is running inside a restricted container so it won't attempt actions that can't work (e.g., running `docker`, `podman`, or managing infrastructure).

Instructions are written to each agent's native global instructions file:

| Agent    | Instructions file                        |
|:---------|:-----------------------------------------|
| Claude   | `~/.claude/CLAUDE.md`                    |
| Codex    | `~/.codex/AGENTS.md`                     |
| OpenCode | `~/.config/opencode/AGENTS.md`           |

If the file already exists (e.g., from your own global instructions), ExitBox appends the sandbox notice once. The instructions inform the agent about network restrictions, dropped capabilities, and the read-only nature of the environment so it can focus on writing and debugging code within `/workspace`.

### IDE Integration

ExitBox can relay Unix sockets between the host and container so editors running on the host (VS Code, Cursor, Windsurf, etc.) can communicate with agents inside the sandbox. This enables features like go-to-definition, diagnostics, and code actions to work across the container boundary.

The relay is automatic — when an agent starts, ExitBox detects running IDE instances and establishes a socket tunnel. No manual configuration is needed.

### Full Git Support

By default, containers have no access to host git credentials. Full git support mode mounts the host `.gitconfig` and SSH agent into the container so git operations (clone, push, pull) work transparently with your existing configuration.

```bash
# Enable per-session:
exitbox run claude --full-git-support

# Or enable permanently in the setup wizard:
exitbox setup
# → Settings → Full Git Support
```

When full git support is enabled and the network firewall is active, SSH traffic (e.g., `git push` to GitHub) is automatically tunneled through the Squid proxy. This works even without `SSH_AUTH_SOCK` being set on the host.

### External Tools

External tools are third-party CLI tools (Bun, GitHub CLI, etc.) that can be selected during setup. Their required packages are automatically installed at image build time.

```bash
# Configure via the setup wizard:
exitbox setup
# → Settings → External Tools → select tools
```

### GitHub CLI Authentication

When GitHub CLI is selected as an external tool and the vault is enabled for the workspace, ExitBox provides seamless `gh` authentication:

**Pre-flight prompt (host side):** Before launching an agent, ExitBox checks whether `GITHUB_TOKEN` exists in the workspace vault. If missing, it offers to import it:

```
GitHub CLI is enabled but GITHUB_TOKEN is not in your vault.
Import it now? [y/N]: y
Enter vault password: ****
Paste your GitHub token: ****
✓ GITHUB_TOKEN stored in vault for workspace 'default'
```

**Auto-export (container side):** On container startup, the entrypoint fetches `GITHUB_TOKEN` from the vault via IPC (triggering an approval popup on the host), exports it as `GH_TOKEN`, and runs `gh auth setup-git` to configure git credential helpers. This means `gh` commands and HTTPS git operations authenticate transparently.

```bash
# Inside the container, these just work:
gh pr list
gh issue create --title "Bug report"
git push origin main   # uses gh credential helper for HTTPS
```

The token is never stored in the container filesystem. Every vault read triggers a host-side approval popup, giving you full control over secret access.

### OpenCode Authentication

OpenCode supports OAuth login for providers like Google (Gemini). The OAuth flow starts a local callback server inside the container, and ExitBox automatically publishes the callback port (8085) to the host so the browser redirect works.

```bash
exitbox run opencode -- auth login    # starts OAuth flow, opens browser URL on host
exitbox run opencode -- auth logout   # remove stored credentials
```

When the firewall is active, Google OAuth domains (`google.com`, `googleapis.com`) are included in the default allowlist. If you use a provider with a different OAuth domain, add it via `exitbox-allow <domain>` or the `-a` flag.

### Codex Account Switching

Codex CLI stores its login state in `.codex`, but ExitBox can keep multiple named Codex accounts per workspace and swap the active one without touching the rest of the workspace.

```bash
# Save the current login under a name
exitbox codex accounts save personal

# Create an empty slot for another login
exitbox codex accounts add work

# Log into that slot from your host shell
# Do not type /login inside the Codex prompt
exitbox run codex --workspace default -- login
exitbox codex accounts save work

# Switch back later
exitbox codex accounts switch personal
exitbox codex accounts list
exitbox codex accounts current
```

How it works:

- The active account for a workspace stays in that workspace's normal Codex config directory.
- Saved accounts are stored as named sidecar copies under the same workspace profile.
- `save <name>` snapshots the currently active Codex login.
- `add <name>` creates a new empty active slot and clears the current Codex login so you can run `codex login`.
- `switch <name>` saves the current active login back to its named slot, then restores the requested one.
- `list` shows saved accounts and whether one is current, previous, or still pending login.
- `current` shows the current and previous account names recorded for that workspace.

Notes:

- Account switching is workspace-local. `personal` in workspace `work` is separate from `personal` in workspace `default`.
- Use `--workspace <name>` with any of these commands to manage accounts for a non-active workspace.
- If the current Codex login has never been named, `add` or `switch` will ask you to save it first with `exitbox codex accounts save <name>`.

### RTK Token Optimizer (Experimental)

[rtk](https://github.com/rtk-ai/rtk) wraps common CLI commands to produce compact, token-optimized output — reducing agent token consumption by 60-90%. When enabled, rtk is built from source at image build time using a musl-native Rust toolchain. It adds zero image size overhead when disabled.

```bash
# Enable via the setup wizard:
exitbox setup
# → Settings → RTK → Enable
```

When RTK is enabled, sandbox instructions injected into the agent automatically guide it to prefix supported commands with `rtk`:

```bash
rtk git status         # compact git output
rtk go test ./...      # compact test results
rtk ls /workspace      # compact directory listing
rtk gh pr list         # compact GitHub CLI output
rtk grep <pattern>     # compact search results
```

Supported command categories: `git`, `go`, `gh`, `grep`, `ls`, `curl`, `cargo`, `pytest`, `pnpm`, `docker`, `kubectl`, and more. See the [rtk documentation](https://github.com/rtk-ai/rtk) for the full list.

Agent-level management commands:

```bash
exitbox agents list            # Show enabled agents and their status
exitbox agents config claude   # Open agent config in $EDITOR
```

> **Note:** RTK is experimental. If you encounter issues, disable it via `exitbox setup` and rebuild with `exitbox run <agent> --update`.

### Agent Skills

Skills are reusable SKILL.md files that give agents specialized capabilities (coding conventions, deploy workflows, review checklists, etc.). ExitBox provides a universal skill installer that works across all agents. Skills are installed once per workspace and automatically linked into each agent's skill directory at container start.

```bash
# Install from a GitHub directory (fetches all files recursively)
exitbox skills install https://github.com/anthropics/skills/tree/main/skills/frontend-design

# Install from a raw URL
exitbox skills install https://example.com/path/to/SKILL.md

# Install from a local directory or file
exitbox skills install ./my-skill/
exitbox skills install ./deploy/SKILL.md

# Override the skill name
exitbox skills install https://github.com/user/repo/tree/main/skills/foo --name my-custom-name

# Install into a specific workspace
exitbox skills install ./my-skill -w work

# List and remove
exitbox skills list
exitbox skills remove frontend-design
```

Skills are stored at `~/.config/exitbox/profiles/global/<workspace>/skills/<name>/`. On container start, ExitBox symlinks each skill into the active agent's native skill path:

| Agent    | Skill path inside container              |
|:---------|:-----------------------------------------|
| Claude   | `~/.claude/skills/<name>/SKILL.md`       |
| Codex    | `~/.agents/skills/<name>/SKILL.md`       |
| OpenCode | `~/.config/opencode/skills/<name>/SKILL.md` |
| Qwen     | `~/.config/qwen/skills/<name>/SKILL.md`  |

All agents use the same [Agent Skills](https://agentskills.io) SKILL.md format with YAML frontmatter, so a single skill works across agents.

### Named Resumable Sessions

When agents like Claude Code and Codex exit, they display a resume token (e.g. `claude --resume <id>`). ExitBox captures this token and can pass it on the next run, so you seamlessly resume where you left off.

- **Named sessions** — set a session name explicitly with `--name "<session>"`; if omitted, ExitBox auto-generates one using local time (`YYYY-MM-DD HH:MM:SS`)
- **Named session resume** — when `--name "<session>"` is provided, ExitBox resumes that named session automatically if it exists (use `--no-resume` to force fresh)
- **Disabled by default** — enable via "Auto-resume sessions" in `exitbox setup` or set `auto_resume: true` in `config.yaml`
- **Always shown at exit** — ExitBox always prints a resume command after a session ends (e.g. `exitbox run codex --name "2026-02-11 14:51:02" --resume`)
- **Workspace-aware** — resume commands include `--workspace` when running a non-default workspace
- **Disable per-session** with `--no-resume` to start a fresh session
- Resume tokens are stored per-workspace, per-agent, per-project, and per-session at `~/.config/exitbox/profiles/global/<workspace>/<agent>/projects/<project_key>/sessions/<session_key>/.resume-token`

### Encrypted Vault

ExitBox includes a built-in encrypted secret vault so agents can read and write API keys, tokens, and credentials without `.env` files being exposed inside the container.

- **AES-256 encryption** with **Argon2id** key derivation — no external dependencies required
- **Per-access approval**: Every secret read or write triggers a tmux popup requiring explicit user confirmation
- **Agent-initiated writes**: Agents can store secrets directly via `exitbox-vault set <KEY> <VALUE>` from inside the container — the host user approves each write via popup
- **`.env` masking**: When vault is enabled, all `.env*` files are automatically hidden inside the container
- **Embedded storage**: Secrets are stored in an encrypted [Badger](https://github.com/dgraph-io/badger) database per workspace

The first access in a session prompts for the vault password. Subsequent reads only require the per-key approval popup. Inside the container, agents use `exitbox-vault` to read and write secrets via IPC. See [Vault Management](#vault-management) for the full CLI reference.

## Supported Agents

| Agent       | Description                  | Host Requirement |
|:------------|:-----------------------------|:-----------------|
| `claude`    | Anthropic's Claude Code CLI  | None (installed in container) |
| `codex`     | OpenAI's Codex CLI           | None (downloaded)|
| `opencode`  | OpenCode AI assistant        | None (binary download)  |
| `qwen`      | Qwen Code AI assistant       | None (npm install)      |

All agents are installed inside the container. Existing host config (`~/.claude`, etc.) is imported once into managed storage on first run. Use `exitbox config import <agent>` (or `exitbox config import all`) to re-seed from host config. Use `--workspace` to target a specific workspace. Use `--config`/`-c` to import a specific config file. Use `exitbox config edit <agent>` to open the agent's primary config file in your editor:

```bash
exitbox config import codex -c config.toml          # Import a config file into Codex workspace
exitbox config import opencode -c opencode.json     # Import OpenCode config file
exitbox config import codex -c config.toml -w work  # Import into specific workspace
exitbox config edit claude                           # Edit Claude settings.json in $EDITOR
exitbox config edit codex -w work                    # Edit Codex config.toml for 'work' workspace
exitbox config edit claude --profile openrouter      # Edit profile-scoped settings.json (seeded from default)
```

### Environment Variable Profiles

Named bundles of environment variables stored per-workspace in the KV store. Apply a profile at run time to inject variables via container `-e` flags. Useful for switching between providers (e.g. OpenRouter, direct Anthropic) without editing config.

```bash
exitbox env create openrouter           # Opens $EDITOR; paste KEY=VALUE lines, save
exitbox env edit openrouter             # Edit existing profile
exitbox env list                        # List profiles in active workspace
exitbox env show openrouter             # Show keys (values redacted)
exitbox env show openrouter --unsafe    # Show raw values
exitbox env delete openrouter
exitbox env default openrouter      # Auto-load this profile on every run
exitbox env default                 # Show current default
exitbox env default --clear         # Remove the default
```

When a default profile is set, it is loaded automatically on every `exitbox run` without needing `--profile`. CLI `--profile` overrides the default.

Example profile for routing Claude Code via OpenRouter:

```
OPENROUTER_API_KEY=sk-or-v1-...
ANTHROPIC_BASE_URL=https://openrouter.ai/api
ANTHROPIC_AUTH_TOKEN=$OPENROUTER_API_KEY
ANTHROPIC_API_KEY=
```

Apply at run time:

```bash
exitbox run claude --profile openrouter       # Loads env vars + uses profile-scoped agent config
exitbox run claude -- --profile openrouter    # Same (after --)
```

Config files can be scoped per-profile — `exitbox config edit claude --profile openrouter` creates a copy of the default config under the profile, which takes effect only when `--profile openrouter` is used at run time. CLI `-e KEY=VAL` flags override profile values.

## Installation

### Prerequisites

- **Podman** (recommended) or **Docker** — at least one is required; Podman is preferred for its rootless, daemonless design
- For Windows: **Docker Desktop** provides the Docker CLI that ExitBox uses

### Linux

```bash
# Install Podman (recommended) or Docker
sudo apt update && sudo apt install -y podman   # Ubuntu/Debian
# OR: install Docker - see https://docs.docker.com/engine/install/

# Download the latest release binary
mkdir -p ~/.local/bin
curl -fsSL https://github.com/Cloud-Exit/ExitBox/releases/latest/download/exitbox-linux-amd64 -o ~/.local/bin/exitbox
chmod +x ~/.local/bin/exitbox
# For ARM64: replace exitbox-linux-amd64 with exitbox-linux-arm64

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

# Download the latest release binary
mkdir -p ~/.local/bin
curl -fsSL https://github.com/Cloud-Exit/ExitBox/releases/latest/download/exitbox-darwin-arm64 -o ~/.local/bin/exitbox
chmod +x ~/.local/bin/exitbox
# For Intel Macs: replace exitbox-darwin-arm64 with exitbox-darwin-amd64

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
mkdir -p ~/.local/bin
curl -fsSL https://github.com/Cloud-Exit/ExitBox/releases/latest/download/exitbox-linux-amd64 -o ~/.local/bin/exitbox
chmod +x ~/.local/bin/exitbox
exitbox setup
```

### Build from Source

```bash
git clone https://github.com/Cloud-Exit/exitbox.git
cd exitbox
make build       # builds ./exitbox
make install     # installs to ~/.local/bin/exitbox
```

### Script Installer (not recommended)

A convenience install script is available but **not advised from a security perspective**. Piping a remote script into your shell executes arbitrary code with your user's full permissions — you cannot review what runs before it runs. If the hosting server, DNS, or CDN is compromised, the script could be replaced with something malicious. You also lose the ability to verify checksums or signatures before execution.

If you still want to use it:

```bash
# Review the script first
curl -fsSL https://raw.githubusercontent.com/cloud-exit/exitbox/main/scripts/install.sh -o install.sh
less install.sh
sh install.sh
```

Prefer the manual binary download or building from source described above.

### Updating

```bash
exitbox update
```

This downloads the latest release binary and replaces the current installation in-place. To update agent images (Claude Code, Codex, etc.) to their latest versions:

```bash
exitbox run --update claude    # rebuild with latest agent version
exitbox rebuild all            # rebuild all enabled agents
```

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
exitbox run qwen [args]       # Run Qwen Code
```

### Management

```bash
exitbox list              # List available agents and build status
exitbox enable <agent>    # Enable an agent
exitbox disable <agent>   # Disable an agent
exitbox rebuild <agent>   # Force rebuild of agent image
exitbox rebuild all       # Rebuild all enabled agents
exitbox uninstall <agent> # Remove agent images and config
exitbox update            # Update ExitBox to the latest version
exitbox aliases           # Print shell aliases for ~/.bashrc
exitbox agents list       # List enabled agents and their status
exitbox agents config <agent>  # Open agent config in $EDITOR
exitbox codex accounts list     # List saved Codex accounts
exitbox codex accounts current  # Show current/previous Codex account
exitbox codex accounts save <name>   # Save active Codex login
exitbox codex accounts add <name>    # Create a new empty Codex login slot
exitbox codex accounts switch <name> # Activate a saved Codex login
exitbox config import <agent|all>  # Import agent config from host
exitbox config edit <agent>        # Open agent config file in $EDITOR
exitbox skills install <source>    # Install a skill from URL/path
exitbox skills list                # List installed skills
exitbox skills remove <name>       # Remove an installed skill
exitbox env create <profile>       # Create a new env profile
exitbox env edit <profile>         # Edit env profile in $EDITOR
exitbox env list                   # List env profiles
exitbox env show <profile>         # Show env profile (values redacted)
exitbox env delete <profile>       # Delete env profile
exitbox env default [profile]      # Get or set the default env profile
exitbox env default --clear        # Remove the default env profile
```

### Config Generation

Generate agent configuration files for third-party LLM servers (Ollama, vLLM, LM Studio, etc.):

```bash
exitbox generate opencode              # Configure OpenCode for a custom provider
exitbox generate claude -w work        # Configure Claude Code in 'work' workspace
exitbox generate codex                 # Configure Codex for a custom provider
```

The wizard prompts for server URL, API key, tests connectivity via `GET /v1/models`,
lets you pick a model from the discovered list, and writes the correct config file
into the workspace profile directory. Existing config is preserved via deep merge.

When vault is enabled for the workspace, the wizard offers to store the API key
in the vault instead of writing it to the config file.

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

### Vault Management

```bash
exitbox vault init -w <workspace>       # Initialize a new vault
exitbox vault set <KEY> -w <workspace>  # Set a secret (value prompted securely)
exitbox vault get <KEY> -w <workspace>  # Retrieve a secret
exitbox vault list -w <workspace>       # List secret keys
exitbox vault delete <KEY>              # Delete a secret
exitbox vault import <file>             # Import key-value pairs from a .env file
exitbox vault edit                      # Edit secrets in $EDITOR (KEY=VALUE format)
exitbox vault status                    # Show vault state for a workspace
exitbox vault destroy                   # Permanently delete a vault
```

#### In-Container Vault Commands

Inside the container, agents use the `exitbox-vault` binary to interact with the vault over IPC:

```bash
exitbox-vault list                    # List key names
exitbox-vault get <KEY>               # Get a secret value (stdout)
exitbox-vault set <KEY> <VALUE>       # Store a secret (host approval required)
exitbox-vault env                     # Print all KEY=VALUE pairs
```

Every `get` and `set` triggers a tmux popup on the host terminal requiring explicit approval before the operation proceeds.

#### Agent Secret Workflow

When an agent detects or generates a secret (API key, token, password), it follows this workflow:

1. **Ask for a key name** — the agent prompts the user for a vault key name before attempting to store anything
2. **Generate and store in one step** — the secret is piped directly into `exitbox-vault set` so it never appears in command output:
   ```bash
   exitbox-vault set MY_TOKEN "$(python3 -c "import secrets; print(secrets.token_urlsafe(32))")"
   ```
3. **Host approves** — a tmux popup appears on the host terminal; the host user must approve the write
4. **Use via variable** — the agent retrieves the secret into a shell variable and uses it inline:
   ```bash
   TOKEN=$(exitbox-vault get MY_TOKEN)
   curl -H "Authorization: Bearer $TOKEN" https://api.example.com
   ```
5. **Redact output** — any command output that might echo the secret is captured and redacted before display

Agents are automatically informed about vault commands and these rules via sandbox instructions injected at container start.

#### How Workspaces Work

- **Isolated credentials**: Each workspace has its own agent config directory at `~/.config/exitbox/profiles/global/<workspace>/<agent>/`. API keys, auth tokens, and conversation history are not shared between workspaces.
- **Development stacks**: Each workspace can have its own set of dev roles (languages/tools). The setup wizard or `exitbox workspaces add` lets you pick the stack for each workspace.
- **Per-project auto-detection**: Workspaces can be scoped to a directory. When you run an agent from that directory, ExitBox automatically uses the matching workspace.
- **Default workspace**: Set via `exitbox setup` or `exitbox workspaces default`. Used when no directory-scoped workspace matches.
- **Credential import**: When creating a workspace, you can import credentials from the host or copy them from an existing workspace. You can also import later with `exitbox config import <agent> --workspace <name>`.

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
exitbox run --full-git-support claude    # Mount host .gitconfig and SSH agent
exitbox run --ollama claude              # Use host Ollama for local models
exitbox run --memory 16g --cpus 8 claude # Custom resource limits
exitbox run --version 1.0.123 claude   # Pin specific agent version
exitbox run --profile openrouter claude # Apply env profile (vars + profile-scoped config)
```

All flags have long forms: `-f`/`--no-firewall`, `-r`/`--read-only`, `-v`/`--verbose`, `-n`/`--no-env`, `--resume [SESSION|TOKEN]`, `--no-resume`, `--name`, `-i`/`--include-dir`, `-t`/`--tools`, `-a`/`--allow-urls`, `-u`/`--update`, `-w`/`--workspace`, `--profile`, `--full-git-support`, `--ollama`, `--memory`, `--cpus`, `--version`.

## Available Roles

Roles (formerly "profiles") are pre-configured development environments (toolchains, language runtimes, CLI stacks). The setup wizard suggests roles based on the developer role preset you pick (Frontend, Backend, AI Developer, etc.), or you can add them manually.

| Role          | Description                              |
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
| `ml`          | AI/ML tooling (huggingface-cli, safetensors) — requires `python` |
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
      vault:
        enabled: true

agents:
  claude:
    enabled: true
    version: "1.0.123"    # pin agent version (omit for latest)
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

ExitBox uses **managed config** (import-only) with per-workspace isolation. On first run, host config is copied into the active workspace's managed directory. Host originals are never modified. Use `exitbox config import <agent>` to re-seed from host config at any time, optionally with `--workspace <name>` to target a specific workspace.

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
| Qwen     | `.qwen/`                                     | `/home/user/.qwen`                |
| Qwen     | `.config/qwen/`                              | `/home/user/.config/qwen`         |

Your project directory is mounted at `/workspace`.

When Codex is enabled, ExitBox publishes callback port `1455` on the shared `exitbox-squid` container and relays it to the active Codex container, so OrbStack/private-networking callback flows work reliably.

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

Agent binaries are installed via **direct download with SHA-256 checksum verification**:

- **Claude Code**: binary download verified against Anthropic's signed manifest. Download URL auto-discovered from the official installer if the hardcoded endpoint changes.
- **OpenCode**: GitHub release tarball verified against the `digest` field from the GitHub Releases API.

No `curl | bash` for any agent. Builds abort on any checksum mismatch.

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
