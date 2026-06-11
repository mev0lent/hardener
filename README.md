# Hardener

A cross-platform security auditing and hardening tool. Load a ruleset, run an audit, apply fixes, and roll back if needed, all from a single binary with no external dependencies.

---

## Prerequisites

**Go 1.24+** is required to build the binary. The `golang-go` package in most distro
repos is too old; install from the official source instead:

```bash
GO_VERSION=$(curl -s https://go.dev/VERSION?m=text | head -1)
curl -fsSL "https://go.dev/dl/${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf /tmp/go.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
source ~/.profile
go version
```

> **macOS:** use `darwin-arm64` or `darwin-amd64` instead of `linux-amd64` in the URL above,
> or install via `brew install go`.

---

## Quick Start

### Linux

**1. Download the ruleset and the guide**

| Resource | Link |
|----------|------|
| Ruleset (YAML) | [ruleset.yaml](https://github.com/ernw/hardening/tree/master/operating_system/linux/ruleset.yaml) |
| Hardening Guide (PDF/MD) | [ERNW\_Hardening\_Linux.md](https://github.com/ernw/hardening/blob/master/operating_system/linux/ERNW_Hardening_Linux.md) |

**2. Build the binary**

```bash
git clone https://github.com/mev0lent/hardener.git
cd hardener
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o hardener-linux .
```

**3. Audit your system**

```bash
# Interactive suite picker
./hardener-linux audit --ruleset ruleset.yaml

# Run all suites non-interactively
./hardener-linux audit --ruleset ruleset.yaml --all

# Include high-security checks (default: baseline only)
./hardener-linux audit --ruleset ruleset.yaml --all --security-level high
```

**4. Apply fixes**

```bash
./hardener-linux fix --ruleset ruleset.yaml --all
```

**5. Roll back**

```bash
# Undo the last fix run
./hardener-linux rollback --latest

# Undo a specific run
./hardener-linux rollback --timestamp 2026-05-18T14:32:00Z

# Roll back specific files only
./hardener-linux rollback --files /etc/sysctl.conf,/etc/hosts.deny
```

---

### macOS

**1. Download the ruleset and the guide**

| Resource | Link |
|----------|------|
| Ruleset (YAML) | [ruleset.yaml](https://github.com/ernw/hardening/tree/master/operating_system/osx/26/ruleset.yaml) |
| Hardening Guide (PDF/MD) | [Hardening\_Guide-macOS\_26\_Tahoe\_1.0.md](https://github.com/ernw/hardening/blob/master/operating_system/osx/26/Hardening_Guide-macOS_26_Tahoe_1.0.md) |

**2. Build the binary**

```bash
git clone https://github.com/mev0lent/hardener.git
cd hardener

# Apple Silicon
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o hardener-macos .

# Intel
GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o hardener-macos .
```

**3. Audit your system**

```bash
# Interactive suite picker
./hardener-macos audit --ruleset ruleset.yaml

# Run all suites non-interactively
./hardener-macos audit --ruleset ruleset.yaml --all

# Include high-security checks (default: baseline only)
./hardener-macos audit --ruleset ruleset.yaml --all --security-level high
```

**4. Apply fixes**

```bash
./hardener-macos fix --ruleset ruleset.yaml --all
```

**5. Roll back**

```bash
./hardener-macos rollback --latest
```

---

## Command Reference

| Command | Flag | Short | Description |
|---------|------|-------|-------------|
| `audit` / `fix` | `--ruleset <file>` | `-r` | Load checks from a ruleset YAML file |
| `audit` / `fix` | `--path <dir>` | `-p` | Load checks from a hardening-guide directory (alternative to `--ruleset`) |
| `audit` / `fix` | `--all` | `-A` | Skip interactive suite picker, run everything |
| `audit` / `fix` | `--security-level <level>` | `-s` | `baseline` (default), `medium`, or `high` — runs checks at or below this level |
| `audit` / `fix` | `--profile <name>` | `-P` | Role profile for distro overrides (e.g. `server`, `client`) |
| `audit` / `fix` | `--label <l1,l2>` | `-l` | Run only suites whose `labels:` list contains at least one match |
| `rollback` | `--latest` | | Roll back to the most recent fix run |
| `rollback` | `--timestamp <ts>` | `-t` | Roll back to a specific run timestamp |
| `rollback` | `--files <f1,f2>` | | Roll back specific files only |

`--ruleset` and `--path` are mutually exclusive; exactly one must be provided for `audit` and `fix`.

---

## Ruleset Features

### Security levels

Every check has a `security_level` field (`baseline`, `medium`, `high`). Only checks at or below the level requested via `--security-level` are executed; the rest are reported as `skipped`. Default is `baseline`.

### Distro-specific overrides

Checks can carry a `distro:` map to override `command`, `fix`, `expected`, `sudo`, or `fix_sudo` for a specific Linux distribution. The key is the lowercase `ID=` value from `/etc/os-release`:

```yaml
- id: secure-boot-active
  command: mokutil --sb-state | grep -c enabled
  distro:
    debian:
      command: |
        if ! command -v mokutil >/dev/null 2>&1; then echo 1
        else mokutil --sb-state | grep -c enabled; fi
```

If a check has a `distro:` map but the current distro is not listed, the check is skipped with a distinct `skipped_distro` state — it does not count as a failure.

### Profiles (`--profile`)

For role-based differences (server vs. client), pass a profile at runtime:

```bash
./hardener-linux audit --ruleset ruleset.yaml --profile server
```

The engine tries the combined key `distro-profile` (e.g. `debian-server`) first, then falls back to `distro` alone, then runs the universal check. This lets you express distro+role overrides without duplicating entries:

```yaml
distro:
  debian-server:
    command: systemctl is-active sshd | grep -c active
  debian:
    command: command -v sshd >/dev/null 2>&1 && echo 0 || echo 0
```

### Labels (`--label`)

Each suite in a ruleset has a `labels:` list. Use `--label` to run only matching suites:

```bash
# Run only kernel and network suites
./hardener-linux audit --ruleset ruleset.yaml --label kernel,network
```

Available labels in the Linux ruleset: `auth`, `network`, `kernel`, `filesystem`, `boot`, `services`, `audit`, `logging`.

### `requires_command`

A check with `requires_command: <binary>` is silently skipped with a `missing-command` state when that binary is not in the system's `PATH`. This keeps checks that depend on optional tools from showing as errors on systems where the tool isn't installed:

```yaml
- id: secure-boot-active
  requires_command: mokutil
  command: mokutil --sb-state | grep -c enabled
  expected: 1
```

Missing-command skips appear in suite summaries and are tracked separately from failures.

### `expected_op`

By default the check compares command output against `expected` as a string. Set `expected_op` to use a numeric comparison instead:

```yaml
expected: 1
expected_op: '>='
```

Supported operators: `>=`, `>`, `<=`, `<`.

---

## Cross-Distro Test Harness (contributor use)

Automated VM-based testing across Linux distributions. Each distro gets a fresh
KVM VM, runs the full audit → fix → rollback cycle, and produces structured results.

> **Linux bare-metal host required.**
> KVM/libvirt needs hardware virtualisation (VT-x or AMD-V) available directly on the CPU.
> This test harness does not work on macOS, Windows, or inside a VM (VMware/VirtualBox/WSL2).

### Vagrant / KVM

**1. Install KVM and libvirt**

```bash
sudo apt install qemu-kvm libvirt-daemon-system libvirt-clients virt-manager
sudo systemctl enable --now libvirtd
sudo usermod -aG libvirt,kvm $USER
# log out and back in for group membership to take effect
```

Verify:

```bash
virt-host-validate
```

`IOMMU` and secure-guest `WARN` lines are fine. A `FAIL` on the **KVM** line means
hardware virtualisation is not available; on bare metal, enable VT-x/AMD-V in
BIOS/UEFI; inside a VM this setup cannot be used, use Docker instead.

**2. Install Vagrant**

The key at HashiCorp's `/gpg` URL does not always match the key currently signing
the repo. Fetch it by fingerprint from a keyserver instead, which is always authoritative:

```bash
sudo gpg --keyserver hkps://keyserver.ubuntu.com --recv-keys AA16FCBCA621E701
sudo gpg --export AA16FCBCA621E701 \
  | sudo tee /usr/share/keyrings/hashicorp-archive-keyring.gpg > /dev/null

echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] \
  https://apt.releases.hashicorp.com $(grep -oP '(?<=UBUNTU_CODENAME=).*' /etc/os-release || lsb_release -cs) main" \
  | sudo tee /etc/apt/sources.list.d/hashicorp.list

sudo apt update && sudo apt install vagrant
vagrant --version   # should print 2.4+
```

> **Kali Linux:** `lsb_release -cs` returns `kali-rolling` which has no entry in the HashiCorp repo.
> Replace the codename part of the `echo` line with a hardcoded `bookworm` (Kali's Debian base):
> ```bash
> echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] \
>   https://apt.releases.hashicorp.com bookworm main" \
>   | sudo tee /etc/apt/sources.list.d/hashicorp.list
> ```

**3. Install the vagrant-libvirt plugin**

```bash
sudo apt install libvirt-dev ruby-dev build-essential
vagrant plugin install vagrant-libvirt
vagrant plugin list   # should list vagrant-libvirt
```

**4. Run the tests**

Build the binary from the repo root (Go 1.24+ required, see Prerequisites above):

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o hardener-linux .
```

Then from the `testing/` directory:

```bash
cd testing
chmod +x run_tests.sh
```

With a markdown guide directory:

```bash
./run_tests.sh \
  --guide   path/to/linux-guide/sections \
  --binary  ../hardener-linux \
  --distros "ubuntu debian rocky opensuse archlinux"
```

With a standalone ruleset YAML (`--binary` is required in this mode):

```bash
./run_tests.sh \
  --ruleset ../ruleset.yaml \
  --binary  ../hardener-linux \
  --distros "ubuntu"
```

| Flag | Default | Description |
|------|---------|-------------|
| `--guide` | | Path to a hardening-guide directory of `.md` files |
| `--ruleset` | | Path to a standalone `ruleset.yaml` (alternative to `--guide`) |
| `--binary` | *(required with `--ruleset`)* | Path to the pre-built hardener binary |
| `--level` | `baseline` | Security level: `baseline`, `medium`, or `high` |
| `--distros` | all | Space-separated list of distros to run |
| `--profile` | | Passed as `--profile` to hardener (e.g. `server`, `client`) |
| `--label` | | Passed as `--label` to hardener (e.g. `kernel,network`) |

`--guide` and `--ruleset` are mutually exclusive; one must be provided.

### What the runner does

For each distro:

1. Boots a fresh KVM VM via Vagrant
2. Installs distro-specific prerequisites inside the VM
3. Rsyncs the guide and binary into the VM at `/opt/hardener/`
4. Runs the 5-step test cycle: initial audit → fix → post-fix audit → rollback → post-rollback audit
5. Pulls structured results out of the VM over SSH
6. Destroys the VM

### Artefacts

Every run creates a timestamped directory under `testing/results/`:

```
results/
└── <TIMESTAMP>/
    ├── summary.txt          # cross-distro table
    ├── ubuntu.json          # per-suite results for all 5 steps
    ├── ubuntu.log           # verbose hardener output
    ├── ubuntu-vagrant.log   # raw vagrant up / provisioning output
    └── ...
```

Summary table columns:

| Column | Meaning |
|--------|---------|
| `STATE` | `OK` = results extracted · `SKIP` = VM failed or no results |
| `A1_P / A1_F / A1_E` | Initial audit: passed / failed / errors |
| `A1_S` | Skipped: check above the requested security level (neutral) |
| `A1_DS` | Distro-skipped: check has a `distro:` map but current distro not listed (neutral) |
| `A1_MC` | Missing-command: `requires_command` binary not found on this distro (neutral) |
| `A1_T` | Total checks in suite |
| `FX_OK / FX_F` | Fixes applied / still failing after fix |
| `A3_*` | Post-rollback audit: same columns as A1 |

A healthy run has low `A1_F` and `A1_E`. `A1_S`, `A1_DS`, and `A1_MC` are all neutral — they are expected to vary by distro and security level and do not indicate broken checks.

After the table, the runner prints a **missing commands per distro** section listing the exact binary names that triggered `A1_MC` on each distro. Use this to decide whether to add a package to the Vagrantfile's `pkg_cmd` or to add a `requires_command:` field to the relevant checks.

### Supported distros

| Distro | Vagrant box | Firewall |
|--------|-------------|----------|
| ubuntu | `bento/ubuntu-24.04` | ufw |
| debian | `generic/debian12` | ufw |
| rocky | `bento/rockylinux-9` | firewalld |
| opensuse | `opensuse/Leap-15.6.x86_64` | firewalld |
| archlinux | `generic/arch` | ufw |
| fedora | `bento/fedora-latest` | firewalld |
| rhel | `generic/rhel9` *(requires subscription)* | firewalld |

### Troubleshooting

**Distro shows `SKIP`**: open `<distro>-vagrant.log`. Common causes: box has no libvirt provider variant; package install failed during provisioning.

**All results are zero**: the binary failed system validation. Open `<distro>.log` and look for `[> ERROR]` lines. Usually a required tool from preconditions is missing on that distro.

**High `A1_MC`**: many checks are skipped due to missing binaries. Check the "Missing commands per distro" section of `summary.txt` for the exact binary names. Either add the package to the Vagrantfile `pkg_cmd` for that distro, or add `requires_command:` to the affected checks so the skip is intentional and documented.

**`vagrant plugin install` fails**: install dev libraries first:

```bash
sudo apt install libvirt-dev ruby-dev build-essential
```

### Adding a distro

1. Add an entry to the `DISTROS` hash in `testing/Vagrantfile` with a libvirt-compatible box and its prereq install command
2. Run: `./run_tests.sh --guide path/to/sections --binary ../hardener-linux --distros "newdistro"`
3. Check the vagrant log if it shows `SKIP`, the hardener log if results are all zero
