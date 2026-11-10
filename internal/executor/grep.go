package executor

import (
	"path/filepath"
	"strings"
)

// grepArgOptions are the short grep options that consume an argument (GNU and
// BSD agree on these). In a cluster such as "-m1" or "-ec", everything after
// one of them is that argument, not further options.
const grepArgOptions = "ABCDdefm"

// grepLongArgOptions are long options that take their argument as the next
// word when it is not attached with "=".
var grepLongArgOptions = map[string]bool{
	"--regexp": true, "--file": true, "--max-count": true, "--after-context": true,
	"--before-context": true, "--context": true, "--label": true, "--include": true,
	"--exclude": true, "--exclude-from": true, "--exclude-dir": true, "--devices": true,
	"--directories": true, "--binary-files": true, "--group-separator": true,
}

// countsGrepMatches reports whether command runs grep in count mode. A
// counting grep prints "0" and exits 1 when nothing matches; that is a valid
// result, not a command error. Every spelling of the flag counts: "-c", "-ic",
// "-Eci", "-v -c", "--count", and egrep/fgrep or a path such as /usr/bin/grep.
func countsGrepMatches(command string) bool {
	// Earlier versions matched the literal "grep -c" anywhere in the command.
	// Keep accepting that, so no existing ruleset loses the exception.
	if strings.Contains(command, "grep -c") {
		return true
	}
	tokens := shellTokens(command)
	for i, tok := range tokens {
		if tok.operator {
			continue
		}
		switch filepath.Base(tok.text) {
		case "grep", "egrep", "fgrep":
			if grepArgsCount(tokens[i+1:]) {
				return true
			}
		default:
			// A quoted script, as in sh -c '... | grep -ic x'. Quotes make
			// the word shorter than the command, so this terminates.
			if strings.Contains(tok.text, "grep") && strings.ContainsAny(tok.text, " \t\n") && countsGrepMatches(tok.text) {
				return true
			}
		}
	}
	return false
}

// grepArgsCount scans one grep invocation's arguments up to the next shell
// operator. Operands do not end the scan, since GNU grep accepts options after
// them; the caller's output check guards against BSD grep, which does not.
func grepArgsCount(args []shellToken) bool {
	for i := 0; i < len(args); i++ {
		if args[i].operator {
			return false
		}
		arg := args[i].text
		switch {
		case arg == "--":
			return false
		case strings.HasPrefix(arg, "--"):
			name, _, attached := strings.Cut(arg, "=")
			if name == "--count" {
				return true
			}
			if grepLongArgOptions[name] && !attached {
				i++
			}
		case strings.HasPrefix(arg, "-") && len(arg) > 1:
			cluster := arg[1:]
			for j, opt := range cluster {
				if opt == 'c' {
					return true
				}
				if strings.ContainsRune(grepArgOptions, opt) {
					if j == len(cluster)-1 {
						i++ // the argument is the next word
					}
					break
				}
			}
		}
	}
	return false
}

type shellToken struct {
	text     string
	operator bool
}

// shellTokens splits a shell command into words and operators. Quotes and
// backslashes are honoured, so a quoted pattern such as '-c|x' stays one word
// and is never taken for an option or a pipe. "|", ";", "&", "(", ")", "<",
// ">", "`" and newlines become operator tokens. This is not a shell parser;
// it only has to find grep invocations and their option words.
func shellTokens(command string) []shellToken {
	var tokens []shellToken
	var word strings.Builder
	inWord := false
	flush := func() {
		if inWord {
			tokens = append(tokens, shellToken{text: word.String()})
			word.Reset()
			inWord = false
		}
	}
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '\'':
			inWord = true
			for i++; i < len(runes) && runes[i] != '\''; i++ {
				word.WriteRune(runes[i])
			}
		case r == '"':
			inWord = true
			for i++; i < len(runes) && runes[i] != '"'; i++ {
				if runes[i] == '\\' && i+1 < len(runes) {
					i++
				}
				word.WriteRune(runes[i])
			}
		case r == '\\' && i+1 < len(runes):
			inWord = true
			i++
			if runes[i] != '\n' {
				word.WriteRune(runes[i])
			}
		case r == ' ' || r == '\t':
			flush()
		case strings.ContainsRune("|;&()<>`\n", r):
			flush()
			tokens = append(tokens, shellToken{text: string(r), operator: true})
		default:
			inWord = true
			word.WriteRune(r)
		}
	}
	flush()
	return tokens
}
