# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Hardener, please report it via
GitHub's private disclosure channel rather than opening a public issue:

**https://github.com/mev0lent/hardener/security/advisories/new**

The maintainer receives the report privately, can discuss a fix with you,
and will coordinate public disclosure only after a fix is available.

## What to include

- The Hardener version (`hardener --version` or the release tag)
- Your operating system and, if relevant, distribution and version
- A minimal reproduction (commands, ruleset excerpt if applicable)
- Your assessment of impact (data integrity, privilege escalation,
  denial of service, etc.)

## Scope

The following are in scope:

- Bugs in the audit / fix / rollback engine that allow the framework to
  modify files outside the declared scope of a policy, or to leave the
  system in an unrecoverable state
- Ruleset-parsing bugs that allow crafted YAML to trigger unintended
  shell execution beyond what the policy declares
- Rollback bugs that fail to restore a file whose SHA-256 was recorded
  as `pre-fix`
- Supply-chain concerns about the released binaries (checksum
  mismatches, unexpected build artefacts)

The following are explicitly **out of scope**:

- Vulnerabilities in the target system's software that Hardener is
  configured to harden (report those to the respective vendor)
- Policies that are dangerous by construction (`fix: rm -rf /`);
  Hardener executes the fix commands the operator loads and does not
  attempt to sandbox them
- Attacks that require operator-level write access to the policy
  repository itself; the operator's write control over the ruleset is
  part of the framework's trust model

## Supported versions

The most recent tagged release receives security fixes. Older releases
do not; upgrade to the latest tag before reporting an issue against an
older one.
