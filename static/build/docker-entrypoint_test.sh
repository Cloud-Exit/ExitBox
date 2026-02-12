#!/usr/bin/env bash
# Tests for docker-entrypoint shell functions.
# Run: bash static/build/docker-entrypoint_test.sh

PASS=0
FAIL=0
ERRORS=()

assert_eq() {
    local desc="$1" expected="$2" actual="$3"
    if [[ "$expected" == "$actual" ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: $desc: expected '$expected', got '$actual'")
    fi
}

assert_contains() {
    local desc="$1" haystack="$2" needle="$3"
    if [[ "$haystack" == *"$needle"* ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: $desc: expected output to contain '$needle'")
    fi
}

assert_file_content() {
    local desc="$1" filepath="$2" expected="$3"
    if [[ -f "$filepath" ]]; then
        local actual
        actual="$(cat "$filepath")"
        assert_eq "$desc" "$expected" "$actual"
    else
        ((FAIL++))
        ERRORS+=("FAIL: $desc: file $filepath does not exist")
    fi
}

assert_file_missing() {
    local desc="$1" filepath="$2"
    if [[ ! -f "$filepath" ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: $desc: file $filepath should not exist")
    fi
}

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ENTRYPOINT="$SCRIPT_DIR/docker-entrypoint"
TEST_TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TEST_TMPDIR"' EXIT

# Clear env vars that might leak from a host ExitBox sandbox and affect tests.
unset EXITBOX_PROJECT_KEY EXITBOX_WORKSPACE_NAME EXITBOX_WORKSPACE_SCOPE
unset EXITBOX_AGENT EXITBOX_AUTO_RESUME EXITBOX_IPC_SOCKET EXITBOX_KEYBINDINGS
unset EXITBOX_SESSION_NAME EXITBOX_RESUME_TOKEN

# Extract functions from the entrypoint using awk (handles nested braces)
extract_func() {
    local func_name="$1"
    awk "/^${func_name}\\(\\)/{found=1; depth=0} found{
        for(i=1;i<=length(\$0);i++){
            c=substr(\$0,i,1)
            if(c==\"{\") depth++
            if(c==\"}\") depth--
        }
        print
        if(found && depth==0) exit
    }" "$ENTRYPOINT"
}

PARSE_KB_FUNC="$(extract_func parse_keybindings)"
CAPTURE_FUNC="$(extract_func capture_resume_token)"
BUILD_FUNC="$(extract_func build_resume_args)"
DISPLAY_FUNC="$(extract_func agent_display_name)"
TMUX_CONF_FUNC="$(extract_func write_tmux_conf)"
DEFAULT_SESSION_NAME_FUNC="$(extract_func default_session_name)"
CURRENT_SESSION_NAME_FUNC="$(extract_func current_session_name)"
EFFECTIVE_SESSION_NAME_FUNC="$(extract_func effective_session_name)"
PROJECT_RESUME_DIR_FUNC="$(extract_func project_resume_dir)"
ACTIVE_SESSION_FILE_FUNC="$(extract_func active_session_file)"
SET_ACTIVE_SESSION_NAME_FUNC="$(extract_func set_active_session_name)"
GET_ACTIVE_SESSION_NAME_FUNC="$(extract_func get_active_session_name)"
SESSION_KEY_FOR_NAME_FUNC="$(extract_func session_key_for_name)"
SESSION_DIR_FOR_NAME_FUNC="$(extract_func session_dir_for_name)"
ENSURE_NAMED_SESSION_DIR_FUNC="$(extract_func ensure_named_session_dir)"
LEGACY_RESUME_FILE_FUNC="$(extract_func legacy_resume_file)"

SESSION_HELPER_FUNCS="${DEFAULT_SESSION_NAME_FUNC}
${CURRENT_SESSION_NAME_FUNC}
${EFFECTIVE_SESSION_NAME_FUNC}
${PROJECT_RESUME_DIR_FUNC}
${ACTIVE_SESSION_FILE_FUNC}
${SET_ACTIVE_SESSION_NAME_FUNC}
${GET_ACTIVE_SESSION_NAME_FUNC}
${SESSION_KEY_FOR_NAME_FUNC}
${SESSION_DIR_FOR_NAME_FUNC}
${ENSURE_NAMED_SESSION_DIR_FUNC}
${LEGACY_RESUME_FILE_FUNC}"

session_key_for_test() {
    local name="$1"
    local slug hash
    slug="$(printf '%s' "$name" | tr -c 'A-Za-z0-9._-' '_' | sed 's/^_\\+//; s/_\\+$//; s/_\\+/_/g')"
    if [[ -z "$slug" ]]; then
        slug="session"
    fi
    hash="$(printf '%s' "$name" | cksum | awk '{print $1}')"
    printf '%s_%s' "$slug" "$hash"
}

project_resume_dir_for_test() {
    local root="$1" workspace="$2" agent="$3" project_key="$4"
    local dir="${root}/${workspace}/${agent}"
    if [[ -n "$project_key" ]]; then
        dir="${dir}/projects/${project_key}"
    fi
    printf '%s' "$dir"
}

session_token_file_for_test() {
    local root="$1" workspace="$2" agent="$3" session_name="$4" project_key="${5:-}"
    local key
    key="$(session_key_for_test "$session_name")"
    printf '%s/sessions/%s/.resume-token' "$(project_resume_dir_for_test "$root" "$workspace" "$agent" "$project_key")" "$key"
}

session_name_file_for_test() {
    local root="$1" workspace="$2" agent="$3" session_name="$4" project_key="${5:-}"
    local key
    key="$(session_key_for_test "$session_name")"
    printf '%s/sessions/%s/.name' "$(project_resume_dir_for_test "$root" "$workspace" "$agent" "$project_key")" "$key"
}

# ============================================================================
# Test: capture_resume_token for Claude
# ============================================================================
test_capture_resume_token_claude() {
    local tmpdir="$TEST_TMPDIR/crt_claude"
    mkdir -p "$tmpdir"
    local session_name="session-alpha"

    local result
    result="$(
        AGENT="claude"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_SESSION_NAME="$session_name"
        tmux() { echo "some output"; echo "claude --resume abc123def"; echo "more"; }
        eval "$SESSION_HELPER_FUNCS"
        eval "$CAPTURE_FUNC"
        capture_resume_token
    )" 2>/dev/null

    local token_file
    token_file="$(session_token_file_for_test "$tmpdir" "default" "claude" "$session_name")"
    local name_file
    name_file="$(session_name_file_for_test "$tmpdir" "default" "claude" "$session_name")"
    assert_file_content "capture_resume_token (claude)" \
        "$token_file" "abc123def"
    assert_file_content "capture_resume_token (claude session name marker)" \
        "$name_file" "$session_name"
}

# ============================================================================
# Test: capture_resume_token for Claude with -r flag
# ============================================================================
test_capture_resume_token_claude_short() {
    local tmpdir="$TEST_TMPDIR/crt_claude_short"
    mkdir -p "$tmpdir"
    local session_name="session-short"

    local result
    result="$(
        AGENT="claude"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_SESSION_NAME="$session_name"
        tmux() { echo "claude -r shorttoken456"; }
        eval "$SESSION_HELPER_FUNCS"
        eval "$CAPTURE_FUNC"
        capture_resume_token
    )" 2>/dev/null

    local token_file
    token_file="$(session_token_file_for_test "$tmpdir" "default" "claude" "$session_name")"
    assert_file_content "capture_resume_token (claude -r)" \
        "$token_file" "shorttoken456"
}

# ============================================================================
# Test: capture_resume_token for Codex (should write "last")
# ============================================================================
test_capture_resume_token_codex() {
    local tmpdir="$TEST_TMPDIR/crt_codex"
    mkdir -p "$tmpdir"
    local session_name="session-codex"

    local result
    result="$(
        AGENT="codex"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_SESSION_NAME="$session_name"
        tmux() { echo "some codex output"; }
        eval "$SESSION_HELPER_FUNCS"
        eval "$CAPTURE_FUNC"
        capture_resume_token
    )" 2>/dev/null

    local token_file
    token_file="$(session_token_file_for_test "$tmpdir" "default" "codex" "$session_name")"
    assert_file_content "capture_resume_token (codex)" \
        "$token_file" "last"
}

# ============================================================================
# Test: capture_resume_token for OpenCode (should write "last")
# ============================================================================
test_capture_resume_token_opencode() {
    local tmpdir="$TEST_TMPDIR/crt_opencode"
    mkdir -p "$tmpdir"
    local session_name="session-opencode"

    local result
    result="$(
        AGENT="opencode"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_SESSION_NAME="$session_name"
        tmux() { echo "some opencode output"; }
        eval "$SESSION_HELPER_FUNCS"
        eval "$CAPTURE_FUNC"
        capture_resume_token
    )" 2>/dev/null

    local token_file
    token_file="$(session_token_file_for_test "$tmpdir" "default" "opencode" "$session_name")"
    assert_file_content "capture_resume_token (opencode)" \
        "$token_file" "last"
}

# ============================================================================
# Test: capture_resume_token always captures (even when auto-resume is off)
# Token is always saved so the user can use explicit --resume later.
# ============================================================================
test_capture_resume_token_always() {
    local tmpdir="$TEST_TMPDIR/crt_always"
    mkdir -p "$tmpdir"
    local session_name="session-always"

    local result
    result="$(
        AGENT="claude"
        EXITBOX_AUTO_RESUME="false"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_SESSION_NAME="$session_name"
        tmux() { echo "claude --resume alwayscaptured"; }
        eval "$SESSION_HELPER_FUNCS"
        eval "$CAPTURE_FUNC"
        capture_resume_token
    )" 2>/dev/null

    local token_file
    token_file="$(session_token_file_for_test "$tmpdir" "default" "claude" "$session_name")"
    assert_file_content "capture_resume_token (always captures)" \
        "$token_file" "alwayscaptured"
}

# ============================================================================
# Test: build_resume_args for Claude
# ============================================================================
test_build_resume_args_claude() {
    local tmpdir="$TEST_TMPDIR/bra_claude"
    local session_name="session-build-claude"
    local token_file
    token_file="$(session_token_file_for_test "$tmpdir" "default" "claude" "$session_name")"
    mkdir -p "$(dirname "$token_file")"
    echo "mytoken123" > "$token_file"

    local result
    result="$(
        AGENT="claude"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_SESSION_NAME="$session_name"
        eval "$SESSION_HELPER_FUNCS"
        eval "$BUILD_FUNC"
        build_resume_args
        echo "${RESUME_ARGS[*]}"
    )" 2>/dev/null

    assert_eq "build_resume_args (claude)" "--resume mytoken123" "$result"
}

# ============================================================================
# Test: build_resume_args for Codex
# ============================================================================
test_build_resume_args_codex() {
    local tmpdir="$TEST_TMPDIR/bra_codex"
    local session_name="session-build-codex"
    local token_file
    token_file="$(session_token_file_for_test "$tmpdir" "default" "codex" "$session_name")"
    mkdir -p "$(dirname "$token_file")"
    echo "last" > "$token_file"

    local result
    result="$(
        AGENT="codex"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_SESSION_NAME="$session_name"
        eval "$SESSION_HELPER_FUNCS"
        eval "$BUILD_FUNC"
        build_resume_args
        echo "${RESUME_ARGS[*]}"
    )" 2>/dev/null

    assert_eq "build_resume_args (codex)" "resume --last" "$result"
}

# ============================================================================
# Test: build_resume_args for OpenCode
# ============================================================================
test_build_resume_args_opencode() {
    local tmpdir="$TEST_TMPDIR/bra_opencode"
    local session_name="session-build-opencode"
    local token_file
    token_file="$(session_token_file_for_test "$tmpdir" "default" "opencode" "$session_name")"
    mkdir -p "$(dirname "$token_file")"
    echo "last" > "$token_file"

    local result
    result="$(
        AGENT="opencode"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_SESSION_NAME="$session_name"
        eval "$SESSION_HELPER_FUNCS"
        eval "$BUILD_FUNC"
        build_resume_args
        echo "${RESUME_ARGS[*]}"
    )" 2>/dev/null

    assert_eq "build_resume_args (opencode)" "--continue" "$result"
}

# ============================================================================
# Test: build_resume_args disabled does not use token (but file remains)
# ============================================================================
test_build_resume_args_disabled() {
    local tmpdir="$TEST_TMPDIR/bra_disabled"
    local session_name="session-build-disabled"
    local token_file
    token_file="$(session_token_file_for_test "$tmpdir" "default" "claude" "$session_name")"
    mkdir -p "$(dirname "$token_file")"
    echo "oldtoken" > "$token_file"

    local result
    result="$(
        AGENT="claude"
        EXITBOX_AUTO_RESUME="false"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_SESSION_NAME="$session_name"
        eval "$SESSION_HELPER_FUNCS"
        eval "$BUILD_FUNC"
        build_resume_args
        echo "${RESUME_ARGS[*]}"
    )" 2>/dev/null

    assert_eq "build_resume_args (disabled gives empty args)" "" "$result"
}

# ============================================================================
# Test: build_resume_args with no token file
# ============================================================================
test_build_resume_args_no_token() {
    local tmpdir="$TEST_TMPDIR/bra_notoken"
    local session_name="session-build-notoken"

    local result
    result="$(
        AGENT="claude"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_SESSION_NAME="$session_name"
        eval "$SESSION_HELPER_FUNCS"
        eval "$BUILD_FUNC"
        build_resume_args
        echo "${RESUME_ARGS[*]}"
    )" 2>/dev/null

    assert_eq "build_resume_args (no token)" "" "$result"
}

# ============================================================================
# Test: capture_resume_token is scoped by project key
# ============================================================================
test_capture_resume_token_project_scoped() {
    local tmpdir="$TEST_TMPDIR/crt_project_scoped"
    mkdir -p "$tmpdir"
    local session_name="project-scoped-session"

    local result
    result="$(
        AGENT="codex"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_SESSION_NAME="$session_name"
        EXITBOX_PROJECT_KEY="project_a"
        tmux() { echo "some codex output"; }
        eval "$SESSION_HELPER_FUNCS"
        eval "$CAPTURE_FUNC"
        capture_resume_token
    )" 2>/dev/null

    local token_file
    token_file="$(session_token_file_for_test "$tmpdir" "default" "codex" "$session_name" "project_a")"
    assert_file_content "capture_resume_token (project scoped)" \
        "$token_file" "last"
    assert_file_missing "capture_resume_token (project scoped avoids legacy path)" \
        "$tmpdir/default/codex/.resume-token"
}

# ============================================================================
# Test: build_resume_args with explicit --name does NOT fall back to legacy token
# A new named session must start fresh, not resume some other session's token.
# ============================================================================
test_build_resume_args_named_session_no_legacy_fallback() {
    local tmpdir="$TEST_TMPDIR/bra_named_no_legacy"
    local session_name="brand-new-session"
    mkdir -p "$tmpdir/default/codex/projects/project_b"
    echo "last" > "$tmpdir/default/codex/projects/project_b/.resume-token"

    local result
    result="$(
        AGENT="codex"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_SESSION_NAME="$session_name"
        EXITBOX_PROJECT_KEY="project_b"
        eval "$SESSION_HELPER_FUNCS"
        eval "$BUILD_FUNC"
        build_resume_args
        echo "${RESUME_ARGS[*]}"
    )" 2>/dev/null

    assert_eq "build_resume_args (named session ignores legacy)" "" "$result"
}

# ============================================================================
# Test: build_resume_args with NO session name falls back to .active-session + legacy
# This is the backward-compat path for pre-session users.
# ============================================================================
test_build_resume_args_active_session_legacy_fallback() {
    local tmpdir="$TEST_TMPDIR/bra_active_legacy"
    local active_name="old-session"
    mkdir -p "$tmpdir/default/codex/projects/project_d"
    echo "last" > "$tmpdir/default/codex/projects/project_d/.resume-token"
    echo "$active_name" > "$tmpdir/default/codex/projects/project_d/.active-session"

    local result
    result="$(
        AGENT="codex"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_SESSION_NAME=""
        EXITBOX_PROJECT_KEY="project_d"
        eval "$SESSION_HELPER_FUNCS"
        eval "$BUILD_FUNC"
        build_resume_args
        echo "${RESUME_ARGS[*]}"
    )" 2>/dev/null

    assert_eq "build_resume_args (active-session legacy fallback)" "resume --last" "$result"
}

# ============================================================================
# Test: build_resume_args with project key reads matching scoped token
# ============================================================================
test_build_resume_args_project_scoped_reads_scoped_token() {
    local tmpdir="$TEST_TMPDIR/bra_project_scope_reads_scoped"
    local session_name="project-read-scoped"
    local token_file
    token_file="$(session_token_file_for_test "$tmpdir" "default" "codex" "$session_name" "project_c")"
    mkdir -p "$(dirname "$token_file")"
    echo "last" > "$token_file"

    local result
    result="$(
        AGENT="codex"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_SESSION_NAME="$session_name"
        EXITBOX_PROJECT_KEY="project_c"
        eval "$SESSION_HELPER_FUNCS"
        eval "$BUILD_FUNC"
        build_resume_args
        echo "${RESUME_ARGS[*]}"
    )" 2>/dev/null

    assert_eq "build_resume_args (project scoped reads scoped token)" "resume --last" "$result"
}

# ============================================================================
# Test: write_tmux_conf includes scrolling settings
# ============================================================================
test_write_tmux_conf_scroll_settings() {
    local output
    output="$(
        AGENT="codex"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_VERSION="test"
        EXITBOX_STATUS_BAR="true"
        unset EXITBOX_KEYBINDINGS
        eval "$PARSE_KB_FUNC"
        parse_keybindings
        eval "$DISPLAY_FUNC"
        eval "$TMUX_CONF_FUNC"
        conf_path="$(write_tmux_conf)"
        cat "$conf_path"
    )" 2>/dev/null

    assert_contains "write_tmux_conf enables mouse scrolling" "$output" 'set -g mouse on'
    assert_contains "write_tmux_conf sets large history" "$output" 'set -g history-limit 100000'
    assert_contains "write_tmux_conf shows workspace shortcut" "$output" 'C-M-p: workspaces'
    assert_contains "write_tmux_conf shows session shortcut" "$output" 'C-M-s: sessions'
}

# ============================================================================
# Test: parse_keybindings defaults (no env var)
# ============================================================================
test_parse_keybindings_default() {
    local result
    result="$(
        unset EXITBOX_KEYBINDINGS
        eval "$PARSE_KB_FUNC"
        parse_keybindings
        echo "wm=$KB_WORKSPACE_MENU sm=$KB_SESSION_MENU"
    )" 2>/dev/null

    assert_eq "parse_keybindings default" "wm=C-M-p sm=C-M-s" "$result"
}

# ============================================================================
# Test: parse_keybindings custom values
# ============================================================================
test_parse_keybindings_custom() {
    local result
    result="$(
        EXITBOX_KEYBINDINGS="workspace_menu=F1,session_menu=F2"
        eval "$PARSE_KB_FUNC"
        parse_keybindings
        echo "wm=$KB_WORKSPACE_MENU sm=$KB_SESSION_MENU"
    )" 2>/dev/null

    assert_eq "parse_keybindings custom" "wm=F1 sm=F2" "$result"
}

# ============================================================================
# Test: parse_keybindings partial override
# ============================================================================
test_parse_keybindings_partial() {
    local result
    result="$(
        EXITBOX_KEYBINDINGS="session_menu=C-b"
        eval "$PARSE_KB_FUNC"
        parse_keybindings
        echo "wm=$KB_WORKSPACE_MENU sm=$KB_SESSION_MENU"
    )" 2>/dev/null

    assert_eq "parse_keybindings partial" "wm=C-M-p sm=C-b" "$result"
}

# ============================================================================
# Test: write_tmux_conf uses dynamic keybinding labels
# ============================================================================
test_write_tmux_conf_dynamic_keybindings() {
    local output
    output="$(
        AGENT="codex"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_VERSION="test"
        EXITBOX_STATUS_BAR="true"
        EXITBOX_KEYBINDINGS="workspace_menu=F5,session_menu=F6"
        eval "$PARSE_KB_FUNC"
        parse_keybindings
        eval "$DISPLAY_FUNC"
        eval "$TMUX_CONF_FUNC"
        conf_path="$(write_tmux_conf)"
        cat "$conf_path"
    )" 2>/dev/null

    assert_contains "write_tmux_conf dynamic workspace key" "$output" 'F5: workspaces'
    assert_contains "write_tmux_conf dynamic session key" "$output" 'F6: sessions'
}

# ============================================================================
# Run all tests
# ============================================================================

echo "Running docker-entrypoint tests..."
echo ""

test_capture_resume_token_claude
test_capture_resume_token_claude_short
test_capture_resume_token_codex
test_capture_resume_token_opencode
test_capture_resume_token_always
test_build_resume_args_claude
test_build_resume_args_codex
test_build_resume_args_opencode
test_build_resume_args_disabled
test_build_resume_args_no_token
test_capture_resume_token_project_scoped
test_build_resume_args_named_session_no_legacy_fallback
test_build_resume_args_active_session_legacy_fallback
test_build_resume_args_project_scoped_reads_scoped_token
test_write_tmux_conf_scroll_settings
test_parse_keybindings_default
test_parse_keybindings_custom
test_parse_keybindings_partial
test_write_tmux_conf_dynamic_keybindings

# ============================================================================
# Results
# ============================================================================

echo ""
echo "Results: $PASS passed, $FAIL failed"

if [[ ${#ERRORS[@]} -gt 0 ]]; then
    echo ""
    for err in "${ERRORS[@]}"; do
        echo "  $err"
    done
    exit 1
fi

echo "All tests passed!"
exit 0
