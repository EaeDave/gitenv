package tui

import (
	"fmt"
	"strings"

	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

const syncInventoryLineLimit = 8

func (m model) renderSyncInventory() string {
	if m.syncStatus.State == gitops.SyncChecking {
		return ""
	}
	if !m.syncInventory.Available {
		if m.syncInventory.Detail == "" {
			return ""
		}
		return styles.muted.Render(m.syncInventory.Detail)
	}
	sections := make([]string, 0, 2)
	if !m.syncInventory.Committed.Empty() {
		title := "Committed, not published"
		if m.syncStatus.State == gitops.SyncRemoteAhead {
			title = "Incoming from remote"
		}
		sections = append(sections, renderInventorySection(title, m.syncInventory.Committed))
	} else if m.syncStatus.State == gitops.SyncLocalAhead && m.syncStatus.Ahead > 0 {
		sections = append(sections, renderCommitOnlySummary("Committed, not published", "↑", m.syncStatus.Ahead))
	} else if m.syncStatus.State == gitops.SyncRemoteAhead && m.syncStatus.Behind > 0 {
		sections = append(sections, renderCommitOnlySummary("Incoming from remote", "↓", m.syncStatus.Behind))
	}
	if !m.syncInventory.Uncommitted.Empty() {
		sections = append(sections, renderInventorySection("Uncommitted vault changes", m.syncInventory.Uncommitted))
	}
	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n\n") + "\n" + styles.muted.Render("Values hidden")
}

func renderCommitOnlySummary(title, direction string, count int) string {
	return styles.label.Render(title) + "\n" + styles.muted.Render(fmt.Sprintf("%s %d commit(s), no vault content changes", direction, count))
}

func renderInventorySection(title string, delta vault.VaultDelta) string {
	lines := []string{styles.label.Render(title)}
	for _, profile := range delta.Profiles {
		lines = append(lines, renderProfileDelta(profile)...)
	}
	if delta.MetadataChanged {
		lines = append(lines, styles.warning.Render("~ vault metadata"))
	}
	if delta.OtherFilesChanged > 0 {
		lines = append(lines, styles.warning.Render(fmt.Sprintf("~ %d other vault file(s)", delta.OtherFilesChanged)))
	}
	return limitInventoryLines(lines, syncInventoryLineLimit)
}

func renderProfileDelta(profile vault.ProfileDelta) []string {
	name := profile.Project + " / " + profile.Profile
	switch profile.Kind {
	case vault.ProfileAdded:
		return []string{styles.success.Render("+ " + name)}
	case vault.ProfileRemoved:
		return []string{styles.danger.Render("- " + name)}
	}
	if profile.Diff.Empty() {
		return []string{styles.warning.Render("~ " + name), styles.muted.Render("  encrypted profile refreshed; content unchanged")}
	}
	lines := []string{styles.value.Render(name)}
	for _, change := range profile.Diff.Changes {
		lines = append(lines, "  "+renderCaptureChange(change))
	}
	if profile.Diff.CommentChanges > 0 {
		lines = append(lines, styles.muted.Render(fmt.Sprintf("  • %d comment change(s)", profile.Diff.CommentChanges)))
	}
	if profile.Diff.UnknownChanges > 0 {
		lines = append(lines, styles.warning.Render(fmt.Sprintf("  ! %d unrecognized line change(s)", profile.Diff.UnknownChanges)))
	}
	return lines
}

func limitInventoryLines(lines []string, limit int) string {
	if len(lines) <= limit {
		return strings.Join(lines, "\n")
	}
	hidden := len(lines) - limit
	visible := append([]string(nil), lines[:limit]...)
	visible = append(visible, styles.muted.Render(fmt.Sprintf("+ %d more change line(s)", hidden)))
	return strings.Join(visible, "\n")
}
