package envdiff

import (
	"crypto/sha256"
	"regexp"
	"strings"
)

type Kind string

const (
	Added    Kind = "added"
	Removed  Kind = "removed"
	Changed  Kind = "changed"
	Enabled  Kind = "enabled"
	Disabled Kind = "disabled"
)

type Change struct {
	Key  string
	Kind Kind
}

type Diff struct {
	Changes        []Change
	CommentChanges int
	UnknownChanges int
}

func (d Diff) Empty() bool {
	return len(d.Changes) == 0 && d.CommentChanges == 0 && d.UnknownChanges == 0
}

type assignment struct {
	key      string
	disabled bool
	value    [sha256.Size]byte
}

type parsedEnv struct {
	assignments []assignment
	comments    [][sha256.Size]byte
	unknown     [][sha256.Size]byte
}

var assignmentPattern = regexp.MustCompile(`^(?:export[\t ]+)?([A-Za-z_][A-Za-z0-9_]*)[\t ]*=(.*)$`)

func Compare(base, current []byte) Diff {
	before, after := parse(base), parse(current)
	return Diff{
		Changes:        compareAssignments(before.assignments, after.assignments),
		CommentChanges: digestDifference(before.comments, after.comments),
		UnknownChanges: digestDifference(before.unknown, after.unknown),
	}
}

func parse(content []byte) parsedEnv {
	var result parsedEnv
	for _, raw := range strings.Split(string(content), "\n") {
		line := strings.TrimSuffix(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if assignment, ok := parseAssignment(trimmed, false); ok {
			result.assignments = append(result.assignments, assignment)
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			commentBody := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
			if assignment, ok := parseAssignment(commentBody, true); ok {
				result.assignments = append(result.assignments, assignment)
			} else {
				result.comments = append(result.comments, sha256.Sum256([]byte(trimmed)))
			}
			continue
		}
		result.unknown = append(result.unknown, sha256.Sum256([]byte(trimmed)))
	}
	return result
}

func parseAssignment(line string, disabled bool) (assignment, bool) {
	matches := assignmentPattern.FindStringSubmatch(line)
	if matches == nil {
		return assignment{}, false
	}
	return assignment{
		key:      matches[1],
		disabled: disabled,
		value:    sha256.Sum256([]byte(matches[2])),
	}, true
}

func compareAssignments(base, current []assignment) []Change {
	baseByKey := groupAssignments(base)
	seen := make(map[string]int, len(baseByKey))
	changes := make([]Change, 0)
	for _, currentAssignment := range current {
		occurrence := seen[currentAssignment.key]
		seen[currentAssignment.key] = occurrence + 1
		matching := baseByKey[currentAssignment.key]
		if occurrence >= len(matching) {
			changes = append(changes, Change{Key: currentAssignment.key, Kind: Added})
			continue
		}
		if kind, changed := compareAssignment(matching[occurrence], currentAssignment); changed {
			changes = append(changes, Change{Key: currentAssignment.key, Kind: kind})
		}
	}
	baseOccurrence := make(map[string]int, len(baseByKey))
	for _, baseAssignment := range base {
		occurrence := baseOccurrence[baseAssignment.key]
		baseOccurrence[baseAssignment.key] = occurrence + 1
		if occurrence >= seen[baseAssignment.key] {
			changes = append(changes, Change{Key: baseAssignment.key, Kind: Removed})
		}
	}
	return changes
}

func groupAssignments(assignments []assignment) map[string][]assignment {
	grouped := make(map[string][]assignment)
	for _, item := range assignments {
		grouped[item.key] = append(grouped[item.key], item)
	}
	return grouped
}

func compareAssignment(base, current assignment) (Kind, bool) {
	switch {
	case base.disabled && !current.disabled:
		return Enabled, true
	case !base.disabled && current.disabled:
		return Disabled, true
	case base.value != current.value:
		return Changed, true
	default:
		return "", false
	}
}

func digestDifference(base, current [][sha256.Size]byte) int {
	counts := make(map[[sha256.Size]byte]int, len(base)+len(current))
	for _, digest := range base {
		counts[digest]++
	}
	for _, digest := range current {
		counts[digest]--
	}
	difference := 0
	for _, count := range counts {
		if count < 0 {
			count = -count
		}
		difference += count
	}
	return difference
}
