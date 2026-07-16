package envdiff

import "strings"

type LineKind string

const (
	LineContext LineKind = "context"
	LineRemoved LineKind = "removed"
	LineAdded   LineKind = "added"
)

type LineChange struct {
	Kind    LineKind
	OldLine int
	NewLine int
	Text    string
}

func CompareLines(base, current []byte) []LineChange {
	before := splitLines(base)
	after := splitLines(current)
	columns := len(after) + 1
	lcs := make([]int, (len(before)+1)*columns)
	for beforeIndex := len(before) - 1; beforeIndex >= 0; beforeIndex-- {
		for afterIndex := len(after) - 1; afterIndex >= 0; afterIndex-- {
			cell := beforeIndex*columns + afterIndex
			if before[beforeIndex] == after[afterIndex] {
				lcs[cell] = lcs[(beforeIndex+1)*columns+afterIndex+1] + 1
			} else {
				lcs[cell] = max(lcs[(beforeIndex+1)*columns+afterIndex], lcs[beforeIndex*columns+afterIndex+1])
			}
		}
	}
	changes := make([]LineChange, 0, len(before)+len(after))
	beforeIndex, afterIndex := 0, 0
	for beforeIndex < len(before) || afterIndex < len(after) {
		switch {
		case beforeIndex < len(before) && afterIndex < len(after) && before[beforeIndex] == after[afterIndex]:
			changes = append(changes, LineChange{Kind: LineContext, OldLine: beforeIndex + 1, NewLine: afterIndex + 1, Text: before[beforeIndex]})
			beforeIndex++
			afterIndex++
		case afterIndex < len(after) && (beforeIndex == len(before) || lcs[beforeIndex*columns+afterIndex+1] > lcs[(beforeIndex+1)*columns+afterIndex]):
			changes = append(changes, LineChange{Kind: LineAdded, NewLine: afterIndex + 1, Text: after[afterIndex]})
			afterIndex++
		default:
			changes = append(changes, LineChange{Kind: LineRemoved, OldLine: beforeIndex + 1, Text: before[beforeIndex]})
			beforeIndex++
		}
	}
	return changes
}

func splitLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
