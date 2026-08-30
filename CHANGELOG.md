   # Changelog

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
