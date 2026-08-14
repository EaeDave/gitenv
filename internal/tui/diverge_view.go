package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/eaedave/gitenv/internal/app"
)

// renderDiverged explains, without Git vocabulary, that two copies of the vault
// changed independently, shows how much each side changed, and offers the ways
// out. It never shows a commit hash.
func (m model) renderDiverged(width int) string {
	local, remote := 0, 0
	if m.diverged != nil {
		local, remote = m.diverged.LocalCommits, m.diverged.RemoteCommits
	}
	intro := styles.value.Render(
		"This computer and the vault's shared copy have both changed since they were last in sync. " +
			"Neither has seen the other's changes, so they cannot be combined automatically — choose how to continue.")
	counts := labelValue("Changed on this computer", fmt.Sprintf("%d %s", local, pluralize(local, "update", "updates"))) +
		"\n" + labelValue("Changed on the shared copy", fmt.Sprintf("%d %s", remote, pluralize(remote, "update", "updates")))

	items := m.divergedMenuItems()
	labels := make([]string, len(items))
	for index, item := range items {
		labels[index] = item.label
	}
	body := intro + "\n\n" + counts + "\n\n" + renderMenu(labels, m.divergedCursor)
	panel := renderPanel("Vault changed in two places", body, min(width, 76), true)
	help := renderHelp("↑↓", "select", "enter", "choose", "?", "help", "esc", "back")
	return lipgloss.JoinVertical(lipgloss.Left, panel, "", help)
}

// renderDivergedProfiles lists the environments that changed on both sides and
// the current keep-mine / take-remote choice for each.
func (m model) renderDivergedProfiles(width int) string {
	conflicts := m.divergedConflicts()
	var body string
	if len(conflicts) == 0 {
		body = styles.value.Render("No environments changed on both sides.")
	} else {
		intro := styles.value.Render(
			"These environments changed both here and on the shared copy. For each one, choose whether to keep " +
				"this computer's version or take the shared copy's.")
		rows := make([]string, 0, len(conflicts))
		for index, profile := range conflicts {
			label := profile.Project + " / " + profile.Profile
			choice := styles.value.Render("take the shared copy")
			if m.divergedChoice(profile) == app.DivergenceKeepMine {
				choice = styles.success.Render("keep my version")
			}
			marker := "  " + label
			if index == m.menuCursor {
				marker = styles.selected.Render("› " + label)
			}
			rows = append(rows, marker+styles.muted.Render("  →  ")+choice)
		}
		body = intro + "\n\n" + strings.Join(rows, "\n")
	}
	panel := renderPanel("Environments changed on both sides", body, min(width, 76), true)
	help := renderHelp("↑↓", "select", "←/→/space", "keep mine / take theirs", "enter", "done", "esc", "back")
	return lipgloss.JoinVertical(lipgloss.Left, panel, "", help)
}

// renderConfirmDiverged states exactly what the chosen resolution does and that
// the previous vault stays recoverable. It never shows a commit hash.
func (m model) renderConfirmDiverged(width int) string {
	const recoverable = "Before anything changes, a recoverable backup of your current vault is saved, so nothing is lost for good."
	if m.pendingDivergedAction() == divergeDiscard {
		message := "This computer's unpublished vault changes will be thrown away and the shared copy kept as-is.\n" +
			"Your local .env files are not modified.\n" + recoverable + " [y/N]"
		return m.renderConfirmation("Discard this computer's vault changes?", message, width)
	}
	message := "Your vault will be rebuilt on the shared copy: this computer's version is kept for every environment " +
		"you chose to keep and for anything only this computer changed, and the shared copy is accepted everywhere else.\n" +
		"Your local .env files are not modified.\n" + recoverable + " [y/N]"
	return m.renderConfirmation("Rebuild your vault on top of the shared copy?", message, width)
}
