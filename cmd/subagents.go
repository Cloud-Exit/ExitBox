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
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/env"
	"github.com/cloud-exit/exitbox/internal/profile"
	"github.com/cloud-exit/exitbox/internal/skills"
	"github.com/cloud-exit/exitbox/internal/subagents"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const nextStartNote = "Running sessions pick this up at the next container start."

// hostClaudeAgentsDir is the host's user-level Claude Code agents directory.
var hostClaudeAgentsDir = func() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "agents"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "agents"), nil
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
		Short: "Manage per-workspace Claude Code subagents",
		Long: `Manage the Claude Code subagents (agents/*.md files) of a workspace.

Each workspace has its own subagent store. On container start, ExitBox links
every stored file into ~/.claude/agents/ inside the Claude container. The
store is mounted read-only, so the agent cannot change its own definitions.
Files are checked against the loading rules of Claude Code, which skips a
broken file without telling you.

A subagent is identified by the name in its frontmatter, not its file name.
Changes made on the host reach running sessions at the next container start.`,
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
	var workspace string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List subagents with their model, tools, source and validation status",
		Long: `List the workspace's stored subagents, plus the Claude agent files the
container and the current project already have.

Sources:
  managed    created or added with exitbox subagents
  imported   copied from the host ~/.claude/agents with 'import'
  container  a real file in the workspace's ~/.claude/agents (created in the
             container, or copied by 'exitbox config import claude')
  project    the current project's .claude/agents

A stored subagent is shadowed when a project agent has the same name, or a
container file has the same file name or name.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, err := resolveSubagentsWorkspace(workspace)
			if err != nil {
				return err
			}
			projectDir, _ := os.Getwd()
			entries, err := subagents.Inventory(ws, projectDir)
			if err != nil {
				return err
			}
			wsFindings := workspaceEnvFindings(ws)
			out := cmd.OutOrStdout()

			if asJSON {
				return writeSubagentsJSON(out, ws, entries, wsFindings)
			}
			if len(entries) == 0 {
				_, _ = fmt.Fprintf(out, "No subagents in workspace '%s'.\n", ws)
				_, _ = fmt.Fprintf(out, "Create one with: exitbox subagents new <name> -w %s\n", ws)
			} else {
				tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintln(tw, "NAME\tMODEL\tTOOLS\tSOURCE\tSTATUS\tNOTES")
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
					_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", name, model, tools, e.Source, entryStatus(e), entryNote(e))
				}
				if err := tw.Flush(); err != nil {
					return err
				}
			}
			if len(wsFindings) > 0 {
				_, _ = fmt.Fprintln(out)
				printFindings(out, "", wsFindings)
			}
			return nil
		},
	}
	addWorkspaceFlag(cmd, &workspace)
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

func writeSubagentsJSON(w io.Writer, ws string, entries []subagents.Entry, wsFindings []subagents.Finding) error {
	items := make([]subagentJSON, 0, len(entries))
	for _, e := range entries {
		it := subagentJSON{
			File: e.File, Path: e.Path, Source: e.Source, Origin: e.Origin,
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
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{
		"workspace": ws,
		"subagents": items,
		"findings":  wsFindings,
	})
}

// --- new ---

type scaffoldFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Tools       string `yaml:"tools,omitempty"`
	Model       string `yaml:"model,omitempty"`
}

func scaffoldSubagent(name, description, tools, model string) ([]byte, error) {
	if description == "" {
		description = "Describe when Claude should delegate to this subagent."
	}
	fm, err := yaml.Marshal(scaffoldFrontmatter{Name: name, Description: description, Tools: tools, Model: model})
	if err != nil {
		return nil, err
	}
	body := "You are " + name + ". Describe the role, the steps to follow, and what to return.\n"
	return []byte("---\n" + string(fm) + "---\n\n" + body), nil
}

func newSubagentsNewCmd() *cobra.Command {
	var workspace, model, tools, description string
	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a subagent and open it in $EDITOR",
		Long: `Create a subagent with valid frontmatter in the workspace store, open it in
$EDITOR (default vi), and validate it when the editor exits.

Refuses to overwrite an existing subagent; use 'edit' for that.

Examples:
  exitbox subagents new code-reviewer
  exitbox subagents new log-reader --model haiku --tools "Read, Grep" \
    --description "Reads long log files and reports the errors it finds"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := resolveSubagentsWorkspace(workspace)
			if err != nil {
				return err
			}
			data, err := scaffoldSubagent(args[0], description, tools, model)
			if err != nil {
				return err
			}
			path, err := subagents.Save(ws, data, subagents.SourceManaged, "", false)
			if err != nil {
				if errors.Is(err, subagents.ErrExists) {
					return fmt.Errorf("%w; use 'exitbox subagents edit %s'", err, args[0])
				}
				return err
			}
			if err := runEditor(path); err != nil {
				return fmt.Errorf("editor exited with error: %w", err)
			}
			if err := reportStoreFile(cmd, ws, path); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n%s\n", path, nextStartNote)
			return nil
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	cmd.Flags().StringVar(&model, "model", "", "Model: sonnet, opus, haiku, fable, inherit, or a claude-* ID")
	cmd.Flags().StringVar(&tools, "tools", "", "Comma-separated tools (default: inherit all)")
	cmd.Flags().StringVar(&description, "description", "", "When Claude should delegate to this subagent")
	return cmd
}

// reportStoreFile validates one stored file in the context of the whole
// store (for duplicate names), prints its findings, and fails on errors.
func reportStoreFile(cmd *cobra.Command, ws, path string) error {
	entries, err := subagents.ListStore(ws)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Path != path {
			continue
		}
		printFindings(cmd.ErrOrStderr(), "  ", e.Findings)
		if subagents.HasErrors(e.Findings) {
			return fmt.Errorf("%s has errors; Claude Code will skip it. Fix with: exitbox subagents edit %s -w %s",
				e.File, strings.TrimSuffix(e.File, ".md"), ws)
		}
		return nil
	}
	return fmt.Errorf("%s is no longer in the store", path)
}

// --- add / import ---

type addResult struct {
	added, skipped, failed int
}

// storeFiles saves fetched files into the store. Files without a name are
// documentation and skipped quietly; missing names in a single-file source
// are an error.
func storeFiles(cmd *cobra.Command, ws string, files map[string][]byte, origin func(rel string) string,
	src subagents.Source, force, skipExisting bool) addResult {
	var res addResult
	out, errw := cmd.OutOrStdout(), cmd.ErrOrStderr()
	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	seen := map[string]string{}
	for _, rel := range rels {
		data := files[rel]
		if len(data) > subagents.MaxFileSize {
			_, _ = fmt.Fprintf(errw, "  %s: larger than %d bytes, skipped\n", rel, subagents.MaxFileSize)
			res.failed++
			continue
		}
		def, findings := subagents.Parse(data)
		if def == nil || def.Name == "" {
			if len(files) == 1 {
				_, _ = fmt.Fprintf(errw, "  %s: not a subagent\n", rel)
				printFindings(errw, "    ", findings)
				res.failed++
			} else {
				res.skipped++
			}
			continue
		}
		if prev, ok := seen[def.Name]; ok {
			_, _ = fmt.Fprintf(errw, "  %s: name %q is also used by %s in this source, skipped\n", rel, def.Name, prev)
			res.failed++
			continue
		}
		seen[def.Name] = rel
		path, err := subagents.Save(ws, data, src, origin(rel), force)
		var inv *subagents.InvalidError
		switch {
		case errors.As(err, &inv):
			_, _ = fmt.Fprintf(errw, "  %s: invalid, skipped\n", rel)
			printFindings(errw, "    ", inv.Findings)
			res.failed++
			continue
		case errors.Is(err, subagents.ErrExists) && skipExisting:
			_, _ = fmt.Fprintf(out, "  %s: %v, skipped (use --force to replace)\n", rel, err)
			res.skipped++
			continue
		case errors.Is(err, subagents.ErrExists):
			_, _ = fmt.Fprintf(errw, "  %s: %v; use --force to replace\n", rel, err)
			res.failed++
			continue
		case err != nil:
			_, _ = fmt.Fprintf(errw, "  %s: %v\n", rel, err)
			res.failed++
			continue
		}
		_, _ = fmt.Fprintf(out, "  %s -> %s\n", def.Name, filepath.Base(path))
		printFindings(errw, "    ", findings)
		res.added++
	}
	return res
}

func newSubagentsAddCmd() *cobra.Command {
	var workspace string
	var force bool
	cmd := &cobra.Command{
		Use:   "add <source>",
		Short: "Add subagents from a file, directory, URL, or GitHub path",
		Long: `Add subagent files to the workspace store.

Sources:
  Local file:        ./code-reviewer.md
  Local directory:   ./agents/        (every *.md with a name, recursively)
  Raw URL:           https://example.com/agents/reviewer.md
  GitHub file:       https://github.com/org/repo/blob/main/agents/reviewer.md
  GitHub directory:  https://github.com/org/repo/tree/main/agents

Files are stored as <name>.md by their frontmatter name. Files without a name
are treated as documentation and skipped. Use --force to replace a stored
subagent with the same name.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := resolveSubagentsWorkspace(workspace)
			if err != nil {
				return err
			}
			source := args[0]
			files, err := skills.FetchMarkdown(source)
			if err != nil {
				return err
			}
			single := len(files) == 1
			res := storeFiles(cmd, ws, files, func(rel string) string {
				if single {
					return source
				}
				return strings.TrimSuffix(source, "/") + "/" + rel
			}, subagents.SourceManaged, force, false)
			return summarize(cmd, ws, "Added", res)
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	cmd.Flags().BoolVar(&force, "force", false, "Replace stored subagents with the same name")
	return cmd
}

func newSubagentsImportCmd() *cobra.Command {
	var workspace string
	var force bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Copy the host's ~/.claude/agents into the workspace store",
		Long: `Copy every subagent in the host's ~/.claude/agents (or $CLAUDE_CONFIG_DIR/agents)
into the workspace store. Subagents whose name is already stored are skipped
unless --force is given.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, err := resolveSubagentsWorkspace(workspace)
			if err != nil {
				return err
			}
			dir, err := hostClaudeAgentsDir()
			if err != nil {
				return err
			}
			entries, err := subagents.ScanDir(dir, subagents.SourceImported, false)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No subagent files in %s.\n", dir)
				return nil
			}
			files := map[string][]byte{}
			for _, e := range entries {
				data, err := subagents.ReadFile(e.Path)
				if err != nil {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "  %s: %v\n", e.File, err)
					continue
				}
				files[e.File] = data
			}
			res := storeFiles(cmd, ws, files, func(rel string) string {
				return filepath.Join(dir, filepath.FromSlash(rel))
			}, subagents.SourceImported, force, true)
			return summarize(cmd, ws, "Imported", res)
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	cmd.Flags().BoolVar(&force, "force", false, "Replace stored subagents with the same name")
	return cmd
}

func summarize(cmd *cobra.Command, ws, verb string, res addResult) error {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "%s %d subagent(s) into workspace '%s'", verb, res.added, ws)
	if res.skipped > 0 {
		_, _ = fmt.Fprintf(out, ", skipped %d", res.skipped)
	}
	_, _ = fmt.Fprintln(out, ".")
	if res.added > 0 {
		_, _ = fmt.Fprintln(out, nextStartNote)
	}
	if res.failed > 0 {
		return fmt.Errorf("%d file(s) not added", res.failed)
	}
	return nil
}

// --- edit / show / rm / validate ---

func newSubagentsEditCmd() *cobra.Command {
	var workspace string
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
			e, err := subagents.Find(ws, args[0])
			if err != nil {
				return err
			}
			if err := runEditor(e.Path); err != nil {
				return fmt.Errorf("editor exited with error: %w", err)
			}
			if err := reportStoreFile(cmd, ws, e.Path); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), nextStartNote)
			return nil
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	return cmd
}

func newSubagentsShowCmd() *cobra.Command {
	var workspace string
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
			e, err := subagents.Find(ws, args[0])
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
	return cmd
}

func newSubagentsRmCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:               "rm <name>",
		Aliases:           []string{"remove"},
		Short:             "Remove a subagent from the store",
		Long:              "Remove a subagent from the workspace store. Its link in the container is dropped at the next start.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeSubagentNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := resolveSubagentsWorkspace(workspace)
			if err != nil {
				return err
			}
			e, err := subagents.Remove(ws, args[0])
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed %s from workspace '%s'.\n%s\n", entryLabel(e), ws, nextStartNote)
			return nil
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	return cmd
}

func newSubagentsValidateCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "validate [name]",
		Short: "Check subagents against Claude Code's loading rules",
		Long: fmt.Sprintf(`Check stored subagents against the rules Claude Code applies when it loads
agent files. Errors are files Claude Code skips without telling you; warnings
are fields it ignores or values that will not do what you expect.

Exits non-zero when any error is found.

Rules follow %s (checked against Claude Code %s).`, subagents.DocsURL, subagents.CheckedVersion),
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeSubagentNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := resolveSubagentsWorkspace(workspace)
			if err != nil {
				return err
			}
			entries, err := subagents.ListStore(ws)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				e, err := subagents.Find(ws, args[0])
				if err != nil {
					return err
				}
				for _, s := range entries {
					if s.Path == e.Path {
						e = s // keeps duplicate-name findings
					}
				}
				entries = []subagents.Entry{e}
			}
			out := cmd.OutOrStdout()
			if len(entries) == 0 {
				_, _ = fmt.Fprintf(out, "No subagents in workspace '%s'.\n", ws)
			}
			errs := 0
			for _, e := range entries {
				if len(e.Findings) == 0 {
					_, _ = fmt.Fprintf(out, "%s: ok\n", entryLabel(e))
					continue
				}
				_, _ = fmt.Fprintf(out, "%s:\n", entryLabel(e))
				printFindings(out, "  ", e.Findings)
				if subagents.HasErrors(e.Findings) {
					errs++
				}
			}
			if wsFindings := workspaceEnvFindings(ws); len(wsFindings) > 0 {
				_, _ = fmt.Fprintf(out, "workspace '%s':\n", ws)
				printFindings(out, "  ", wsFindings)
			}
			if errs > 0 {
				return fmt.Errorf("%d subagent file(s) have errors", errs)
			}
			return nil
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	return cmd
}

func completeSubagentNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	flag, _ := cmd.Flags().GetString("workspace")
	ws, err := resolveSubagentsWorkspace(flag)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, n := range subagents.Names(ws) {
		if strings.HasPrefix(n, toComplete) {
			out = append(out, n)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

func init() {
	rootCmd.AddCommand(newSubagentsCmd())
}
