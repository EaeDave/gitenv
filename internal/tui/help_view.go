package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// renderHelpScreen draws the full keymap for the screen help was opened from
// alongside a glossary that explains the app's vocabulary in the user's terms.
// It scrolls only when the content is taller than the terminal; on a normal
// terminal (or when the height is unknown) everything is shown at once.
func (m model) renderHelpScreen(width int) string {
	lines := m.helpLines(width)
	pageSize := m.helpPageSize()
	help := renderHelp("↑↓", "scroll", "esc", "back")
	if pageSize <= 0 || len(lines) <= pageSize {
		return lipgloss.JoinVertical(lipgloss.Left, strings.Join(lines, "\n"), "", help)
	}
	offset := clampHelpOffset(m.helpOffset, len(lines), pageSize)
	end := min(len(lines), offset+pageSize)
	body := strings.Join(lines[offset:end], "\n")
	position := styles.muted.Render(helpPosition(offset, end, len(lines)))
	return lipgloss.JoinVertical(lipgloss.Left, body, "", position, "", help)
}

// helpLines is the scrollable content: the keymap panel followed by the
// glossary panel, flattened to individual lines so the scroll window can page
// through them the way the diff viewer pages through syncDiffLines.
func (m model) helpLines(width int) []string {
	origin := m.helpReturnScreen()
	panelWidth := min(width, 76)
	keymap := renderPanel("Keys — "+helpScreenTitle(origin), keymapBody(keymapFor(origin)), panelWidth, true)
	everywhere := renderPanel("Everywhere", keymapBody(globalKeymap()), panelWidth, false)
	glossary := renderPanel("Glossary", helpGlossaryBody(), panelWidth, false)
	combined := lipgloss.JoinVertical(lipgloss.Left, keymap, "", everywhere, "", glossary)
	return strings.Split(combined, "\n")
}

// helpScreenTitle is the human name of the screen a keymap belongs to.
func helpScreenTitle(s screen) string {
	switch s {
	case screenProfiles:
		return "Profiles"
	case screenSyncDiff:
		return "Vault changes"
	case screenDevices:
		return "Devices"
	case screenDiverged:
		return "Diverged vault"
	default:
		return "Projects"
	}
}

// keymapBody renders one aligned "keys  action" row per binding, padding the key
// column to its widest entry so the actions line up.
func keymapBody(bindings []keyBinding) string {
	keyWidth := 0
	for _, b := range bindings {
		if w := lipgloss.Width(b.keys); w > keyWidth {
			keyWidth = w
		}
	}
	rows := make([]string, 0, len(bindings))
	for _, b := range bindings {
		pad := strings.Repeat(" ", keyWidth-lipgloss.Width(b.keys))
		rows = append(rows, styles.key.Render(b.keys)+pad+"   "+styles.value.Render(b.action))
	}
	return strings.Join(rows, "\n")
}

// glossaryEntry is one "term — meaning" line of the glossary.
type glossaryEntry struct{ term, def string }

// helpConcepts explains the nouns and verbs the whole app rests on, written for
// somebody who just installed it.
func helpConcepts() []glossaryEntry {
	return []glossaryEntry{
		{"Vault", "The Git repository holding your environments, encrypted. Only you can read it."},
		{"Profile", "One saved version of a project's env file (dev, prod, …)."},
		{"Capture", "Save the env file as it is on disk right now into the vault."},
		{"Apply", "Write a saved profile back onto disk, replacing the env file."},
		{"Publish / sync", "Send your encrypted vault changes to the remote, or bring the remote's changes down. Never touches your env files."},
	}
}

// helpStatusWords explains the status labels as the user actually sees them.
// These deliberately mirror renderStatus rather than vault.Status's internal
// vocabulary: explaining words like "unmanaged" would document terms the
// interface no longer shows.
func helpStatusWords() []glossaryEntry {
	return []glossaryEntry{
		{"up to date", "The file on disk matches what is saved in the vault."},
		{"uncaptured changes", "The file has edits you have not captured yet."},
		{"env file missing", "This project's env file is not on disk."},
		{"not captured yet", "There is an env file, but no profile saved from it."},
		{"no local copy", "The vault has this project, but this computer does not."},
	}
}

// helpGlossaryBody renders the concept glossary followed by the status-word
// glossary under its own subheading.
func helpGlossaryBody() string {
	rows := make([]string, 0, len(helpConcepts())+len(helpStatusWords())+2)
	for _, e := range helpConcepts() {
		rows = append(rows, glossaryRow(e))
	}
	rows = append(rows, "", styles.subtitle.Render("Status words"))
	for _, e := range helpStatusWords() {
		rows = append(rows, glossaryRow(e))
	}
	return strings.Join(rows, "\n")
}

// glossaryRow styles one glossary entry: the term stands out, the meaning is
// muted. The panel width word-wraps long meanings for us.
func glossaryRow(e glossaryEntry) string {
	return styles.brand.Render(e.term) + " " + styles.muted.Render("— "+e.def)
}

// helpPosition is the "Lines a–b of n" indicator shown only while scrolling.
func helpPosition(offset, end, total int) string {
	if total == 0 {
		return "No lines"
	}
	return fmt.Sprintf("Lines %d–%d of %d", offset+1, end, total)
}
