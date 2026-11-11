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
unset EXITBOX_SESSION_NAME EXITBOX_RESUME_TOKEN EXITBOX_VAULT_ENABLED
unset EXITBOX_VAULT_READONLY EXITBOX_RTK EXITBOX_HOST_PORTS

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
TMUX_EXTKEYS_FUNC="$(extract_func tmux_supports_extended_keys_format)"
LINK_PATH_FUNC="$(extract_func link_path)"
DEFAULT_SESSION_NAME_FUNC="$(extract_func default_session_name)"
CURRENT_SESSION_NAME_FUNC="$(extract_func current_session_name)"
EFFECTIVE_SESSION_NAME_FUNC="$(extract_func effective_session_name)"
PROJECT_RESUME_DIR_FUNC="$(extract_func project_resume_dir)"
ACTIVE_SESSION_FILE_FUNC="$(extract_func active_session_file)"
KV_SESSION_PREFIX_FUNC="$(extract_func kv_session_prefix)"
SET_ACTIVE_SESSION_NAME_FUNC="$(extract_func set_active_session_name)"
GET_ACTIVE_SESSION_NAME_FUNC="$(extract_func get_active_session_name)"
SESSION_KEY_FOR_NAME_FUNC="$(extract_func session_key_for_name)"
SESSION_DIR_FOR_NAME_FUNC="$(extract_func session_dir_for_name)"
ENSURE_NAMED_SESSION_DIR_FUNC="$(extract_func ensure_named_session_dir)"
LEGACY_RESUME_FILE_FUNC="$(extract_func legacy_resume_file)"
CODEX_SESSION_STORAGE_NAME_FUNC="$(extract_func codex_session_storage_name)"
HAS_AUTH_ACTION_FUNC="$(extract_func has_auth_action)"
CODEX_USES_DIRECT_HOME_FUNC="$(extract_func codex_uses_direct_home)"
CONFIGURE_CODEX_SESSION_STORAGE_FUNC="$(extract_func configure_codex_session_storage)"
SHOULD_EXEC_RAW_FUNC="$(extract_func should_exec_raw)"
RUN_RAW_COMMAND_FUNC="$(extract_func run_raw_command)"
BUILD_AGENT_LOOP_COMMAND_FUNC="$(extract_func build_agent_loop_command)"
CLEAR_STALE_FUNC="$(extract_func clear_stale_resume_token)"

SESSION_HELPER_FUNCS="${DEFAULT_SESSION_NAME_FUNC}
${CURRENT_SESSION_NAME_FUNC}
${EFFECTIVE_SESSION_NAME_FUNC}
${PROJECT_RESUME_DIR_FUNC}
${ACTIVE_SESSION_FILE_FUNC}
${KV_SESSION_PREFIX_FUNC}
${SET_ACTIVE_SESSION_NAME_FUNC}
${GET_ACTIVE_SESSION_NAME_FUNC}
${SESSION_KEY_FOR_NAME_FUNC}
${SESSION_DIR_FOR_NAME_FUNC}
${ENSURE_NAMED_SESSION_DIR_FUNC}
${LEGACY_RESUME_FILE_FUNC}
${HAS_AUTH_ACTION_FUNC}
${CODEX_USES_DIRECT_HOME_FUNC}"

CODEX_SESSION_HELPERS="${LINK_PATH_FUNC}
${SESSION_HELPER_FUNCS}
${CODEX_SESSION_STORAGE_NAME_FUNC}
${CONFIGURE_CODEX_SESSION_STORAGE_FUNC}"

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
# Test: configure_codex_session_storage isolates named sessions
# ============================================================================
test_configure_codex_session_storage_isolates_named_sessions() {
    local tmpdir="$TEST_TMPDIR/codex_storage_named"
    local home="$tmpdir/home"
    local shared="$tmpdir/shared-codex"
    local session_name="infra"
    local session_key
    session_key="$(session_key_for_test "$session_name")"

    mkdir -p "$home" "$shared/sessions/legacy"
    echo "model = \"gpt-5\"" > "$shared/config.toml"
    ln -s "$shared" "$home/.codex"

    local result
    result="$(
        AGENT="codex"
        HOME="$home"
        EXITBOX_SESSION_NAME="$session_name"
        eval "$CODEX_SESSION_HELPERS"
        configure_codex_session_storage
        printf 'codex=%s\n' "$(readlink -f "$HOME/.codex")"
        printf 'config=%s\n' "$(readlink -f "$HOME/.codex/config.toml")"
        printf 'sessions=%s\n' "$(readlink -f "$HOME/.codex/sessions")"
    )" 2>/dev/null

    assert_contains "configure_codex_session_storage (named overlay)" \
        "$result" "codex=${shared}/.exitbox/session-home/${session_key}"
    assert_contains "configure_codex_session_storage (shared config)" \
        "$result" "config=${shared}/config.toml"
    assert_contains "configure_codex_session_storage (isolated sessions)" \
        "$result" "sessions=${shared}/.exitbox/session-data/${session_key}/sessions"
}

# ============================================================================
# Test: configure_codex_session_storage uses active session for bare resume
# ============================================================================
test_configure_codex_session_storage_uses_active_session() {
    local tmpdir="$TEST_TMPDIR/codex_storage_active"
    local home="$tmpdir/home"
    local shared="$tmpdir/shared-codex"
    local active_name="exitmail"
    local session_key
    session_key="$(session_key_for_test "$active_name")"

    mkdir -p "$home" "$shared" "$tmpdir/default/codex"
    ln -s "$shared" "$home/.codex"
    echo "$active_name" > "$tmpdir/default/codex/.active-session"

    local result
    result="$(
        AGENT="codex"
        HOME="$home"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_AUTO_RESUME="true"
        EXITBOX_SESSION_NAME=""
        eval "$CODEX_SESSION_HELPERS"
        configure_codex_session_storage
        printf 'session=%s\n' "$EXITBOX_SESSION_NAME"
        printf 'sessions=%s\n' "$(readlink -f "$HOME/.codex/sessions")"
    )" 2>/dev/null

    assert_contains "configure_codex_session_storage (active session name)" \
        "$result" "session=${active_name}"
    assert_contains "configure_codex_session_storage (active session path)" \
        "$result" "sessions=${shared}/.exitbox/session-data/${session_key}/sessions"
}

# ============================================================================
# Test: configure_codex_session_storage bypasses overlay for login/logout flows
# ============================================================================
test_configure_codex_session_storage_bypasses_login_flow() {
    local tmpdir="$TEST_TMPDIR/codex_storage_login"
    local home="$tmpdir/home"
    local shared="$tmpdir/shared-codex"

    mkdir -p "$home" "$shared"
    ln -s "$shared" "$home/.codex"

    local result
    result="$(
        AGENT="codex"
        HOME="$home"
        EXITBOX_SESSION_NAME="login-run"
        eval "$CODEX_SESSION_HELPERS"
        configure_codex_session_storage login
        printf 'codex=%s\n' "$(readlink -f "$HOME/.codex")"
    )" 2>/dev/null

    assert_contains "configure_codex_session_storage (login keeps shared home)" \
        "$result" "codex=${shared}"
}

# ============================================================================
# Test: build_resume_args does not inject resume flags into codex login
# ============================================================================
test_build_resume_args_skips_codex_login() {
    local result
    result="$(
        AGENT="codex"
        EXITBOX_AUTO_RESUME="true"
        EXITBOX_RESUME_TOKEN="last"
        eval "$SESSION_HELPER_FUNCS"
        eval "$CODEX_USES_DIRECT_HOME_FUNC"
        eval "$BUILD_FUNC"
        build_resume_args login
        echo "${RESUME_ARGS[*]}"
    )" 2>/dev/null

    assert_eq "build_resume_args (codex login has no resume args)" "" "$result"
}

# ============================================================================
# Test: should_exec_raw only triggers for auth/login/logout flows
# ============================================================================
test_should_exec_raw_only_for_auth_flows() {
    local result
    result="$(
        eval "$HAS_AUTH_ACTION_FUNC"
        eval "$SHOULD_EXEC_RAW_FUNC"
        if should_exec_raw --name box --dangerously-skip-permissions; then
            echo "raw"
        else
            echo "wrapped"
        fi
    )" 2>/dev/null

    assert_eq "should_exec_raw (normal flags stay wrapped)" "wrapped" "$result"
}

# ============================================================================
# Test: should_exec_raw detects auth keywords across args (case-insensitive)
# ============================================================================
test_should_exec_raw_detects_auth_keywords() {
    local result
    result="$(
        eval "$HAS_AUTH_ACTION_FUNC"
        eval "$SHOULD_EXEC_RAW_FUNC"
        if should_exec_raw codex LoGiN; then
            echo "raw"
        else
            echo "wrapped"
        fi
    )" 2>/dev/null

    assert_eq "should_exec_raw (login keyword across args)" "raw" "$result"
}

# ============================================================================
# Test: run_raw_command still prefixes AGENT for auth/login flows
# ============================================================================
test_run_raw_command_prefixes_agent() {
    local result
    result="$(
        AGENT="codex"
        codex() {
            echo "codex:$*"
        }
        eval "$RUN_RAW_COMMAND_FUNC"
        run_raw_command login
    )" 2>/dev/null

    assert_eq "run_raw_command (login executes via agent binary)" "codex:login" "$result"
}

# ============================================================================
# Test: build_agent_loop_command preserves passthrough args
# ============================================================================
test_build_agent_loop_command_passthrough() {
    local result
    result="$(
        eval "$BUILD_AGENT_LOOP_COMMAND_FUNC"
        build_agent_loop_command --dangerously-skip-permissions --name box
    )" 2>/dev/null

    assert_contains "build_agent_loop_command includes agent loop marker" "$result" "__agent-loop"
    assert_contains "build_agent_loop_command includes dangerous flag" "$result" "--dangerously-skip-permissions"
    assert_contains "build_agent_loop_command includes name flag" "$result" "--name"
    assert_contains "build_agent_loop_command includes name value" "$result" "box"
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
        eval "$TMUX_EXTKEYS_FUNC"
        eval "$TMUX_CONF_FUNC"
        tmux() { [[ "$1" == "-V" ]] && echo "tmux 3.5a"; }
        conf_path="$(write_tmux_conf)"
        cat "$conf_path"
    )" 2>/dev/null

    assert_contains "write_tmux_conf enables mouse scrolling" "$output" 'set -g mouse on'
    assert_contains "write_tmux_conf sets large history" "$output" 'set -g history-limit 100000'
    assert_contains "write_tmux_conf enables extended-keys" "$output" 'set -g extended-keys on'
    assert_contains "write_tmux_conf advertises extkeys for all terminals" "$output" 'terminal-features "*:extkeys"'
    assert_contains "write_tmux_conf uses csi-u extended-keys format" "$output" 'set -g extended-keys-format csi-u'
    assert_contains "write_tmux_conf shows workspace shortcut" "$output" 'C-M-p: workspaces'
    assert_contains "write_tmux_conf shows session shortcut" "$output" 'C-M-s: sessions'
}

# ============================================================================
# Test: write_tmux_conf always sets default-terminal tmux-256color
# ============================================================================
test_write_tmux_conf_default_terminal() {
    # Alpine images have the tmux-256color terminfo but no infocmp; an infocmp
    # probe used to fall back to xterm-256color there, which breaks tmux.
    local out
    out="$(
        AGENT="pi"; EXITBOX_WORKSPACE_NAME="default"; EXITBOX_VERSION="test"
        unset EXITBOX_KEYBINDINGS
        eval "$PARSE_KB_FUNC"; parse_keybindings
        eval "$DISPLAY_FUNC"; eval "$TMUX_CONF_FUNC"
        infocmp() { return 127; }
        cat "$(write_tmux_conf)"
    )" 2>/dev/null
    assert_contains "default-terminal is tmux-256color without infocmp" "$out" 'set-option -g default-terminal "tmux-256color"'
    assert_eq "no xterm-256color default-terminal" "" "$(printf '%s\n' "$out" | grep 'default-terminal "xterm-256color"' || true)"
    assert_contains "outer terminal gets synchronized output" "$out" 'set -as terminal-features "*:sync"'
    assert_contains "focus events reach the agent" "$out" 'set -g focus-events on'
}

# ============================================================================
# Test: extended-keys-format is gated on the tmux version
# ============================================================================
test_write_tmux_conf_extended_keys_format() {
    # tmux 3.5 added extended-keys-format. Emitting it on an older tmux
    # (Ubuntu 24.04 ships 3.4, so the GPU image does too) makes every session
    # start with "invalid option".
    local modern legacy
    modern="$(
        AGENT="pi"; EXITBOX_WORKSPACE_NAME="default"; EXITBOX_VERSION="test"
        unset EXITBOX_KEYBINDINGS
        eval "$PARSE_KB_FUNC"; parse_keybindings
        eval "$DISPLAY_FUNC"; eval "$TMUX_EXTKEYS_FUNC"; eval "$TMUX_CONF_FUNC"
        infocmp() { return 0; }
        tmux() { [[ "$1" == "-V" ]] && echo "tmux 3.5a"; }
        cat "$(write_tmux_conf)"
    )" 2>/dev/null
    assert_contains "tmux 3.5 keeps csi-u encoding" "$modern" "set -g extended-keys-format csi-u"
    assert_contains "extended keys stay on" "$modern" "set -g extended-keys on"

    legacy="$(
        AGENT="pi"; EXITBOX_WORKSPACE_NAME="default"; EXITBOX_VERSION="test"
        unset EXITBOX_KEYBINDINGS
        eval "$PARSE_KB_FUNC"; parse_keybindings
        eval "$DISPLAY_FUNC"; eval "$TMUX_EXTKEYS_FUNC"; eval "$TMUX_CONF_FUNC"
        infocmp() { return 0; }
        tmux() { [[ "$1" == "-V" ]] && echo "tmux 3.4"; }
        cat "$(write_tmux_conf)"
    )" 2>/dev/null
    if [[ "$legacy" == *"set -g extended-keys-format"* ]]; then
        ((FAIL++))
        ERRORS+=("FAIL: tmux 3.4 must not get extended-keys-format")
    else
        ((PASS++))
    fi
    assert_contains "tmux 3.4 still forwards extended keys" "$legacy" "set -g extended-keys on"
}

test_tmux_supports_extended_keys_format_versions() {
    local out
    out="$(
        eval "$TMUX_EXTKEYS_FUNC"
        for v in "tmux 3.5a" "tmux 3.6" "tmux 4.0" "tmux 3.4" "tmux 3.2a" "tmux next-3.6" "bogus"; do
            tmux() { [[ "$1" == "-V" ]] && echo "$v"; }
            if tmux_supports_extended_keys_format; then echo "$v yes"; else echo "$v no"; fi
        done
    )"
    assert_contains "3.5a supported" "$out" "tmux 3.5a yes"
    assert_contains "3.6 supported" "$out" "tmux 3.6 yes"
    assert_contains "4.0 supported" "$out" "tmux 4.0 yes"
    assert_contains "3.4 unsupported" "$out" "tmux 3.4 no"
    assert_contains "3.2a unsupported" "$out" "tmux 3.2a no"
    assert_contains "unparsable version is unsupported" "$out" "bogus no"
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
test_configure_codex_session_storage_isolates_named_sessions
test_configure_codex_session_storage_uses_active_session
test_configure_codex_session_storage_bypasses_login_flow
test_build_resume_args_skips_codex_login
test_should_exec_raw_only_for_auth_flows
test_should_exec_raw_detects_auth_keywords
test_run_raw_command_prefixes_agent
test_build_agent_loop_command_passthrough
test_write_tmux_conf_scroll_settings
test_write_tmux_conf_default_terminal
test_parse_keybindings_default
test_parse_keybindings_custom
test_parse_keybindings_partial
test_write_tmux_conf_dynamic_keybindings
test_write_tmux_conf_extended_keys_format
test_tmux_supports_extended_keys_format_versions

# ============================================================================
# Vault sandbox instructions
# ============================================================================
echo ""
echo "Testing vault sandbox instructions..."

# Extract the sandbox instructions block from the entrypoint.
# The block starts with SANDBOX_INSTRUCTIONS= and the vault conditional follows.
extract_sandbox_instructions() {
    # Extract everything from the SANDBOX_INSTRUCTIONS section through the vault conditional.
    awk '/^SANDBOX_INSTRUCTIONS="/,/^fi$/{print}' "$ENTRYPOINT"
}

SANDBOX_BLOCK="$(extract_sandbox_instructions)"

test_vault_instructions_absent_when_disabled() {
    local result
    result="$(unset EXITBOX_VAULT_ENABLED; eval "$SANDBOX_BLOCK"; printf '%s' "$SANDBOX_INSTRUCTIONS")"
    if [[ "$result" == *"exitbox-vault"* ]]; then
        ((FAIL++))
        ERRORS+=("FAIL: vault instructions should be absent when EXITBOX_VAULT_ENABLED is unset")
    else
        ((PASS++))
    fi
}

test_vault_instructions_present_when_enabled() {
    local result
    result="$(EXITBOX_VAULT_ENABLED=true; eval "$SANDBOX_BLOCK"; printf '%s' "$SANDBOX_INSTRUCTIONS")"
    if [[ "$result" == *"exitbox-vault"* ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: vault instructions should be present when EXITBOX_VAULT_ENABLED=true")
    fi
}

test_vault_instructions_contain_security_rules() {
    local result
    result="$(EXITBOX_VAULT_ENABLED=true; eval "$SANDBOX_BLOCK"; printf '%s' "$SANDBOX_INSTRUCTIONS")"
    if [[ "$result" == *"NEVER print"* && "$result" == *"NEVER commit"* ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: vault instructions should contain security rules about not printing/committing secrets")
    fi
}

test_vault_instructions_contain_usage_pattern() {
    local result
    result="$(EXITBOX_VAULT_ENABLED=true; eval "$SANDBOX_BLOCK"; printf '%s' "$SANDBOX_INSTRUCTIONS")"
    if [[ "$result" == *"exitbox-vault get"* && "$result" == *"Bearer"* ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: vault instructions should contain usage patterns with Bearer token example")
    fi
}

test_sandbox_workspace_restriction() {
    local result
    result="$(eval "$SANDBOX_BLOCK"; printf '%s' "$SANDBOX_INSTRUCTIONS")"
    if [[ "$result" == *"ALWAYS mounted at /workspace"* && "$result" == *"CANNOT access files"* ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: sandbox should instruct that workspace is at /workspace and nothing else is accessible")
    fi
}

test_sandbox_redacted_instructions() {
    local result
    result="$(eval "$SANDBOX_BLOCK"; printf '%s' "$SANDBOX_INSTRUCTIONS")"
    if [[ "$result" == *"<redacted>"* && "$result" == *"SENSITIVE DATA"* ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: sandbox should instruct to use <redacted> for sensitive data")
    fi
}

test_vault_instructions_contain_redacted() {
    local result
    result="$(EXITBOX_VAULT_ENABLED=true; eval "$SANDBOX_BLOCK"; printf '%s' "$SANDBOX_INSTRUCTIONS")"
    if [[ "$result" == *"<redacted>"* && "$result" == *"redact"* ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: vault instructions should contain redaction guidance")
    fi
}

test_sandbox_workspace_restriction
test_sandbox_redacted_instructions
test_vault_instructions_absent_when_disabled
test_vault_instructions_present_when_enabled
test_vault_instructions_contain_security_rules
test_vault_instructions_contain_usage_pattern
test_vault_instructions_contain_redacted

test_vault_readonly_no_set_command() {
    local result
    result="$(EXITBOX_VAULT_ENABLED=true EXITBOX_VAULT_READONLY=true; eval "$SANDBOX_BLOCK"; printf '%s' "$SANDBOX_INSTRUCTIONS")"
    if [[ "$result" == *"exitbox-vault set"* ]]; then
        ((FAIL++))
        ERRORS+=("FAIL: read-only vault instructions should not contain 'exitbox-vault set'")
    else
        ((PASS++))
    fi
}

test_vault_readonly_contains_readonly_note() {
    local result
    result="$(EXITBOX_VAULT_ENABLED=true EXITBOX_VAULT_READONLY=true; eval "$SANDBOX_BLOCK"; printf '%s' "$SANDBOX_INSTRUCTIONS")"
    if [[ "$result" == *"read-only"* && "$result" == *"cannot store new secrets"* ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: read-only vault instructions should contain read-only note about not storing secrets")
    fi
}

test_vault_readwrite_has_set_command() {
    local result
    result="$(EXITBOX_VAULT_ENABLED=true; unset EXITBOX_VAULT_READONLY; eval "$SANDBOX_BLOCK"; printf '%s' "$SANDBOX_INSTRUCTIONS")"
    if [[ "$result" == *"exitbox-vault set"* ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: read-write vault instructions should contain 'exitbox-vault set'")
    fi
}

test_vault_readonly_no_set_command
test_vault_readonly_contains_readonly_note
test_vault_readwrite_has_set_command

# Extract inject_sandbox_instructions function for re-injection tests.
INJECT_FUNC="$(awk '/^inject_sandbox_instructions\(\)/,/^}/' "$ENTRYPOINT")"

test_vault_block_no_duplicates_on_reinject() {
    local tmpdir
    tmpdir="$(mktemp -d)"
    mkdir -p "$tmpdir/.claude"

    # Build SANDBOX_INSTRUCTIONS with vault enabled.
    EXITBOX_VAULT_ENABLED=true eval "$SANDBOX_BLOCK"

    # Define the inject function in this shell, then call it twice.
    eval "$INJECT_FUNC"
    GLOBAL_WORKSPACE_ROOT="$tmpdir/ws" EXITBOX_WORKSPACE_NAME="default" \
        AGENT="claude" HOME="$tmpdir" inject_sandbox_instructions
    GLOBAL_WORKSPACE_ROOT="$tmpdir/ws" EXITBOX_WORKSPACE_NAME="default" \
        AGENT="claude" HOME="$tmpdir" inject_sandbox_instructions

    local count
    count=$(grep -c "BEGIN-EXITBOX-VAULT" "$tmpdir/.claude/CLAUDE.md")
    if [[ "$count" -eq 1 ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: vault block should appear exactly once after re-injection, got $count")
    fi
    rm -rf "$tmpdir"
}

test_vault_block_no_duplicates_on_reinject

test_agents_md_included_in_injection() {
    local tmpdir
    tmpdir="$(mktemp -d)"
    mkdir -p "$tmpdir/.claude"
    mkdir -p "$tmpdir/ws/myworkspace"
    echo "# My custom rules" > "$tmpdir/ws/myworkspace/agents.md"

    eval "$SANDBOX_BLOCK"
    eval "$INJECT_FUNC"
    GLOBAL_WORKSPACE_ROOT="$tmpdir/ws" EXITBOX_WORKSPACE_NAME="myworkspace" \
        AGENT="claude" HOME="$tmpdir" inject_sandbox_instructions

    local result
    result="$(cat "$tmpdir/.claude/CLAUDE.md")"
    if [[ "$result" == *"My custom rules"* && "$result" == *"BEGIN-EXITBOX-SANDBOX"* ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: inject should include user agents.md content and sandbox block")
    fi
    rm -rf "$tmpdir"
}

test_agents_md_without_file() {
    local tmpdir
    tmpdir="$(mktemp -d)"
    mkdir -p "$tmpdir/.claude"

    eval "$SANDBOX_BLOCK"
    eval "$INJECT_FUNC"
    GLOBAL_WORKSPACE_ROOT="$tmpdir/ws" EXITBOX_WORKSPACE_NAME="default" \
        AGENT="claude" HOME="$tmpdir" inject_sandbox_instructions

    local result
    result="$(cat "$tmpdir/.claude/CLAUDE.md")"
    if [[ "$result" == *"BEGIN-EXITBOX-SANDBOX"* ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: inject without agents.md should still have sandbox block")
    fi
    # Should NOT contain "My custom rules" since no agents.md exists
    if [[ "$result" != *"My custom rules"* ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: inject without agents.md should not contain user content")
    fi
    rm -rf "$tmpdir"
}

test_agents_md_included_in_injection
test_agents_md_without_file

# ============================================================================
# IDE relay guard-condition tests
# ============================================================================
echo ""
echo "Testing IDE relay guard conditions..."

IDE_RELAY_FUNC="$(extract_func start_ide_relay)"
CLEANUP_IDE_FUNC="$(extract_func cleanup_ide_relay)"

test_ide_relay_skips_non_claude() {
    local result
    result="$(
        AGENT="codex"
        ENABLE_IDE_INTEGRATION="true"
        EXITBOX_IDE_PORT="12345"
        IDE_RELAY_PID=""
        eval "$IDE_RELAY_FUNC"
        start_ide_relay
        echo "PID=$IDE_RELAY_PID"
    )" 2>/dev/null
    assert_eq "ide_relay skips non-claude" "PID=" "$result"
}

test_ide_relay_skips_disabled() {
    local result
    result="$(
        AGENT="claude"
        unset ENABLE_IDE_INTEGRATION
        EXITBOX_IDE_PORT="12345"
        IDE_RELAY_PID=""
        eval "$IDE_RELAY_FUNC"
        start_ide_relay
        echo "PID=$IDE_RELAY_PID"
    )" 2>/dev/null
    assert_eq "ide_relay skips when disabled" "PID=" "$result"
}

test_ide_relay_skips_no_port() {
    local result
    result="$(
        AGENT="claude"
        ENABLE_IDE_INTEGRATION="true"
        unset EXITBOX_IDE_PORT
        IDE_RELAY_PID=""
        eval "$IDE_RELAY_FUNC"
        start_ide_relay
        echo "PID=$IDE_RELAY_PID"
    )" 2>/dev/null
    assert_eq "ide_relay skips when no port" "PID=" "$result"
}

test_ide_relay_skips_missing_socket() {
    local result
    result="$(
        AGENT="claude"
        ENABLE_IDE_INTEGRATION="true"
        EXITBOX_IDE_PORT="12345"
        IDE_RELAY_PID=""
        eval "$IDE_RELAY_FUNC"
        start_ide_relay
        echo "PID=$IDE_RELAY_PID"
    )" 2>/dev/null
    assert_eq "ide_relay skips when socket missing" "PID=" "$result"
}

test_cleanup_ide_relay_noop() {
    # cleanup should not fail when no relay PID is set
    local result
    result="$(
        IDE_RELAY_PID=""
        eval "$CLEANUP_IDE_FUNC"
        cleanup_ide_relay
        echo "ok"
    )" 2>/dev/null
    assert_eq "cleanup_ide_relay noop" "ok" "$result"
}

test_ide_relay_skips_non_claude
test_ide_relay_skips_disabled
test_ide_relay_skips_no_port
test_ide_relay_skips_missing_socket
test_cleanup_ide_relay_noop

# ============================================================================
# Git credential helper tests
# ============================================================================
echo ""
echo "Testing git credential helper..."

SETUP_GIT_FUNC="$(extract_func setup_git_credential_helper)"

test_git_credential_helper_skips_without_gh() {
    local result
    result="$(
        # Mock gh as not found
        gh() { return 1; }
        command() {
            if [[ "$1" == "-v" && "$2" == "gh" ]]; then return 1; fi
            builtin command "$@"
        }
        eval "$SETUP_GIT_FUNC"
        setup_git_credential_helper
        echo "ok"
    )" 2>/dev/null
    assert_eq "git_credential_helper skips without gh" "ok" "$result"
}

test_git_credential_helper_configures_with_gh() {
    if ! command -v git >/dev/null 2>&1; then
        ((PASS++))
        return
    fi
    local tmpdir
    tmpdir="$(mktemp -d)"
    local result
    result="$(
        export HOME="$tmpdir"
        # Mock gh as available
        gh() { return 0; }
        command() {
            if [[ "$1" == "-v" && "$2" == "gh" ]]; then return 0; fi
            builtin command "$@"
        }
        eval "$SETUP_GIT_FUNC"
        setup_git_credential_helper
        git config --global --get credential."https://github.com".helper
    )" 2>/dev/null
    assert_eq "git_credential_helper configures gh" '!gh auth git-credential' "$result"
    rm -rf "$tmpdir"
}

test_git_credential_helper_skips_readonly_gitconfig() {
    if ! command -v git >/dev/null 2>&1; then
        ((PASS++))
        return
    fi
    local tmpdir
    tmpdir="$(mktemp -d)"
    # Create a read-only .gitconfig (simulates host mount with :ro)
    echo "[user]" > "$tmpdir/.gitconfig"
    echo "    name = Test" >> "$tmpdir/.gitconfig"
    chmod 444 "$tmpdir/.gitconfig"

    local result
    result="$(
        export HOME="$tmpdir"
        gh() { return 0; }
        command() {
            if [[ "$1" == "-v" && "$2" == "gh" ]]; then return 0; fi
            builtin command "$@"
        }
        eval "$SETUP_GIT_FUNC"
        setup_git_credential_helper
        echo "ok"
    )" 2>/dev/null
    assert_eq "git_credential_helper skips read-only gitconfig" "ok" "$result"
    # Verify the file was NOT modified (no credential helper added)
    local content
    content="$(cat "$tmpdir/.gitconfig")"
    if [[ "$content" == *"credential"* ]]; then
        ((FAIL++))
        ERRORS+=("FAIL: read-only .gitconfig should not be modified")
    else
        ((PASS++))
    fi
    chmod 644 "$tmpdir/.gitconfig"
    rm -rf "$tmpdir"
}

test_git_credential_helper_skips_without_gh
test_git_credential_helper_configures_with_gh
test_git_credential_helper_skips_readonly_gitconfig

# ============================================================================
# SSH proxy tunnel tests
# ============================================================================
echo ""
echo "Testing SSH proxy tunnel setup..."

SETUP_SSH_FUNC="$(extract_func setup_ssh_proxy_tunnel)"

test_ssh_proxy_tunnel_works_without_ssh_auth_sock() {
    # SSH proxy tunnel should still be configured even without SSH_AUTH_SOCK.
    # Without it, SSH fails with DNS errors on the isolated internal network.
    if ! command -v socat >/dev/null 2>&1; then
        ((PASS++))
        return
    fi
    local tmpdir
    tmpdir="$(mktemp -d)"
    (
        export HOME="$tmpdir"
        unset SSH_AUTH_SOCK
        export http_proxy="http://172.18.0.2:3128"
        eval "$SETUP_SSH_FUNC"
        setup_ssh_proxy_tunnel
    ) 2>/dev/null
    if [[ -f "$tmpdir/.ssh/config" ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: ssh proxy tunnel should create config even without SSH_AUTH_SOCK")
    fi
    rm -rf "$tmpdir"
}

test_ssh_proxy_tunnel_skips_without_http_proxy() {
    local tmpdir
    tmpdir="$(mktemp -d)"
    local result
    result="$(
        export HOME="$tmpdir"
        unset http_proxy
        eval "$SETUP_SSH_FUNC"
        setup_ssh_proxy_tunnel
        echo "ok"
    )" 2>/dev/null
    assert_file_missing "ssh tunnel skips without http_proxy" "$tmpdir/.ssh/config"
    rm -rf "$tmpdir"
}

test_ssh_proxy_tunnel_creates_config() {
    if ! command -v socat >/dev/null 2>&1; then
        ((PASS++))
        return
    fi
    local tmpdir
    tmpdir="$(mktemp -d)"
    (
        export HOME="$tmpdir"
        export SSH_AUTH_SOCK="/run/exitbox/ssh-agent.sock"
        export http_proxy="http://172.18.0.2:3128"
        eval "$SETUP_SSH_FUNC"
        setup_ssh_proxy_tunnel
    ) 2>/dev/null
    if [[ -f "$tmpdir/.ssh/config" ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: ssh proxy tunnel should create ~/.ssh/config")
        rm -rf "$tmpdir"
        return
    fi
    local content
    content="$(cat "$tmpdir/.ssh/config")"
    assert_contains "ssh config has github entry" "$content" "Host github.com"
    assert_contains "ssh config has ssh.github.com hostname" "$content" "ssh.github.com"
    assert_contains "ssh config has port 443" "$content" "Port 443"
    assert_contains "ssh config has proxy host" "$content" "172.18.0.2"
    assert_contains "ssh config has proxyport 3128" "$content" "proxyport=3128"
    assert_contains "ssh config has gitlab entry" "$content" "Host gitlab.com"
    assert_contains "ssh config has bitbucket entry" "$content" "Host bitbucket.org"
    assert_contains "ssh config has StrictHostKeyChecking yes" "$content" "StrictHostKeyChecking yes"
    rm -rf "$tmpdir"
}

test_ssh_proxy_tunnel_parses_proxy_without_port() {
    if ! command -v socat >/dev/null 2>&1; then
        ((PASS++))
        return
    fi
    local tmpdir
    tmpdir="$(mktemp -d)"
    (
        export HOME="$tmpdir"
        export SSH_AUTH_SOCK="/run/exitbox/ssh-agent.sock"
        export http_proxy="http://10.0.0.1"
        eval "$SETUP_SSH_FUNC"
        setup_ssh_proxy_tunnel
    ) 2>/dev/null
    local content
    content="$(cat "$tmpdir/.ssh/config" 2>/dev/null)"
    assert_contains "ssh config defaults to proxyport 3128" "$content" "proxyport=3128"
    assert_contains "ssh config uses correct proxy host" "$content" "10.0.0.1"
    rm -rf "$tmpdir"
}

test_ssh_proxy_tunnel_correct_permissions() {
    if ! command -v socat >/dev/null 2>&1; then
        ((PASS++))
        return
    fi
    local tmpdir
    tmpdir="$(mktemp -d)"
    (
        export HOME="$tmpdir"
        export SSH_AUTH_SOCK="/run/exitbox/ssh-agent.sock"
        export http_proxy="http://172.18.0.2:3128"
        eval "$SETUP_SSH_FUNC"
        setup_ssh_proxy_tunnel
    ) 2>/dev/null
    local dir_perms file_perms
    dir_perms="$(stat -c '%a' "$tmpdir/.ssh" 2>/dev/null || stat -f '%Lp' "$tmpdir/.ssh" 2>/dev/null)"
    file_perms="$(stat -c '%a' "$tmpdir/.ssh/config" 2>/dev/null || stat -f '%Lp' "$tmpdir/.ssh/config" 2>/dev/null)"
    kh_perms="$(stat -c '%a' "$tmpdir/.ssh/known_hosts" 2>/dev/null || stat -f '%Lp' "$tmpdir/.ssh/known_hosts" 2>/dev/null)"
    assert_eq "ssh dir has 700 permissions" "700" "$dir_perms"
    assert_eq "ssh config has 600 permissions" "600" "$file_perms"
    assert_eq "ssh known_hosts has 600 permissions" "600" "$kh_perms"
    rm -rf "$tmpdir"
}

test_ssh_proxy_tunnel_writes_known_hosts() {
    if ! command -v socat >/dev/null 2>&1; then
        ((PASS++))
        return
    fi
    local tmpdir
    tmpdir="$(mktemp -d)"
    (
        export HOME="$tmpdir"
        export SSH_AUTH_SOCK="/run/exitbox/ssh-agent.sock"
        export http_proxy="http://172.18.0.2:3128"
        eval "$SETUP_SSH_FUNC"
        setup_ssh_proxy_tunnel
    ) 2>/dev/null
    if [[ -f "$tmpdir/.ssh/known_hosts" ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: ssh proxy tunnel should create ~/.ssh/known_hosts")
        rm -rf "$tmpdir"
        return
    fi
    local content
    content="$(cat "$tmpdir/.ssh/known_hosts")"
    assert_contains "known_hosts has ssh.github.com" "$content" "[ssh.github.com]:443"
    assert_contains "known_hosts has altssh.gitlab.com" "$content" "[altssh.gitlab.com]:443"
    assert_contains "known_hosts has altssh.bitbucket.org" "$content" "[altssh.bitbucket.org]:443"
    assert_contains "known_hosts has ed25519 key" "$content" "ssh-ed25519"
    assert_contains "known_hosts has ecdsa key" "$content" "ecdsa-sha2-nistp256"
    assert_contains "known_hosts has rsa key" "$content" "ssh-rsa"
    rm -rf "$tmpdir"
}

test_ssh_proxy_tunnel_strict_host_key_checking() {
    if ! command -v socat >/dev/null 2>&1; then
        ((PASS++))
        return
    fi
    local tmpdir
    tmpdir="$(mktemp -d)"
    (
        export HOME="$tmpdir"
        export SSH_AUTH_SOCK="/run/exitbox/ssh-agent.sock"
        export http_proxy="http://172.18.0.2:3128"
        eval "$SETUP_SSH_FUNC"
        setup_ssh_proxy_tunnel
    ) 2>/dev/null
    local content
    content="$(cat "$tmpdir/.ssh/config")"
    assert_contains "ssh config uses StrictHostKeyChecking yes" "$content" "StrictHostKeyChecking yes"
    # Ensure accept-new is NOT used
    if [[ "$content" == *"accept-new"* ]]; then
        ((FAIL++))
        ERRORS+=("FAIL: ssh config should not contain accept-new (TOFU mode)")
    else
        ((PASS++))
    fi
    rm -rf "$tmpdir"
}

test_ssh_proxy_tunnel_works_without_ssh_auth_sock
test_ssh_proxy_tunnel_skips_without_http_proxy
test_ssh_proxy_tunnel_creates_config
test_ssh_proxy_tunnel_parses_proxy_without_port
test_ssh_proxy_tunnel_correct_permissions
test_ssh_proxy_tunnel_writes_known_hosts
test_ssh_proxy_tunnel_strict_host_key_checking

# ============================================================================
# Host port relay tests
# ============================================================================
echo ""
echo "Testing host port relays..."

START_HOST_PORT_RELAYS_FUNC="$(extract_func start_host_port_relays)"
CLEANUP_HOST_PORT_RELAYS_FUNC="$(extract_func cleanup_host_port_relays)"

test_host_port_relays_skip_without_ports() {
    local tmpdir
    tmpdir="$(mktemp -d)"
    local result
    result="$(
        export HOME="$tmpdir"
        unset EXITBOX_HOST_PORTS
        export http_proxy="http://172.18.0.2:3128"
        eval "$START_HOST_PORT_RELAYS_FUNC"
        start_host_port_relays
        echo "ok"
    )" 2>/dev/null
    assert_eq "host port relay skips without ports" "ok" "$result"
    rm -rf "$tmpdir"
}

test_host_port_relays_start_configured_ports() {
    local tmpdir bindir
    tmpdir="$(mktemp -d)"
    bindir="$tmpdir/bin"
    mkdir -p "$bindir"
    cat > "$bindir/socat" <<'FAKESOCAT'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$SOCAT_LOG"
sleep 30
FAKESOCAT
    chmod +x "$bindir/socat"

    (
        export PATH="$bindir:$PATH"
        export SOCAT_LOG="$tmpdir/socat.log"
        export EXITBOX_HOST_PORTS="3000,5173 bad 0"
        export http_proxy="http://172.18.0.2:3128"
        eval "$START_HOST_PORT_RELAYS_FUNC"
        eval "$CLEANUP_HOST_PORT_RELAYS_FUNC"
        start_host_port_relays
        sleep 0.2
        cleanup_host_port_relays
    ) 2>/dev/null

    local content
    content="$(cat "$tmpdir/socat.log" 2>/dev/null)"
    assert_contains "host port relay listens on 3000" "$content" "TCP-LISTEN:3000,bind=127.0.0.1"
    assert_contains "host port relay proxies 3000 to host" "$content" "PROXY:172.18.0.2:host.docker.internal:3000,proxyport=3128"
    assert_contains "host port relay listens on 5173" "$content" "TCP-LISTEN:5173,bind=127.0.0.1"
    assert_contains "host port relay proxies 5173 to host" "$content" "PROXY:172.18.0.2:host.docker.internal:5173,proxyport=3128"
    if [[ "$content" == *"bad"* || "$content" == *"TCP-LISTEN:0"* ]]; then
        ((FAIL++))
        ERRORS+=("FAIL: host port relay should ignore invalid ports")
    else
        ((PASS++))
    fi
    rm -rf "$tmpdir"
}

test_host_port_relays_skip_without_ports
test_host_port_relays_start_configured_ports

# ============================================================================
# RTK setup tests
# ============================================================================
echo ""
echo "Testing RTK setup..."

SETUP_RTK_FUNC="$(extract_func setup_rtk)"

test_rtk_skips_when_disabled() {
    local tmpdir
    tmpdir="$(mktemp -d)"
    local result
    result="$(
        export HOME="$tmpdir"
        unset EXITBOX_RTK
        AGENT="claude"
        eval "$SETUP_RTK_FUNC"
        setup_rtk
        echo "ok"
    )" 2>/dev/null
    assert_eq "rtk skips when disabled" "ok" "$result"
    # No hook files should be created
    assert_file_missing "rtk no hooks when disabled" "$tmpdir/.claude/hooks/rtk-rewrite.sh"
    rm -rf "$tmpdir"
}

test_rtk_skips_without_binary() {
    local tmpdir
    tmpdir="$(mktemp -d)"
    local result
    result="$(
        export HOME="$tmpdir"
        EXITBOX_RTK="true"
        AGENT="claude"
        # Override command to always fail for rtk
        command() {
            if [[ "$1" == "-v" && "$2" == "rtk" ]]; then return 1; fi
            builtin command "$@"
        }
        eval "$SETUP_RTK_FUNC"
        setup_rtk
        echo "ok"
    )" 2>/dev/null
    assert_eq "rtk skips without binary" "ok" "$result"
    rm -rf "$tmpdir"
}

test_rtk_sandbox_instructions_absent_when_disabled() {
    local result
    result="$(unset EXITBOX_RTK; eval "$SANDBOX_BLOCK"; printf '%s' "$SANDBOX_INSTRUCTIONS")"
    if [[ "$result" == *"BEGIN-EXITBOX-RTK"* ]]; then
        ((FAIL++))
        ERRORS+=("FAIL: rtk instructions should be absent when EXITBOX_RTK is unset")
    else
        ((PASS++))
    fi
}

test_rtk_sandbox_instructions_present_when_enabled() {
    local result
    result="$(
        EXITBOX_RTK=true
        # Mock rtk as available
        command() {
            if [[ "$1" == "-v" && "$2" == "rtk" ]]; then return 0; fi
            builtin command "$@"
        }
        eval "$SANDBOX_BLOCK"
        printf '%s' "$SANDBOX_INSTRUCTIONS"
    )"
    if [[ "$result" == *"BEGIN-EXITBOX-RTK"* && "$result" == *"rtk git"* ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: rtk instructions should be present when EXITBOX_RTK=true and rtk binary exists")
    fi
}

test_rtk_skips_when_disabled
test_rtk_skips_without_binary
test_rtk_sandbox_instructions_absent_when_disabled
test_rtk_sandbox_instructions_present_when_enabled

# ============================================================================
# Tests: clear_stale_resume_token
# ============================================================================
test_clear_stale_resume_token_removes_files() {
    local tmpdir="$TEST_TMPDIR/clear_stale_files"
    local session_name="mysession"
    local token_file legacy_file
    token_file="$(session_token_file_for_test "$tmpdir" "default" "claude" "$session_name")"
    mkdir -p "$(dirname "$token_file")"
    echo "staletoken" > "$token_file"
    legacy_file="${tmpdir}/default/claude/.resume-token"
    mkdir -p "$(dirname "$legacy_file")"
    echo "staletoken" > "$legacy_file"

    (
        AGENT="claude"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_SESSION_NAME="$session_name"
        EXITBOX_PROJECT_KEY=""
        # shellcheck disable=SC2317
        exitbox-kv() { return 0; }
        export -f exitbox-kv
        eval "$SESSION_HELPER_FUNCS"
        eval "$CLEAR_STALE_FUNC"
        clear_stale_resume_token
    ) 2>/dev/null

    assert_file_missing "clear_stale_resume_token (removes session token file)" "$token_file"
    assert_file_missing "clear_stale_resume_token (removes legacy token file)" "$legacy_file"
}

test_clear_stale_resume_token_no_error_when_missing() {
    local tmpdir="$TEST_TMPDIR/clear_stale_missing"
    local session_name="nosuchsession"

    local rc=0
    (
        AGENT="claude"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        EXITBOX_SESSION_NAME="$session_name"
        EXITBOX_PROJECT_KEY=""
        # shellcheck disable=SC2317
        exitbox-kv() { return 1; }
        export -f exitbox-kv
        eval "$SESSION_HELPER_FUNCS"
        eval "$CLEAR_STALE_FUNC"
        clear_stale_resume_token
    ) 2>/dev/null || rc=$?

    assert_eq "clear_stale_resume_token (no error when files absent)" "0" "$rc"
}

test_clear_stale_resume_token_removes_files
test_clear_stale_resume_token_no_error_when_missing

# ============================================================================
# Test: agent_display_name covers the newer agents
# ============================================================================
test_agent_display_name_new_agents() {
    local out
    out="$(
        eval "$DISPLAY_FUNC"
        echo "copilot=$(agent_display_name copilot)"
        echo "cursor=$(agent_display_name cursor)"
        echo "kimi=$(agent_display_name kimi)"
        echo "pi=$(agent_display_name pi)"
        echo "qwen=$(agent_display_name qwen)"
    )" 2>/dev/null
    assert_contains "agent_display_name copilot" "$out" "copilot=GitHub Copilot CLI"
    assert_contains "agent_display_name cursor" "$out" "cursor=Cursor CLI"
    assert_contains "agent_display_name kimi" "$out" "kimi=Kimi Code CLI"
    assert_contains "agent_display_name pi" "$out" "pi=Pi Coding Agent"
    assert_contains "agent_display_name qwen" "$out" "qwen=Qwen Code"
}

# ============================================================================
# Test: apply_active_workspace_links symlinks newer agents' config dirs to the
# persistent workspace root (so `exitbox config edit <agent>` edits the same
# file the agent reads at runtime).
# ============================================================================
APPLY_LINKS_FUNC="$(extract_func apply_active_workspace_links)"

test_workspace_links_new_agents() {
    local agent dir
    for pair in "copilot:.copilot" "cursor:.cursor" "kimi:.kimi-code" "pi:.pi" "qwen:.qwen"; do
        agent="${pair%%:*}"
        dir="${pair#*:}"
        local base home target
        base="$(mktemp -d "$TEST_TMPDIR/ws.XXXXXX")"
        home="$(mktemp -d "$TEST_TMPDIR/home.XXXXXX")"
        target="$(
            export GLOBAL_WORKSPACE_ROOT="$base"
            export HOME="$home"
            export AGENT="$agent"
            export EXITBOX_WORKSPACE_NAME="default"
            export EXITBOX_MOUNTED_WORKSPACE="default"
            unset EXITBOX_ENV_PROFILE
            ensure_workspace_files() { :; }
            eval "$LINK_PATH_FUNC"
            eval "$APPLY_LINKS_FUNC"
            apply_active_workspace_links
            # Prove the runtime path resolves to the persistent workspace file.
            echo "ok" > "$home/$dir/config-probe"
            readlink -f "$home/$dir/config-probe"
        )" 2>/dev/null
        assert_eq "workspace link $agent -> persistent dir" \
            "$base/default/$agent/$dir/config-probe" "$target"
    done
}

test_agent_display_name_new_agents
test_workspace_links_new_agents

# ============================================================================
# Test: link_skills wires ExitBox-managed skills into Pi's skills dir
# ============================================================================
test_link_skills_pi() {
    local base home result
    base="$(mktemp -d "$TEST_TMPDIR/skillws.XXXXXX")"
    home="$(mktemp -d "$TEST_TMPDIR/skillhome.XXXXXX")"
    mkdir -p "$base/default/skills/my-skill"
    printf -- '---\nname: my-skill\n---\n' > "$base/default/skills/my-skill/SKILL.md"
    mkdir -p "$home/.pi/agent"
    result="$(
        export GLOBAL_WORKSPACE_ROOT="$base"
        export HOME="$home"
        export AGENT="pi"
        export EXITBOX_WORKSPACE_NAME="default"
        eval "$(extract_func link_skills)"
        link_skills
        [[ -L "$home/.pi/agent/skills/my-skill" ]] && echo "LINKED"
    )" 2>/dev/null
    assert_contains "link_skills links ExitBox skills into Pi ~/.pi/agent/skills" "$result" "LINKED"
}

test_link_skills_pi

# ============================================================================
# Test: link_subagents links the store into ~/.claude/agents, keeps real
# files, removes dangling store links, and leaves foreign files alone
# ============================================================================
test_link_subagents_claude() {
    local base home other result
    base="$(mktemp -d "$TEST_TMPDIR/subws.XXXXXX")"
    home="$(mktemp -d "$TEST_TMPDIR/subhome.XXXXXX")"
    other="$(mktemp -d "$TEST_TMPDIR/subother.XXXXXX")"
    mkdir -p "$base/default/subagents/claude" "$home/claude-real/agents"
    printf -- '---\nname: reviewer\ndescription: d\n---\n' > "$base/default/subagents/claude/reviewer.md"
    printf -- '---\nname: mine\ndescription: managed\n---\n' > "$base/default/subagents/claude/mine.md"
    printf -- 'x' > "$base/default/subagents/claude/notes.txt"
    mkdir -p "$base/default/subagents/claude/master-skill"
    printf -- '---\nname: master-skill\ndescription: d\n---\n' > "$base/default/subagents/claude/master-skill/SKILL.md"
    ln -s "$home/claude-real" "$home/.claude"
    # Real file created in the container with the same file name as a managed one.
    printf -- '---\nname: mine\ndescription: container\n---\n' > "$home/claude-real/agents/mine.md"
    # Stale link into the store (subagent removed on the host).
    ln -s "$base/default/subagents/claude/gone.md" "$home/claude-real/agents/gone.md"
    # Foreign files: a real file, a link elsewhere, a dangling link elsewhere.
    printf -- 'own' > "$home/claude-real/agents/own.md"
    printf -- 'x' > "$other/elsewhere.md"
    ln -s "$other/elsewhere.md" "$home/claude-real/agents/elsewhere.md"
    ln -s "$other/missing.md" "$home/claude-real/agents/foreign-dangling.md"
    result="$(
        export GLOBAL_WORKSPACE_ROOT="$base"
        export HOME="$home"
        export AGENT="claude"
        export EXITBOX_WORKSPACE_NAME="default"
        eval "$(extract_func link_subagents)"
        link_subagents 2>&1
        a="$home/claude-real/agents"
        [[ -L "$a/reviewer.md" && "$(readlink "$a/reviewer.md")" == "$base/default/subagents/claude/reviewer.md" ]] && echo "LINKED"
        [[ ! -L "$a/mine.md" ]] && grep -q container "$a/mine.md" && echo "REAL_KEPT"
        [[ ! -e "$a/gone.md" && ! -L "$a/gone.md" ]] && echo "DANGLING_REMOVED"
        [[ -f "$a/own.md" && ! -L "$a/own.md" ]] && echo "OWN_KEPT"
        [[ "$(readlink "$a/elsewhere.md")" == "$other/elsewhere.md" ]] && echo "FOREIGN_LINK_KEPT"
        [[ -L "$a/foreign-dangling.md" ]] && echo "FOREIGN_DANGLING_KEPT"
        [[ ! -e "$a/notes.txt" ]] && echo "NON_MD_SKIPPED"
        [[ -z "$(find "$a" -iname '*master-skill*' -o -name 'SKILL.md')" ]] && echo "MASTER_SKILL_SKIPPED"
    )"
    assert_contains "link_subagents links store files" "$result" "LINKED"
    assert_contains "link_subagents keeps a real file with the same name" "$result" "REAL_KEPT"
    assert_contains "link_subagents warns about the skipped name" "$result" "[WARN] Subagent mine.md"
    assert_contains "link_subagents removes dangling store links" "$result" "DANGLING_REMOVED"
    assert_contains "link_subagents leaves real foreign files" "$result" "OWN_KEPT"
    assert_contains "link_subagents leaves foreign links" "$result" "FOREIGN_LINK_KEPT"
    assert_contains "link_subagents leaves dangling foreign links" "$result" "FOREIGN_DANGLING_KEPT"
    assert_contains "link_subagents only links .md files" "$result" "NON_MD_SKIPPED"
    assert_contains "link_subagents does not link the master skill as a subagent" "$result" "MASTER_SKILL_SKIPPED"
}

test_link_subagents_claude

# ============================================================================
# Test: link_subagents does nothing for an agent without stored subagents:
# another agent's store is not its own
# ============================================================================
test_link_subagents_other_agents() {
    local base home result
    base="$(mktemp -d "$TEST_TMPDIR/subws.XXXXXX")"
    home="$(mktemp -d "$TEST_TMPDIR/subhome.XXXXXX")"
    mkdir -p "$base/default/subagents/claude" "$home/.claude" "$home/.codex"
    printf -- '---\nname: r\ndescription: d\n---\n' > "$base/default/subagents/claude/r.md"
    result="$(
        export GLOBAL_WORKSPACE_ROOT="$base"
        export HOME="$home"
        export EXITBOX_WORKSPACE_NAME="default"
        eval "$(extract_func link_subagents)"
        for AGENT in codex opencode pi kimi; do
            export AGENT
            link_subagents
        done
        [[ -e "$home/.claude/agents" ]] && echo "TOUCHED_CLAUDE"
        find "$home" -name 'r.md' | grep -q . && echo "LINKED_SOMEWHERE"
        export AGENT=claude EXITBOX_WORKSPACE_NAME=empty
        link_subagents
        [[ -e "$home/.claude/agents" ]] && echo "CREATED_EMPTY_DIR"
        echo done
    )" 2>/dev/null
    assert_eq "link_subagents does nothing for an agent without its own store" "done" "$result"
}

test_link_subagents_other_agents

# ============================================================================
# Test: link_subagents copies each other agent's store into that agent's own
# agents directory, with the agent's own file extension
# ============================================================================
test_link_subagents_copy() {
    local base home result agent
    base="$(mktemp -d "$TEST_TMPDIR/subws.XXXXXX")"
    home="$(mktemp -d "$TEST_TMPDIR/subhome.XXXXXX")"
    mkdir -p "$home/.codex" "$home/.copilot" "$home/.cursor" "$home/.kimi-code" "$home/.config/opencode" "$home/.qwen" "$home/.pi"
    for agent in codex copilot cursor kimi opencode qwen pi; do
        mkdir -p "$base/default/subagents/$agent/master-skill"
        printf -- 'skill' > "$base/default/subagents/$agent/master-skill/SKILL.md"
        printf -- 'wrong extension' > "$base/default/subagents/$agent/notes.txt"
    done
    printf -- 'name = "x"\n' > "$base/default/subagents/codex/x.toml"
    printf -- 'not toml' > "$base/default/subagents/codex/x.md"
    printf -- 'copilot' > "$base/default/subagents/copilot/x.agent.md"
    printf -- 'plain md is not a copilot agent' > "$base/default/subagents/copilot/y.md"
    for agent in cursor kimi opencode qwen pi; do
        printf -- '%s' "$agent" > "$base/default/subagents/$agent/x.md"
    done
    result="$(
        export GLOBAL_WORKSPACE_ROOT="$base"
        export HOME="$home"
        export EXITBOX_WORKSPACE_NAME="default"
        eval "$(extract_func link_subagents)"
        for AGENT in codex copilot cursor kimi opencode qwen pi; do
            export AGENT
            link_subagents 2>&1
        done
        check() { # agent dir file
            local f="$home/$2/agents/$3"
            [[ -f "$f" && ! -L "$f" ]] && cmp -s "$f" "$base/default/subagents/$1/$3" \
                && [[ "$(cat "$home/$2/agents/.exitbox-subagents")" == "$3" ]] && echo "COPIED:$1"
        }
        check codex .codex x.toml
        check copilot .copilot x.agent.md
        check cursor .cursor x.md
        check kimi .kimi-code x.md
        check opencode .config/opencode x.md
        check qwen .qwen x.md
        [[ ! -e "$home/.codex/agents/x.md" && ! -e "$home/.copilot/agents/y.md" ]] && echo "OTHER_EXT_SKIPPED"
        [[ -z "$(find "$home" -name 'notes.txt' -o -name 'SKILL.md' -o -name 'master-skill')" ]] && echo "ONLY_AGENT_FILES"
        [[ ! -e "$home/.pi/agents" ]] && echo "PI_UNTOUCHED"
    )"
    for agent in codex copilot cursor kimi opencode qwen; do
        assert_contains "link_subagents copies the $agent store into its agents directory" "$result" "COPIED:$agent"
    done
    assert_contains "link_subagents copies only files with the agent's extension" "$result" "OTHER_EXT_SKIPPED"
    assert_contains "link_subagents copies no notes or master skill as subagents" "$result" "ONLY_AGENT_FILES"
    assert_contains "link_subagents does nothing for Pi, which has no subagents" "$result" "PI_UNTOUCHED"
}

test_link_subagents_copy

# ============================================================================
# Test: copied subagents are refreshed and removed on the next start, and
# the container's own files are never touched
# ============================================================================
test_link_subagents_copy_refresh() {
    local base home result store a
    base="$(mktemp -d "$TEST_TMPDIR/subws.XXXXXX")"
    home="$(mktemp -d "$TEST_TMPDIR/subhome.XXXXXX")"
    store="$base/default/subagents/qwen"
    a="$home/.qwen/agents"
    mkdir -p "$store" "$a" "$home/outside"
    printf -- 'v1' > "$store/keep.md"
    printf -- 'v1' > "$store/gone.md"
    printf -- 'managed' > "$store/mine.md"
    # The container has its own file with a managed file's name, and one of its own.
    printf -- 'container' > "$a/mine.md"
    printf -- 'own' > "$a/own.md"
    printf -- 'safe' > "$home/outside/victim.md"
    result="$(
        export GLOBAL_WORKSPACE_ROOT="$base"
        export HOME="$home"
        export AGENT="qwen"
        export EXITBOX_WORKSPACE_NAME="default"
        eval "$(extract_func link_subagents)"
        link_subagents 2>&1
        [[ "$(cat "$a/mine.md")" == "container" ]] && echo "REAL_KEPT"
        ! grep -qx 'mine.md' "$a/.exitbox-subagents" && echo "REAL_NOT_CLAIMED"

        # Next start: the session edited a copy, one subagent was removed on
        # the host, one changed, and the manifest was tampered with.
        printf -- 'edited in the container' > "$a/keep.md"
        rm "$store/gone.md"
        printf -- 'v2' > "$store/keep.md"
        printf -- '../../outside/victim.md\n' >> "$a/.exitbox-subagents"
        link_subagents 2>/dev/null
        [[ "$(cat "$a/keep.md")" == "v2" ]] && echo "REFRESHED"
        [[ ! -e "$a/gone.md" ]] && echo "REMOVED"
        [[ "$(cat "$a/own.md")" == "own" && "$(cat "$a/mine.md")" == "container" ]] && echo "OWN_KEPT"
        [[ "$(cat "$home/outside/victim.md")" == "safe" ]] && echo "MANIFEST_CONFINED"
        [[ "$(cat "$a/.exitbox-subagents")" == "keep.md" ]] && echo "MANIFEST_REWRITTEN"

        # The whole store is removed on the host: the copies go, the rest stays.
        rm -rf "$store"
        link_subagents 2>/dev/null
        [[ ! -e "$a/keep.md" && ! -e "$a/.exitbox-subagents" && -f "$a/own.md" ]] && echo "STORE_GONE_CLEANED"
    )"
    assert_contains "link_subagents keeps a container file with a managed name" "$result" "REAL_KEPT"
    assert_contains "link_subagents warns about the skipped copy" "$result" "[WARN] Subagent mine.md"
    assert_contains "link_subagents does not claim a file it did not copy" "$result" "REAL_NOT_CLAIMED"
    assert_contains "link_subagents refreshes a copy from the store" "$result" "REFRESHED"
    assert_contains "link_subagents removes the copy of a removed subagent" "$result" "REMOVED"
    assert_contains "link_subagents leaves the container's own files" "$result" "OWN_KEPT"
    assert_contains "link_subagents ignores manifest entries outside the agents directory" "$result" "MANIFEST_CONFINED"
    assert_contains "link_subagents rewrites its manifest" "$result" "MANIFEST_REWRITTEN"
    assert_contains "link_subagents removes its copies when the store is gone" "$result" "STORE_GONE_CLEANED"
}

test_link_subagents_copy_refresh

# ============================================================================
# Test: workspace_switch_menu signals the host and never writes config.yaml
# (mounted read-only; a writable config let agents alter the next run).
# ============================================================================
test_workspace_switch_menu_no_config_write() {
    local cfgdir result
    cfgdir="$(mktemp -d "$TEST_TMPDIR/wscfg.XXXXXX")"
    printf 'workspaces:\n  active: work\n' > "$cfgdir/config.yaml"
    chmod 0444 "$cfgdir/config.yaml"
    result="$(
        export GLOBAL_CONFIG_FILE="$cfgdir/config.yaml"
        export EXITBOX_WORKSPACE_NAME="work"
        ensure_workspace_files() { :; }
        effective_session_name() { echo "default"; }
        workspace_entries() { echo "global:personal"; }
        fzf() { echo "global:personal"; }
        yq() { echo "YQ_CALLED $*"; }
        set_active_workspace() { echo "YQ_CALLED set_active_workspace $*"; }
        write_session_action() { echo "ACTION ws=$1 session=$2 resume=$3"; }
        request_graceful_switch() { :; }
        eval "$(extract_func workspace_switch_menu)"
        workspace_switch_menu
    )" 2>/dev/null
    assert_contains "switch menu writes session action for host" "$result" "ACTION ws=personal session=default resume=true"
    assert_eq "switch menu does not call yq" "" "$(printf '%s' "$result" | grep YQ_CALLED || true)"
    assert_file_content "switch menu leaves config.yaml untouched" "$cfgdir/config.yaml" "$(printf 'workspaces:\n  active: work')"
}

test_workspace_switch_menu_no_config_write

# ============================================================================
# Test: link_skills wires skills into Kimi Code's skills dirs
# ============================================================================
test_link_skills_kimi() {
    local base home result
    base="$(mktemp -d "$TEST_TMPDIR/skillws.XXXXXX")"
    home="$(mktemp -d "$TEST_TMPDIR/skillhome.XXXXXX")"
    mkdir -p "$base/default/skills/my-skill"
    printf -- '---\nname: my-skill\n---\n' > "$base/default/skills/my-skill/SKILL.md"
    mkdir -p "$home/.kimi-code"
    result="$(
        export GLOBAL_WORKSPACE_ROOT="$base"
        export HOME="$home"
        export AGENT="kimi"
        export EXITBOX_WORKSPACE_NAME="default"
        eval "$(extract_func link_skills)"
        link_skills
        [[ -L "$home/.kimi-code/skills/my-skill" ]] && echo "LINKED_KIMI_CODE"
        [[ -L "$home/.agents/skills/my-skill" ]] && echo "LINKED_AGENTS"
    )" 2>/dev/null
    assert_contains "link_skills links skills into ~/.kimi-code/skills" "$result" "LINKED_KIMI_CODE"
    assert_contains "link_skills links skills into ~/.agents/skills for Kimi" "$result" "LINKED_AGENTS"
}

test_link_skills_kimi

# ============================================================================
# Test: link_skills does not nest a link inside a real skill dir
# ============================================================================
test_link_skills_skips_real_dir() {
    local base home result
    base="$(mktemp -d "$TEST_TMPDIR/skillws.XXXXXX")"
    home="$(mktemp -d "$TEST_TMPDIR/skillhome.XXXXXX")"
    mkdir -p "$base/default/skills/graphify"
    printf -- '---\nname: graphify\n---\n' > "$base/default/skills/graphify/SKILL.md"
    # graphify's own CLI already installed a real directory at that name.
    mkdir -p "$home/.kimi-code/skills/graphify"
    result="$(
        export GLOBAL_WORKSPACE_ROOT="$base"
        export HOME="$home"
        export AGENT="kimi"
        export EXITBOX_WORKSPACE_NAME="default"
        eval "$(extract_func link_skills)"
        link_skills
        [[ -e "$home/.kimi-code/skills/graphify/graphify" ]] && echo NESTED || echo CLEAN
    )" 2>/dev/null
    assert_eq "link_skills leaves an existing real skill dir alone" "CLEAN" "$result"
}

test_link_skills_skips_real_dir

# ============================================================================
# Test: link_skills links the master skill from the read-only subagent store
# into every skill-capable agent, without needing a skills store
# ============================================================================
test_link_skills_master_skill() {
    local base home result agent
    base="$(mktemp -d "$TEST_TMPDIR/masterws.XXXXXX")"
    home="$(mktemp -d "$TEST_TMPDIR/masterhome.XXXXXX")"
    mkdir -p "$home/.claude" "$home/.config/opencode" "$home/.pi" "$home/.kimi-code" "$home/.qwen"
    for agent in claude codex opencode pi kimi qwen; do
        mkdir -p "$base/default/subagents/$agent/master-skill"
        printf -- '---\nname: master-skill\ndescription: routes for %s\n---\n' "$agent" > "$base/default/subagents/$agent/master-skill/SKILL.md"
    done
    result="$(
        export GLOBAL_WORKSPACE_ROOT="$base"
        export HOME="$home"
        export EXITBOX_WORKSPACE_NAME="default"
        eval "$(extract_func link_skills)"
        check() { # agent dir
            [[ "$(readlink "$home/$2/master-skill")" == "$base/default/subagents/$1/master-skill" ]] \
                && grep -q "routes for $1" "$home/$2/master-skill/SKILL.md" && echo "LINKED:$1:$2"
        }
        for AGENT in claude codex opencode pi kimi qwen; do
            export AGENT
            link_skills
            # Each agent gets its own master skill, also in the shared ~/.agents/skills.
            [[ "$AGENT" != claude && "$AGENT" != qwen ]] && check "$AGENT" .agents/skills
        done
        check claude .claude/skills
        check opencode .config/opencode/skills
        check pi .pi/agent/skills
        check kimi .kimi-code/skills
        [[ ! -e "$home/.qwen/skills" ]] && echo "QWEN_UNTOUCHED"
    )" 2>/dev/null
    assert_contains "link_skills links Claude's master skill" "$result" "LINKED:claude:.claude/skills"
    assert_contains "link_skills links Codex's master skill into ~/.agents/skills" "$result" "LINKED:codex:.agents/skills"
    assert_contains "link_skills links OpenCode's master skill" "$result" "LINKED:opencode:.config/opencode/skills"
    assert_contains "link_skills links Pi's master skill" "$result" "LINKED:pi:.pi/agent/skills"
    assert_contains "link_skills links Kimi's master skill" "$result" "LINKED:kimi:.kimi-code/skills"
    assert_contains "link_skills creates no skill dir for an agent without skill support" "$result" "QWEN_UNTOUCHED"
}

test_link_skills_master_skill

# ============================================================================
# Test: a removed master skill loses its link, and a skill of the same name
# from another source is kept
# ============================================================================
test_link_skills_master_skill_conflicts() {
    local base home result agent
    base="$(mktemp -d "$TEST_TMPDIR/masterws.XXXXXX")"
    home="$(mktemp -d "$TEST_TMPDIR/masterhome.XXXXXX")"
    mkdir -p "$base/default/subagents" "$home/.claude/skills" "$home/.kimi-code/skills/master-skill" "$home/.pi/agent/skills"
    # Claude: a link left behind by a master skill that was removed on the host.
    ln -s "$base/default/subagents/claude/master-skill" "$home/.claude/skills/master-skill"
    # Pi: a link to somebody else's skill.
    mkdir -p "$home/other-skill"
    ln -s "$home/other-skill" "$home/.pi/agent/skills/master-skill"
    result="$(
        export GLOBAL_WORKSPACE_ROOT="$base"
        export HOME="$home"
        export EXITBOX_WORKSPACE_NAME="default"
        eval "$(extract_func link_skills)"
        AGENT=claude link_skills 2>&1
        [[ ! -e "$home/.claude/skills/master-skill" && ! -L "$home/.claude/skills/master-skill" ]] && echo "STALE_REMOVED"
        AGENT=pi link_skills 2>&1
        [[ "$(readlink "$home/.pi/agent/skills/master-skill")" == "$home/other-skill" ]] && echo "UNRELATED_LINK_UNTOUCHED"

        for agent in claude kimi pi; do
            mkdir -p "$base/default/subagents/$agent/master-skill"
            printf -- '---\nname: master-skill\ndescription: routes\n---\n' > "$base/default/subagents/$agent/master-skill/SKILL.md"
        done
        AGENT=kimi link_skills 2>&1
        [[ -d "$home/.kimi-code/skills/master-skill" && ! -L "$home/.kimi-code/skills/master-skill" ]] && echo "REAL_DIR_KEPT"
        [[ ! -e "$home/.kimi-code/skills/master-skill/master-skill" ]] && echo "NOT_NESTED"
        AGENT=pi link_skills 2>/dev/null
        [[ "$(readlink "$home/.pi/agent/skills/master-skill")" == "$home/other-skill" ]] && echo "FOREIGN_LINK_KEPT"

        # A skill installed with `exitbox skills install` under the same name wins.
        mkdir -p "$base/default/skills/master-skill"
        printf -- '---\nname: master-skill\n---\n' > "$base/default/skills/master-skill/SKILL.md"
        AGENT=claude link_skills 2>/dev/null
        [[ "$(readlink "$home/.claude/skills/master-skill")" == "$base/default/skills/master-skill/" ]] && echo "INSTALLED_SKILL_WINS"
    )"
    assert_contains "link_skills drops the link of a removed master skill" "$result" "STALE_REMOVED"
    assert_contains "link_skills leaves a foreign link alone when there is no master skill" "$result" "UNRELATED_LINK_UNTOUCHED"
    assert_contains "link_skills keeps a real skill dir named master-skill" "$result" "REAL_DIR_KEPT"
    assert_contains "link_skills does not nest the master skill in a real dir" "$result" "NOT_NESTED"
    assert_contains "link_skills warns when another master-skill is in the way" "$result" "[WARN] Master skill"
    assert_contains "link_skills keeps a foreign link named master-skill" "$result" "FOREIGN_LINK_KEPT"
    assert_contains "link_skills lets an installed skill of the same name win" "$result" "INSTALLED_SKILL_WINS"
}

test_link_skills_master_skill_conflicts

# ============================================================================
# Test: setup_graphify installs/removes the /graphify skill per agent
# ============================================================================
test_setup_graphify() {
    local gpf sdf sgf
    gpf="$(extract_func graphify_platform_for_agent)"
    sdf="$(extract_func graphify_skill_dir_for_agent)"
    sgf="$(extract_func setup_graphify)"

    # graphify CLI present (External Tool selected) + compatible agent → runs
    # `graphify install --platform <X>`. setup_graphify redirects the command's
    # stdout to /dev/null, so the stub records its args to a file instead.
    local marker="$TEST_TMPDIR/gfy_args"
    rm -f "$marker"
    (
        export HOME; HOME="$(mktemp -d "$TEST_TMPDIR/gfy1.XXXXXX")"
        export AGENT="pi" GFY_MARKER="$marker"
        eval "$gpf"; eval "$sdf"; eval "$sgf"
        graphify() { echo "$*" > "$GFY_MARKER"; }
        setup_graphify
    ) 2>/dev/null
    local enabled=""
    [[ -f "$marker" ]] && enabled="$(cat "$marker")"
    assert_contains "setup_graphify installs when graphify CLI present" "$enabled" "install --platform pi"

    # graphify CLI absent (not selected) → removes a previously installed skill dir.
    local home2 removed
    home2="$(mktemp -d "$TEST_TMPDIR/gfy2.XXXXXX")"
    mkdir -p "$home2/.pi/agent/skills/graphify"
    removed="$(
        export HOME="$home2"
        export AGENT="pi"
        eval "$gpf"; eval "$sdf"; eval "$sgf"
        # No graphify stub and graphify is not installed in the test env, so
        # `command -v graphify` is false → the removal branch runs.
        setup_graphify
        [[ -d "$home2/.pi/agent/skills/graphify" ]] && echo PRESENT || echo GONE
    )" 2>/dev/null
    assert_eq "setup_graphify removes skill when CLI absent" "GONE" "$removed"

    # Incompatible agent (qwen) → no graphify invocation even when CLI present.
    local skipped
    skipped="$(
        export HOME; HOME="$(mktemp -d "$TEST_TMPDIR/gfy3.XXXXXX")"
        export AGENT="qwen"
        eval "$gpf"; eval "$sdf"; eval "$sgf"
        graphify() { echo "CALLED"; }
        setup_graphify
        echo DONE
    )" 2>/dev/null
    assert_eq "setup_graphify skips incompatible agent" "DONE" "$skipped"
}

test_setup_graphify

# ============================================================================
# GPU
# ============================================================================
echo ""
echo "Testing GPU diagnostics..."

SETUP_GPU_FUNC="$(extract_func setup_gpu)"
GPU_HINT_FUNC="$(extract_func gpu_userspace_hint)"

test_setup_gpu_noop_when_disabled() {
    local out
    out="$(
        unset EXITBOX_GPU
        eval "$SETUP_GPU_FUNC"
        setup_gpu
        echo DONE
    )" 2>&1
    assert_eq "setup_gpu does nothing when GPU is off" "DONE" "$out"
}

test_setup_gpu_reports_missing_devices() {
    local out
    out="$(
        export EXITBOX_GPU="true"
        eval "$SETUP_GPU_FUNC"
        # The test machine may itself have NVIDIA device nodes, so the probes
        # are stubbed: this asserts the diagnosis, not the host.
        ls() { return 1; }
        setup_gpu 2>&1
    )"
    assert_contains "setup_gpu names the missing device nodes" "$out" "no /dev/nvidia* device node is visible"
    assert_contains "setup_gpu points at the host check" "$out" "exitbox gpu status"
}

test_setup_gpu_reports_missing_cuda() {
    local out fakedev
    # /dev is not writable in the test environment, so drive the device probe
    # through a stub `ls` that reports a device node.
    out="$(
        export EXITBOX_GPU="true"
        eval "$SETUP_GPU_FUNC"
        eval "$GPU_HINT_FUNC"
        ls() { if [[ "$*" == *"/dev/nvidia"* ]]; then echo "/dev/nvidia0"; return 0; fi; command ls "$@"; }
        find() { return 0; }
        nvidia-smi() { return 1; }
        setup_gpu 2>&1
    )"
    assert_contains "setup_gpu reports a missing CUDA driver library" "$out" "no CUDA driver library was injected"
    assert_contains "setup_gpu names the capability that injects it" "$out" "NVIDIA_DRIVER_CAPABILITIES=compute"
    assert_contains "setup_gpu explains why nvidia-smi failed" "$out" "nvidia-smi did not run"
}

test_gpu_userspace_hint_per_flavor() {
    local musl glibc marker
    marker="$TEST_TMPDIR/alpine-release"
    : > "$marker"
    musl="$(eval "$GPU_HINT_FUNC"; gpu_userspace_hint "$marker")"
    glibc="$(eval "$GPU_HINT_FUNC"; gpu_userspace_hint "$TEST_TMPDIR/not-alpine")"

    assert_contains "gpu hint names the musl limit on Alpine" "$musl" "Alpine/musl"
    assert_contains "gpu hint points at the CUDA image on Alpine" "$musl" "CUDA image"
    assert_contains "gpu hint blames the host on the CUDA flavor" "$glibc" "exitbox gpu status"
}

test_setup_gpu_noop_when_disabled
test_setup_gpu_reports_missing_devices
test_setup_gpu_reports_missing_cuda
test_gpu_userspace_hint_per_flavor

# ============================================================================
# Plugins
# ============================================================================
echo ""
echo "Testing plugin setup..."

PLUGIN_ENV_VAR_FUNC="$(extract_func plugin_env_var)"
PLUGIN_SETTING_FUNC="$(extract_func plugin_setting)"
SETUP_PLUGINS_FUNC="$(extract_func setup_plugins)"
SETUP_CODEX_ROUTER_FUNC="$(extract_func setup_codex_router)"
CODEX_ROUTER_PROVIDER_ENV_FUNC="$(extract_func codex_router_provider_env)"
CODEX_ROUTER_STATE_DIR_FUNC="$(extract_func codex_router_state_dir)"

test_plugin_env_var_naming() {
    local out
    out="$(
        eval "$PLUGIN_ENV_VAR_FUNC"
        plugin_env_var codex-router providers
    )"
    assert_eq "plugin_env_var uppercases and replaces dashes" \
        "EXITBOX_PLUGIN_CODEX_ROUTER_PROVIDERS" "$out"

    # A provider id with a dash must land on the same variable ExitBox
    # generates on the host for that provider's key setting.
    out="$(
        eval "$PLUGIN_ENV_VAR_FUNC"
        plugin_env_var codex-router anthropic-api_key
    )"
    assert_eq "plugin_env_var maps a provider key setting" \
        "EXITBOX_PLUGIN_CODEX_ROUTER_ANTHROPIC_API_KEY" "$out"
}

test_plugin_setting_plain_value() {
    local out
    out="$(
        export EXITBOX_PLUGIN_CODEX_ROUTER_PROVIDERS="anthropic-api,deepseek"
        eval "$PLUGIN_ENV_VAR_FUNC"; eval "$PLUGIN_SETTING_FUNC"
        plugin_setting codex-router providers
    )"
    assert_eq "plugin_setting returns a plain value" "anthropic-api,deepseek" "$out"
}

test_plugin_setting_unset_value() {
    local out
    out="$(
        unset EXITBOX_PLUGIN_CODEX_ROUTER_PORT
        eval "$PLUGIN_ENV_VAR_FUNC"; eval "$PLUGIN_SETTING_FUNC"
        plugin_setting codex-router port
    )"
    assert_eq "plugin_setting returns empty for an unset setting" "" "$out"
}

test_plugin_setting_resolves_vault_reference() {
    local out
    out="$(
        export PATH="$TEST_TMPDIR/bin:$PATH"
        mkdir -p "$TEST_TMPDIR/bin"
        {
            echo '#!/usr/bin/env bash'
            echo '[[ "$1" == "get" ]] || exit 1'
            echo 'echo "secret-for-$2"'
        } > "$TEST_TMPDIR/bin/exitbox-vault"
        chmod +x "$TEST_TMPDIR/bin/exitbox-vault"
        export EXITBOX_PLUGIN_CODEX_ROUTER_ANTHROPIC_API_KEY="vault:MY_KEY"
        eval "$PLUGIN_ENV_VAR_FUNC"; eval "$PLUGIN_SETTING_FUNC"
        plugin_setting codex-router anthropic-api_key
    )"
    assert_eq "plugin_setting resolves a vault reference" "secret-for-MY_KEY" "$out"
}

test_setup_plugins_noop_without_env() {
    local out
    out="$(
        unset EXITBOX_PLUGINS
        eval "$SETUP_PLUGINS_FUNC"
        setup_codex_router() { echo "CALLED"; }
        setup_plugins
        echo DONE
    )" 2>&1
    assert_eq "setup_plugins does nothing without EXITBOX_PLUGINS" "DONE" "$out"
}

test_setup_plugins_dispatches_known_plugin() {
    local out
    out="$(
        export EXITBOX_PLUGINS="codex-router"
        eval "$SETUP_PLUGINS_FUNC"
        setup_codex_router() { echo "ROUTER"; }
        setup_plugins
    )" 2>&1
    assert_eq "setup_plugins dispatches codex-router" "ROUTER" "$out"
}

test_setup_plugins_warns_on_unknown_plugin() {
    local out
    out="$(
        export EXITBOX_PLUGINS="not-a-plugin"
        eval "$SETUP_PLUGINS_FUNC"
        setup_codex_router() { echo "ROUTER"; }
        setup_plugins 2>&1
    )"
    assert_contains "setup_plugins warns about an unknown plugin" "$out" "not-a-plugin"
}

test_setup_codex_router_requires_installed_binary() {
    local out
    out="$(
        export EXITBOX_PLUGINS="codex-router"
        export AGENT="codex"
        export PATH="/nonexistent-dir"
        eval "$PLUGIN_ENV_VAR_FUNC"; eval "$PLUGIN_SETTING_FUNC"; eval "$CODEX_ROUTER_STATE_DIR_FUNC"
        eval "$SETUP_CODEX_ROUTER_FUNC"
        setup_codex_router 2>&1
    )"
    assert_contains "setup_codex_router reports a missing install" "$out" "not installed in this image"
}

test_setup_codex_router_skips_unsupported_agent() {
    local out
    out="$(
        export AGENT="qwen"
        export PATH="$TEST_TMPDIR/bin2:$PATH"
        mkdir -p "$TEST_TMPDIR/bin2"
        {
            echo '#!/usr/bin/env bash'
            echo 'echo ROUTER "$@"'
        } > "$TEST_TMPDIR/bin2/model-router"
        chmod +x "$TEST_TMPDIR/bin2/model-router"
        eval "$PLUGIN_ENV_VAR_FUNC"; eval "$PLUGIN_SETTING_FUNC"; eval "$CODEX_ROUTER_STATE_DIR_FUNC"
        eval "$SETUP_CODEX_ROUTER_FUNC"
        setup_codex_router
        echo DONE
    )" 2>&1
    assert_eq "setup_codex_router leaves unsupported agents alone" "DONE" "$out"
}

test_setup_codex_router_configures_and_starts() {
    local home log out
    home="$(mktemp -d "$TEST_TMPDIR/router.XXXXXX")"
    log="$TEST_TMPDIR/router-calls.log"
    : > "$log"
    out="$(
        export HOME="$home"
        export AGENT="codex"
        export MODEL_ROUTER_LOG="$log"
        export PATH="$TEST_TMPDIR/bin3:$PATH"
        mkdir -p "$TEST_TMPDIR/bin3"
        {
            echo '#!/usr/bin/env bash'
            echo 'echo "$@" >> "$MODEL_ROUTER_LOG"'
            echo 'exit 0'
        } > "$TEST_TMPDIR/bin3/model-router"
        chmod +x "$TEST_TMPDIR/bin3/model-router"
        export EXITBOX_PLUGIN_CODEX_ROUTER_PROVIDERS="deepseek"
        export EXITBOX_PLUGIN_CODEX_ROUTER_AUTO_START="false"
        eval "$PLUGIN_ENV_VAR_FUNC"; eval "$PLUGIN_SETTING_FUNC"
        eval "$CODEX_ROUTER_PROVIDER_ENV_FUNC"; eval "$CODEX_ROUTER_STATE_DIR_FUNC"
        eval "$SETUP_CODEX_ROUTER_FUNC"
        setup_codex_router
        echo "STATE=$CODEX_ROUTER_STATE_DIR"
    )" 2>&1

    assert_contains "setup_codex_router persists state in the agent config dir" "$out" "$home/.codex/codex-router"
    assert_contains "setup_codex_router runs the headless setup" "$(cat "$log")" "setup --no-tray --providers deepseek"
    assert_file_content "setup_codex_router records the configured providers" \
        "$home/.codex/codex-router/.exitbox-providers" "deepseek"
    if grep -q "start --foreground" "$log"; then
        ((FAIL++))
        ERRORS+=("FAIL: setup_codex_router started the router although auto_start was false")
    else
        ((PASS++))
    fi
}

test_setup_codex_router_without_providers() {
    local home out
    home="$(mktemp -d "$TEST_TMPDIR/router2.XXXXXX")"
    out="$(
        export HOME="$home"
        export AGENT="codex"
        export PATH="$TEST_TMPDIR/bin3:$PATH"
        export MODEL_ROUTER_LOG="$TEST_TMPDIR/router-calls2.log"
        unset EXITBOX_PLUGIN_CODEX_ROUTER_PROVIDERS
        eval "$PLUGIN_ENV_VAR_FUNC"; eval "$PLUGIN_SETTING_FUNC"
        eval "$CODEX_ROUTER_PROVIDER_ENV_FUNC"; eval "$CODEX_ROUTER_STATE_DIR_FUNC"
        eval "$SETUP_CODEX_ROUTER_FUNC"
        setup_codex_router 2>&1
    )"
    assert_contains "setup_codex_router asks for providers when none are configured" "$out" "no providers configured"
}

test_codex_router_state_dir_per_agent() {
    local out
    out="$(
        export HOME="/home/user"
        eval "$CODEX_ROUTER_STATE_DIR_FUNC"
        printf '%s %s %s' "$(codex_router_state_dir codex)" "$(codex_router_state_dir claude)" "$(codex_router_state_dir cursor)"
    )"
    assert_eq "codex_router_state_dir keeps state in each agent's mounted config dir" \
        "/home/user/.codex/codex-router /home/user/.claude/codex-router /home/user/.cursor/codex-router" "$out"
}

# ---- claude-router ----------------------------------------------------------

SETUP_CLAUDE_ROUTER_FUNC="$(extract_func setup_claude_router)"
CLAUDE_ROUTER_DIR_FUNC="$(extract_func claude_router_dir)"

# make_router_stub writes a stand-in for exitbox-router and exitbox-vault
# into a bin dir and prints that dir. The stand-in records what it was
# started with in $ROUTER_STUB_DIR and reports healthy once "up" exists.
make_router_stub() {
    local bin
    bin="$(mktemp -d "$TEST_TMPDIR/crbin.XXXXXX")"
    cat > "$bin/exitbox-router" <<'STUB'
#!/usr/bin/env bash
case "${1:-serve}" in
    vault-keys)
        if [[ "$EXITBOX_ROUTER_ROUTES" == *not-a-route* ]]; then
            echo "exitbox-router: bad route" >&2
            exit 2
        fi
        for k in $ROUTER_STUB_KEYS; do echo "$k"; done
        ;;
    health)
        echo "${2:-}" >> "$ROUTER_STUB_DIR/health-ports"
        [[ -f "$ROUTER_STUB_DIR/up" ]]
        ;;
    serve)
        env | grep '^EXITBOX_ROUTER_' | sort > "$ROUTER_STUB_DIR/env"
        if [[ -n "${ROUTER_STUB_FAIL:-}" ]]; then
            echo "listen tcp 127.0.0.1: address already in use" >&2
            exit 1
        fi
        touch "$ROUTER_STUB_DIR/up"
        ;;
esac
STUB
    cat > "$bin/exitbox-vault" <<'STUB'
#!/usr/bin/env bash
# get <KEY>: one key is missing from the vault.
[[ "$1" == "get" ]] || exit 1
[[ "$2" == "KEY_MISSING" ]] && exit 1
echo "secret-for-$2"
STUB
    chmod +x "$bin/exitbox-router" "$bin/exitbox-vault"
    echo "$bin"
}

# run_claude_router runs setup_claude_router in a clean shell and prints its
# output followed by what the calling shell ends up with.
#   $1 home dir, $2 stub state dir, further args: VAR=value settings
run_claude_router() {
    local home="$1" state="$2" bin
    shift 2
    bin="$(make_router_stub)"
    (
        unset ANTHROPIC_BASE_URL ANTHROPIC_AUTH_TOKEN ANTHROPIC_API_KEY
        unset EXITBOX_PLUGIN_CLAUDE_ROUTER_ROUTES EXITBOX_PLUGIN_CLAUDE_ROUTER_PORT
        unset EXITBOX_PLUGIN_CLAUDE_ROUTER_AUTO_START EXITBOX_PLUGIN_CLAUDE_ROUTER_SANITIZE
        unset ROUTER_STUB_KEYS ROUTER_STUB_FAIL
        export HOME="$home" AGENT="claude" ROUTER_STUB_DIR="$state"
        export PATH="$bin:$PATH"
        export EXITBOX_PLUGIN_CLAUDE_ROUTER_AUTO_START="true"
        export EXITBOX_PLUGIN_CLAUDE_ROUTER_SANITIZE="true"
        local kv
        for kv in "$@"; do export "$kv"; done
        eval "$PLUGIN_ENV_VAR_FUNC"; eval "$PLUGIN_SETTING_FUNC"
        eval "$CLAUDE_ROUTER_DIR_FUNC"; eval "$SETUP_CLAUDE_ROUTER_FUNC"
        setup_claude_router
        echo "BASE_URL=${ANTHROPIC_BASE_URL:-unset}"
        echo "AUTH_TOKEN=${ANTHROPIC_AUTH_TOKEN:-unset}"
        echo "API_KEY=${ANTHROPIC_API_KEY:-unset}"
        echo "LEAKED=$(env | grep -c 'secret-for-')"
    ) 2>&1
}

assert_not_contains() {
    local desc="$1" haystack="$2" needle="$3"
    if [[ "$haystack" != *"$needle"* ]]; then
        ((PASS++))
    else
        ((FAIL++))
        ERRORS+=("FAIL: $desc: output must not contain '$needle'")
    fi
}

test_setup_claude_router_starts_and_points_claude_at_it() {
    local home state out env
    home="$(mktemp -d "$TEST_TMPDIR/cr.XXXXXX")"; state="$(mktemp -d "$TEST_TMPDIR/crs.XXXXXX")"
    out="$(run_claude_router "$home" "$state" \
        "EXITBOX_PLUGIN_CLAUDE_ROUTER_ROUTES=qwen=http://10.0.0.21:8080=vault:KEY_A,gemma=http://10.0.0.22:8000=vault:KEY_MISSING,phi=http://10.0.0.23:8000=vault:KEY_C" \
        "ROUTER_STUB_KEYS=KEY_A KEY_MISSING KEY_C")"
    env="$(cat "$state/env" 2>/dev/null)"

    assert_contains "setup_claude_router exports the base URL on the default port" "$out" "BASE_URL=http://127.0.0.1:4203"
    assert_contains "setup_claude_router leaves ANTHROPIC_AUTH_TOKEN alone" "$out" "AUTH_TOKEN=unset"
    assert_contains "setup_claude_router leaves ANTHROPIC_API_KEY alone" "$out" "API_KEY=unset"
    assert_contains "setup_claude_router keeps tokens out of the agent's environment" "$out" "LEAKED=0"
    assert_contains "setup_claude_router passes the routes to the router" "$env" \
        "EXITBOX_ROUTER_ROUTES=qwen=http://10.0.0.21:8080=vault:KEY_A,gemma=http://10.0.0.22:8000=vault:KEY_MISSING,phi=http://10.0.0.23:8000=vault:KEY_C"
    assert_contains "setup_claude_router passes the port" "$env" "EXITBOX_ROUTER_PORT=4203"
    assert_contains "setup_claude_router passes sanitize" "$env" "EXITBOX_ROUTER_SANITIZE=true"
    assert_contains "setup_claude_router logs into the Claude config dir" "$env" "EXITBOX_ROUTER_LOG=$home/.claude/exitbox-router/router.log"
    # Tokens go by position, so a missing key must leave its slot empty and
    # not shift the next token onto it.
    assert_contains "setup_claude_router hands over the first token" "$env" "EXITBOX_ROUTER_SECRET_0=secret-for-KEY_A"
    assert_not_contains "setup_claude_router leaves the slot of a missing key empty" "$env" "EXITBOX_ROUTER_SECRET_1="
    assert_contains "setup_claude_router keeps the third token in its slot" "$env" "EXITBOX_ROUTER_SECRET_2=secret-for-KEY_C"
    assert_contains "setup_claude_router warns about a missing vault key" "$out" "vault key 'KEY_MISSING' is not available"
    assert_not_contains "setup_claude_router never prints a token" "$out" "secret-for-"
}

test_setup_claude_router_uses_the_configured_port() {
    local home state out
    home="$(mktemp -d "$TEST_TMPDIR/cr.XXXXXX")"; state="$(mktemp -d "$TEST_TMPDIR/crs.XXXXXX")"
    out="$(run_claude_router "$home" "$state" \
        "EXITBOX_PLUGIN_CLAUDE_ROUTER_ROUTES=qwen=http://10.0.0.21:8080" \
        "EXITBOX_PLUGIN_CLAUDE_ROUTER_PORT=4999" "EXITBOX_PLUGIN_CLAUDE_ROUTER_SANITIZE=false")"
    assert_contains "setup_claude_router exports the configured port" "$out" "BASE_URL=http://127.0.0.1:4999"
    assert_contains "setup_claude_router starts the router on the configured port" "$(cat "$state/env")" "EXITBOX_ROUTER_PORT=4999"
    assert_contains "setup_claude_router passes sanitize=false on" "$(cat "$state/env")" "EXITBOX_ROUTER_SANITIZE=false"
    assert_contains "setup_claude_router checks health on the configured port" "$(cat "$state/health-ports")" "4999"
}

test_setup_claude_router_without_routes() {
    local home state out
    home="$(mktemp -d "$TEST_TMPDIR/cr.XXXXXX")"; state="$(mktemp -d "$TEST_TMPDIR/crs.XXXXXX")"
    out="$(run_claude_router "$home" "$state")"
    assert_contains "setup_claude_router asks for routes when none are configured" "$out" "no routes configured"
    assert_contains "setup_claude_router sets no base URL without routes" "$out" "BASE_URL=unset"
    assert_file_missing "setup_claude_router starts nothing without routes" "$state/env"
}

test_setup_claude_router_auto_start_off() {
    local home state out
    home="$(mktemp -d "$TEST_TMPDIR/cr.XXXXXX")"; state="$(mktemp -d "$TEST_TMPDIR/crs.XXXXXX")"
    out="$(run_claude_router "$home" "$state" \
        "EXITBOX_PLUGIN_CLAUDE_ROUTER_ROUTES=qwen=http://10.0.0.21:8080" "EXITBOX_PLUGIN_CLAUDE_ROUTER_AUTO_START=false")"
    assert_contains "setup_claude_router sets no base URL with auto_start off" "$out" "BASE_URL=unset"
    assert_file_missing "setup_claude_router starts nothing with auto_start off" "$state/env"
}

test_setup_claude_router_failed_start_leaves_claude_alone() {
    local home state out
    home="$(mktemp -d "$TEST_TMPDIR/cr.XXXXXX")"; state="$(mktemp -d "$TEST_TMPDIR/crs.XXXXXX")"
    out="$(run_claude_router "$home" "$state" \
        "EXITBOX_PLUGIN_CLAUDE_ROUTER_ROUTES=qwen=http://10.0.0.21:8080" "ROUTER_STUB_FAIL=1")"
    assert_contains "setup_claude_router leaves the base URL unset when the router fails" "$out" "BASE_URL=unset"
    assert_contains "setup_claude_router says the router did not start" "$out" "did not start"
    assert_contains "setup_claude_router names the log file" "$out" "$home/.claude/exitbox-router/start.log"
    assert_contains "setup_claude_router keeps the router's own error in that log" \
        "$(cat "$home/.claude/exitbox-router/start.log" 2>/dev/null)" "address already in use"
}

test_setup_claude_router_respects_an_existing_base_url() {
    local home state out
    home="$(mktemp -d "$TEST_TMPDIR/cr.XXXXXX")"; state="$(mktemp -d "$TEST_TMPDIR/crs.XXXXXX")"
    out="$(run_claude_router "$home" "$state" \
        "EXITBOX_PLUGIN_CLAUDE_ROUTER_ROUTES=qwen=http://10.0.0.21:8080" "ANTHROPIC_BASE_URL=https://gateway.example.com")"
    assert_contains "setup_claude_router keeps a base URL that is already set" "$out" "BASE_URL=https://gateway.example.com"
    assert_contains "setup_claude_router says why it did not start" "$out" "ANTHROPIC_BASE_URL is already set"
    assert_file_missing "setup_claude_router starts nothing next to another base URL" "$state/env"
}

test_setup_claude_router_rejects_invalid_routes() {
    local home state out
    home="$(mktemp -d "$TEST_TMPDIR/cr.XXXXXX")"; state="$(mktemp -d "$TEST_TMPDIR/crs.XXXXXX")"
    out="$(run_claude_router "$home" "$state" "EXITBOX_PLUGIN_CLAUDE_ROUTER_ROUTES=not-a-route")"
    assert_contains "setup_claude_router reports routes that do not parse" "$out" "routes setting is not valid"
    assert_contains "setup_claude_router sets no base URL for invalid routes" "$out" "BASE_URL=unset"
    assert_file_missing "setup_claude_router starts nothing for invalid routes" "$state/env"
}

test_setup_claude_router_reuses_a_running_router() {
    local home state out
    home="$(mktemp -d "$TEST_TMPDIR/cr.XXXXXX")"; state="$(mktemp -d "$TEST_TMPDIR/crs.XXXXXX")"
    touch "$state/up"
    out="$(run_claude_router "$home" "$state" \
        "EXITBOX_PLUGIN_CLAUDE_ROUTER_ROUTES=qwen=http://10.0.0.21:8080=vault:KEY_A" "ROUTER_STUB_KEYS=KEY_A")"
    assert_contains "setup_claude_router points at a router that already runs" "$out" "BASE_URL=http://127.0.0.1:4203"
    assert_file_missing "setup_claude_router does not start a second router" "$state/env"
}

test_setup_claude_router_only_for_claude() {
    local out
    out="$(
        unset ANTHROPIC_BASE_URL
        export AGENT="codex" EXITBOX_PLUGIN_CLAUDE_ROUTER_ROUTES="qwen=http://10.0.0.21:8080"
        eval "$PLUGIN_ENV_VAR_FUNC"; eval "$PLUGIN_SETTING_FUNC"
        eval "$CLAUDE_ROUTER_DIR_FUNC"; eval "$SETUP_CLAUDE_ROUTER_FUNC"
        setup_claude_router
        echo "BASE_URL=${ANTHROPIC_BASE_URL:-unset}"
    )" 2>&1
    assert_eq "setup_claude_router leaves other agents alone" "BASE_URL=unset" "$out"
}

test_setup_claude_router_requires_installed_binary() {
    local out
    out="$(
        unset ANTHROPIC_BASE_URL
        export AGENT="claude" PATH="/nonexistent-dir"
        export EXITBOX_PLUGIN_CLAUDE_ROUTER_ROUTES="qwen=http://10.0.0.21:8080"
        eval "$PLUGIN_ENV_VAR_FUNC"; eval "$PLUGIN_SETTING_FUNC"
        eval "$CLAUDE_ROUTER_DIR_FUNC"; eval "$SETUP_CLAUDE_ROUTER_FUNC"
        setup_claude_router 2>&1
        echo "BASE_URL=${ANTHROPIC_BASE_URL:-unset}"
    )"
    assert_contains "setup_claude_router reports a missing install" "$out" "not installed in this image"
    assert_contains "setup_claude_router sets no base URL without the binary" "$out" "BASE_URL=unset"
}

test_setup_plugins_dispatches_claude_router() {
    local out
    out="$(
        export EXITBOX_PLUGINS="codex-router,claude-router"
        eval "$SETUP_PLUGINS_FUNC"
        setup_codex_router() { echo "CODEX"; }
        setup_claude_router() { echo "CLAUDE"; }
        setup_plugins
    )"
    assert_eq "setup_plugins dispatches claude-router" "CODEX
CLAUDE" "$out"
}

test_codex_router_state_dir_per_agent
test_plugin_env_var_naming
test_plugin_setting_plain_value
test_plugin_setting_unset_value
test_plugin_setting_resolves_vault_reference
test_setup_plugins_noop_without_env
test_setup_plugins_dispatches_known_plugin
test_setup_plugins_warns_on_unknown_plugin
test_setup_codex_router_requires_installed_binary
test_setup_codex_router_skips_unsupported_agent
test_setup_codex_router_configures_and_starts
test_setup_codex_router_without_providers
test_setup_claude_router_starts_and_points_claude_at_it
test_setup_claude_router_uses_the_configured_port
test_setup_claude_router_without_routes
test_setup_claude_router_auto_start_off
test_setup_claude_router_failed_start_leaves_claude_alone
test_setup_claude_router_respects_an_existing_base_url
test_setup_claude_router_rejects_invalid_routes
test_setup_claude_router_reuses_a_running_router
test_setup_claude_router_only_for_claude
test_setup_claude_router_requires_installed_binary
test_setup_plugins_dispatches_claude_router

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
