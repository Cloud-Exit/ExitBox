// ExitBox - Multi-Agent Container Sandbox
// Copyright (C) 2026 Cloud Exit B.V.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/agents"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/env"
	"github.com/cloud-exit/exitbox/internal/image"
	"github.com/cloud-exit/exitbox/internal/profile"
	"github.com/cloud-exit/exitbox/internal/project"
	"github.com/cloud-exit/exitbox/internal/run"
	"github.com/cloud-exit/exitbox/internal/session"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/cloud-exit/exitbox/internal/update"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run <agent> [args...]",
	Short: "Run an agent in a container",
	Long:  runCmdLong(),
}

// runCmdLong builds the `run` long help, listing the available agents from the
// registry so the list stays in sync as agents are added or removed.
func runCmdLong() string {
	var b strings.Builder
	b.WriteString("Run an AI coding assistant in an isolated container.\n\nAvailable agents:\n")

	list := agents.All()
	sort.Slice(list, func(i, j int) bool { return list[i].Name() < list[j].Name() })
	for _, a := range list {
		fmt.Fprintf(&b, "  %-11s %s\n", a.Name(), a.DisplayName())
	}

	b.WriteString(runCmdLongRest)
	return b.String()
}

const runCmdLongRest = `
Workspaces:
  Workspaces are named contexts (e.g. personal/work) with development stacks
  and separate agent config storage.
  Manage them with: exitbox workspaces --help

Sessions:
  Each run creates a session. By default sessions are named with a timestamp.
  Use --name to give a session a memorable name. Named sessions auto-resume:
  running --name "foo" resumes "foo" if it exists, or starts fresh if new.
  Use --no-resume with --name to force a fresh start.

Flags (passed after the agent name):
  -f, --no-firewall       Disable network firewall
  -r, --read-only         Mount workspace as read-only
  -n, --no-env            Don't pass host environment variables
      --name SESSION      Name this session (resumes if it already exists)
      --resume [SESSION]  Resume a session by name/id (or last active if bare)
      --no-resume         Force a fresh session (overrides --name auto-resume)
  -u, --update            Check for and apply agent updates
      --version VERSION   Pin specific agent version (e.g., 1.0.123)
  -v, --verbose           Enable verbose output
  -w, --workspace NAME    Use a specific workspace for this session
      --profile NAME      Apply an env profile (loads vars + profile-scoped config)
  -e, --env KEY=VALUE     Pass environment variables
  -t, --tools PKG         Add Alpine packages to the image
  -i, --include-dir DIR   Mount host dir inside /workspace
  -a, --allow-urls DOM    Allow extra domains for this session
      --ollama            Use host Ollama for local models
      --memory SIZE       Container memory limit (default: 8g)
      --cpus COUNT        Container CPU limit (default: 4)

Examples:
  exitbox run claude                        Start a new session
  exitbox run claude --name "feature-x"     Start or resume session "feature-x"
  exitbox run claude --name "feature-x" --no-resume  Fresh start, named "feature-x"
  exitbox run claude --resume               Resume last active session
  exitbox run claude --resume "feature-x"   Resume session "feature-x" by name
  exitbox run claude -f -e GITHUB_TOKEN=$GITHUB_TOKEN
  exitbox run claude --workspace work
  exitbox run opencode --ollama --memory 16g --cpus 8`

func newAgentRunCmd(agt agent.Agent) *cobra.Command {
	if agt == nil {
		return nil
	}
	display := agt.DisplayName()
	return &cobra.Command{
		Use:                agt.Name() + " [args...]",
		Short:              "Run " + display,
		Long:               "Run " + display + " in an isolated container.",
		DisableFlagParsing: true,
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return completeAgentRunArgs(agt.Name(), args, toComplete)
		},
		Run: func(cmd *cobra.Command, args []string) {
			// DisableFlagParsing swallows --help; handle it manually
			for _, a := range args {
				if a == "--" {
					break
				}
				if a == "--help" || a == "-h" {
					_ = cmd.Help()
					return
				}
				if !strings.HasPrefix(a, "-") {
					break
				}
			}
			runAgent(agt.Name(), args)
		},
	}
}

func runAgent(agentName string, passthrough []string) {
	cfg := config.LoadOrDefault()
	if !cfg.IsAgentEnabled(agentName) {
		ui.Errorf("Agent '%s' is not enabled. Run 'exitbox enable %s' first.", agentName, agentName)
	}

	rt := container.Detect()
	if rt == nil {
		ui.Error("No container runtime found. Install Podman or Docker.")
	}

	projectDir, _ := os.Getwd()
	ctx := context.Background()

	// Initialize project
	if err := project.Init(projectDir); err != nil {
		ui.Warnf("Failed to initialize project directory: %v", err)
	}

	image.Version = Version

	flags := parseRunFlags(passthrough, cfg.Settings.DefaultFlags)
	flags = applySessionResumeDefaults(flags)

	if flags.Verbose {
		ui.Verbose = true
	}

	image.SessionTools = flags.Tools
	image.ForceRebuild = flags.ForceUpdate
	image.AutoUpdate = cfg.Settings.AutoUpdate || flags.ForceUpdate

	// Resolve agent version: CLI flag → config → empty (latest)
	if flags.AgentVersion != "" {
		image.AgentVersion = flags.AgentVersion
	} else {
		image.AgentVersion = cfg.GetAgentVersion(agentName)
	}

	// Validate workspace exists before attempting to run.
	if flags.Workspace != "" {
		if profile.FindWorkspace(cfg, flags.Workspace) == nil {
			available := profile.WorkspaceNames(cfg)
			if len(available) > 0 {
				ui.Errorf("Unknown workspace '%s'. Available workspaces: %s", flags.Workspace, strings.Join(available, ", "))
			} else {
				ui.Errorf("Unknown workspace '%s'. No workspaces configured. Run 'exitbox setup' first.", flags.Workspace)
			}
		}
	}

	// Load env profile vars and prepend to EnvVars so CLI -e flags override
	// profile values. If no --profile given, check for a workspace default.
	if flags.EnvProfile == "" {
		active, err := profile.ResolveActiveWorkspace(cfg, projectDir, flags.Workspace)
		if err != nil {
			ui.Warnf("Could not resolve workspace for default env profile: %v", err)
		}
		if active != nil {
			if defName, err := env.GetDefault(active.Workspace.Name); err != nil {
				ui.Warnf("Could not read default env profile: %v", err)
			} else if defName != "" {
				flags.EnvProfile = defName
			}
		}
	}
	if flags.EnvProfile != "" {
		if err := validateProfileName(flags.EnvProfile); err != nil {
			ui.Errorf("%v", err)
			return
		}
		active, err := profile.ResolveActiveWorkspace(cfg, projectDir, flags.Workspace)
		if err != nil {
			ui.Errorf("Cannot resolve workspace for env profile '%s': %v", flags.EnvProfile, err)
			return
		}
		envProfile, err := env.Load(active.Workspace.Name, flags.EnvProfile)
		if err != nil {
			ui.Errorf("%v", err)
			return
		}
		profileKeys := make([]string, 0, len(envProfile.Vars))
		for k := range envProfile.Vars {
			profileKeys = append(profileKeys, k)
		}
		sort.Strings(profileKeys)
		profileVars := make([]string, 0, len(profileKeys))
		for _, k := range profileKeys {
			profileVars = append(profileVars, k+"="+envProfile.Vars[k])
		}
		flags.EnvVars = append(profileVars, flags.EnvVars...)
		ui.Infof("Loaded env profile '%s' (%d vars) from workspace '%s'",
			flags.EnvProfile, len(envProfile.Vars), active.Workspace.Name)
	}

	// Session resolution logic:
	//
	// --name "X"            → SessionName="X", Resume=true (implied). Container
	//                         resumes "X" if it has a stored token, starts fresh
	//                         otherwise. Session token is saved under "X" on exit.
	// --name "X" --no-resume → SessionName="X", Resume=false. Always fresh start,
	//                          but still saves the session under "X" for later.
	// --resume "X"          → Resolves "X" as a session name/id below, then
	//                         sets SessionName="X", Resume=true.
	// --resume              → Resume=true, SessionName="". Entrypoint loads last
	//                         active session from .active-session file.
	// (no flags)            → Resume=false, SessionName=<timestamp>. Fresh session.
	//
	// The entrypoint (build_resume_args) handles the actual token lookup:
	// it checks the per-session .resume-token file for the given session name
	// and only falls back to the legacy single-slot .resume-token when the
	// session name came from .active-session (not from an explicit --name).

	// If user passed --resume <value> without --name, treat value as a possible
	// named session selector first (name or session id). Fall back to token.
	if flags.Resume && flags.ResumeToken != "" && strings.TrimSpace(flags.SessionName) == "" {
		active, err := profile.ResolveActiveWorkspace(cfg, projectDir, flags.Workspace)
		if err == nil && active != nil {
			if resolved, ok, resolveErr := session.ResolveSelector(active.Workspace.Name, agentName, projectDir, flags.ResumeToken); resolveErr != nil {
				ui.Warnf("Could not resolve session selector '%s': %v", flags.ResumeToken, resolveErr)
			} else if ok {
				flags.SessionName = resolved
				flags.ResumeToken = ""
			}
		}
	}
	// Only assign a default session name when NOT doing a bare resume.
	// Bare --resume (no --name, no selector) should let the entrypoint
	// resolve the last active session from .active-session.
	if strings.TrimSpace(flags.SessionName) == "" && !flags.Resume {
		flags.SessionName = defaultSessionName()
	}

	switchFile := filepath.Join(projectDir, ".exitbox", "workspace-switch")
	actionFile := filepath.Join(projectDir, ".exitbox", "session-action")

	// Background update check: shows a tmux popup during the session if
	// a newer version is available. If user approves, update runs after exit.
	var wantUpdate atomic.Int32
	var latestVersion atomic.Value
	containerCmd := container.Cmd(rt)
	containerName := project.ContainerName(agentName, projectDir)
	update.RunUpdatePopup(containerCmd, containerName, Version, &wantUpdate, &latestVersion)

	// Main run loop: re-launches on workspace switch.
	for {
		// Reload config each iteration (workspace switch updates it).
		cfg = config.LoadOrDefault()

		if err := image.BuildProject(ctx, rt, agentName, projectDir, flags.Workspace, false); err != nil {
			ui.Errorf("Failed to build images: %v", err)
		}

		workspaceHash := image.WorkspaceHash(cfg, projectDir, flags.Workspace)

		opts := run.Options{
			Agent:             agentName,
			ProjectDir:        projectDir,
			WorkspaceHash:     workspaceHash,
			WorkspaceOverride: flags.Workspace,
			EnvProfile:        flags.EnvProfile,
			NoFirewall:        flags.NoFirewall,
			ReadOnly:          flags.ReadOnly,
			NoEnv:             flags.NoEnv,
			Resume:            flags.Resume,
			ResumeToken:       flags.ResumeToken,
			SessionName:       flags.SessionName,
			EnvVars:           flags.EnvVars,
			IncludeDirs:       flags.IncludeDirs,
			AllowURLs:         flags.AllowURLs,
			Passthrough:       flags.Remaining,
			Verbose:           flags.Verbose,
			StatusBar:         cfg.Settings.StatusBar,
			Version:           Version,
			Ollama:            flags.Ollama,
			Memory:            flags.Memory,
			CPUs:              flags.CPUs,
			Keybindings:       cfg.Settings.Keybindings.EnvValue(),
			FullGitSupport:    cfg.Settings.DefaultFlags.FullGitSupport,
			RTK:               cfg.Settings.RTK,
		}

		exitCode, err := run.AgentContainer(rt, opts)
		if err != nil {
			ui.Errorf("%v", err)
		}

		// Check for session action signal from the container.
		if data, readErr := os.ReadFile(actionFile); readErr == nil {
			_ = os.Remove(actionFile)
			action := parseSessionAction(string(data))
			shouldContinue := false

			if action.Workspace != "" {
				switchCfg := config.LoadOrDefault()
				if profile.FindWorkspace(switchCfg, action.Workspace) == nil {
					ui.Warnf("Workspace '%s' not found. Available: %s", action.Workspace, strings.Join(profile.WorkspaceNames(switchCfg), ", "))
				} else {
					ui.Infof("Switching to workspace '%s'...", action.Workspace)
					flags.Workspace = action.Workspace
					shouldContinue = true
				}
			}

			if action.SessionName != "" {
				flags.SessionName = action.SessionName
				ui.Infof("Switching to session '%s'...", action.SessionName)
				shouldContinue = true
			}

			if action.Resume {
				flags.Resume = true
				flags.ResumeToken = "" // Use stored token for the selected session
			}

			if shouldContinue {
				continue
			}
		}

		// Check for workspace switch signal from the container.
		if data, readErr := os.ReadFile(switchFile); readErr == nil {
			newWorkspace := strings.TrimSpace(string(data))
			_ = os.Remove(switchFile)
			if newWorkspace != "" {
				switchCfg := config.LoadOrDefault()
				if profile.FindWorkspace(switchCfg, newWorkspace) == nil {
					ui.Warnf("Workspace '%s' not found. Available: %s", newWorkspace, strings.Join(profile.WorkspaceNames(switchCfg), ", "))
				} else {
					ui.Infof("Switching to workspace '%s'...", newWorkspace)
					flags.Workspace = newWorkspace
					flags.Resume = true    // Auto-resume when switching workspaces
					flags.ResumeToken = "" // Use stored token, not an explicit one
					continue
				}
			}
		}

		// If the user approved an update via the tmux popup, apply it now.
		if wantUpdate.Load() == 1 {
			if latest, ok := latestVersion.Load().(string); ok && latest != "" {
				fmt.Printf("\nUpdating ExitBox: v%s → v%s...\n", Version, latest)
				url := update.BinaryURL(latest)
				if err := update.DownloadAndReplace(url); err != nil {
					ui.Warnf("Update failed: %v", err)
				} else {
					ui.Successf("ExitBox updated to v%s", latest)
				}
			}
		}

		os.Exit(exitCode)
	}
}

type parsedFlags struct {
	NoFirewall     bool
	ReadOnly       bool
	NoEnv          bool
	Resume         bool
	NoResumeSet    bool
	ResumeToken    string
	SessionName    string
	SessionNameSet bool // true when --name was explicitly passed
	Verbose        bool
	AgentVersion   string
	ForceUpdate    bool
	Workspace      string
	EnvProfile     string
	Ollama         bool
	Memory         string
	CPUs           string
	EnvVars        []string
	IncludeDirs    []string
	AllowURLs      []string
	Tools          []string
	Remaining      []string
}

func parseRunFlags(passthrough []string, defaults config.DefaultFlags) parsedFlags {
	f := parsedFlags{
		NoFirewall: defaults.NoFirewall,
		ReadOnly:   defaults.ReadOnly,
		NoEnv:      defaults.NoEnv,
		Resume:     defaults.AutoResume,
		Memory:     defaults.Memory,
		CPUs:       defaults.CPUs,
	}

	for i := 0; i < len(passthrough); i++ {
		arg := passthrough[i]
		switch arg {
		case "-f", "--no-firewall":
			f.NoFirewall = true
		case "-r", "--read-only":
			f.ReadOnly = true
		case "-n", "--no-env":
			f.NoEnv = true
		case "--resume":
			f.Resume = true
			// Optional token: peek at next arg (if it doesn't start with -)
			if i+1 < len(passthrough) && !strings.HasPrefix(passthrough[i+1], "-") {
				i++
				f.ResumeToken = passthrough[i]
			}
		case "--no-resume":
			f.Resume = false
			f.NoResumeSet = true
		case "--name":
			if i+1 < len(passthrough) {
				i++
				f.SessionName = passthrough[i]
				f.SessionNameSet = true
			}
		case "-v", "--verbose":
			f.Verbose = true
		case "-u", "--update":
			f.ForceUpdate = true
		case "--version":
			if i+1 < len(passthrough) {
				i++
				f.AgentVersion = passthrough[i]
			}
		case "-w", "--workspace":
			if i+1 < len(passthrough) {
				i++
				f.Workspace = passthrough[i]
			}
		case "--profile":
			if i+1 < len(passthrough) {
				i++
				f.EnvProfile = passthrough[i]
			}
		case "-e", "--env":
			if i+1 < len(passthrough) {
				i++
				f.EnvVars = append(f.EnvVars, passthrough[i])
			}
		case "-t", "--tools":
			if i+1 < len(passthrough) {
				i++
				f.Tools = append(f.Tools, passthrough[i])
			}
		case "-i", "--include-dir":
			if i+1 < len(passthrough) {
				i++
				f.IncludeDirs = append(f.IncludeDirs, passthrough[i])
			}
		case "-a", "--allow-urls":
			if i+1 < len(passthrough) {
				i++
				f.AllowURLs = append(f.AllowURLs, passthrough[i])
			}
		case "--ollama":
			f.Ollama = true
		case "--memory":
			if i+1 < len(passthrough) {
				i++
				f.Memory = passthrough[i]
			}
		case "--cpus":
			if i+1 < len(passthrough) {
				i++
				f.CPUs = passthrough[i]
			}
		case "--":
			f.Remaining = append(f.Remaining, passthrough[i+1:]...)
			i = len(passthrough)
		default:
			f.Remaining = append(f.Remaining, arg)
		}
	}

	return f
}

func applySessionResumeDefaults(f parsedFlags) parsedFlags {
	// Named sessions (--name) imply auto-resume: if the session already
	// exists it will be resumed, otherwise a fresh one is created.
	// Users can force a fresh start with --no-resume.
	if f.SessionNameSet && !f.NoResumeSet {
		f.Resume = true
	}
	return f
}

func defaultSessionName() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

type sessionAction struct {
	Workspace   string
	SessionName string
	Resume      bool
}

func parseSessionAction(raw string) sessionAction {
	var out sessionAction
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "workspace":
			out.Workspace = value
		case "session":
			out.SessionName = value
		case "resume":
			if b, err := strconv.ParseBool(value); err == nil {
				out.Resume = b
			}
		}
	}
	return out
}

func init() {
	for _, agt := range agents.All() {
		agentCmd := newAgentRunCmd(agt)
		runCmd.AddCommand(agentCmd)
	}

	rootCmd.AddCommand(runCmd)
}
