#!/usr/bin/env bash
# =============================================================================
# entrypoint.sh: runs INSIDE each Vagrant VM
# Cycle: audit → fix → audit → rollback → audit
# Writes structured results to /tmp/results/<distro>.json
# =============================================================================
set -uo pipefail

HARDENER="/opt/hardener/hardener"
RESULTS_DIR="/tmp/results"
DISTRO="${DISTRO_NAME:-unknown}"
SECURITY_LEVEL="${SECURITY_LEVEL:-baseline}"
PROFILE="${PROFILE:-}"
LABELS="${LABELS:-}"

# Prevent terminal wrapping — keeps summary lines intact for the text parser
export COLUMNS=300
export TERM=dumb

# Auto-detect input mode from what run_tests.sh staged
if [ -f "/opt/hardener/ruleset.yaml" ]; then
    HARDENER_INPUT="--ruleset /opt/hardener/ruleset.yaml"
    INPUT_DESC="ruleset: /opt/hardener/ruleset.yaml"
else
    HARDENER_INPUT="--path /opt/hardener/guide"
    INPUT_DESC="guide: /opt/hardener/guide"
fi

# Build optional flags for profile and label
EXTRA_FLAGS=""
[ -n "$PROFILE" ] && EXTRA_FLAGS="$EXTRA_FLAGS --profile $PROFILE"
[ -n "$LABELS"  ] && EXTRA_FLAGS="$EXTRA_FLAGS --label $LABELS"

mkdir -p "$RESULTS_DIR"

REPORT="$RESULTS_DIR/${DISTRO}.json"
LOG="$RESULTS_DIR/${DISTRO}.log"

# ── helpers ──────────────────────────────────────────────────────────────────

ts() { date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date; }

run_step() {
    local step_name="$1"
    shift
    local cmd=("$@")

    echo "────────────────────────────────────────" >> "$LOG"
    echo "[$(ts)] STEP: $step_name"                >> "$LOG"
    echo "CMD:  ${cmd[*]}"                         >> "$LOG"
    echo "────────────────────────────────────────" >> "$LOG"

    local output rc
    output=$("${cmd[@]}" 2>&1) && rc=$? || rc=$?

    echo "$output" >> "$LOG"
    echo "[$(ts)] EXIT CODE: $rc" >> "$LOG"
    echo "" >> "$LOG"

    _STEP_OUTPUT="$output"
    _STEP_RC=$rc
}

# Parse summary lines from hardener output.
# With COLUMNS=300 set above, summaries won't wrap — each box fits on one line.
parse_summaries() {
    local output="$1"
    local summaries=""

    local cleaned_output
    cleaned_output=$(echo "$output" | sed 's/│//g; s/╭//g; s/╰//g; s/─//g' | tr -s ' ')

    while IFS= read -r line; do
        local title
        title=$(echo "$line" | sed -n "s/.*Summary for '\([^']*\)'.*/\1/p")
        [ -z "$title" ] && continue

        # Join this line with the next (handles any minor 2-line wrap that slips through)
        local combined
        combined=$(echo "$cleaned_output" | grep -A 1 "Summary for '$title'" | tr '\n' ' ' | tr -s ' ')

        local total passed failed errors skipped fixed distro_skipped missing_cmd
        total=$(echo "$combined"          | grep -oE '[0-9]+ total'          | head -1 | awk '{print $1}')
        passed=$(echo "$combined"         | grep -oE '[0-9]+ passed'         | head -1 | awk '{print $1}')
        failed=$(echo "$combined"         | grep -oE '[0-9]+ failed'         | head -1 | awk '{print $1}')
        errors=$(echo "$combined"         | grep -oE '[0-9]+ errors'         | head -1 | awk '{print $1}')
        skipped=$(echo "$combined"        | grep -oE '[0-9]+ skipped'        | head -1 | awk '{print $1}')
        fixed=$(echo "$combined"          | grep -oE '[0-9]+ fixed'          | head -1 | awk '{print $1}')
        distro_skipped=$(echo "$combined" | grep -oE '[0-9]+ distro-skipped' | head -1 | awk '{print $1}')
        missing_cmd=$(echo "$combined"    | grep -oE '[0-9]+ missing-command'| head -1 | awk '{print $1}')

        total=${total:-0};          passed=${passed:-0};           failed=${failed:-0}
        errors=${errors:-0};        skipped=${skipped:-0};         fixed=${fixed:-0}
        distro_skipped=${distro_skipped:-0}; missing_cmd=${missing_cmd:-0}

        local entry
        entry=$(cat <<ENTRY
      {
        "suite": "$title",
        "total": $total,
        "passed": $passed,
        "failed": $failed,
        "fixed": $fixed,
        "errors": $errors,
        "skipped": $skipped,
        "distro_skipped": $distro_skipped,
        "missing_command": $missing_cmd
      }
ENTRY
        )

        if [ -n "$summaries" ]; then
            summaries="$summaries,
$entry"
        else
            summaries="$entry"
        fi
    done < <(echo "$cleaned_output" | grep "Summary for '")

    if [ -z "$summaries" ]; then
        summaries='      { "suite": "NO_OUTPUT", "total": 0, "passed": 0, "failed": 0, "fixed": 0, "errors": 0, "skipped": 0, "distro_skipped": 0, "missing_command": 0 }'
    fi

    echo "$summaries"
}

# Collect names of commands/files reported missing (one per line, sorted, unique).
# Matches both legacy "Required command" and current "Required resource" wording.
# Returns comma-separated list or empty string.
collect_missing_cmds() {
    local output="$1"
    echo "$output" \
        | grep -oE 'Required (command|resource) "[^"]+" (not found|not present)' \
        | grep -oE '"[^"]+"' \
        | tr -d '"' \
        | sort -u \
        | paste -sd ',' -
}

# Format a comma-separated list as a JSON string array.
fmt_json_array() {
    local cmds="$1"
    if [ -z "$cmds" ]; then
        echo "[]"
    else
        echo "[$( echo "$cmds" | tr ',' '\n' | sed 's/.*/  "&"/' | paste -sd ',' - | sed 's/  "/"/' )]"
    fi
}

# ── preflight ────────────────────────────────────────────────────────────────

echo "╔══════════════════════════════════════════════════════════╗"
echo "║  Hardener Test — $DISTRO"
echo "║  $(ts)"
[ -n "$PROFILE" ] && echo "║  Profile: $PROFILE"
[ -n "$LABELS"  ] && echo "║  Labels:  $LABELS"
echo "╚══════════════════════════════════════════════════════════╝"

if [ ! -x "$HARDENER" ]; then
    echo "FATAL: $HARDENER not found or not executable" | tee -a "$LOG"
    cat > "$REPORT" <<EOF
{ "distro": "$DISTRO", "timestamp": "$(ts)", "error": "hardener binary not found" }
EOF
    exit 1
fi

if [ -f "/opt/hardener/ruleset.yaml" ]; then
    SUITE_FILES=$(grep -c '^title:' /opt/hardener/ruleset.yaml 2>/dev/null || echo 0)
    echo "Found $SUITE_FILES suite(s) in ruleset" | tee -a "$LOG"
elif [ -d "/opt/hardener/guide" ]; then
    SUITE_FILES=$(find /opt/hardener/guide -name '*.md' -exec grep -l 'checksuites:' {} + 2>/dev/null | wc -l)
    echo "Found $SUITE_FILES guide files with checksuites" | tee -a "$LOG"
else
    echo "FATAL: neither /opt/hardener/ruleset.yaml nor /opt/hardener/guide found" | tee -a "$LOG"
    cat > "$REPORT" <<EOF
{ "distro": "$DISTRO", "timestamp": "$(ts)", "error": "no input staged (guide directory or ruleset.yaml)" }
EOF
    exit 1
fi

# ── step 1: initial audit ────────────────────────────────────────────────────

echo ""
echo "▶ STEP 1/5: Initial Audit"
# shellcheck disable=SC2086
run_step "01-audit-initial" \
    "$HARDENER" audit $HARDENER_INPUT --security-level "$SECURITY_LEVEL" --all $EXTRA_FLAGS
AUDIT1_RC=$_STEP_RC
AUDIT1_SUMMARIES=$(parse_summaries "$_STEP_OUTPUT")
AUDIT1_MISSING=$(collect_missing_cmds "$_STEP_OUTPUT")
echo "  Exit code: $AUDIT1_RC"
[ -n "$AUDIT1_MISSING" ] && echo "  Missing commands: $AUDIT1_MISSING"

# ── step 2: fix ──────────────────────────────────────────────────────────────

echo ""
echo "▶ STEP 2/5: Fix"
# shellcheck disable=SC2086
run_step "02-fix" \
    "$HARDENER" fix $HARDENER_INPUT --security-level "$SECURITY_LEVEL" --all $EXTRA_FLAGS
FIX_RC=$_STEP_RC
FIX_SUMMARIES=$(parse_summaries "$_STEP_OUTPUT")
echo "  Exit code: $FIX_RC"

# ── step 3: post-fix audit ───────────────────────────────────────────────────

echo ""
echo "▶ STEP 3/5: Post-Fix Audit"
# shellcheck disable=SC2086
run_step "03-audit-postfix" \
    "$HARDENER" audit $HARDENER_INPUT --security-level "$SECURITY_LEVEL" --all $EXTRA_FLAGS
AUDIT2_RC=$_STEP_RC
AUDIT2_SUMMARIES=$(parse_summaries "$_STEP_OUTPUT")
AUDIT2_MISSING=$(collect_missing_cmds "$_STEP_OUTPUT")
echo "  Exit code: $AUDIT2_RC"

# ── step 4: rollback ────────────────────────────────────────────────────────

echo ""
echo "▶ STEP 4/5: Rollback"
run_step "04-rollback" \
    "$HARDENER" rollback --latest
ROLLBACK_RC=$_STEP_RC
echo "  Exit code: $ROLLBACK_RC"

# ── step 5: post-rollback audit ─────────────────────────────────────────────

echo ""
echo "▶ STEP 5/5: Post-Rollback Audit"
# shellcheck disable=SC2086
run_step "05-audit-postrollback" \
    "$HARDENER" audit $HARDENER_INPUT --security-level "$SECURITY_LEVEL" --all $EXTRA_FLAGS
AUDIT3_RC=$_STEP_RC
AUDIT3_SUMMARIES=$(parse_summaries "$_STEP_OUTPUT")
AUDIT3_MISSING=$(collect_missing_cmds "$_STEP_OUTPUT")
echo "  Exit code: $AUDIT3_RC"

# ── assemble JSON report ─────────────────────────────────────────────────────

cat > "$REPORT" <<EOF
{
  "distro": "$DISTRO",
  "timestamp": "$(ts)",
  "security_level": "$SECURITY_LEVEL",
  "profile": "$PROFILE",
  "labels": "$LABELS",
  "guide_files_found": $SUITE_FILES,
  "steps": {
    "01_audit_initial": {
      "exit_code": $AUDIT1_RC,
      "missing_commands": $(fmt_json_array "$AUDIT1_MISSING"),
      "suites": [
$AUDIT1_SUMMARIES
      ]
    },
    "02_fix": {
      "exit_code": $FIX_RC,
      "suites": [
$FIX_SUMMARIES
      ]
    },
    "03_audit_postfix": {
      "exit_code": $AUDIT2_RC,
      "missing_commands": $(fmt_json_array "$AUDIT2_MISSING"),
      "suites": [
$AUDIT2_SUMMARIES
      ]
    },
    "04_rollback": {
      "exit_code": $ROLLBACK_RC
    },
    "05_audit_postrollback": {
      "exit_code": $AUDIT3_RC,
      "missing_commands": $(fmt_json_array "$AUDIT3_MISSING"),
      "suites": [
$AUDIT3_SUMMARIES
      ]
    }
  }
}
EOF

echo ""
echo "════════════════════════════════════════════════════════════"
echo "  Results: $REPORT"
echo "  Log:     $LOG"
echo "════════════════════════════════════════════════════════════"
