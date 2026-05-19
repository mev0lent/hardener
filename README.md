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
git clone <repo-url>
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
git clone <repo-url>
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

## Cross-Distro Test Harness (Linux, contributor use)

Automated VM-based testing of the Hardener tool across Linux distributions.
Each distro gets a fresh KVM VM, runs the full audit → fix → rollback cycle,
and produces structured results.

### Prerequisites

**KVM / libvirt**

```bash
sudo apt install qemu-kvm libvirt-daemon-system libvirt-clients virt-manager
sudo systemctl enable --now libvirtd
sudo usermod -aG libvirt,kvm $USER
# log out and back in for group membership to take effect
```

Verify the host is capable:

```bash
virt-host-validate
```

All lines should say `PASS`. The `IOMMU` line may say `WARN`, that is fine.
If you see `FAIL` on the KVM line, enable Intel VT-x or AMD-V in BIOS/UEFI.

**Vagrant**

The Ubuntu package is outdated. Install from the HashiCorp apt repo:

```bash
wget -O - https://apt.releases.hashicorp.com/gpg \
  | sudo gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg

echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] \
  https://apt.releases.hashicorp.com $(lsb_release -cs) main" \
  | sudo tee /etc/apt/sources.list.d/hashicorp.list

sudo apt update && sudo apt install vagrant
vagrant --version   # should print 2.4+
```

**vagrant-libvirt plugin**

```bash
sudo apt install libvirt-dev ruby-dev build-essential
vagrant plugin install vagrant-libvirt
vagrant plugin list   # should list vagrant-libvirt
```

### Running the tests

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

Single distro (faster iteration):

```bash
./run_tests.sh --guide path/to/sections --binary /tmp/hardener-linux --distros "archlinux"
```

### What the runner does

For each distro:

1. Boots a fresh KVM VM via Vagrant
2. Installs distro-specific prerequisites inside the VM
3. Rsyncs the guide and binary into the VM at `/opt/hardener/`
4. Runs the 5-step test cycle:
   - **Step 1**: Initial audit (`hardener audit --all`)
   - **Step 2**: Fix (`hardener fix --all`)
   - **Step 3**: Post-fix audit
   - **Step 4**: Rollback (`hardener rollback --latest`)
   - **Step 5**: Post-rollback audit
5. Pulls results out of the VM over SSH
6. Destroys the VM

### Artefacts

Every run creates a timestamped directory under `testing/results/`:

```
results/
└── <TIMESTAMP>/
    ├── summary.txt
    ├── ubuntu.json
    ├── ubuntu.log
    ├── ubuntu-vagrant.log
    └── ...
```

### Summary table columns

```
DISTRO   STATE │ A1_P A1_F A1_E A1_S A1_NC A1_T │ FX_OK FX_F FX_E FX_T │ A3_P A3_F A3_E A3_T
ubuntu   OK    │   25   38    0   36     6  105  │    15   21    0  105  │   30   33    0  105
```

| Column | Meaning                                                              |
|--------|----------------------------------------------------------------------|
| `STATE` | `OK` = results extracted · `SKIP` = VM failed or produced no results |
| `A1_P / A1_F / A1_E` | Initial audit: passed / failed / errors                              |
| `A1_S` | Skipped: above requested security level                              |
| `A1_NC` | Not configured: required file absent on this distro                  |
| `A1_T` | Total checks                                                         |
| `FX_OK / FX_F / FX_E` | Fixes applied / still failing / errors                               |
| `A3_*` | Post-rollback audit: same columns as A1                              |

### Supported distros

| Distro | Vagrant box | Firewall |
|--------|-------------|----------|
| ubuntu | `bento/ubuntu-24.04` | ufw |
| debian | `generic/debian12` | ufw |
| rocky | `bento/rockylinux-9` | firewalld |
| opensuse | `opensuse/Leap-15.6.x86_64` | firewalld |
| archlinux | `generic/arch` | ufw |
| fedora | `generic/fedora40` | firewalld |
| rhel | *(requires subscription box)* | firewalld |

### Troubleshooting

**Distro shows `SKIP`**: open `<distro>-vagrant.log`. Common causes:
- Box has no libvirt provider variant → use a `generic/*` or `bento/*` box
- Package install failed during provisioning

**All results are zero**: the binary failed system validation. Open `<distro>.log` and look for `[> ERROR]` lines. Usually a tool in `00_README.md` preconditions is missing.

**`vagrant plugin install` fails**: install dev libraries first:

```bash
sudo apt install libvirt-dev ruby-dev build-essential
```

### Adding a distro

1. Add an entry to the `DISTROS` hash in `testing/Vagrantfile`
2. Run: `./run_tests.sh --guide path/to/sections --binary /tmp/hardener-linux --distros "newdistro"`
3. Check the vagrant log if it shows `SKIP`, the hardener log if results are all zero
