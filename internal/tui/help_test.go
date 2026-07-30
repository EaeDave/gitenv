package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// keymapHasKey reports whether a keymap binds the given key column.
func keymapHasKey(bindings []keyBinding, keys string) bool {
	for _, b := range bindings {
		if b.keys == keys {
			return true
		}
	}
	return false
}

// TestHelpReturnsToOriginatingScreen verifies the audit's core promise: `?`
// records where it was opened from and `esc` lands the user back there, not on
// some fixed default.
func TestHelpReturnsToOriginatingScreen(t *testing.T) {
	for _, origin := range []screen{screenProjects, screenProfiles} {
		m := model{screen: origin}
		opened, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
		got := opened.(model)
		if cmd != nil {
			t.Fatalf("opening help must not start a command (origin %v)", origin)
		}
		if got.screen != screenHelp || got.helpReturn != origin {
			t.Fatalf("? did not open help remembering origin %v: screen=%v return=%v", origin, got.screen, got.helpReturn)
		}
		back, _ := got.helpKey(tea.KeyMsg{Type: tea.KeyEsc})
		if back.(model).screen != origin {
			t.Fatalf("esc did not return to %v: screen=%v", origin, back.(model).screen)
		}
	}
}

// TestHelpScreenDocumentsProjectUtilityKeys guards the two less-frequent
// project actions: recovery backup and device approval. Both use lowercase
// letters because routine navigation must not require Shift.
func TestHelpScreenDocumentsProjectUtilityKeys(t *testing.T) {
	km := keymapFor(screenProjects)
	if !keymapHasKey(km, "b") || !keymapHasKey(km, "d") {
		t.Fatalf("projects keymap is missing b or d: %#v", km)
	}

	m := model{screen: screenHelp, helpReturn: screenProjects}
	out := m.renderHelpScreen(80)
	if !strings.Contains(out, "save recovery") {
		t.Fatalf("help screen does not document the b/save-recovery binding:\n%s", out)
	}
	if !strings.Contains(out, "devices") {
		t.Fatalf("help screen does not document the d/devices binding:\n%s", out)
	}
}

// TestHelpGlossaryExplainsEveryStatusLabel ensures the glossary explains every
// label the TUI actually shows, and stays in lockstep with renderStatus: a
// glossary defining internal words the interface no longer displays would be
// worse than none.
func TestHelpGlossaryExplainsEveryStatusLabel(t *testing.T) {
	m := model{screen: screenHelp, helpReturn: screenProjects}
	out := m.renderHelpScreen(80)
	for _, status := range []string{"clean", "modified", "missing", "unmanaged", "unlinked"} {
		label := strings.TrimSpace(strings.TrimLeft(stripStyling(renderStatus(status)), "●○ "))
		if !strings.Contains(out, label) {
			t.Fatalf("glossary does not explain the label %q shown for %q:\n%s", label, status, out)
		}
	}
}

// stripStyling removes ANSI sequences so a rendered label can be compared as
// plain text.
func stripStyling(s string) string {
	var out strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape && r == 'm':
			inEscape = false
		case !inEscape:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// TestKeymapForCoveredScreensIsComplete verifies every covered screen has a
// non-empty keymap with no blank keys or actions.
func TestKeymapForCoveredScreensIsComplete(t *testing.T) {
	covered := []screen{screenProjects, screenProfiles, screenSyncDiff, screenDevices, screenDiverged}
	for _, s := range covered {
		km := keymapFor(s)
		if len(km) == 0 {
			t.Fatalf("keymapFor(%v) returned no bindings", s)
		}
		for _, b := range km {
			if b.keys == "" || b.action == "" {
				t.Fatalf("keymapFor(%v) has an incomplete binding: %#v", s, b)
			}
		}
	}
}
