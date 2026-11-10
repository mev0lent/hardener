   # Changelog

   ## Unreleased

   Fix verification and rollback safety (issues #13 to #18):

   - Checks are re-run after each fix; only a passing re-check counts as
     fixed. Fixes that exit 0 but leave the check failing are reported as
     "not effective" (#13)
   - Rollback removes files that a fix created instead of leaving them empty,
     and recreates files that a fix deleted (#14)
   - A fix is skipped when its pre-fix backup cannot be taken; file modes are
     read with `sudo stat` instead of being guessed (#15)
   - Rollback records are written before the fix runs and completed after it;
     an interrupted fix leaves a pending record that rollback can still
     restore (#16)
   - Rollback checks that a file still holds the post-fix content and
     re-reads it before replacing it (#17)
   - Live kernel values referenced by a check are recorded and restored on
     rollback on Linux (#18, partial: package transactions are not covered)
   - New `runs.json` fields are optional; records from older versions still
     roll back

   Bug fixes:

   - A counting grep that matches nothing is a failure, not an error, in every
     spelling of the flag (`-ic`, `-Eci`, `-v -c`, `--count`, `egrep -c`), not
     only the literal `grep -c`, also inside a quoted `sh -c '...'`. Every
     command the old rule accepted is still accepted. Fixes
     airdrop-discoverability-restricted and filevault-recovery-present-check
     in the macOS 26 ruleset
   - `security_level: optional` checks can run: `--security-level optional`
     selects every check. `high` still excludes them. Levels are
     case-insensitive, an unknown `--security-level` is rejected instead of
     silently running baseline, and checks with an unknown level are listed
   - `fix --dry-run` was ignored and applied every fix; it now audits and
     lists the fixes it would apply. `--arch`, which never had an effect, is
     deprecated
   - Suite summaries count errors from the recorded error instead of searching
     the output for "error", and count manual checks separately. Manual checks
     no longer raise the check pass rate, and audits no longer print a 0% fix
     rate
   - A failed check shows the command and expected value of the distro
     override that ran
   - `--path` mode keeps `--profile`, `--label` and the distro name, and
     matches the guide's `arch` like suites do (case-insensitive, `amd64` =
     `x86_64`)
   - Suites declaring `os: linux` no longer run, fixes included, on macOS;
     per-check `os`/`arch` lists are applied. `aarch64` matches `arm64` like
     `x86_64` matches `amd64`
   - The report message names the file actually written (under the working
     directory, not the binary's)
   - A directory as `affected_file` no longer blocks the fix (`/var/log`,
     `/var/log/journal`, `/etc/apparmor.d/` in the Linux ruleset); rollback
     does not cover it
   - Rollback handles `systemctl mask` links: the fix's record completes, and
     rollback removes the link instead of failing and skipping the live-state
     hooks for all other files. It never writes through such a link
   - The rollback hook restarts `sshd.service` or `ssh.service`, whichever
     exists, and only if running; it reloads systemd after restoring unit files
   - A `---` line inside a block scalar (e.g. a heredoc in a `fix: |`) no
     longer splits the YAML document; `---` elsewhere separates documents at
     any indentation, as before
   - Test harness: summaries wrapped at a hyphen (`distro- skipped`) are
     parsed, and fixes that were not effective count as still failing in FX_F

   ## v1.1 : 2026-08-30

   Reference build for the term paper. Additions over v1.0:

   - Distro-specific overrides via `distro:` YAML map with family fallback
   - Role profiles via `--profile` (`server`, `client`)
   - Suite label filtering via `--label`
   - Per-check `requires_command` / `requires_file` guards
   - Numeric comparison via `expected_op` (`>=`, `>`, `<=`, `<`)
   - PATH prepend of `/usr/local/sbin`, `/usr/sbin`, `/sbin`
   - Cross-distro test harness (`testing/`) with KVM/Vagrant integration
   - Published SHA-256 checksums for release binaries

   ## v1.0 — 2026-02-23

   Initial release; published as ERNW White Paper 77.
