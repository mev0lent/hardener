package ui

import "testing"

func TestLogSinkRoutesUnstyledMessagesAndRestoresPreviousSink(t *testing.T) {
	var outer, inner []LogEntry
	restoreOuter := SetLogSink(func(entry LogEntry) { outer = append(outer, entry) })
	defer restoreOuter()
	PrintInfo("first")
	restoreInner := SetLogSink(func(entry LogEntry) { inner = append(inner, entry) })
	PrintPassed("check-id")
	restoreInner()
	PrintInfo("last")
	if len(outer) != 2 || outer[0].Message != "first" || outer[1].Message != "last" {
		t.Fatalf("outer sink was not restored: %+v", outer)
	}
	if len(inner) != 1 || inner[0].Level != "pass" || inner[0].Message != "check-id | check passed" {
		t.Fatalf("unexpected inner event: %+v", inner)
	}
}
