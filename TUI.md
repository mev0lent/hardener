# Hardener live terminal dashboard

This feature is based on commit `67cea3f` (the published binary rollback fix).
It adds an optional dashboard to the existing audit and fix commands.

## Run it

```bash
go mod tidy
go test ./...
go build -o hardener .

# Simulated preview: no shell checks, fixes, backups, or reports.
./hardener tui-demo

# Real audit and fix runs.
./hardener audit --ruleset ruleset.yaml --all --tui
./hardener fix --ruleset ruleset.yaml --all --tui

# Existing filters and suite selection still work.
./hardener audit --ruleset ruleset.yaml --label network,auth --tui
```

Without `--tui`, existing terminal output remains available. If stdin or stdout
is redirected, or `TERM=dumb`, `--tui` falls back to plain output. `tui-demo`
requires an interactive terminal.

## Layout

- Left: one rectangular card per selected ruleset suite, with its verified pass
  percentage, a colored bar, progress, and finding counts. Categories are read
  from the ruleset rather than hard-coded.
- Right: a bounded, scrolling activity feed containing check starts, results,
  fix messages, backup errors, and report messages.
- Top: verified pass percentage, completed/total checks, findings, and successful
  fix commands. Elapsed time and current run state remain visible.
- Wide terminals (at least 96 columns) use two columns. Narrow terminals show
  one browsable category above the feed. Minimum useful size: 64 × 24.
- Colors adapt to dark/light terminal backgrounds. Scores below 35% are red,
  35–64% orange, and 65–100% green. Labels convey status without relying on color.

## What the percentage means

`passed / assessed × 100`, using results received so far. Pending checks do not
enter the denominator. Skipped and manual checks are excluded. A category with
no assessed checks shows `—`, not a misleading zero or 100%.

A successful fix command is counted separately and remains in the assessed
set, but does not count as a verified pass. The executor has not rerun that
check, and some changes require a service reload or reboot. Run a new audit to
measure the resulting pass rate. This percentage describes the selected
ruleset checks; it is not a universal security rating.

The dashboard indexes suites and checks by position so duplicate titles or IDs
do not merge their counters. Existing report/scoring APIs remain available;
report results gain optional `error`, `manual`, and `skip_reason` fields for structured status.

## Keyboard

| Key | Action |
| --- | --- |
| Tab | Switch focus between categories and activity |
| Left / Right or h / l | Focus categories / activity |
| Up / Down or k / j | Browse categories or scroll activity |
| Page Up / Page Down | Move five categories or activity rows |
| g / Home | Go to the beginning |
| G / End | Go to the end |
| o | Open a saved JSON report after the run finishes |
| f | Follow the active category and newest activity again |
| q / Esc / Ctrl+C | Request stop while running; close once finished |

A stop request is honored between checks. It does not kill an in-progress fix:
the current check, fix, and rollback backup are allowed to finish. Partial
results are still reported, with `incomplete: true` in the report JSON. The completed/stopped dashboard remains available
for review until closed. There is no in-UI force-kill shortcut.

## Credentials and interactive commands

When applicable checks need sudo, Hardener validates credentials before opening
the dashboard, then refreshes the cache noninteractively during execution.
A failed refresh requests a stop at the next check boundary. No password is
handled by the dashboard model or activity log.

Ruleset commands must be noninteractive for dashboard mode. Commands that
request other input, invalidate sudo credentials, or use policies requiring
fresh authentication for every command should use plain mode. This feature
does not change what a fix command is authorized to do.

## Implementation

Bubble Tea v1.3.10 manages input, resize events, and terminal restoration;
Lip Gloss and the existing ANSI helpers render the layout. Existing execution
runs in one worker, and typed events update the display. `ui.SetLogSink` routes
messages while the dashboard is open and restores the plain printer afterward.
The renderer itself never executes a hardening command.

Each category activity feed retains at most 800 entries and truncates unusually large
messages for display. Command escape sequences are removed before rendering.
Existing JSON reports retain the executor's result output.

## Validation

```bash
go test ./...
go test -race ./internal/dashboard ./internal/executor ./internal/ui
```

Tests cover score semantics, duplicate IDs, terminal-size limits, escape
sanitization, bounded logs, stop behavior, event order, backup completion,
structured errors/manual checks, label filtering, and log-sink restoration.

For a macOS release, also exercise `tui-demo` and a real audit in Terminal or
iTerm2. Fix mode with real sudo policies and live system settings needs a
controlled test machine, as with the plain runner.

## Open saved JSON reports

```bash
./hardener view reports/audit-2026-09-08_120000.json
./hardener view  # opens a path prompt
```

You can also press **o** in a completed or stopped dashboard. Enter the path
without shell quotes, then press Enter. Relative paths start at your current
working directory; ~/ paths are supported. Esc cancels, Ctrl+U clears the path.
Loading errors stay in the prompt and preserve the previously displayed results.

The viewer reconstructs category scores and check results without executing
commands, requesting sudo, or writing reports. Select a category on the left to
read its saved results on the right. Tab switches focus for scrolling. All
recorded checks remain accessible, including reports exceeding the live feed's
800-entry limit. Press o to open another report.

Saved views display the original report timestamp and a READ ONLY label.
Incomplete reports are marked PARTIAL. Planned totals and per-check timestamps
are not stored in existing JSONs, so the viewer shows recorded check counts.
This reconstructs results; it does not replay the original timed activity log.
Older reports without structured error/manual fields retain their recorded
pass/fail flags; the viewer does not guess missing status information.

Skipped checks display their explicit reason, falling back to output in older
reports. Execution errors remain ERROR entries with error details.
Use audit/fix result JSONs from reports/, not rollback runs.json. Malformed or
unrelated files are rejected; report imports are limited to 32 MiB.

## Synchronized category activity

Selecting a category switches the right pane to that category's retained logs,
starting at the top. Each category retains its latest 800 log entries independently.
New output from other categories does not change the category you are browsing.
Press f to return to the active category and its newest output. Scrolling the
activity pane pauses automatic category following too.

Run-wide notices (such as report saving or completion) remain visible in every
category and carry a [RUN] prefix. Saved-report navigation works as before.
