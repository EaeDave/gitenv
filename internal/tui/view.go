package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	gitops "github.com/eaedave/gitenv/internal/git"
)

func (m model) View() string {
	width := availableWidth(m.width)
	body := m.renderScreen(width)
	sections := []string{m.renderHeader(width), body}
	if notice := m.renderNotice(); notice != "" {
		sections = append(sections, notice)
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(sections, "\n\n"))
}

func (m model) renderHeader(width int) string {
	brand := styles.brand.Render("gitenv")
	tagline := styles.subtitle.Render("encrypted environment profiles")
	left := lipgloss.JoinVertical(lipgloss.Left, brand, tagline)
	state := styles.success.Render("● ready")
	if m.busy {
		activity := m.spinner.View()
		if activity == "" {
			activity = "●"
		}
		state = styles.warning.Render(activity + " working…")
	}
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(state)-4)
	return left + strings.Repeat(" ", gap) + state
}

func (m model) renderScreen(width int) string {
	switch m.screen {
	case screenOnboarding:
		return m.renderOnboarding(width)
	case screenCreate:
		return m.renderForm("Create a new protected vault", width)
	case screenClone:
		return m.renderForm("Clone an existing vault", width)
	case screenAddProject:
		return m.renderForm("Add current project", width)
	case screenNewProfile:
		return m.renderForm("Capture .env as a new profile", width)
	case screenRemote:
		return m.renderRemoteMenu(width)
	case screenRemoteChange:
		return m.renderForm("Vault sync repository", width)
	case screenMigrate:
		return m.renderForm("Migrate vault to protected access", width)
	case screenUnlock:
		return m.renderUnlockMenu(width)
	case screenUnlockPassword:
		return m.renderForm("Unlock vault", width)
	case screenEnrollRequest:
		return m.renderForm("Request device approval", width)
	case screenImportRecovery:
		return m.renderForm("Paste recovery identity", width)
	case screenRecovery:
		return m.renderForm("Export recovery identity", width)
	case screenProjects:
		return m.renderProjects(width)
	case screenProfiles:
		return m.renderProfiles(width)
	case screenConfirm:
		return m.renderConfirmation("Discard local changes?", fmt.Sprintf("Local .env has uncaptured content.\nApply %q and discard it? [y/N]", m.pendingProfile), width)
	case screenConfirmDelete:
		return m.renderConfirmation("Remove encrypted profile?", fmt.Sprintf("Remove encrypted profile %q? This cannot be undone. [y/N]", m.pendingProfile), width)
	case screenConfirmRemoveRemote:
		return m.renderConfirmation("Remove sync repository?", "Remove vault sync repository? [y/N]", width)
	case screenConfirmDisconnect:
		return m.renderConfirmation("Disconnect vault?", "Disconnect this vault from this computer?\nEncrypted vault files and its remote will not be deleted. [y/N]", width)
	case screenConfirmSync:
		return m.renderSyncConfirmation(width)
	case screenConfirmCapture:
		return m.renderCapturePreview(width)
	case screenSyncDiff:
		return m.renderSyncDiff(width)
	case screenConfirmDiffPublish:
		return m.renderConfirmation("Publish selected environment?", fmt.Sprintf("Capture %s/%s and publish its encrypted vault change? [y/N]", m.pendingDiffProject, m.pendingDiffProfile), width)
	case screenConfirmDiffDiscard:
		return m.renderConfirmation("Discard selected environment change?", fmt.Sprintf("Restore %s/%s from its active encrypted profile? [y/N]", m.pendingDiffProject, m.pendingDiffProfile), width)
	case screenEditor:
		return m.renderEditor(width)
	case screenConfirmEditorDiscard:
		return m.renderConfirmation("Discard unsaved changes?", "The .env editor has unsaved changes. Discard them? [y/N]", width)
	default:
		return ""
	}
}

func (m model) renderOnboarding(width int) string {
	body := styles.value.Render("No vault configured. Choose how to begin:") + "\n\n" + renderMenu([]string{
		"Create a new vault",
		"Clone an existing vault",
	}, m.menuCursor)
	panel := renderPanel("Welcome", body, min(width, 64), true)
	help := renderHelp("↑↓", "select", "enter", "continue", "q", "quit")
	return lipgloss.JoinVertical(lipgloss.Left, panel, "", help)
}

func (m model) renderForm(title string, width int) string {
	panelWidth := min(width, 76)
	rows := make([]string, 0, len(m.fields))
	for index, field := range m.fields {
		rows = append(rows, renderField(field, index == m.fieldCursor, panelWidth))
	}
	panel := renderPanel(title, strings.Join(rows, "\n"), panelWidth, true)
	help := renderHelp("tab", "next field", "enter", "confirm", "ctrl+u", "clear", "esc", "cancel")
	return lipgloss.JoinVertical(lipgloss.Left, panel, "", help)
}

func renderField(field field, active bool, width int) string {
	value := field.value
	if field.masked {
		value = strings.Repeat("*", utf8.RuneCountInString(field.value))
	}
	if value == "" {
		value = " "
	}
	marker := "  "
	if active {
		marker = styles.selected.Render("›") + " "
	}
	if width < 58 {
		input := renderInput(value, active, width-8)
		return marker + styles.label.Render(field.label) + "\n  " + input
	}
	labelWidth := min(34, width/2)
	input := renderInput(value, active, width-labelWidth-7)
	return marker + styles.label.Width(labelWidth).Render(field.label) + " " + input
}

func renderInput(value string, active bool, width int) string {
	return lipgloss.NewStyle().
		Foreground(colors.text).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colors.muted).
		Width(max(12, width)).
		Render(value + inputCursor(active))
}

func (m model) renderRemoteMenu(width int) string {
	remote := m.remoteDisplayURL
	if remote == "" {
		remote = "(none)"
	}
	body := styles.label.Render("Repository") + "  " + styles.value.Render(remote) + "\n\n" + renderMenu([]string{"Change", "Test", "Remove", "Back"}, m.menuCursor)
	panel := renderPanel("Vault sync", body, min(width, 76), true)
	return lipgloss.JoinVertical(lipgloss.Left, panel, "", renderHelp("↑↓", "select", "enter", "confirm", "esc", "back"))
}

func (m model) renderUnlockMenu(width int) string {
	passwordOption := "Unlock with master password"
	if m.migrationRecoveryRequired {
		passwordOption = "Master password unavailable for this legacy vault"
	}
	approvalOption := "Request approval from another device"
	if id := m.cfg.PendingEnrollmentID; id != "" {
		if len(id) > 8 {
			id = id[:8] + "…"
		}
		approvalOption = "Check pending device approval (" + id + ")"
	}
	items := []string{passwordOption, approvalOption, "Paste recovery key (advanced)", "Disconnect this vault and start again"}
	body := styles.warning.Render("Vault access is required.") + "\n\n" + renderMenu(items, m.menuCursor)
	panel := renderPanel("Protected vault", body, min(width, 76), true)
	return lipgloss.JoinVertical(lipgloss.Left, panel, "", renderHelp("↑↓", "select", "enter", "confirm", "esc", "back"))
}

func (m model) renderProjects(width int) string {
	workspace := m.renderProjectContext()
	projectList := m.renderProjectList()
	syncPanel := renderPanel("Sync", m.renderSyncStatus(), width, false)
	if width >= compactViewWidth {
		listWidth := max(28, width/3)
		list := renderPanel("Projects", projectList, listWidth, true)
		details := renderPanel("Workspace", workspace, width-listWidth-2, false)
		workspace = lipgloss.JoinHorizontal(lipgloss.Top, list, "  ", details)
	} else {
		workspace = lipgloss.JoinVertical(lipgloss.Left, renderPanel("Workspace", workspace, width, false), "", renderPanel("Projects", projectList, width, true))
	}
	help := renderHelp("enter", "profiles", "v", "view changes", "s", "sync", "c", "capture", "a", "add", "g", "remote", "r", "reload", "q", "quit")
	return lipgloss.JoinVertical(lipgloss.Left, workspace, "", syncPanel, "", help)
}

func (m model) renderProjectContext() string {
	badges := make([]string, 0, 2)
	if m.current.HasEnv {
		badges = append(badges, styles.success.Render("● .env found"))
	}
	if m.current.LinkedName != "" {
		badges = append(badges, styles.success.Render("● linked: "+m.current.LinkedName))
	}
	if len(badges) == 0 {
		badges = append(badges, styles.muted.Render("○ no local .env link"))
	}
	return labelValue("Vault", m.cfg.VaultPath) + "\n" + labelValue("Current", m.current.Path) + "\n\n" + strings.Join(badges, "  ")
}

func (m model) renderProjectList() string {
	if len(m.projects) == 0 {
		return styles.muted.Render("No projects linked on this computer.\nPress a in a directory with .env.")
	}
	rows := make([]string, 0, len(m.projects))
	for index, name := range m.projects {
		active := m.cfg.Projects[name].ActiveProfile
		if active == "" {
			active = "(none)"
		}
		row := fmt.Sprintf("  %-18s %-12s %s", name, active, renderStatus(m.statuses[name]))
		if index == m.projectCursor {
			row = styles.selected.Render("› "+name) + "  " + styles.muted.Render(active) + "  " + renderStatus(m.statuses[name])
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

func (m model) renderProfiles(width int) string {
	local := m.cfg.Projects[m.selectedProject]
	details := labelValue("Path", local.Path) + "\n" + labelValue("Active", valueOrNone(local.ActiveProfile)) + "\n" + styles.label.Render("Status  ") + renderStatus(m.statuses[m.selectedProject])
	profiles := m.renderProfileList(local.ActiveProfile)
	if width >= compactViewWidth {
		listWidth := max(28, width/3)
		profiles = lipgloss.JoinHorizontal(lipgloss.Top, renderPanel("Profiles", profiles, listWidth, true), "  ", renderPanel(m.selectedProject, details, width-listWidth-2, false))
	} else {
		profiles = lipgloss.JoinVertical(lipgloss.Left, renderPanel(m.selectedProject, details, width, false), "", renderPanel("Profiles", profiles, width, true))
	}
	syncPanel := renderPanel("Sync", m.renderSyncStatus(), width, false)
	help := renderHelp("enter", "apply", "e", "edit", "v", "changes", "s", "sync", "c", "capture", "n", "new", "d", "remove", "r", "reload", "p", "projects", "esc", "back")
	if m.isFocusedProject() {
		help = renderHelp("enter", "apply", "e", "edit", "v", "changes", "s", "sync", "c", "capture", "n", "new", "d", "remove", "r", "reload", "p", "browse projects", "q", "quit")
	}
	return lipgloss.JoinVertical(lipgloss.Left, profiles, "", syncPanel, "", help)
}

func (m model) renderProfileList(activeProfile string) string {
	if len(m.profiles) == 0 {
		return styles.muted.Render("No profiles captured.")
	}
	statuses := m.profileStatuses[m.selectedProject]
	rows := make([]string, 0, len(m.profiles))
	for index, profile := range m.profiles {
		label := "  " + profile
		if index == m.profileCursor {
			label = styles.selected.Render("› " + profile)
		}
		rows = append(rows, label+profileBadge(profile == activeProfile, statuses[profile]))
	}
	return strings.Join(rows, "\n")
}

func profileBadge(active bool, status string) string {
	switch {
	case active && status == "modified":
		return "  " + styles.success.Render("● active") + styles.muted.Render(" · ") + styles.warning.Render("modified")
	case active && status == "missing":
		return "  " + styles.success.Render("● active") + styles.muted.Render(" · ") + styles.warning.Render("no .env")
	case active:
		return "  " + styles.success.Render("● active")
	case status == "current":
		return "  " + styles.muted.Render("○ matches .env")
	}
	return ""
}

func (m model) renderSyncStatus() string {
	statusText, recommendation := syncStatusText(m.syncStatus)
	rows := []string{
		labelValue("Remote", valueOrNone(m.remoteDisplayURL)),
		styles.label.Render("Status  ") + statusText,
	}
	if !m.syncStatus.CheckedAt.IsZero() {
		rows = append(rows, labelValue("Checked", m.syncStatus.CheckedAt.Local().Format("15:04:05")))
	}
	if m.syncStatus.Dirty {
		rows = append(rows, styles.warning.Render("● unpublished vault changes"))
	}
	if inventory := m.renderSyncInventory(); inventory != "" {
		rows = append(rows, "", inventory)
	}
	if recommendation != "" {
		rows = append(rows, "", styles.label.Render("Recommended  ")+styles.value.Render(recommendation))
	}
	return strings.Join(rows, "\n")
}

func syncStatusText(status gitops.SyncStatus) (string, string) {
	switch status.State {
	case gitops.SyncChecking:
		return styles.warning.Render("◌ checking…"), "Wait for remote check"
	case gitops.SyncSynced:
		return styles.success.Render("● synchronized"), ""
	case gitops.SyncLocalAhead:
		return styles.warning.Render(fmt.Sprintf("↑ %d local update(s)", max(1, status.Ahead))), "Press s to publish"
	case gitops.SyncRemoteAhead:
		return styles.warning.Render(fmt.Sprintf("↓ %d remote update(s)", status.Behind)), "Press s to download"
	case gitops.SyncDiverged:
		return styles.danger.Render(fmt.Sprintf("↕ diverged (%d local, %d remote)", status.Ahead, status.Behind)), "Resolve divergence before syncing"
	case gitops.SyncNoRemote:
		return styles.muted.Render("○ not configured"), "Press g to configure sync"
	case gitops.SyncOffline:
		return styles.warning.Render("○ offline"), "Check connection and press r"
	case gitops.SyncAuthError:
		return styles.danger.Render("● authentication failed"), "Verify Git credentials and press r"
	default:
		return styles.danger.Render("● check failed"), "Press r to retry"
	}
}

func (m model) renderSyncConfirmation(width int) string {
	action := "Publish local vault changes?"
	detail := "Encrypted vault changes will be published.\nLocal .env files will not be modified. [y/N]"
	if m.pendingSync == gitops.SyncRemoteAhead {
		action = "Download remote vault updates?"
		detail = "Encrypted vault updates will be downloaded.\nLocal .env files will not be modified. [y/N]"
	}
	return m.renderConfirmation(action, detail, width)
}

func (m model) renderConfirmation(title, message string, width int) string {
	body := styles.danger.Render("!") + "  " + styles.value.Render(message)
	panel := renderPanel(title, body, min(width, 68), true)
	return lipgloss.JoinVertical(lipgloss.Left, panel, "", renderHelp("y", "confirm", "n/esc", "cancel"))
}

func (m model) renderNotice() string {
	if m.errText != "" {
		return styles.danger.Render("✗ " + m.errText)
	}
	if m.info != "" {
		return styles.success.Render("✓ " + m.info)
	}
	return ""
}

func renderMenu(items []string, cursor int) string {
	rows := make([]string, 0, len(items))
	for index, item := range items {
		row := "  " + item
		if index == cursor {
			row = styles.selected.Render("› " + item)
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

func renderStatus(status string) string {
	switch status {
	case "clean", "synced":
		return styles.success.Render("● " + status)
	case "modified", "dirty":
		return styles.warning.Render("● " + status)
	case "error":
		return styles.danger.Render("● error")
	case "":
		return styles.muted.Render("○ unknown")
	default:
		return styles.muted.Render("● " + status)
	}
}

func labelValue(label, value string) string {
	return styles.label.Render(label+"  ") + styles.value.Render(valueOrNone(value))
}

func valueOrNone(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
}

func inputCursor(active bool) string {
	if active {
		return "█"
	}
	return ""
}
