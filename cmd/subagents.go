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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/env"
	"github.com/cloud-exit/exitbox/internal/profile"
	"github.com/cloud-exit/exitbox/internal/skills"
	"github.com/cloud-exit/exitbox/internal/subagents"
	"github.com/spf13/cobra"
)

const nextStartNote = "Running sessions pick this up at the next container start."

// hostAgentsDir is the host's user-level agents directory of a coding agent.
var hostAgentsDir = func(h *subagents.Harness) (string, error) {
	return h.HostDir()
}

// runEditor opens a file in $EDITOR (default vi).
var runEditor = func(path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	c := exec.Command(editor, path)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func newSubagentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "subagents",
		Short: "Manage per-workspace subagents of each coding agent",
		Long: `Manage the subagents of a workspace, per coding agent.

Every coding agent has its own subagent format, so a workspace keeps one
store per agent: ` + strings.Join(agentNames(hasSubagents), ", ") + `.
Pick the agent with --agent. You can leave it out when only one enabled agent
has subagents, and for edit, show, rm and validate when the name is stored
for one agent only.

On container start, ExitBox puts every stored file into the agent's own
agents directory (for Claude Code: ~/.claude/agents). The stores are mounted
read-only, so an agent cannot change its own definitions. Files are checked
against the loading rules of their agent, which mostly skips a broken file
without telling you.

Changes made on the host reach running sessions at the next container start.

Which tasks go to which subagent is decided in the agent's master skill:
exitbox agents skill edit`,
		SilenceUsage: true,
	}
	cmd.AddCommand(
		newSubagentsListCmd(),
		newSubagentsNewCmd(),
		newSubagentsAddCmd(),
		newSubagentsImportCmd(),
		newSubagentsEditCmd(),
		newSubagentsShowCmd(),
		newSubagentsRmCmd(),
		newSubagentsValidateCmd(),
	)
	for _, c := range cmd.Commands() {
		c.SilenceUsage = true
	}
	return cmd
}

func addWorkspaceFlag(cmd *cobra.Command, ws *string) {
	cmd.Flags().StringVarP(ws, "workspace", "w", "", "Workspace name (default: active)")
	_ = cmd.RegisterFlagCompletionFunc("workspace", completeWorkspaceFlagValues)
}

// harnessKind is a feature a command needs from a coding agent.
type harnessKind struct {
	fits    func(*subagents.Harness) bool
	missing string // why an agent does not fit; %s is its display name
}

var (
	hasSubagents = harnessKind{
		fits:    func(h *subagents.Harness) bool { return h.HasSubagents() },
		missing: "%s has no native subagents",
	}
	hasSkills = harnessKind{
		fits:    func(h *subagents.Harness) bool { return h.Skills },
		missing: "ExitBox links no skills for %s, so it cannot have a master skill",
	}
)

func agentNames(kind harnessKind) []string {
	var out []string
	for _, h := range subagents.Harnesses() {
		if kind.fits(h) {
			out = append(out, h.Agent)
		}
	}
	return out
}

func addAgentFlag(cmd *cobra.Command, agent *string, kind harnessKind) {
	names := agentNames(kind)
	cmd.Flags().StringVar(agent, "agent", "", "Coding agent: "+strings.Join(names, ", "))
	_ = cmd.RegisterFlagCompletionFunc("agent", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return names, cobra.ShellCompDirectiveNoFileComp
	})
}

// pickHarness returns the coding agent a command works on: the one named by
// --agent, or else the only enabled agent that has the feature.
func pickHarness(flag string, kind harnessKind) (*subagents.Harness, error) {
	all := agentNames(kind)
	if flag != "" {
		h := subagents.HarnessFor(flag)
		switch {
		case h == nil:
			return nil, fmt.Errorf("unknown agent %q; choose one of: %s", flag, strings.Join(all, ", "))
		case !kind.fits(h):
			return nil, fmt.Errorf(kind.missing+"; agents that can: %s", h.Label, strings.Join(all, ", "))
		}
		return h, nil
	}
	cfg := config.LoadOrDefault()
	var enabled []string
	for _, name := range all {
		if cfg.IsAgentEnabled(name) {
			enabled = append(enabled, name)
		}
	}
	switch len(enabled) {
	case 1:
		return subagents.HarnessFor(enabled[0]), nil
	case 0:
		enabled = all
	}
	return nil, fmt.Errorf("pick a coding agent with --agent (%s)", strings.Join(enabled, ", "))
}

// subagentHarnesses returns the agents a listing covers: the one named by
// --agent, or every agent that has subagents.
func subagentHarnesses(flag string) ([]*subagents.Harness, error) {
	if flag == "" {
		return subagents.WithSubagents(), nil
	}
	h, err := pickHarness(flag, hasSubagents)
	if err != nil {
		return nil, err
	}
	return []*subagents.Harness{h}, nil
}

// findStored finds a stored subagent by name. Without --agent it looks in
// every agent's store and wants exactly one match.
func findStored(ws, flag, name string) (*subagents.Harness, subagents.Entry, error) {
	set, err := subagentHarnesses(flag)
	if err != nil {
		return nil, subagents.Entry{}, err
	}
	var hits []*subagents.Harness
	var found subagents.Entry
	for _, h := range set {
		e, err := h.Find(ws, name)
		if errors.Is(err, subagents.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, subagents.Entry{}, err
		}
		hits, found = append(hits, h), e
	}
	switch len(hits) {
	case 0:
		return nil, subagents.Entry{}, fmt.Errorf("subagent %q %w in workspace '%s'", name, subagents.ErrNotFound, ws)
	case 1:
		return hits[0], found, nil
	}
	var names []string
	for _, h := range hits {
		names = append(names, h.Agent)
	}
	return nil, subagents.Entry{}, fmt.Errorf("subagent %q is stored for %s; pick one with --agent", name, strings.Join(names, " and "))
}

// resolveSubagentsWorkspace resolves the workspace like resolveAgentsWorkspace
// and rejects names that would escape the profiles directory.
func resolveSubagentsWorkspace(flag string) (string, error) {
	ws := flag
	if ws == "" {
		cfg := config.LoadOrDefault()
		projectDir, _ := os.Getwd()
		if active, err := profile.ResolveActiveWorkspace(cfg, projectDir, ""); err == nil && active != nil {
			ws = active.Workspace.Name
		} else {
			ws = cfg.Settings.DefaultWorkspace
		}
	}
	if ws == "" {
		return "", errors.New("no workspace specified; use -w <workspace> or set a default workspace")
	}
	if !subagents.ValidWorkspaceName(ws) {
		return "", fmt.Errorf("invalid workspace name %q", ws)
	}
	return ws, nil
}

// workspaceEnvFindings checks the workspace's env profiles. The KV store is
// only opened when it already exists, so listing never creates it.
func workspaceEnvFindings(ws string) []subagents.Finding {
	if _, err := os.Stat(config.KVDir(ws)); err != nil {
		return nil
	}
	names, err := env.List(ws)
	if err != nil {
		return nil
	}
	envs := map[string]map[string]string{}
	for _, n := range names {
		p, err := env.Load(ws, n)
		if err != nil {
			continue
		}
		envs[fmt.Sprintf("env profile '%s'", n)] = p.Vars
	}
	return subagents.EnvFindings(envs)
}

// workspaceNotes returns what is about a whole agent or workspace rather
// than one file: Claude Code's model override as findings, and the known
// limits of the agents that have stored subagents as notes.
func workspaceNotes(ws string, stored map[*subagents.Harness]int) ([]subagents.Finding, []string) {
	var findings []subagents.Finding
	if stored[subagents.Claude] > 0 {
		findings = workspaceEnvFindings(ws)
	}
	var notes []string
	for _, h := range subagents.WithSubagents() {
		if stored[h] > 0 && h.Caveat != "" {
			notes = append(notes, fmt.Sprintf("note: %s: %s", h.Agent, h.Caveat))
		}
	}
	return findings, notes
}

func printWorkspaceNotes(w io.Writer, findings []subagents.Finding, notes []string) {
	printFindings(w, "", findings)
	for _, n := range notes {
		_, _ = fmt.Fprintln(w, n)
	}
}

func printFindings(w io.Writer, indent string, fs []subagents.Finding) {
	for _, f := range fs {
		_, _ = fmt.Fprintf(w, "%s%s\n", indent, f)
	}
}

func entryLabel(e subagents.Entry) string {
	if n := e.Name(); n != "" {
		return fmt.Sprintf("%s (%s)", n, e.File)
	}
	return e.File
}

func entryStatus(e subagents.Entry) string {
	errs, warns := 0, 0
	for _, f := range e.Findings {
		if f.Severity == subagents.Error {
			errs++
		} else {
			warns++
		}
	}
	switch {
	case errs > 0:
		return "error"
	case warns > 0:
		return fmt.Sprintf("%d warning(s)", warns)
	}
	return "ok"
}

// --- list ---

type subagentJSON struct {
	Agent       string              `json:"agent"`
	Name        string              `json:"name"`
	File        string              `json:"file"`
	Path        string              `json:"path"`
	Source      subagents.Source    `json:"source"`
	Origin      string              `json:"origin,omitempty"`
	Description string              `json:"description,omitempty"`
	Model       string              `json:"model,omitempty"`
	Tools       []string            `json:"tools,omitempty"`
	Status      string              `json:"status"`
	Findings    []subagents.Finding `json:"findings"`
	ShadowedBy  string              `json:"shadowed_by,omitempty"`
}

func newSubagentsListCmd() *cobra.Command {
	var workspace, agent string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List subagents with their agent, model, tools, source and validation status",
		Long: `List the workspace's stored subagents, plus the agent files the container
and the current project already have. Without --agent, every coding agent
is listed.

Sources:
  managed    created or added with exitbox subagents
  imported   copied from the host's agents directory with 'import'
  container  a real file in the workspace's agents directory (created in the
             container, or copied by 'exitbox config import')
  project    the current project's agents directory, for agents whose
             project agents win over user-level ones

A stored subagent is shadowed when a project agent has the same name, or a
container file has the same file name or name.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, err := resolveSubagentsWorkspace(workspace)
			if err != nil {
				return err
			}
			set, err := subagentHarnesses(agent)
			if err != nil {
				return err
			}
			projectDir, _ := os.Getwd()
			var entries []subagents.Entry
			stored := map[*subagents.Harness]int{}
			for _, h := range set {
				es, err := h.Inventory(ws, projectDir)
				if err != nil {
					return err
				}
				for _, e := range es {
					if e.Source == subagents.SourceManaged || e.Source == subagents.SourceImported {
						stored[h]++
					}
				}
				entries = append(entries, es...)
			}
			wsFindings, notes := workspaceNotes(ws, stored)
			out := cmd.OutOrStdout()

			if asJSON {
				return writeSubagentsJSON(out, ws, entries, wsFindings, notes)
			}
			if len(entries) == 0 {
				_, _ = fmt.Fprintf(out, "No subagents in workspace '%s'.\n", ws)
				_, _ = fmt.Fprintf(out, "Create one with: exitbox subagents new <name> --agent <agent> -w %s\n", ws)
			} else {
				tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintln(tw, "AGENT\tNAME\tMODEL\tTOOLS\tSOURCE\tSTATUS\tNOTES")
				for _, e := range entries {
					name, model, tools := "-", "-", "all"
					if e.Def != nil {
						if e.Def.Name != "" {
							name = e.Def.Name
						}
						if e.Def.Model != "" {
							model = e.Def.Model
						}
						if len(e.Def.Tools) > 0 {
							tools = strings.Join(e.Def.Tools, ",")
						}
					}
					if name == "-" {
						name = e.File
					}
					_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", e.Agent, name, model, tools, e.Source, entryStatus(e), entryNote(e))
				}
				if err := tw.Flush(); err != nil {
					return err
				}
			}
			if len(wsFindings)+len(notes) > 0 {
				_, _ = fmt.Fprintln(out)
				printWorkspaceNotes(out, wsFindings, notes)
			}
			return nil
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	addAgentFlag(cmd, &agent, hasSubagents)
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print JSON")
	return cmd
}

func entryNote(e subagents.Entry) string {
	if e.Shadow != "" {
		return "shadowed by " + e.Shadow
	}
	for _, f := range e.Findings {
		if f.Severity == subagents.Error {
			return f.String()
		}
	}
	if len(e.Findings) > 0 {
		return "run 'exitbox subagents validate' for details"
	}
	return ""
}

func writeSubagentsJSON(w io.Writer, ws string, entries []subagents.Entry, wsFindings []subagents.Finding, notes []string) error {
	items := make([]subagentJSON, 0, len(entries))
	for _, e := range entries {
		it := subagentJSON{
			Agent: e.Agent, File: e.File, Path: e.Path, Source: e.Source, Origin: e.Origin,
			Status: entryStatus(e), Findings: e.Findings, ShadowedBy: e.Shadow,
		}
		if it.Findings == nil {
			it.Findings = []subagents.Finding{}
		}
		if e.Def != nil {
			it.Name, it.Description, it.Model, it.Tools = e.Def.Name, e.Def.Description, e.Def.Model, e.Def.Tools
		}
		items = append(items, it)
	}
	if wsFindings == nil {
		wsFindings = []subagents.Finding{}
	}
	if notes == nil {
		notes = []string{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{
		"workspace": ws,
		"subagents": items,
		"findings":  wsFindings,
		"notes":     notes,
	})
}

// --- new ---

func newSubagentsNewCmd() *cobra.Command {
	var workspace, agent, model, tools, description string
	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a subagent and open it in $EDITOR",
		Long: `Create a subagent in the agent's own format, with the fields it needs, open
it in $EDITOR (default vi), and validate it when the editor exits.

Refuses to overwrite an existing subagent; use 'edit' for that. A flag the
agent's format has no field for is refused.

Examples:
  exitbox subagents new code-reviewer --agent claude
  exitbox subagents new log-reader --agent claude --model haiku --tools "Read, Grep" \
    --description "Reads long log files and reports the errors it finds"
  exitbox subagents new pr_explorer --agent codex --description "Read-only codebase explorer"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := resolveSubagentsWorkspace(workspace)
			if err != nil {
				return err
			}
			h, err := pickHarness(agent, hasSubagents)
			if err != nil {
				return err
			}
			data, err := h.Scaffold(subagents.ScaffoldOptions{Name: args[0], Description: description, Tools: tools, Model: model})
			if err != nil {
				return err
			}
			path, err := h.Save(ws, args[0]+h.Ext, data, subagents.SourceManaged, "", false)
			if err != nil {
				if errors.Is(err, subagents.ErrExists) {
					return fmt.Errorf("%w; use 'exitbox subagents edit %s --agent %s'", err, args[0], h.Agent)
				}
				return err
			}
			if err := runEditor(path); err != nil {
				return fmt.Errorf("editor exited with error: %w", err)
			}
			if err := reportStoreFile(cmd, h, ws, path); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n%s\n", path, nextStartNote)
			return nil
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	addAgentFlag(cmd, &agent, hasSubagents)
	cmd.Flags().StringVar(&model, "model", "", "Model, in the form the agent expects")
	cmd.Flags().StringVar(&tools, "tools", "", "Comma-separated tools (default: inherit all)")
	cmd.Flags().StringVar(&description, "description", "", "When the agent should delegate to this subagent")
	return cmd
}

// reportStoreFile validates one stored file in the context of the whole
// store (for duplicate names), prints its findings, and fails on errors.
func reportStoreFile(cmd *cobra.Command, h *subagents.Harness, ws, path string) error {
	entries, err := h.ListStore(ws)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Path != path {
			continue
		}
		printFindings(cmd.ErrOrStderr(), "  ", e.Findings)
		if subagents.HasErrors(e.Findings) {
			return fmt.Errorf("%s has errors; %s will skip it. Fix with: exitbox subagents edit %s --agent %s -w %s",
				e.File, h.Label, strings.TrimSuffix(e.File, h.Ext), h.Agent, ws)
		}
		return nil
	}
	return fmt.Errorf("%s is no longer in the store", path)
}

// --- add / import ---

// printOutcomes reports each file of an add or import. Documentation that
// was skipped gets no line.
func printOutcomes(out, errw io.Writer, outcomes []subagents.Outcome) {
	for _, o := range outcomes {
		switch o.Status {
		case subagents.Added:
			_, _ = fmt.Fprintf(out, "  %s -> %s\n", o.Name, o.File)
			printFindings(errw, "    ", o.Findings)
		case subagents.Skipped:
			if o.Reason != "" {
				_, _ = fmt.Fprintf(out, "  %s: %s\n", o.Source, o.Reason)
			}
		case subagents.Failed:
			_, _ = fmt.Fprintf(errw, "  %s: %s\n", o.Source, o.Reason)
			printFindings(errw, "    ", o.Findings)
		}
	}
}

func newSubagentsAddCmd() *cobra.Command {
	var workspace, agent string
	var force bool
	cmd := &cobra.Command{
		Use:   "add <source>",
		Short: "Add subagents from a file, directory, URL, or GitHub path",
		Long: `Add subagent files to an agent's store.

Sources:
  Local file:        ./code-reviewer.md
  Local directory:   ./agents/        (every file with the agent's extension, recursively)
  Raw URL:           https://example.com/agents/reviewer.md
  GitHub file:       https://github.com/org/repo/blob/main/agents/reviewer.md
  GitHub directory:  https://github.com/org/repo/tree/main/agents

The files must be in the agent's own format: adding does not convert between
agents. Files are stored by subagent name; files that define no subagent are
treated as documentation and skipped. Use --force to replace a stored
subagent with the same name.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := resolveSubagentsWorkspace(workspace)
			if err != nil {
				return err
			}
			h, err := pickHarness(agent, hasSubagents)
			if err != nil {
				return err
			}
			source := args[0]
			files, err := skills.FetchFiles(source, h.Ext)
			if err != nil {
				return err
			}
			single := len(files) == 1
			outcomes := h.StoreFiles(ws, files, func(rel string) string {
				if single {
					return source
				}
				return strings.TrimSuffix(source, "/") + "/" + rel
			}, subagents.SourceManaged, force, false)
			printOutcomes(cmd.OutOrStdout(), cmd.ErrOrStderr(), outcomes)
			return summarize(cmd, h, ws, "Added", outcomes)
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	addAgentFlag(cmd, &agent, hasSubagents)
	cmd.Flags().BoolVar(&force, "force", false, "Replace stored subagents with the same name")
	return cmd
}

func newSubagentsImportCmd() *cobra.Command {
	var workspace, agent string
	var force bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Copy the host's subagents of an agent into the workspace store",
		Long: `Copy every subagent in the agent's user-level agents directory on the host
(for Claude Code: ~/.claude/agents, or $CLAUDE_CONFIG_DIR/agents) into the
workspace store. Subagents whose name is already stored are skipped unless
--force is given.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, err := resolveSubagentsWorkspace(workspace)
			if err != nil {
				return err
			}
			h, err := pickHarness(agent, hasSubagents)
			if err != nil {
				return err
			}
			dir, err := hostAgentsDir(h)
			if err != nil {
				return err
			}
			outcomes, err := h.ImportDir(ws, dir, force)
			if err != nil {
				return err
			}
			if len(outcomes) == 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No %s subagent files in %s.\n", h.Label, dir)
				return nil
			}
			printOutcomes(cmd.OutOrStdout(), cmd.ErrOrStderr(), outcomes)
			return summarize(cmd, h, ws, "Imported", outcomes)
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	addAgentFlag(cmd, &agent, hasSubagents)
	cmd.Flags().BoolVar(&force, "force", false, "Replace stored subagents with the same name")
	return cmd
}

func summarize(cmd *cobra.Command, h *subagents.Harness, ws, verb string, outcomes []subagents.Outcome) error {
	added, skipped, failed := subagents.Tally(outcomes)
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "%s %d %s subagent(s) into workspace '%s'", verb, added, h.Label, ws)
	if skipped > 0 {
		_, _ = fmt.Fprintf(out, ", skipped %d", skipped)
	}
	_, _ = fmt.Fprintln(out, ".")
	if added > 0 {
		_, _ = fmt.Fprintln(out, nextStartNote)
	}
	if failed > 0 {
		return fmt.Errorf("%d file(s) not added", failed)
	}
	return nil
}

// --- edit / show / rm / validate ---

func newSubagentsEditCmd() *cobra.Command {
	var workspace, agent string
	cmd := &cobra.Command{
		Use:               "edit <name>",
		Short:             "Edit a subagent in $EDITOR and validate it",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSubagentNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := resolveSubagentsWorkspace(workspace)
			if err != nil {
				return err
			}
			h, e, err := findStored(ws, agent, args[0])
			if err != nil {
				return err
			}
			if err := runEditor(e.Path); err != nil {
				return fmt.Errorf("editor exited with error: %w", err)
			}
			if err := reportStoreFile(cmd, h, ws, e.Path); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), nextStartNote)
			return nil
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	addAgentFlag(cmd, &agent, hasSubagents)
	return cmd
}

func newSubagentsShowCmd() *cobra.Command {
	var workspace, agent string
	cmd := &cobra.Command{
		Use:               "show <name>",
		Short:             "Print a subagent file",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSubagentNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := resolveSubagentsWorkspace(workspace)
			if err != nil {
				return err
			}
			_, e, err := findStored(ws, agent, args[0])
			if err != nil {
				return err
			}
			data, err := subagents.ReadFile(e.Path)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	addAgentFlag(cmd, &agent, hasSubagents)
	return cmd
}

func newSubagentsRmCmd() *cobra.Command {
	var workspace, agent string
	cmd := &cobra.Command{
		Use:               "rm <name>",
		Aliases:           []string{"remove"},
		Short:             "Remove a subagent from the store",
		Long:              "Remove a subagent from the workspace store. It leaves the container at the next start.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSubagentNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := resolveSubagentsWorkspace(workspace)
			if err != nil {
				return err
			}
			h, _, err := findStored(ws, agent, args[0])
			if err != nil {
				return err
			}
			e, err := h.Remove(ws, args[0])
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed %s subagent %s from workspace '%s'.\n%s\n", h.Label, entryLabel(e), ws, nextStartNote)
			return nil
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	addAgentFlag(cmd, &agent, hasSubagents)
	return cmd
}

// validateLong lists where each agent's rules come from.
func validateLong() string {
	var b strings.Builder
	b.WriteString(`Check stored subagents against the rules their coding agent applies when it
loads agent files. Errors are files the agent skips, mostly without telling
you; warnings are fields it ignores or values that will not do what you
expect.

Without a name, every agent's subagents and master skill are checked. Exits
non-zero when any error is found.

A Claude Code subagent whose model is not a Claude model needs a route in
the claude-router plugin. While that plugin is enabled, a model without a
route is an error. With --probe, each routed backend is also asked for its
context size (llama.cpp: /props, vLLM: /v1/models) and a missing or larger
autoCompactWindow is reported; that contacts the backends from this machine.

Where the rules come from:
`)
	for _, h := range subagents.WithSubagents() {
		b.WriteString(fmt.Sprintf("  %-9s %s (checked against %s)\n", h.Agent, h.DocsURL, h.Checked))
	}
	return strings.TrimRight(b.String(), "\n")
}

func newSubagentsValidateCmd() *cobra.Command {
	var workspace, agent string
	var probe bool
	cmd := &cobra.Command{
		Use:               "validate [name]",
		Short:             "Check subagents against their agent's loading rules",
		Long:              validateLong(),
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeSubagentNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := resolveSubagentsWorkspace(workspace)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			errs := 0
			report := func(label string, fs []subagents.Finding) {
				if len(fs) == 0 {
					_, _ = fmt.Fprintf(out, "%s: ok\n", label)
					return
				}
				_, _ = fmt.Fprintf(out, "%s:\n", label)
				printFindings(out, "  ", fs)
				if subagents.HasErrors(fs) {
					errs++
				}
			}

			// With --probe, a Claude Code subagent on a routed model is also
			// checked against the context its backend serves.
			var prober *routeProber
			if probe {
				prober = newRouteProber(ws)
			}
			checked := func(h *subagents.Harness, e subagents.Entry) []subagents.Finding {
				if prober == nil || h != subagents.Claude {
					return e.Findings
				}
				return append(append([]subagents.Finding(nil), e.Findings...), prober.findings(e.Def)...)
			}

			stored := map[*subagents.Harness]int{}
			if len(args) == 1 {
				h, e, err := findStored(ws, agent, args[0])
				if err != nil {
					return err
				}
				// The store listing carries the duplicate-name findings.
				entries, err := h.ListStore(ws)
				if err != nil {
					return err
				}
				for _, s := range entries {
					if s.Path == e.Path {
						e = s
					}
				}
				stored[h]++
				report(h.Agent+": "+entryLabel(e), checked(h, e))
			} else {
				set, err := subagentHarnesses(agent)
				if err != nil {
					return err
				}
				if agent == "" {
					set = subagents.Harnesses()
				}
				count := 0
				for _, h := range set {
					entries, err := h.ListStore(ws)
					if err != nil {
						return err
					}
					for _, e := range entries {
						stored[h]++
						report(h.Agent+": "+entryLabel(e), checked(h, e))
					}
					count += len(entries)
					if h.MasterSkillExists(ws) {
						fs, err := h.MasterSkillFindings(ws)
						if err != nil {
							return err
						}
						report(h.Agent+": master skill", fs)
						count++
					}
				}
				if count == 0 {
					_, _ = fmt.Fprintf(out, "No subagents in workspace '%s'.\n", ws)
				}
			}
			wsFindings, notes := workspaceNotes(ws, stored)
			if len(wsFindings) > 0 {
				_, _ = fmt.Fprintf(out, "workspace '%s':\n", ws)
				printFindings(out, "  ", wsFindings)
			}
			printWorkspaceNotes(out, nil, notes)
			if errs > 0 {
				return fmt.Errorf("%d file(s) have errors", errs)
			}
			return nil
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	addAgentFlag(cmd, &agent, hasSubagents)
	cmd.Flags().BoolVar(&probe, "probe", false, "Ask each claude-router backend for its context size and compare it with autoCompactWindow")
	return cmd
}

func completeSubagentNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	wsFlag, _ := cmd.Flags().GetString("workspace")
	agentFlag, _ := cmd.Flags().GetString("agent")
	ws, err := resolveSubagentsWorkspace(wsFlag)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	set, err := subagentHarnesses(agentFlag)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	seen := map[string]bool{}
	var out []string
	for _, h := range set {
		for _, n := range h.Names(ws) {
			if strings.HasPrefix(n, toComplete) && !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out, cobra.ShellCompDirectiveNoFileComp
}

func init() {
	rootCmd.AddCommand(newSubagentsCmd())
}
