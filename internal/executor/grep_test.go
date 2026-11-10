package executor

import (
	"testing"

	"hardener/internal/config"
)

func TestCountsGrepMatches(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    bool
	}{
		{"grep -c foo /etc/x", true},
		{"cmd | grep -c enabled", true},
		// The two published macOS 26 checks that reported an error.
		{"diskutil apfs listUsers / | grep -Eci 'Personal Recovery User|Institutional'", true},
		{"defaults read com.apple.sharingd DiscoverableMode 2>/dev/null | grep -v -c '^Everyone$'", true},
		{"grep -ic x f", true},
		{"grep -E -i -c x f", true},
		{"grep --count x f", true},
		{"grep -E 'x' -c f", true}, // GNU grep takes options after operands
		{"egrep -c 'a|b' f", true},
		{"fgrep -c x f", true},
		{"/usr/bin/grep -c x f", true},
		{"x=$(grep -c foo f); echo $x", true},
		{"if true; then\n  mokutil --sb-state | grep -c enabled\nfi", true},
		{"grep -c '' f", true},
		// A quoted script, which the old substring rule caught for "grep -c".
		{"sh -c 'printf x | grep -c nomatch'", true},
		{`sudo sh -c "cmd | grep -Eci 'a|b'"`, true},
		{"bash -c 'cmd | /usr/bin/grep -ic x'", true},
		// Earlier versions accepted any command containing "grep -c"; that
		// stays accepted so no existing ruleset changes behavior.
		{"pgrep -c sshd", true},
		{"awk '/grep -c/ {print}' f", true},

		{"grep foo f", false},
		{"grep -q foo f", false},
		{"grep -C 3 foo f", false},           // context, not count
		{"grep -ec f", false},                // -e takes "c" as its pattern
		{"grep -m1 foo f", false},            // -m takes "1"
		{"grep -e -c f", false},              // "-c" is the pattern
		{"grep -- -c f", false},              // "-c" is an operand
		{"grep 'x -c y' f", false},           // quoted pattern
		{"grep foo f | wc -c", false},        // -c belongs to wc
		{"grep -F '|' f; wc -c f", false},    // quoted pipe is not an operator
		{"grep --context 3 foo f", false},    // --context takes "3"
		{"grep --regexp=-c f", false},        // attached argument
		{"pgrep -l sshd", false},             // a different tool
		{"echo grepping -c", false},          // not grep
		{"awk '/grep -v/ {print}' f", false}, // not count mode
		{"sh -c 'grep -q x f'", false},
		{"sysctl -n kernel.randomize_va_space", false},
	} {
		if got := countsGrepMatches(tc.command); got != tc.want {
			t.Errorf("countsGrepMatches(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}

// A counting grep that matches nothing prints 0 and exits 1. That is a
// finding, not a command error, whatever the spelling of the count flag.
func TestCountingGrepWithNoMatchFailsWithoutError(t *testing.T) {
	for _, command := range []string{
		"printf 'x\\n' | grep -c nomatch",
		"printf 'x\\n' | grep -Eci 'no|match'",
		"printf 'x\\n' | grep -v -c x",
		"printf 'x\\n' | grep --count nomatch",
	} {
		passed, output, err := RunCheck(config.Check{Command: command, Expected: "1"})
		if err != nil || passed || output != "0" {
			t.Errorf("%q: passed=%v output=%q err=%v, want a plain failure with output 0", command, passed, output, err)
		}
		passed, _, err = RunCheck(config.Check{Command: command, Expected: "0"})
		if err != nil || !passed {
			t.Errorf("%q: expected 0 should pass, got passed=%v err=%v", command, passed, err)
		}
	}

	// Exit 1 without a count is still an error.
	if _, _, err := RunCheck(config.Check{Command: "printf 'x\\n' | grep -q nomatch", Expected: "1"}); err == nil {
		t.Error("grep -q exiting 1 was not reported as an error")
	}
	// grep exits 2 for a real error, e.g. a missing file.
	if _, _, err := RunCheck(config.Check{Command: "grep -c x /nonexistent/hardener-test", Expected: "0"}); err == nil {
		t.Error("grep exiting 2 was not reported as an error")
	}
}
