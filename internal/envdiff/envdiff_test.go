package envdiff

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestCompareClassifiesStructuralChangesWithoutValues(t *testing.T) {
	base := []byte("UNCHANGED=same\nCHANGED=old-secret\n# ENABLED=off-secret\nDISABLED=on-secret\nREMOVED=gone-secret\n")
	current := []byte("UNCHANGED=same\nCHANGED=new-secret\nENABLED=off-secret\n# DISABLED=on-secret\nADDED=added-secret\n")
	got := Compare(base, current)
	want := []Change{
		{Key: "CHANGED", Kind: Changed},
		{Key: "ENABLED", Kind: Enabled},
		{Key: "DISABLED", Kind: Disabled},
		{Key: "ADDED", Kind: Added},
		{Key: "REMOVED", Kind: Removed},
	}
	if !reflect.DeepEqual(got.Changes, want) {
		t.Fatalf("changes = %#v, want %#v", got.Changes, want)
	}
	assertContainsNoFixtureValue(t, got, "same", "old-secret", "new-secret", "off-secret", "on-secret", "gone-secret", "added-secret")
}

func TestCompareHandlesExportWhitespaceCRLFAndDuplicateOccurrences(t *testing.T) {
	base := []byte("export API=first\r\nAPI=second\r\n")
	current := []byte(" API=first\nexport API=changed\nAPI=third\n")
	got := Compare(base, current)
	want := []Change{{Key: "API", Kind: Changed}, {Key: "API", Kind: Added}}
	if !reflect.DeepEqual(got.Changes, want) {
		t.Fatalf("changes = %#v, want %#v", got.Changes, want)
	}
}

func TestCompareCountsCommentsAndUnknownLinesWithoutContent(t *testing.T) {
	base := []byte("# old operational note\nsource ./base.env\n")
	current := []byte("# new operational note\nsource ./current.env\n")
	got := Compare(base, current)
	if got.CommentChanges != 2 || got.UnknownChanges != 2 || len(got.Changes) != 0 {
		t.Fatalf("unexpected diff: %#v", got)
	}
	assertContainsNoFixtureValue(t, got, "old operational note", "new operational note", "./base.env", "./current.env")
}

func TestCompareEmptyRecognizesEquivalentStructure(t *testing.T) {
	base := []byte("export API=value\r\n# COMMENT\r\n")
	current := []byte("API=value\n# COMMENT\n")
	if diff := Compare(base, current); !diff.Empty() {
		t.Fatalf("equivalent envs differ: %#v", diff)
	}
}

func assertContainsNoFixtureValue(t *testing.T, diff Diff, values ...string) {
	t.Helper()
	dump := fmt.Sprintf("%#v", diff)
	for _, value := range values {
		if strings.Contains(dump, value) {
			t.Fatalf("diff exposed value %q: %s", value, dump)
		}
	}
}
