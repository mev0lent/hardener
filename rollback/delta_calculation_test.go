package rollback

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestDeltaRoundTrip(t *testing.T) {
	tests := []struct {
		name, source, target string
		snapshot             bool
	}{
		{"invalid source UTF-8", "\xe0", "", true},
		{"invalid target UTF-8", "", "\xff\xfe", true},
		{"NUL bytes", "a\x00b", "a\x00c", true},
		{"binary identity", "\xff\x00", "\xff\x00", true},
		{"binary plist marker", "bplist00abc", "bplist00def", true},
		{"XML to binary", "bplist00\xd1\x01\x02\x00", "<?xml version=\"1.0\"?><plist/>", true},
		{"text", "new config line\n", "old config line\n", false},
		{"Unicode", "Grüße 🌍\n設定=有効\n", "Grüße 🌎\n設定=無効\n", false},
		{"empty", "", "", false},
		{"text identity", "unchanged\n", "unchanged\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delta, checksum := ComputeDelta(tt.source, tt.target)
			if got := strings.HasPrefix(delta, snapshotPrefix); tt.snapshot && !got {
				t.Fatal("binary content did not use a snapshot")
			}
			if tt.name == "text" && strings.HasPrefix(delta, snapshotPrefix) {
				t.Fatal("ordinary text should still use a legacy-compatible patch")
			}
			// Exercise the runs.json storage boundary, where raw binary strings
			// would otherwise lose invalid UTF-8 bytes.
			encoded, err := json.Marshal(delta)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &delta); err != nil {
				t.Fatal(err)
			}
			restored, err := ApplyRollbackDelta(tt.source, delta)
			if err != nil {
				t.Fatal(err)
			}
			if restored != tt.target {
				t.Fatalf("got bytes %x, want %x", restored, tt.target)
			}
			if want := fmt.Sprintf("%x", sha256.Sum256([]byte(tt.target))); checksum != want {
				t.Fatalf("checksum = %s, want target checksum %s", checksum, want)
			}
		})
	}
}

func TestLegacyDelta(t *testing.T) {
	// Fixed pre-existing format; do not generate it using the new writer.
	const delta = "@@ -1,4 +1,4 @@\n-new\n+old\n %0A\n"
	got, err := ApplyRollbackDelta("new\n", delta)
	if err != nil || got != "old\n" {
		t.Fatalf("legacy rollback = %q, %v", got, err)
	}
}

func TestTextDeltaContainsDependencyPanic(t *testing.T) {
	// This input panics in go-diff v1.4.0's PatchMake at patch.go:171.
	// The text helper must signal failure so ComputeDelta can use a snapshot.
	if _, ok := computeTextDelta("\xe0", ""); ok {
		t.Fatal("invalid UTF-8 unexpectedly produced a lossless text patch")
	}
}

func TestSnapshotRejectsDrift(t *testing.T) {
	delta, _ := ComputeDelta("\xffnew", "\xfeold")
	if _, err := ApplyRollbackDelta("\xffunrelated edit", delta); err == nil {
		t.Fatal("expected error for changed source")
	}
	if got, err := ApplyRollbackDelta("\xfeold", delta); err != nil || got != "\xfeold" {
		t.Fatalf("already restored snapshot = %q, %v", got, err)
	}
}

func TestMalformedDelta(t *testing.T) {
	for _, delta := range []string{
		snapshotPrefix,
		snapshotPrefix + "bad:YWJj",
		snapshotPrefix + strings.Repeat("z", 64) + ":YWJj",
		snapshotPrefix + strings.Repeat("0", 64) + ":!invalid!",
		"hardener-snapshot-v2:unsupported",
		"invalid patch",
	} {
		if _, err := ApplyRollbackDelta("current", delta); err == nil {
			t.Errorf("expected error for %q", delta)
		}
	}
}
