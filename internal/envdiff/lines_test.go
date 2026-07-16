package envdiff

import (
	"reflect"
	"testing"
)

func TestCompareLinesReturnsLiteralAddedRemovedAndContextLines(t *testing.T) {
	base := []byte("API_URL=https://old.example\nDEBUG=false\n# old comment\n")
	current := []byte("API_URL=https://new.example\nDEBUG=false\n# new comment\nADDED=secret\n")

	got := CompareLines(base, current)
	want := []LineChange{
		{Kind: LineRemoved, OldLine: 1, Text: "API_URL=https://old.example"},
		{Kind: LineAdded, NewLine: 1, Text: "API_URL=https://new.example"},
		{Kind: LineContext, OldLine: 2, NewLine: 2, Text: "DEBUG=false"},
		{Kind: LineRemoved, OldLine: 3, Text: "# old comment"},
		{Kind: LineAdded, NewLine: 3, Text: "# new comment"},
		{Kind: LineAdded, NewLine: 4, Text: "ADDED=secret"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CompareLines() = %#v, want %#v", got, want)
	}
}

func TestCompareLinesHandlesEmptyAndCRLFInputs(t *testing.T) {
	got := CompareLines(nil, []byte("A=1\r\n"))
	want := []LineChange{{Kind: LineAdded, NewLine: 1, Text: "A=1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CompareLines() = %#v, want %#v", got, want)
	}
}
