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
