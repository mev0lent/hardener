#!/usr/bin/env bash
# =============================================================================
# run_tests.sh: Hardener cross-distro test orchestrator (Vagrant/libvirt)
#
# Usage:
#   ./run_tests.sh --guide PATH --binary PATH [--level LEVEL] [--distros "d1 d2"]
#
# Prerequisites:
#   - Vagrant 2.4+ with vagrant-libvirt plugin
#   - libvirt/KVM running (virt-host-validate should pass)
#   - Go toolchain (only if --binary not provided)
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
GUIDE_PATH=""
BINARY_PATH=""
SECURITY_LEVEL="baseline"
DISTROS="ubuntu debian rocky opensuse archlinux"
RESULTS_DIR="$SCRIPT_DIR/results"
STAGING_DIR="$SCRIPT_DIR/.staging"
TIMESTAMP=$(date -u +"%Y%m%d-%H%M%S")

# ── argument parsing ─────────────────────────────────────────────────────────

while [[ $# -gt 0 ]]; do
    case "$1" in
        --guide)    GUIDE_PATH="$2";     shift 2 ;;
        --binary)   BINARY_PATH="$2";    shift 2 ;;
        --level)    SECURITY_LEVEL="$2"; shift 2 ;;
        --distros)  DISTROS="$2";        shift 2 ;;
        --help|-h)
            echo "Usage: $0 --guide PATH --binary PATH [--level baseline|high] [--distros \"d1 d2\"]"
            exit 0 ;;
        *) echo "Unknown arg: $1"; exit 1 ;;
    esac
done

# ── validation ───────────────────────────────────────────────────────────────

if [ -z "$GUIDE_PATH" ]; then
    echo "ERROR: --guide is required"; exit 1
fi
if [ ! -d "$GUIDE_PATH" ]; then
    echo "ERROR: guide directory not found: $GUIDE_PATH"; exit 1
fi
if ! command -v vagrant >/dev/null 2>&1; then
    echo "ERROR: vagrant not found in PATH"; exit 1
fi
if ! vagrant plugin list | grep -q vagrant-libvirt; then
    echo "ERROR: vagrant-libvirt plugin not installed"; exit 1
fi

# ── stage files ──────────────────────────────────────────────────────────────

echo "╔══════════════════════════════════════════════════════════╗"
echo "║  Hardener Cross-Distro Test Runner (Vagrant/libvirt)"
echo "║  $(date -u +'%Y-%m-%d %H:%M:%S UTC')"
echo "║  Guide:    $GUIDE_PATH"
echo "║  Level:    $SECURITY_LEVEL"
echo "║  Distros:  $DISTROS"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""

rm -rf "$STAGING_DIR"
mkdir -p "$STAGING_DIR/guide" "$RESULTS_DIR/$TIMESTAMP"

# Copy guide
cp -r "$GUIDE_PATH"/. "$STAGING_DIR/guide/"

# Copy entrypoint
cp "$SCRIPT_DIR/entrypoint.sh" "$STAGING_DIR/entrypoint.sh"

# Binary: use provided or compile
if [ -n "$BINARY_PATH" ] && [ -f "$BINARY_PATH" ]; then
    echo "Using provided binary: $BINARY_PATH"
    cp "$BINARY_PATH" "$STAGING_DIR/hardener"
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

# ── run per distro ──────────────────────────────────────────────────────────

PASS_DISTROS=""
FAIL_DISTROS=""

for distro in $DISTROS; do
    echo "┌──────────────────────────────────────────────────────────┐"
    echo "│  $distro — booting VM"
    echo "└──────────────────────────────────────────────────────────┘"

    # Export security level so Vagrantfile can pass it through
    export SECURITY_LEVEL

    # Boot, provision, and run test (all in one vagrant up)
    if (cd "$SCRIPT_DIR" && vagrant up "$distro" --provider=libvirt 2>&1 | \
        tee "$RESULTS_DIR/$TIMESTAMP/${distro}-vagrant.log"); then

        echo ""
        echo "  Extracting results..."

        # Pull JSON report out of the VM
        (cd "$SCRIPT_DIR" && vagrant ssh "$distro" -c \
            "cat /tmp/results/${distro}.json 2>/dev/null" \
            > "$RESULTS_DIR/$TIMESTAMP/${distro}.json" 2>/dev/null) || true

        # Pull full log out of the VM
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

    # Tear down
    echo "  Destroying VM..."
    (cd "$SCRIPT_DIR" && vagrant destroy "$distro" -f 2>/dev/null) || true
    echo ""
done

# ── cross-distro summary ────────────────────────────────────────────────────

echo "╔══════════════════════════════════════════════════════════╗"
echo "║  CROSS-DISTRO SUMMARY"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""

SUMMARY_FILE="$RESULTS_DIR/$TIMESTAMP/summary.txt"

{
    printf "%-14s %-6s │ %5s %5s %5s %5s %5s %5s │ %5s %5s %5s %5s │ %5s %5s %5s %5s\n" \
        "DISTRO" "STATE" \
        "A1_P" "A1_F" "A1_E" "A1_S" "A1_NC" "A1_T" \
        "FX_OK" "FX_F" "FX_E" "FX_T" \
        "A3_P" "A3_F" "A3_E" "A3_T"
    echo "───────────────────────┼─────────────────────────────────────┼─────────────────────────┼─────────────────────────"

    for distro in $DISTROS; do
        JSON="$RESULTS_DIR/$TIMESTAMP/${distro}.json"
        if [ ! -s "$JSON" ]; then
            printf "%-14s %-6s │ %39s │ %25s │ %25s\n" \
                "$distro" "SKIP" "-- no results --" "" ""
            continue
        fi

        STATUS="OK"
        echo "$FAIL_DISTROS" | grep -qw "$distro" 2>/dev/null && STATUS="FAIL"

        # Helper: sum a field across all suite entries within a JSON section
        sum_field() {
            local file="$1" start="$2" end="$3" field="$4"
            sed -n "/$start/,/$end/p" "$file" \
                | grep "\"$field\"" \
                | grep -o '[0-9]*' \
                | awk '{s+=$1} END {print s+0}'
        }

        # Initial audit (step 01)
        a1_p=$(sum_field "$JSON" "01_audit_initial" "02_fix" "passed")
        a1_f=$(sum_field "$JSON" "01_audit_initial" "02_fix" "failed")
        a1_e=$(sum_field "$JSON" "01_audit_initial" "02_fix" "errors")
        a1_s=$(sum_field "$JSON" "01_audit_initial" "02_fix" "skipped")
        a1_nc=$(sum_field "$JSON" "01_audit_initial" "02_fix" "not_configured")
        a1_t=$(sum_field "$JSON" "01_audit_initial" "02_fix" "total")

        # Fix (step 02) — FX_T = only checks where a fix was attempted (OK+F+E)
        fx_ok=$(sum_field "$JSON" "02_fix" "03_audit_postfix" "fixed")
        fx_f=$(sum_field "$JSON"  "02_fix" "03_audit_postfix" "failed")
        fx_e=$(sum_field "$JSON"  "02_fix" "03_audit_postfix" "errors")
        fx_t=$((fx_ok + fx_f + fx_e))

        # Post-rollback audit (step 05) — sed to EOF since there is no next section
        a3_p=$(sed -n '/05_audit_postrollback/,$p' "$JSON" | grep '"passed"'  | grep -o '[0-9]*' | awk '{s+=$1} END {print s+0}')
        a3_f=$(sed -n '/05_audit_postrollback/,$p' "$JSON" | grep '"failed"'  | grep -o '[0-9]*' | awk '{s+=$1} END {print s+0}')
        a3_e=$(sed -n '/05_audit_postrollback/,$p' "$JSON" | grep '"errors"'  | grep -o '[0-9]*' | awk '{s+=$1} END {print s+0}')
        a3_t=$(sed -n '/05_audit_postrollback/,$p' "$JSON" | grep '"total"'   | grep -o '[0-9]*' | awk '{s+=$1} END {print s+0}')

        printf "%-14s %-6s │ %5d %5d %5d %5d %5d %5d │ %5d %5d %5d %5d │ %5d %5d %5d %5d\n" \
            "$distro" "$STATUS" \
            "$a1_p" "$a1_f" "$a1_e" "$a1_s" "$a1_nc" "$a1_t" \
            "$fx_ok" "$fx_f" "$fx_e" "$fx_t" \
            "$a3_p" "$a3_f" "$a3_e" "$a3_t"
    done
} | tee "$SUMMARY_FILE"

echo ""
echo "Columns"
echo "  A1/A3  P=passed  F=failed  E=errors  S=skipped  NC=not_configured  T=total"
echo "  FX     OK=fixes applied  F=still failed  E=errors  T=fix-attempted (OK+F+E)"
echo ""
echo "Healthy run"
echo "  A1: F+E low, NC and S are neutral (not failures)"
echo "  FX: OK high, F low — ideally OK/T near 100%"
echo "  A3: F ≈ A1_F  (small drift from runtime state is expected)"
echo ""
echo "  NC = required config file not present on this system"
echo "  S  = check above requested security level"
echo ""

# ── rollback integrity ──────────────────────────────────────────────────────

echo "Rollback delta (A1 failed vs A3 failed — drift expected for runtime state):"
for distro in $DISTROS; do
    JSON="$RESULTS_DIR/$TIMESTAMP/${distro}.json"
    [ ! -s "$JSON" ] && continue

    a1_f=$(sed -n '/01_audit_initial/,/02_fix/p' "$JSON" | grep '"failed"' | grep -o '[0-9]*' | awk '{s+=$1} END {print s+0}')
    a3_f=$(sed -n '/05_audit_postrollback/,$p'   "$JSON" | grep '"failed"' | grep -o '[0-9]*' | awk '{s+=$1} END {print s+0}')

    echo "  $distro: A1_failed=$a1_f → A3_failed=$a3_f"
done

echo ""
echo "Full results: $RESULTS_DIR/$TIMESTAMP/"

# ── cleanup staging ─────────────────────────────────────────────────────────

rm -rf "$STAGING_DIR"
