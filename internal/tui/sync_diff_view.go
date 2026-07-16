package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/eaedave/gitenv/internal/envdiff"
	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

const syncDiffScreenChromeHeight = 12

func (m model) renderSyncDiff(width int) string {
	lines := m.syncDiffLines()
	pageSize := m.syncDiffPageSize()
	offset := clampSyncDiffOffset(m.syncDiffOffset, len(lines), pageSize)
	end := min(len(lines), offset+pageSize)
	visible := lines[offset:end]
	position := styles.muted.Render(syncDiffPosition(offset, end, len(lines)))
	body := strings.Join(visible, "\n")
	if body != "" {
		body += "\n\n"
	}
	body += position
	panel := renderPanel("Environment changes", body, width, true)
	visibility := "reveal values"
	if m.syncLineDiff != nil {
		visibility = "hide values"
	}
	help := renderHelp("tab", "select env", "e", "edit", "p", "publish", "d", "discard", "x", visibility, "↑↓/jk", "scroll", "esc/q", "back")
	return lipgloss.JoinVertical(lipgloss.Left, panel, "", help)
}

func (m model) syncDiffLines() []string {
	statusText, _ := syncStatusText(m.syncStatus)
	lines := []string{
		labelValue("Remote", valueOrNone(m.remoteDisplayURL)),
		styles.label.Render("Status  ") + statusText,
		"",
	}
	if m.syncDiffLoading {
		return append(lines, styles.warning.Render("Decrypting values in memory…"))
	}
	if m.syncLineDiff != nil {
		detailLines := m.revealedSyncDiffLines()
		if len(detailLines) == 0 {
			detailLines = append(detailLines, styles.success.Render("No literal line changes detected."))
		}
		lines = append(lines, detailLines...)
		return append(lines, "", styles.warning.Render("Values visible — press x to hide"))
	}
	detailLines := m.localEnvDiffLines()
	if m.syncInventory.LocalDetail != "" {
		if len(detailLines) > 0 {
			detailLines = append(detailLines, "")
		}
		detailLines = append(detailLines, styles.warning.Render(m.syncInventory.LocalDetail))
	}
	if m.syncStatus.State == gitops.SyncChecking {
		if len(detailLines) > 0 {
			detailLines = append(detailLines, "")
		}
		detailLines = append(detailLines, styles.warning.Render("Vault change inventory is still loading."))
	} else if !m.syncInventory.Available {
		if len(detailLines) > 0 {
			detailLines = append(detailLines, "")
		}
		detail := m.syncInventory.Detail
		if detail == "" {
			detail = "Vault change details unavailable."
		}
		detailLines = append(detailLines, styles.warning.Render(detail))
	} else {
		for _, section := range m.syncInventorySections(m.syncDiffScope()) {
			if len(detailLines) > 0 {
				detailLines = append(detailLines, "")
			}
			detailLines = append(detailLines, section...)
		}
	}
	if len(detailLines) == 0 {
		detailLines = append(detailLines, styles.success.Render("No local .env or vault content changes detected."))
	}
	lines = append(lines, detailLines...)
	return append(lines, "", styles.muted.Render("Values hidden"))
}

func (m model) localEnvDiffLines() []string {
	scope := m.syncDiffScope()
	if len(m.syncInventory.LocalEnvs) == 0 {
		return nil
	}
	lines := []string{styles.label.Render("Local .env changes")}
	shown := 0
	for _, local := range m.syncInventory.LocalEnvs {
		if scope != "" && local.Project != scope {
			continue
		}
		shown++
		lines = append(lines, m.renderSyncDiffTarget(local.Project, local.Profile))
		for _, change := range local.Diff.Changes {
			lines = append(lines, "  "+renderCaptureChange(change))
		}
		if local.Diff.CommentChanges > 0 {
			lines = append(lines, styles.muted.Render(fmt.Sprintf("  • %d comment change(s)", local.Diff.CommentChanges)))
		}
		if local.Diff.UnknownChanges > 0 {
			lines = append(lines, styles.warning.Render(fmt.Sprintf("  ! %d unrecognized line change(s)", local.Diff.UnknownChanges)))
		}
	}
	if shown == 0 {
		return nil
	}
	return lines
}

func (m model) revealedSyncDiffLines() []string {
	lines := make([]string, 0)
	appendSection := func(title string, entries []string) {
		if len(entries) == 0 {
			return
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, styles.label.Render(title))
		lines = append(lines, entries...)
	}
	localEntries := make([]string, 0)
	for _, local := range m.syncLineDiff.LocalEnvs {
		localEntries = append(localEntries, m.renderSyncDiffTarget(local.Project, local.Profile))
		localEntries = append(localEntries, renderLiteralLineChanges(local.Lines)...)
	}
	appendSection("Local .env values", localEntries)
	appendSection("Committed vault values", renderProfileLineDeltas(m.syncLineDiff.Committed))
	appendSection("Uncommitted vault values", renderProfileLineDeltas(m.syncLineDiff.Uncommitted))
	return lines
}

func (m model) renderSyncDiffTarget(project, profile string) string {
	prefix := "  "
	if m.isSelectedSyncDiff(project, profile) {
		prefix = "› "
	}
	return styles.value.Render(prefix + project + " / " + profile)
}

func renderProfileLineDeltas(deltas []vault.ProfileLineDelta) []string {
	lines := make([]string, 0)
	for _, delta := range deltas {
		lines = append(lines, styles.value.Render(delta.Project+" / "+delta.Profile))
		lines = append(lines, renderLiteralLineChanges(delta.Lines)...)
	}
	return lines
}

func renderLiteralLineChanges(changes []envdiff.LineChange) []string {
	lines := make([]string, 0)
	for _, change := range changes {
		text := strconv.QuoteToGraphic(change.Text)
		switch change.Kind {
		case envdiff.LineRemoved:
			lines = append(lines, styles.danger.Render(fmt.Sprintf("- %4d │ %s", change.OldLine, text)))
		case envdiff.LineAdded:
			lines = append(lines, styles.success.Render(fmt.Sprintf("+ %4d │ %s", change.NewLine, text)))
		}
	}
	return lines
}

func (m model) syncDiffPageSize() int {
	return max(4, m.height-syncDiffScreenChromeHeight)
}

func (m model) syncDiffMaxOffset() int {
	return max(0, len(m.syncDiffLines())-m.syncDiffPageSize())
}

func clampSyncDiffOffset(offset, lineCount, pageSize int) int {
	return min(max(0, offset), max(0, lineCount-pageSize))
}

func syncDiffPosition(offset, end, total int) string {
	if total == 0 {
		return "No lines"
	}
	return fmt.Sprintf("Lines %d–%d of %d", offset+1, end, total)
}
