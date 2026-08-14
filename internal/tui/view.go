package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	gitops "github.com/eaedave/gitenv/internal/git"
)

func (m model) View() tea.View {
	content := m.renderView()
	view := tea.NewView(content)
	view.AltScreen = true
	view.WindowTitle = "gitenv"
	view.ReportFocus = true
	view.MouseMode = tea.MouseModeAllMotion
	view.OnMouse = mouseHandler(m.mouseRegions(content), m.hoveredMouseTarget)
	return view
}

func (m model) renderView() string {
	width := availableWidth(m.width)
	body := m.renderScreen(width)
	sections := []string{m.renderHeader(width), body}
	for _, banner := range m.renderBanners() {
		sections = append(sections, banner)
	}
	if notice := m.renderNotice(); notice != "" {
		sections = append(sections, notice)
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(sections, "\n\n"))
}

// renderBanners are the standing notices that must not depend on the user
// happening to visit the right screen: an available update, a device waiting for
// approval, and a vault whose recovery key was never backed up.
func (m model) renderBanners() []string {
	banners := make([]string, 0, 3)
	if m.updateAvailable && !m.updating {
		banners = append(banners, styles.warning.Render("↑ gitenv "+m.updateLatest+" available — press U to update"))
	}
	if m.accessRequired || m.cfg.VaultPath == "" {
		// While the vault is locked or absent these actions are unreachable, so
		// advertising them would only be noise.
		return banners
	}
	if count := len(m.pendingApprovals); count > 0 {
		label := "a computer is waiting for approval"
		if count > 1 {
			label = fmt.Sprintf("%d computers are waiting for approval", count)
		}
		banners = append(banners, styles.warning.Render("● "+label+" — press d to review"))
	}
	if !m.recoveryExported {
		// Worded as "not confirmed" rather than "none saved": a vault created
		// before this was tracked may well have a backup we cannot see.
		banners = append(banners, styles.warning.Render("● recovery key backup not confirmed on this computer — press b to save one"))
	}
	return banners
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
		label := "working…"
		if m.updating {
			label = "updating to " + m.updateLatest + "…"
		}
		state = styles.warning.Render(activity + " " + label)
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
	case screenRecoveryPrompt:
		return m.renderRecoveryPrompt(width)
	case screenDevices:
		return m.renderDevices(width)
	case screenConfirmApprove:
		return m.renderConfirmApprove(width)
	case screenConfirmReject:
		return m.renderConfirmReject(width)
	case screenDiverged:
		return m.renderDiverged(width)
	case screenDivergedProfiles:
		return m.renderDivergedProfiles(width)
	case screenConfirmDiverged:
		return m.renderConfirmDiverged(width)
	case screenSyncActions:
		return m.renderSyncActions(width)
	case screenHelp:
		return m.renderHelpScreen(width)
	case screenProjects:
		return m.renderProjects(width)
	case screenProfiles:
		return m.renderProfiles(width)
	case screenAdoptClone:
		return m.renderAdoptClone(width)
	case screenAdoptProfile:
		return m.renderAdoptProfile(width)
	case screenAdoptLink:
		return m.renderAdoptLink(width)
	case screenAdoptCandidates:
		return m.renderAdoptCandidates(width)
	case screenProjectOptions:
		return m.renderProjectOptions(width)
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
	rows := make([]string, 0, len(m.fields)+2)
	for index, field := range m.fields {
		rows = append(rows, renderField(field, index == m.fieldCursor, panelWidth))
	}
	if hint := m.formHint(); hint != "" {
		rows = append(rows, "", styles.muted.Render(hint))
	}
	rows = append(rows, "", m.renderMouseButtons(m.formMouseButtons()))
	panel := renderPanel(title, strings.Join(rows, "\n"), panelWidth, true)
	help := renderHelp("tab", "next field", "enter", "confirm", "ctrl+u", "clear", "esc", "cancel")
	return lipgloss.JoinVertical(lipgloss.Left, panel, "", help)
}

// formHint states a form's rules up front. Every constraint used to be enforced
// only after submit: the twelve-character master password rule in particular was
// never mentioned until it had already rejected the user's input.
func (m model) formHint() string {
	switch m.screen {
	case screenCreate, screenMigrate:
		return "Master password: at least 12 characters. " + m.passwordMatchHint()
	case screenUnlockPassword:
		return "The master password you chose when the vault was created."
	case screenImportRecovery:
		return "Paste the whole recovery key, including the AGE-SECRET-KEY-1 prefix."
	case screenRecovery, screenRecoveryPrompt:
		return "Store this file somewhere other than this computer — a password manager or an offline copy."
	case screenProjectOptions:
		return "Env file: a path inside the project, like .env or apps/web/.env. Line endings: preserve, native, lf or crlf."
	case screenAdoptClone:
		return "The repository will be cloned here, then linked and its env file written."
	case screenAdoptLink:
		return "An existing clone of this project on this computer."
	case screenRemoteChange:
		return "An empty private Git repository. Only encrypted files are ever pushed to it."
	case screenEnrollRequest:
		return "A name you will recognise on the computer that approves this one."
	default:
		return ""
	}
}

// passwordMatchHint gives live feedback on the confirmation field instead of
// making the user submit twice to learn the two entries differ.
func (m model) passwordMatchHint() string {
	first, second, found := "", "", 0
	for _, field := range m.fields {
		if !field.masked {
			continue
		}
		switch found {
		case 0:
			first = field.value
		case 1:
			second = field.value
		}
		found++
	}
	if found < 2 || second == "" {
		return ""
	}
	if first == second {
		return "Both entries match."
	}
	return "The two entries do not match yet."
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
	listWidth := projectListPanelWidth(width)
	projectList := m.projectListView(listWidth-4, m.projectListHeight())
	count := len(m.projectStates)
	if m.current.HasEnv && m.current.LinkedName == "" {
		count++
	}
	listTitle := fmt.Sprintf("Projects · %d", count)
	details := m.renderProjectContext(width - listWidth - 2)
	var workspace string
	if width >= compactViewWidth {
		listPanel := renderPanel(listTitle, projectList, listWidth, true)
		detailPanel := renderPanel("Details", details, width-listWidth-2, false)
		workspace = lipgloss.JoinHorizontal(lipgloss.Top, listPanel, "  ", detailPanel)
	} else {
		// On narrow terminals the selected row is the overview; Enter opens the
		// project's profile details. Stacking another panel made the primary list
		// disappear below the fold.
		workspace = renderPanel(listTitle, projectList, width, true)
	}
	help := m.renderProjectsHelp(width)
	if m.height > 0 && m.height < 28 {
		return lipgloss.JoinVertical(lipgloss.Left, workspace, "", m.renderProjectSyncSummary(), "", help)
	}
	syncPanel := renderPanel("Sync", m.renderProjectSyncStatus(), width, false)
	return lipgloss.JoinVertical(lipgloss.Left, workspace, "", syncPanel, "", help)
}

func (m model) renderProjectsHelp(width int) string {
	if width < compactViewWidth {
		return lipgloss.JoinVertical(lipgloss.Left,
			renderHelp("↑↓", "move", "enter", "open", "/", "find"),
			renderHelp("a", "add", "s", "sync", "?", "help", "q", "quit"),
		)
	}
	if width < 100 {
		return renderHelp("↑↓", "select", "enter", "open", "/", "find", "a", "add current", "s", "sync", "?", "help", "q", "quit")
	}
	return renderHelp("↑↓", "select", "enter", "open", "/", "find", "a", "add current", "c", "capture", "s", "sync", "v", "changes", "f", "find clones", "o", "options", "?", "help", "q", "quit")
}

func (m model) renderProjectContext(width int) string {
	valueWidth := max(18, width-18)
	if item, ok := m.selectedProjectListItem(); ok {
		if item.current {
			context := styles.warning.Render("● Current folder is not in gitenv yet") + "\n\n" +
				labelValue("Project", item.state.Name) + "\n" +
				labelValue("Path", compactPath(item.state.Path, valueWidth)) + "\n" +
				styles.success.Render("● .env found") + "\n\n" +
				styles.key.Render("enter/a") + styles.muted.Render("  add and capture this project")
			return context + "\n\n" + m.renderMouseButtons(m.projectMouseButtons())
		}
		state := item.state
		lines := []string{
			labelValue("Project", state.Name),
			styles.label.Render("Status  ") + renderStatus(state.Status),
			labelValue("Profile", valueOrNone(state.ActiveProfile)),
		}
		if state.Path != "" {
			lines = append(lines, labelValue("Path", compactPath(state.Path, valueWidth)))
		} else if state.Identity != "" {
			lines = append(lines, labelValue("Repository", ansi.Truncate(state.Identity, valueWidth, "…")))
		}
		if state.Name == m.current.LinkedName {
			lines = append(lines, "", styles.success.Render("● current folder"))
		}
		lines = append(lines, "", m.renderMouseButtons(m.projectMouseButtons()))
		return strings.Join(lines, "\n")
	}

	badges := styles.muted.Render("○ no project selected")
	if m.current.HasEnv {
		badges = styles.success.Render("● .env found")
	}
	return labelValue("Current", compactPath(m.current.Path, valueWidth)) + "\n" +
		labelValue("Vault", compactPath(m.cfg.VaultPath, valueWidth)) + "\n\n" + badges
}

func compactPath(path string, width int) string {
	if path == "" {
		return "(none)"
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if path == home {
			path = "~"
		} else if strings.HasPrefix(path, home+string(os.PathSeparator)) {
			path = "~" + strings.TrimPrefix(path, home)
		}
	}
	width = max(12, width)
	if lipgloss.Width(path) <= width {
		return path
	}
	base := filepath.Base(path)
	if lipgloss.Width(base)+3 >= width {
		return "…/" + ansi.Truncate(base, width-2, "…")
	}
	parentWidth := width - lipgloss.Width(base) - 2
	parent := ansi.Truncate(filepath.Dir(path), parentWidth, "")
	return parent + "…/" + base
}

func (m model) renderProjectSyncSummary() string {
	status, _ := syncStatusText(m.syncStatus)
	return styles.label.Render("Sync  ") + status
}

func (m model) renderProjectSyncStatus() string {
	status, recommendation := syncStatusText(m.syncStatus)
	remote := ansi.Truncate(valueOrNone(m.remoteDisplayURL), max(18, availableWidth(m.width)/2), "…")
	rows := []string{status + styles.muted.Render("  ·  ") + styles.value.Render(remote)}
	if !m.syncStatus.CheckedAt.IsZero() {
		rows[0] += styles.muted.Render("  ·  checked " + m.syncStatus.CheckedAt.Local().Format("15:04"))
	}
	if m.syncStatus.Dirty {
		rows = append(rows, styles.warning.Render("● unpublished vault changes"))
	}
	if inventory := m.renderSyncInventory(); inventory != "" {
		rows = append(rows, "", inventory)
	}
	if recommendation != "" {
		rows = append(rows, "", styles.label.Render("Next  ")+styles.value.Render(recommendation))
	}
	rows = append(rows, "", m.renderMouseButtons(syncMouseButtons()))
	return strings.Join(rows, "\n")
}

func (m model) renderProfiles(width int) string {
	local := m.cfg.Projects[m.selectedProject]
	details := labelValue("Path", compactPath(local.Path, max(18, width/2))) + "\n" +
		labelValue("Active", valueOrNone(local.ActiveProfile)) + "\n" +
		styles.label.Render("Status  ") + renderStatus(m.statuses[m.selectedProject]) + "\n\n" +
		m.renderProfileMouseButtons(width)
	profiles := m.renderProfileList(local.ActiveProfile)
	if width >= compactViewWidth {
		listWidth := max(28, width/3)
		profiles = lipgloss.JoinHorizontal(lipgloss.Top, renderPanel("Profiles", profiles, listWidth, true), "  ", renderPanel(m.selectedProject, details, width-listWidth-2, false))
	} else {
		profiles = lipgloss.JoinVertical(lipgloss.Left, renderPanel(m.selectedProject, details, width, false), "", renderPanel("Profiles", profiles, width, true))
	}
	profileSync := m.renderSyncStatus() + "\n\n" + m.renderMouseButtons(syncMouseButtons())
	syncPanel := renderPanel("Sync", profileSync, width, false)
	help := renderHelp("enter", "apply", "e", "edit", "c", "capture", "n", "new", "d", "remove", "s", "sync", "o", "options", "?", "help", "esc", "back")
	if m.isFocusedProject() {
		help = renderHelp("enter", "apply", "e", "edit", "c", "capture", "n", "new", "d", "remove", "s", "sync", "o", "options", "?", "help", "p", "all projects", "q", "quit")
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
		hovered := m.hoveredMouseTarget.kind == mouseTargetProfileRow && m.hoveredMouseTarget.index == index
		if index == m.profileCursor {
			selectedStyle := styles.selected
			if hovered {
				selectedStyle = selectedStyle.Underline(true)
			}
			label = selectedStyle.Render("› " + profile)
		} else if hovered {
			label = styles.hovered.Render("• " + profile)
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
	body := styles.danger.Render("!") + "  " + styles.value.Render(message) + "\n\n" +
		m.renderMouseButtons(m.confirmationMouseButtons())
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

// renderStatus turns vault.Status's internal vocabulary into something a person
// can read. The raw words leaked to the screen before: "unmanaged" and
// "unlinked" told the user nothing about what to do next, and anything the
// switch did not recognise was printed verbatim.
func renderStatus(status string) string {
	switch status {
	case "clean", "synced":
		return styles.success.Render("● up to date")
	case "modified", "dirty":
		return styles.warning.Render("● uncaptured changes")
	case "missing":
		return styles.warning.Render("● env file missing")
	case "unmanaged":
		return styles.muted.Render("○ not captured yet")
	case "unlinked":
		return styles.muted.Render("○ no local copy")
	case "error":
		return styles.danger.Render("● error")
	case "":
		return styles.muted.Render("○ checking")
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

// renderSyncActions lists what a user can still do with a vault that already
// matches its remote, replacing the old dead-end "already synchronized" message.
func (m model) renderSyncActions(width int) string {
	body := styles.muted.Render("The vault and its remote already match.\nYou can still run either direction explicitly.") + "\n\n" +
		renderMenu([]string{
			"Download remote vault changes",
			"Publish local vault changes",
			"Back",
		}, m.menuCursor)
	panel := renderPanel("Sync", body, min(width, 76), true)
	return lipgloss.JoinVertical(lipgloss.Left, panel, "", renderHelp("↑↓", "select", "enter", "confirm", "esc", "back"))
}

// renderRecoveryPrompt is the post-creation backup step. A new vault's key
// exists in exactly one place, so this asks for a copy while it still matters
// rather than leaving it to an undocumented key the user never presses.
func (m model) renderRecoveryPrompt(width int) string {
	panelWidth := min(width, 76)
	intro := styles.warning.Render("Save your recovery key now.") + "\n" +
		styles.muted.Render("It is the only way back into this vault if you forget the master\npassword and have no other enrolled computer. Nobody can restore it\nfor you.")
	rows := []string{intro, ""}
	for index, field := range m.fields {
		rows = append(rows, renderField(field, index == m.fieldCursor, panelWidth))
	}
	if hint := m.formHint(); hint != "" {
		rows = append(rows, "", styles.muted.Render(hint))
	}
	rows = append(rows, "", m.renderMouseButtons(m.formMouseButtons()))
	panel := renderPanel("Vault created", strings.Join(rows, "\n"), panelWidth, true)
	help := renderHelp("enter", "save", "ctrl+u", "clear", "esc", "skip for now")
	return lipgloss.JoinVertical(lipgloss.Left, panel, "", help)
}
