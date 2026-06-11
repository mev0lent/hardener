#!/usr/bin/env bash
# =============================================================================
# run_tests.sh: Hardener cross-distro test orchestrator (Vagrant/libvirt)
#
# Usage:
#   ./run_tests.sh --guide PATH   --binary PATH [options]
#   ./run_tests.sh --ruleset FILE --binary PATH [options]
#
# Options:
#   --level   baseline|medium|high  (default: baseline)
#   --distros "d1 d2 ..."           (default: ubuntu debian rocky opensuse archlinux)
#   --profile server|client|...     passed as --profile to hardener
#   --label   kernel,network,...    passed as --label to hardener
#
# Prerequisites:
#   - Vagrant 2.4+ with vagrant-libvirt plugin
#   - libvirt/KVM running (virt-host-validate should pass)
#   - Go toolchain (only if --binary not provided and using --guide)
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
GUIDE_PATH=""
RULESET_PATH=""
BINARY_PATH=""
SECURITY_LEVEL="baseline"
DISTROS="ubuntu debian rocky opensuse archlinux"
PROFILE=""
LABELS=""
RESULTS_DIR="$SCRIPT_DIR/results"
STAGING_DIR="$SCRIPT_DIR/.staging"
TIMESTAMP=$(date -u +"%Y%m%d-%H%M%S")

# ── argument parsing ─────────────────────────────────────────────────────────

while [[ $# -gt 0 ]]; do
    case "$1" in
        --guide)    GUIDE_PATH="$2";    shift 2 ;;
        --ruleset)  RULESET_PATH="$2";  shift 2 ;;
        --binary)   BINARY_PATH="$2";   shift 2 ;;
        --level)    SECURITY_LEVEL="$2"; shift 2 ;;
        --distros)  DISTROS="$2";       shift 2 ;;
        --profile)  PROFILE="$2";       shift 2 ;;
        --label)    LABELS="$2";        shift 2 ;;
        --help|-h)
            echo "Usage: $0 (--guide PATH | --ruleset FILE) --binary PATH [--level baseline|medium|high] [--distros \"d1 d2\"] [--profile PROFILE] [--label label1,label2]"
            exit 0 ;;
        *) echo "Unknown arg: $1"; exit 1 ;;
    esac
done

# ── validation ───────────────────────────────────────────────────────────────

if [ -z "$GUIDE_PATH" ] && [ -z "$RULESET_PATH" ]; then
    echo "ERROR: one of --guide or --ruleset is required"; exit 1
fi
if [ -n "$GUIDE_PATH" ] && [ -n "$RULESET_PATH" ]; then
    echo "ERROR: --guide and --ruleset are mutually exclusive"; exit 1
fi
if [ -n "$GUIDE_PATH" ] && [ ! -d "$GUIDE_PATH" ]; then
    echo "ERROR: guide directory not found: $GUIDE_PATH"; exit 1
fi
if [ -n "$RULESET_PATH" ] && [ ! -f "$RULESET_PATH" ]; then
    echo "ERROR: ruleset file not found: $RULESET_PATH"; exit 1
fi
if ! command -v vagrant >/dev/null 2>&1; then
    echo "ERROR: vagrant not found in PATH"; exit 1
fi
if ! vagrant plugin list | grep -q vagrant-libvirt; then
    echo "ERROR: vagrant-libvirt plugin not installed"; exit 1
fi

# ── stage files ──────────────────────────────────────────────────────────────

INPUT_LABEL="${GUIDE_PATH:-$RULESET_PATH}"

echo "╔══════════════════════════════════════════════════════════╗"
echo "║  Hardener Cross-Distro Test Runner (Vagrant/libvirt)"
echo "║  $(date -u +'%Y-%m-%d %H:%M:%S UTC')"
echo "║  Input:    $INPUT_LABEL"
echo "║  Level:    $SECURITY_LEVEL"
echo "║  Distros:  $DISTROS"
[ -n "$PROFILE" ] && echo "║  Profile:  $PROFILE"
[ -n "$LABELS"  ] && echo "║  Labels:   $LABELS"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""

rm -rf "$STAGING_DIR"
mkdir -p "$STAGING_DIR" "$RESULTS_DIR/$TIMESTAMP"

if [ -n "$GUIDE_PATH" ]; then
    mkdir -p "$STAGING_DIR/guide"
    cp -r "$GUIDE_PATH"/. "$STAGING_DIR/guide/"
else
    cp "$RULESET_PATH" "$STAGING_DIR/ruleset.yaml"
fi

cp "$SCRIPT_DIR/entrypoint.sh" "$STAGING_DIR/entrypoint.sh"

if [ -n "$BINARY_PATH" ] && [ ! -f "$BINARY_PATH" ]; then
    echo "ERROR: binary not found: $BINARY_PATH"
    exit 1
fi
if [ -n "$BINARY_PATH" ]; then
    echo "Using provided binary: $BINARY_PATH"
    cp "$BINARY_PATH" "$STAGING_DIR/hardener"
elif [ -n "$RULESET_PATH" ]; then
    echo "ERROR: --binary is required when using --ruleset (no source directory to compile from)"
    exit 1
else
    echo "No --binary provided, compiling for linux/amd64..."
    HARDENER_SRC=$(find "$(dirname "$GUIDE_PATH")/.." -name "go.mod" -maxdepth 3 2>/dev/null | head -1 | xargs dirname 2>/dev/null || true)
    if [ -z "$HARDENER_SRC" ] || [ ! -f "$HARDENER_SRC/go.mod" ]; then
        echo "ERROR: Cannot locate Go project root. Provide --binary."
        exit 1
    fi
    echo "Building from: $HARDENER_SRC"
    (cd "$HARDENER_SRC" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$STAGING_DIR/hardener" .)
fi
chmod +x "$STAGING_DIR/hardener" "$STAGING_DIR/entrypoint.sh"

echo "Staging complete: $(du -sh "$STAGING_DIR" | cut -f1)"
echo ""

# ── run per distro ───────────────────────────────────────────────────────────

PASS_DISTROS=""
FAIL_DISTROS=""

for distro in $DISTROS; do
    echo "┌──────────────────────────────────────────────────────────┐"
    echo "│  $distro — booting VM"
    echo "└──────────────────────────────────────────────────────────┘"

    # Pass all variables via env: in Vagrantfile (see Vagrantfile for why)
    export SECURITY_LEVEL PROFILE LABELS

    if (cd "$SCRIPT_DIR" && vagrant up "$distro" --provider=libvirt 2>&1 | \
        tee "$RESULTS_DIR/$TIMESTAMP/${distro}-vagrant.log"); then

        echo ""
        echo "  Extracting results..."

        (cd "$SCRIPT_DIR" && vagrant ssh "$distro" -c \
            "cat /tmp/results/${distro}.json 2>/dev/null" \
            > "$RESULTS_DIR/$TIMESTAMP/${distro}.json" 2>/dev/null) || true

        (cd "$SCRIPT_DIR" && vagrant ssh "$distro" -c \
            "cat /tmp/results/${distro}.log 2>/dev/null" \
            > "$RESULTS_DIR/$TIMESTAMP/${distro}.log" 2>/dev/null) || true

        if [ -s "$RESULTS_DIR/$TIMESTAMP/${distro}.json" ]; then
            PASS_DISTROS="$PASS_DISTROS $distro"
            echo "  ✓ Results extracted"
        else
            FAIL_DISTROS="$FAIL_DISTROS $distro"
            echo "  ✗ No results (check ${distro}-vagrant.log)"
        fi
    else
        echo "  ✗ VM failed for $distro"
        FAIL_DISTROS="$FAIL_DISTROS $distro"
    fi

    echo "  Destroying VM..."
    (cd "$SCRIPT_DIR" && vagrant destroy "$distro" -f 2>/dev/null) || true
    echo ""
done

# ── cross-distro summary ─────────────────────────────────────────────────────

echo "╔══════════════════════════════════════════════════════════╗"
echo "║  CROSS-DISTRO SUMMARY"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""

SUMMARY_FILE="$RESULTS_DIR/$TIMESTAMP/summary.txt"

{
    # Header: A1=initial audit, FX=fix, A3=post-rollback audit
    # DS=distro-skipped, MC=missing-command
    printf "%-14s %-6s │ %5s %5s %5s %5s %5s %5s %5s │ %5s %5s %5s │ %5s %5s %5s\n" \
        "DISTRO" "STATE" \
        "A1_P" "A1_F" "A1_E" "A1_S" "A1_DS" "A1_MC" "A1_T" \
        "FX_OK" "FX_F" "FX_T" \
        "A3_P" "A3_F" "A3_T"
    echo "───────────────────────┼─────────────────────────────────────────────┼──────────────────┼──────────────────"

    for distro in $DISTROS; do
        JSON="$RESULTS_DIR/$TIMESTAMP/${distro}.json"
        if [ ! -s "$JSON" ]; then
            printf "%-14s %-6s │ %43s │ %18s │ %18s\n" \
                "$distro" "SKIP" "-- no results --" "" ""
            continue
        fi

        STATUS="OK"
        echo "$FAIL_DISTROS" | grep -qw "$distro" 2>/dev/null && STATUS="FAIL"

        # Sum a JSON number field across all suites in a step section
        sum_field() {
            local file="$1" start="$2" end="$3" field="$4"
            sed -n "/$start/,/$end/p" "$file" \
                | grep "\"$field\"" \
                | grep -o '[0-9]*' \
                | awk '{s+=$1} END {print s+0}'
        }

        # Initial audit
        a1_p=$(sum_field  "$JSON" "01_audit_initial" "02_fix" "passed")
        a1_f=$(sum_field  "$JSON" "01_audit_initial" "02_fix" "failed")
        a1_e=$(sum_field  "$JSON" "01_audit_initial" "02_fix" "errors")
        a1_s=$(sum_field  "$JSON" "01_audit_initial" "02_fix" "skipped")
        a1_ds=$(sum_field "$JSON" "01_audit_initial" "02_fix" "distro_skipped")
        a1_mc=$(sum_field "$JSON" "01_audit_initial" "02_fix" "missing_command")
        a1_t=$(sum_field  "$JSON" "01_audit_initial" "02_fix" "total")

        # Fix
        fx_ok=$(sum_field "$JSON" "02_fix" "03_audit_postfix" "fixed")
        fx_f=$(sum_field  "$JSON" "02_fix" "03_audit_postfix" "failed")
        fx_t=$((fx_ok + fx_f))

        # Post-rollback audit
        a3_p=$(sed -n '/05_audit_postrollback/,$p' "$JSON" | grep '"passed"'  | grep -o '[0-9]*' | awk '{s+=$1} END {print s+0}')
        a3_f=$(sed -n '/05_audit_postrollback/,$p' "$JSON" | grep '"failed"'  | grep -o '[0-9]*' | awk '{s+=$1} END {print s+0}')
        a3_t=$(sed -n '/05_audit_postrollback/,$p' "$JSON" | grep '"total"'   | grep -o '[0-9]*' | awk '{s+=$1} END {print s+0}')

        printf "%-14s %-6s │ %5d %5d %5d %5d %5d %5d %5d │ %5d %5d %5d │ %5d %5d %5d\n" \
            "$distro" "$STATUS" \
            "$a1_p" "$a1_f" "$a1_e" "$a1_s" "$a1_ds" "$a1_mc" "$a1_t" \
            "$fx_ok" "$fx_f" "$fx_t" \
            "$a3_p" "$a3_f" "$a3_t"
    done
} | tee "$SUMMARY_FILE"

echo ""
echo "Columns"
echo "  A1/A3  P=passed  F=failed  E=errors  S=skipped  DS=distro-skipped  MC=missing-command  T=total"
echo "  FX     OK=fixes applied  F=still failed  T=fix-attempted (OK+F)"
echo ""
echo "Healthy run"
echo "  A1: F+E low; S, DS, MC are neutral (not failures — expected on partial installs)"
echo "  FX: OK high, F low — ideally OK/T near 100%"
echo "  A3: F ≈ A1_F  (small drift from runtime state is expected)"
echo ""

# ── missing-command report ───────────────────────────────────────────────────

echo "Missing commands per distro (initial audit):"
for distro in $DISTROS; do
    JSON="$RESULTS_DIR/$TIMESTAMP/${distro}.json"
    [ ! -s "$JSON" ] && continue

    # Extract missing_commands array from JSON — try python3 first, fall back to grep
    if command -v python3 >/dev/null 2>&1; then
        missing=$(python3 - "$JSON" <<'PYEOF'
import json, sys
try:
    d = json.load(open(sys.argv[1]))
    cmds = d.get("steps", {}).get("01_audit_initial", {}).get("missing_commands", [])
    print(", ".join(cmds) if cmds else "none")
except Exception:
    print("?")
PYEOF
        )
    else
        # Simple grep fallback — pulls quoted strings from the missing_commands array
        missing=$(sed -n '/"missing_commands"/,/\]/p' "$JSON" \
            | grep -oE '"[^"]+"' | tr -d '"' | grep -v missing_commands \
            | paste -sd ',' - || echo "?")
        [ -z "$missing" ] && missing="none"
    fi

    printf "  %-14s %s\n" "$distro:" "$missing"
done | tee -a "$SUMMARY_FILE"

# ── rollback integrity ───────────────────────────────────────────────────────

echo ""
echo "Rollback delta (A1 failed → A3 failed — small drift expected for runtime state):"
for distro in $DISTROS; do
    JSON="$RESULTS_DIR/$TIMESTAMP/${distro}.json"
    [ ! -s "$JSON" ] && continue

    a1_f=$(sed -n '/01_audit_initial/,/02_fix/p' "$JSON" | grep '"failed"' | grep -o '[0-9]*' | awk '{s+=$1} END {print s+0}')
    a3_f=$(sed -n '/05_audit_postrollback/,$p'   "$JSON" | grep '"failed"' | grep -o '[0-9]*' | awk '{s+=$1} END {print s+0}')

    echo "  $distro: A1_failed=$a1_f → A3_failed=$a3_f"
done | tee -a "$SUMMARY_FILE"

echo ""
echo "Full results: $RESULTS_DIR/$TIMESTAMP/"

# ── cleanup staging ──────────────────────────────────────────────────────────

rm -rf "$STAGING_DIR"
