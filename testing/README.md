# Hardener — Cross-Distro Test Harness (Vagrant/libvirt)

Automated VM-based testing of the Hardener tool across Linux distributions.
Each distro gets a fresh KVM VM, runs the full audit → fix → rollback cycle,
and produces structured results.

---

## Prerequisites

### 1. KVM / libvirt

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

All lines should say `PASS`. The `IOMMU` line may say `WARN` — that is fine.
If you see `FAIL` on the KVM line, enable Intel VT-x or AMD-V in BIOS/UEFI.

### 2. Vagrant

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

### 3. vagrant-libvirt plugin

Install the required dev libraries first, then the plugin:

```bash
sudo apt install libvirt-dev ruby-dev build-essential
vagrant plugin install vagrant-libvirt
vagrant plugin list   # should list vagrant-libvirt
```

---

## Building the binary

The test runner needs a pre-built Linux/amd64 binary from the hardener source
(requires Go 1.21+):

```bash
cd /path/to/hardener-repo
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/hardener-linux .
```

---

## Running the tests

```bash
cd path/to/linux-hardening-guide/report/testing

./run_tests.sh \
  --guide  ../sections \
  --binary /tmp/hardener-linux \
  --distros "ubuntu debian rocky opensuse archlinux"
```

| Flag | Default | Description |
|------|---------|-------------|
| `--guide` | *(required)* | Path to the directory containing the guide `.md` files |
| `--binary` | *(required)* | Path to the pre-built hardener binary |
| `--level` | `baseline` | Security level: `baseline` or `high` |
| `--distros` | all five | Space-separated list of distros to run |

Single distro (faster iteration):

```bash
./run_tests.sh --guide ../report/sections --binary /tmp/hardener-linux \
  --distros "archlinux"
```

High security level:

```bash
./run_tests.sh --guide ../report/sections --binary /tmp/hardener-linux \
  --level high
```

---

## What the runner does

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

---

## Artefacts

Every run creates a timestamped directory under `testing/results/`:

```
results/
└── <TIMESTAMP>/
    ├── summary.txt            # Cross-distro table (human-readable)
    ├── ubuntu.json            # Structured results for ubuntu (all 5 steps)
    ├── ubuntu.log             # Verbose hardener output for ubuntu
    ├── ubuntu-vagrant.log     # Raw vagrant up / provisioning output
    ├── debian.json
    ├── debian.log
    ├── debian-vagrant.log
    ├── ...
```

### `summary.txt`

```
DISTRO   STATE │ A1_P A1_F A1_E A1_S A1_NC A1_T │ FX_OK FX_F FX_E FX_T │ A3_P A3_F A3_E A3_T
ubuntu   OK    │   25   38    0   36     6  105  │    15   21    0  105  │   30   33    0  105
debian   OK    │   23   40    0   36     6  105  │    17   21    0  105  │   27   36    0  105
rocky    OK    │   21   42    0   36     6  105  │    17   23    0  105  │   24   39    0  105
opensuse OK    │   14   49    0   36     6  105  │    24   23    0  105  │   17   46    0  105
```

| Column | Meaning |
|--------|---------|
| `STATE` | `OK` = results extracted · `SKIP` = VM failed or produced no results |
| `A1_P` | Initial audit — checks that passed |
| `A1_F` | Initial audit — checks that failed (*low is good*) |
| `A1_E` | Initial audit — command errors (*should be 0*) |
| `A1_S` | Skipped — above requested security level (neutral, not a failure) |
| `A1_NC` | Not configured — required config file absent on this system (neutral) |
| `A1_T` | Total checks in suites |
| `FX_OK` | Fixes applied successfully (*high is good*) |
| `FX_F` | Checks still failing after fix attempt (*low is good*) |
| `FX_E` | Fix command errors |
| `FX_T` | Checks where a fix was attempted (`OK+F+E`) |
| `A3_P/F/E/T` | Post-rollback audit — same columns as A1 |

*Healthy run:* `A1_F+A1_E` low · `FX_OK/FX_T` near 100% · `A3_F ≈ A1_F`

A3 numbers will differ slightly from A1: runtime state (active kernel
parameters, running services) does not fully reset without a reboot, so some
checks legitimately differ after rollback.

### `<distro>.json`

Structured JSON with per-suite results for all five steps. Example snippet:

```json
{
  "distro": "ubuntu",
  "security_level": "baseline",
  "steps": {
    "01_audit_initial": {
      "exit_code": 0,
      "suites": [
        { "suite": "Kernel Hardening", "total": 20, "passed": 7, "failed": 5, "fixed": 0, "errors": 0, "skipped": 8, "not_configured": 0 }
      ]
    },
    "02_fix": { ... },
    "03_audit_postfix": { ... },
    "04_rollback": { "exit_code": 0 },
    "05_audit_postrollback": { ... }
  }
}
```

### `<distro>.log`

Full verbose hardener output for every step, the first place to look when
a check fails unexpectedly.

### `<distro>-vagrant.log`

Raw output from `vagrant up` including package installation. Check this first
when a distro shows `SKIP` in the summary.

---

## Supported distros

| Distro | Vagrant box | Firewall tool |
|--------|-------------|---------------|
| ubuntu | `bento/ubuntu-24.04` | ufw |
| debian | `generic/debian12` | ufw |
| rocky | `bento/rockylinux-9` | firewalld |
| opensuse | `opensuse/Leap-15.6.x86_64` | firewalld |
| archlinux | `generic/arch` | ufw |

---

## Troubleshooting

**Distro shows `SKIP`** — open `<distro>-vagrant.log`. Common causes:
- Box has no libvirt provider variant → use a `generic/*` or `bento/*` box
- Package install failed during provisioning (package not in official repos)

**All results are zero for a distro** — the hardener binary failed system
validation before running any suites. Open `<distro>.log` and look for
`[> ERROR]` lines. Usually a tool listed in `00_README.md` preconditions
is missing on that distro.

**`vagrant plugin install` fails** — the dev libraries must be installed first:

```bash
sudo apt install libvirt-dev ruby-dev build-essential
```

**`virt-host-validate` shows FAIL on KVM** — virtualisation is disabled in
firmware. Enable Intel VT-x or AMD-V in BIOS/UEFI settings.

---

## Adding a distro

1. Add an entry to the `DISTROS` hash in `Vagrantfile` with a libvirt-compatible box and the package install command for prereqs
2. If the distro uses a firewall other than ufw or firewalld, update the `os_tools` section in `../report/sections/00_README.md`
3. Run: `./run_tests.sh --guide ../report/sections --binary /tmp/hardener-linux --distros "newdistro"`
4. Check the vagrant log if it shows `SKIP`, the hardener log if results are all zero

Browse available boxes with libvirt support at <https://app.vagrantup.com>.
