# Hardener

A cross-platform security auditing and hardening tool. Load a ruleset, run an audit, apply fixes, and roll back if needed, all from a single binary with no external dependencies.

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
| `audit` / `fix` | `--security-level <level>` | `-s` | `baseline` (default) or `high` |
| `rollback` | `--latest` | `-l` | Roll back to the most recent fix run |
| `rollback` | `--timestamp <ts>` | `-t` | Roll back to a specific run timestamp |
| `rollback` | `--files <f1,f2>` | | Roll back specific files only |

`--ruleset` and `--path` are mutually exclusive; exactly one must be provided for `audit` and `fix`.

---

## Cross-Distro Test Harness (contributor use)

Automated VM-based testing across Linux distributions. Each distro gets a fresh
KVM VM, runs the full audit → fix → rollback cycle, and produces structured results.

> **Linux bare-metal host required for the Vagrant path.**
> KVM/libvirt needs hardware virtualisation (VT-x or AMD-V) available directly on the CPU.
> It will not work on macOS, Windows, or inside a VM (VMware/VirtualBox/WSL2).
> If you are on such a host, use the Docker path below.

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
hardware virtualisation is not available — on bare metal, enable VT-x/AMD-V in
BIOS/UEFI; inside a VM this setup cannot be used, use Docker instead.

**2. Install Vagrant**

Follow the official instructions at <https://developer.hashicorp.com/vagrant/install>:

```bash
wget -O - https://apt.releases.hashicorp.com/gpg | sudo gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg

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

```bash
cd testing

./run_tests.sh \
  --guide  path/to/linux-guide/sections \
  --binary /tmp/hardener-linux \
  --distros "ubuntu debian rocky opensuse archlinux"
```

| Flag | Default | Description |
|------|---------|-------------|
| `--guide` | *(required)* | Path to the directory containing the guide `.md` files |
| `--binary` | *(required)* | Path to the pre-built hardener binary |
| `--level` | `baseline` | Security level: `baseline` or `high` |
| `--distros` | all | Space-separated list of distros to run |

### Docker (Windows / macOS / VM hosts)

Quick smoke-test without KVM. Does not run the full Vagrant cycle but verifies
that checks load and execute correctly.

```bash
# Build a Linux binary from any host
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/hardener-linux .

# Run against a ruleset
docker run --rm \
  -v /tmp/hardener-linux:/hardener \
  -v /path/to/ruleset.yaml:/ruleset.yaml \
  ubuntu:24.04 \
  /hardener audit --ruleset /ruleset.yaml --all

# Run against a markdown guide directory
docker run --rm \
  -v /tmp/hardener-linux:/hardener \
  -v /path/to/guide/sections:/guide \
  ubuntu:24.04 \
  /hardener audit --path /guide --all
```

On Windows (PowerShell), replace the build line with:
```powershell
$env:GOOS="linux"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"; go build -o hardener-linux .
```

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
| `A1_S` | Skipped: above requested security level (neutral) |
| `A1_NC` | Not configured: required file absent on this distro (neutral) |
| `FX_OK / FX_F / FX_E` | Fixes applied / still failing / errors |
| `A3_*` | Post-rollback audit: same columns as A1 |

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

**Distro shows `SKIP`** — open `<distro>-vagrant.log`. Common causes: box has no libvirt provider variant; package install failed during provisioning.

**All results are zero** — the binary failed system validation. Open `<distro>.log` and look for `[> ERROR]` lines. Usually a required tool from preconditions is missing on that distro.

**`vagrant plugin install` fails** — install dev libraries first:

```bash
sudo apt install libvirt-dev ruby-dev build-essential
```

### Adding a distro

1. Add an entry to the `DISTROS` hash in `testing/Vagrantfile` with a libvirt-compatible box and its prereq install command
2. Run: `./run_tests.sh --guide path/to/sections --binary /tmp/hardener-linux --distros "newdistro"`
3. Check the vagrant log if it shows `SKIP`, the hardener log if results are all zero
