#!/usr/bin/env bash
# =============================================================================
# entrypoint.sh: runs INSIDE each Vagrant VM
# Cycle: audit → fix → audit → rollback → audit
# Writes structured results to /tmp/results/<distro>.json
# =============================================================================
set -uo pipefail

HARDENER="/opt/hardener/hardener"
GUIDE_PATH="/opt/hardener/guide"
RESULTS_DIR="/tmp/results"
DISTRO="${DISTRO_NAME:-unknown}"
SECURITY_LEVEL="${SECURITY_LEVEL:-baseline}"

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

# Parse summary lines from hardener output
# Handles multi-line visual boxes by joining the "Summary for" line with the next line
parse_summaries() {
    local output="$1"
    local summaries=""

    # Strip box-drawing characters, then join each "Summary for" line with
    # its continuation line before extracting numbers.
    local cleaned_output
    cleaned_output=$(echo "$output" | sed 's/│//g; s/╭//g; s/╰//g; s/─//g' | tr -s ' ')

    while IFS= read -r line; do
        local title
        title=$(echo "$line" | sed -n "s/.*Summary for '\([^']*\)'.*/\1/p")

        local total passed failed errors skipped fixed not_configured
        total=$(echo "$line"         | grep -oE '[0-9]+ total'         | awk '{print $1}')
        passed=$(echo "$line"        | grep -oE '[0-9]+ passed'        | awk '{print $1}')
        failed=$(echo "$line"        | grep -oE '[0-9]+ failed'        | awk '{print $1}')
        errors=$(echo "$line"        | grep -oE '[0-9]+ errors'        | awk '{print $1}')
        skipped=$(echo "$line"       | grep -oE '[0-9]+ skipped'       | awk '{print $1}')
        fixed=$(echo "$line"         | grep -oE '[0-9]+ fixed'         | awk '{print $1}')
        not_configured=$(echo "$line" | grep -oE '[0-9]+ not_configured' | awk '{print $1}')

        total=${total:-0}; passed=${passed:-0}; failed=${failed:-0}
        errors=${errors:-0}; skipped=${skipped:-0}; fixed=${fixed:-0}
        not_configured=${not_configured:-0}

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
        "not_configured": $not_configured
      }
ENTRY
        )

        if [ -n "$summaries" ]; then
            summaries="$summaries,
$entry"
        else
            summaries="$entry"
        fi
    done < <(echo "$cleaned_output" | grep -A 1 "Summary for " | grep -v "^--$" | paste - - | tr '\t' ' ' | tr -s ' ' | grep "Summary for ")

    if [ -z "$summaries" ]; then
        summaries='      { "suite": "NO_OUTPUT", "total": 0, "passed": 0, "failed": 0, "fixed": 0, "errors": 0, "skipped": 0 }'
    fi

    echo "$summaries"
}

# ── preflight ────────────────────────────────────────────────────────────────

echo "╔══════════════════════════════════════════════════════════╗"
echo "║  Hardener Test — $DISTRO"
echo "║  $(ts)"
echo "╚══════════════════════════════════════════════════════════╝"

if [ ! -x "$HARDENER" ]; then
    echo "FATAL: $HARDENER not found or not executable" | tee -a "$LOG"
    cat > "$REPORT" <<EOF
{ "distro": "$DISTRO", "timestamp": "$(ts)", "error": "hardener binary not found" }
EOF
    exit 1
fi

if [ ! -d "$GUIDE_PATH" ]; then
    echo "FATAL: guide directory $GUIDE_PATH not found" | tee -a "$LOG"
    cat > "$REPORT" <<EOF
{ "distro": "$DISTRO", "timestamp": "$(ts)", "error": "guide directory not found" }
EOF
    exit 1
fi

SUITE_FILES=$(find "$GUIDE_PATH" -name '*.md' -exec grep -l 'checksuites:' {} + 2>/dev/null | wc -l)
echo "Found $SUITE_FILES guide files with checksuites" | tee -a "$LOG"

# ── step 1: initial audit ────────────────────────────────────────────────────

echo ""
echo "▶ STEP 1/5: Initial Audit"
run_step "01-audit-initial" \
    "$HARDENER" audit --path "$GUIDE_PATH" --security-level "$SECURITY_LEVEL" --all
AUDIT1_RC=$_STEP_RC
AUDIT1_SUMMARIES=$(parse_summaries "$_STEP_OUTPUT")
echo "  Exit code: $AUDIT1_RC"

# ── step 2: fix ──────────────────────────────────────────────────────────────

echo ""
echo "▶ STEP 2/5: Fix"
run_step "02-fix" \
    "$HARDENER" fix --path "$GUIDE_PATH" --security-level "$SECURITY_LEVEL" --all
FIX_RC=$_STEP_RC
FIX_SUMMARIES=$(parse_summaries "$_STEP_OUTPUT")
echo "  Exit code: $FIX_RC"

# ── step 3: post-fix audit ───────────────────────────────────────────────────

echo ""
echo "▶ STEP 3/5: Post-Fix Audit"
run_step "03-audit-postfix" \
    "$HARDENER" audit --path "$GUIDE_PATH" --security-level "$SECURITY_LEVEL" --all
AUDIT2_RC=$_STEP_RC
AUDIT2_SUMMARIES=$(parse_summaries "$_STEP_OUTPUT")
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
run_step "05-audit-postrollback" \
    "$HARDENER" audit --path "$GUIDE_PATH" --security-level "$SECURITY_LEVEL" --all
AUDIT3_RC=$_STEP_RC
AUDIT3_SUMMARIES=$(parse_summaries "$_STEP_OUTPUT")
echo "  Exit code: $AUDIT3_RC"

# ── assemble JSON report ────────────────────────────────────────────────────

cat > "$REPORT" <<EOF
{
  "distro": "$DISTRO",
  "timestamp": "$(ts)",
  "security_level": "$SECURITY_LEVEL",
  "guide_files_found": $SUITE_FILES,
  "steps": {
    "01_audit_initial": {
      "exit_code": $AUDIT1_RC,
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
      "suites": [
$AUDIT2_SUMMARIES
      ]
    },
    "04_rollback": {
      "exit_code": $ROLLBACK_RC
    },
    "05_audit_postrollback": {
      "exit_code": $AUDIT3_RC,
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
