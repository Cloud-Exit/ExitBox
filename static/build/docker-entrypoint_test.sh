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

CAPTURE_FUNC="$(extract_func capture_resume_token)"
BUILD_FUNC="$(extract_func build_resume_args)"

# ============================================================================
# Test: capture_resume_token for Claude
# ============================================================================
test_capture_resume_token_claude() {
    local tmpdir="$TEST_TMPDIR/crt_claude"
    mkdir -p "$tmpdir"

    local result
    result="$(
        AGENT="claude"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        tmux() { echo "some output"; echo "claude --resume abc123def"; echo "more"; }
        eval "$CAPTURE_FUNC"
        capture_resume_token
    )" 2>/dev/null

    assert_file_content "capture_resume_token (claude)" \
        "$tmpdir/default/claude/.resume-token" "abc123def"
}

# ============================================================================
# Test: capture_resume_token for Claude with -r flag
# ============================================================================
test_capture_resume_token_claude_short() {
    local tmpdir="$TEST_TMPDIR/crt_claude_short"
    mkdir -p "$tmpdir"

    local result
    result="$(
        AGENT="claude"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        tmux() { echo "claude -r shorttoken456"; }
        eval "$CAPTURE_FUNC"
        capture_resume_token
    )" 2>/dev/null

    assert_file_content "capture_resume_token (claude -r)" \
        "$tmpdir/default/claude/.resume-token" "shorttoken456"
}

# ============================================================================
# Test: capture_resume_token for Codex (should write "last")
# ============================================================================
test_capture_resume_token_codex() {
    local tmpdir="$TEST_TMPDIR/crt_codex"
    mkdir -p "$tmpdir"

    local result
    result="$(
        AGENT="codex"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        tmux() { echo "some codex output"; }
        eval "$CAPTURE_FUNC"
        capture_resume_token
    )" 2>/dev/null

    assert_file_content "capture_resume_token (codex)" \
        "$tmpdir/default/codex/.resume-token" "last"
}

# ============================================================================
# Test: capture_resume_token for OpenCode (should write "last")
# ============================================================================
test_capture_resume_token_opencode() {
    local tmpdir="$TEST_TMPDIR/crt_opencode"
    mkdir -p "$tmpdir"

    local result
    result="$(
        AGENT="opencode"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        tmux() { echo "some opencode output"; }
        eval "$CAPTURE_FUNC"
        capture_resume_token
    )" 2>/dev/null

    assert_file_content "capture_resume_token (opencode)" \
        "$tmpdir/default/opencode/.resume-token" "last"
}

# ============================================================================
# Test: capture_resume_token disabled
# ============================================================================
test_capture_resume_token_disabled() {
    local tmpdir="$TEST_TMPDIR/crt_disabled"
    mkdir -p "$tmpdir"

    local result
    result="$(
        AGENT="claude"
        EXITBOX_AUTO_RESUME="false"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        tmux() { echo "claude --resume shouldnotcapture"; }
        eval "$CAPTURE_FUNC"
        capture_resume_token
    )" 2>/dev/null

    assert_file_missing "capture_resume_token (disabled)" \
        "$tmpdir/default/claude/.resume-token"
}

# ============================================================================
# Test: build_resume_args for Claude
# ============================================================================
test_build_resume_args_claude() {
    local tmpdir="$TEST_TMPDIR/bra_claude"
    mkdir -p "$tmpdir/default/claude"
    echo "mytoken123" > "$tmpdir/default/claude/.resume-token"

    local result
    result="$(
        AGENT="claude"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
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
    mkdir -p "$tmpdir/default/codex"
    echo "last" > "$tmpdir/default/codex/.resume-token"

    local result
    result="$(
        AGENT="codex"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
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
    mkdir -p "$tmpdir/default/opencode"
    echo "last" > "$tmpdir/default/opencode/.resume-token"

    local result
    result="$(
        AGENT="opencode"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        eval "$BUILD_FUNC"
        build_resume_args
        echo "${RESUME_ARGS[*]}"
    )" 2>/dev/null

    assert_eq "build_resume_args (opencode)" "--continue" "$result"
}

# ============================================================================
# Test: build_resume_args disabled clears token
# ============================================================================
test_build_resume_args_disabled() {
    local tmpdir="$TEST_TMPDIR/bra_disabled"
    mkdir -p "$tmpdir/default/claude"
    echo "oldtoken" > "$tmpdir/default/claude/.resume-token"

    local result
    result="$(
        AGENT="claude"
        EXITBOX_AUTO_RESUME="false"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        eval "$BUILD_FUNC"
        build_resume_args
    )" 2>/dev/null

    assert_file_missing "build_resume_args (disabled removes token)" \
        "$tmpdir/default/claude/.resume-token"
}

# ============================================================================
# Test: build_resume_args with no token file
# ============================================================================
test_build_resume_args_no_token() {
    local tmpdir="$TEST_TMPDIR/bra_notoken"
    mkdir -p "$tmpdir/default/claude"

    local result
    result="$(
        AGENT="claude"
        EXITBOX_AUTO_RESUME="true"
        GLOBAL_WORKSPACE_ROOT="$tmpdir"
        EXITBOX_WORKSPACE_NAME="default"
        eval "$BUILD_FUNC"
        build_resume_args
        echo "${RESUME_ARGS[*]}"
    )" 2>/dev/null

    assert_eq "build_resume_args (no token)" "" "$result"
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
test_capture_resume_token_disabled
test_build_resume_args_claude
test_build_resume_args_codex
test_build_resume_args_opencode
test_build_resume_args_disabled
test_build_resume_args_no_token

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
