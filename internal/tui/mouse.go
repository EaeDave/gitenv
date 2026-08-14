package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/eaedave/gitenv/internal/app"
)

type mouseTargetKind int

const (
	mouseTargetNone mouseTargetKind = iota
	mouseTargetProjectList
	mouseTargetProjectRow
	mouseTargetProfileRow
	mouseTargetButton
	mouseTargetField
	mouseTargetEditorViewport
	mouseTargetEditorRow
)

type mouseAction int

const (
	mouseActionNone mouseAction = iota
	mouseActionOpenProject
	mouseActionCaptureProject
	mouseActionProjectOptions
	mouseActionApplyProfile
	mouseActionEditProfile
	mouseActionCaptureProfile
	mouseActionProfileOptions
	mouseActionSync
	mouseActionReviewChanges
	mouseActionEditCapture
	mouseActionFilterAll
	mouseActionFilterModified
	mouseActionFilterMissing
	mouseActionSaveEditor
	mouseActionCancelEditor
	mouseActionConfirm
	mouseActionCancel
	mouseActionContinue
)

type mouseTarget struct {
	kind        mouseTargetKind
	action      mouseAction
	index       int
	contentLeft int
}

type mouseBounds struct {
	x, y, width, height int
}

func (bounds mouseBounds) contains(x, y int) bool {
	return x >= bounds.x && x < bounds.x+bounds.width && y >= bounds.y && y < bounds.y+bounds.height
}

type mouseRegion struct {
	target mouseTarget
	bounds mouseBounds
}

type mouseEventKind int

const (
	mouseEventHover mouseEventKind = iota
	mouseEventClick
	mouseEventRelease
	mouseEventWheel
)

type mouseInteractionMsg struct {
	kind   mouseEventKind
	target mouseTarget
	button tea.MouseButton
	x, y   int
	at     time.Time
}

type mouseButton struct {
	label   string
	action  mouseAction
	primary bool
	active  bool
}

func (button mouseButton) text() string {
	return "[ " + button.label + " ]"
}

func (m model) renderMouseButtons(buttons []mouseButton) string {
	parts := make([]string, 0, len(buttons))
	for _, button := range buttons {
		target := mouseTarget{kind: mouseTargetButton, action: button.action}
		style := styles.button
		if button.primary || button.active {
			style = styles.buttonPrimary
		}
		if m.hoveredMouseTarget == target {
			style = styles.buttonHovered
			if button.primary || button.active {
				style = styles.buttonPrimaryHovered
			}
		}
		parts = append(parts, style.Render(button.text()))
	}
	return strings.Join(parts, "  ")
}

func (m model) projectMouseButtons() []mouseButton {
	item, ok := m.selectedProjectListItem()
	if !ok {
		return nil
	}
	if item.current {
		return []mouseButton{{label: "Add & capture", action: mouseActionOpenProject, primary: true}}
	}
	if item.state.Kind == app.ProjectLinked {
		return []mouseButton{
			{label: "Open", action: mouseActionOpenProject, primary: true},
			{label: "Capture", action: mouseActionCaptureProject},
			{label: "Options", action: mouseActionProjectOptions},
		}
	}
	return []mouseButton{
		{label: "Adopt", action: mouseActionOpenProject, primary: true},
		{label: "Options", action: mouseActionProjectOptions},
	}
}

func profileMouseButtons() []mouseButton {
	return []mouseButton{
		{label: "Apply", action: mouseActionApplyProfile, primary: true},
		{label: "Edit", action: mouseActionEditProfile},
		{label: "Capture active", action: mouseActionCaptureProfile},
		{label: "Options", action: mouseActionProfileOptions},
	}
}

func (m model) renderProfileMouseButtons(width int) string {
	buttons := profileMouseButtons()
	if width < 100 {
		return m.renderMouseButtons(buttons[:2]) + "\n" + m.renderMouseButtons(buttons[2:])
	}
	return m.renderMouseButtons(buttons)
}

func syncMouseButtons() []mouseButton {
	return []mouseButton{{label: "Sync", action: mouseActionSync, primary: true}, {label: "Changes", action: mouseActionReviewChanges}}
}

func (m model) projectFilterMouseButtons() []mouseButton {
	return []mouseButton{
		{label: "All", action: mouseActionFilterAll, active: m.projectFilter == projectFilterAll},
		{label: "Modified", action: mouseActionFilterModified, active: m.projectFilter == projectFilterModified},
		{label: "Missing", action: mouseActionFilterMissing, active: m.projectFilter == projectFilterMissing},
	}
}

func editorMouseButtons() []mouseButton {
	return []mouseButton{
		{label: "Save", action: mouseActionSaveEditor, primary: true},
		{label: "Cancel", action: mouseActionCancelEditor},
	}
}

func (m model) confirmationMouseButtons() []mouseButton {
	if m.screen == screenConfirmCapture {
		return []mouseButton{
			{label: "Capture", action: mouseActionConfirm, primary: true},
			{label: "Edit .env", action: mouseActionEditCapture},
			{label: "Cancel", action: mouseActionCancel},
		}
	}
	return []mouseButton{{label: "Confirm", action: mouseActionConfirm, primary: true}, {label: "Cancel", action: mouseActionCancel}}
}

func (m model) formMouseButtons() []mouseButton {
	primary := "Continue"
	switch m.screen {
	case screenUnlockPassword:
		primary = "Unlock"
	case screenRecovery, screenRecoveryPrompt, screenProjectOptions:
		primary = "Save"
	}
	return []mouseButton{{label: primary, action: mouseActionContinue, primary: true}, {label: "Cancel", action: mouseActionCancel}}
}

func (m model) mouseRegions(content string) []mouseRegion {
	regions := make([]mouseRegion, 0, 16)
	if m.screen == screenProjects {
		regions = append(regions, m.projectListMouseRegions(content)...)
		regions = append(regions, findMouseButtonRegions(content, m.projectMouseButtons())...)
		regions = append(regions, findMouseButtonRegions(content, syncMouseButtons())...)
		regions = append(regions, findMouseButtonRegions(content, m.projectFilterMouseButtons())...)
	}
	if m.screen == screenProfiles {
		regions = append(regions, m.profileMouseRegions(content)...)
		regions = append(regions, findMouseButtonRegions(content, profileMouseButtons())...)
		regions = append(regions, findMouseButtonRegions(content, syncMouseButtons())...)
	}
	if m.screen == screenEditor {
		regions = append(regions, m.editorMouseRegions(content)...)
		regions = append(regions, findMouseButtonRegions(content, editorMouseButtons())...)
	}
	if isConfirmationScreen(m.screen) {
		regions = append(regions, findMouseButtonRegions(content, m.confirmationMouseButtons())...)
	}
	if isFormScreen(m.screen) {
		regions = append(regions, findMouseButtonRegions(content, m.formMouseButtons())...)
		regions = append(regions, findFieldMouseRegions(content, m.fields)...)
	}
	return regions
}

func (m model) projectListMouseRegions(content string) []mouseRegion {
	if m.projectList == nil {
		return nil
	}
	panelWidth := projectListPanelWidth(availableWidth(m.width))
	const listLeft = 4 // outer padding + panel border + panel padding
	lines := strings.Split(ansi.Strip(content), "\n")
	panelTop := -1
	for y, line := range lines {
		if strings.Contains(ansi.Truncate(line, panelWidth, ""), "Projects") {
			panelTop = y
			break
		}
	}
	if panelTop < 0 {
		return nil
	}

	regions := []mouseRegion{{
		target: mouseTarget{kind: mouseTargetProjectList},
		bounds: mouseBounds{x: listLeft, y: panelTop + 1, width: panelWidth - 4, height: m.projectListHeight()},
	}}
	items := m.projectList.VisibleItems()
	start, end := m.projectList.Paginator.GetSliceBounds(len(items))
	searchFrom := panelTop + 1
	for index := start; index < end; index++ {
		for y := searchFrom; y < len(lines); y++ {
			// Rows are matched in render order inside the list panel. Matching by
			// project name fails precisely when the layout truncates a long name.
			line := ansi.Truncate(lines[y], panelWidth, "")
			isProjectRow := strings.ContainsAny(line, "●○◍+")
			if !isProjectRow {
				continue
			}
			regions = append(regions, mouseRegion{
				target: mouseTarget{kind: mouseTargetProjectRow, index: index},
				bounds: mouseBounds{x: listLeft, y: y, width: panelWidth - 4, height: 1},
			})
			searchFrom = y + 1
			break
		}
	}
	return regions
}

func (m model) profileMouseRegions(content string) []mouseRegion {
	if len(m.profiles) == 0 {
		return nil
	}
	lines := strings.Split(ansi.Strip(content), "\n")
	for y, line := range lines {
		byteIndex := strings.Index(line, "Profiles")
		if byteIndex < 0 {
			continue
		}
		left := ansi.StringWidth(line[:byteIndex])
		width := availableWidth(m.width) - 4
		if availableWidth(m.width) >= compactViewWidth {
			width = max(28, availableWidth(m.width)/3) - 4
		}
		regions := make([]mouseRegion, 0, len(m.profiles))
		for index := range m.profiles {
			regions = append(regions, mouseRegion{
				target: mouseTarget{kind: mouseTargetProfileRow, index: index},
				bounds: mouseBounds{x: left, y: y + 2 + index, width: width, height: 1},
			})
		}
		return regions
	}
	return nil
}

func (m model) editorMouseRegions(content string) []mouseRegion {
	lines := strings.Split(ansi.Strip(content), "\n")
	titleY := -1
	for y, line := range lines {
		if strings.Contains(line, "Local .env") {
			titleY = y
			break
		}
	}
	if titleY < 0 {
		return nil
	}

	digits := len(fmt.Sprintf("%d", max(1, m.editor.LineCount())))
	top := m.editorTopLine
	bottom := min(m.editor.LineCount(), top+m.editorViewportHeight())
	regions := make([]mouseRegion, 0, bottom-top+1)
	viewportTop := -1
	viewportLeft := 0
	viewportRight := 0
	for lineIndex := top; lineIndex < bottom; lineIndex++ {
		prefix := fmt.Sprintf("%*d │ ", digits, lineIndex+1)
		for y := titleY + 1; y < len(lines); y++ {
			byteIndex := strings.Index(lines[y], prefix)
			if byteIndex < 0 {
				continue
			}
			left := ansi.StringWidth(lines[y][:byteIndex])
			if left > 8 {
				// A value may itself contain text that resembles a line-number
				// gutter; only accept the prefix near the panel's left edge.
				continue
			}
			contentLeft := left + ansi.StringWidth(prefix)
			regions = append(regions, mouseRegion{
				target: mouseTarget{kind: mouseTargetEditorRow, index: lineIndex, contentLeft: contentLeft},
				bounds: mouseBounds{x: left, y: y, width: digits + 3 + m.editorContentWidth(), height: 1},
			})
			if viewportTop < 0 {
				viewportTop = y
				viewportLeft = left
			}
			viewportRight = max(viewportRight, left+digits+3+m.editorContentWidth())
			break
		}
	}
	if viewportTop >= 0 {
		regions = append([]mouseRegion{{
			target: mouseTarget{kind: mouseTargetEditorViewport},
			bounds: mouseBounds{x: viewportLeft, y: viewportTop, width: viewportRight - viewportLeft, height: m.editorViewportHeight()},
		}}, regions...)
	}
	return regions
}

func findMouseButtonRegions(content string, buttons []mouseButton) []mouseRegion {
	lines := strings.Split(ansi.Strip(content), "\n")
	regions := make([]mouseRegion, 0, len(buttons))
	for _, button := range buttons {
		text := button.text()
		// Toolbars are rendered after their associated content. Searching from
		// the bottom prevents a project/profile/value named "[ Sync ]" from
		// stealing the real button's hit region.
		for y := len(lines) - 1; y >= 0; y-- {
			byteIndex := strings.LastIndex(lines[y], text)
			if byteIndex < 0 {
				continue
			}
			regions = append(regions, mouseRegion{
				target: mouseTarget{kind: mouseTargetButton, action: button.action},
				bounds: mouseBounds{x: ansi.StringWidth(lines[y][:byteIndex]), y: y, width: ansi.StringWidth(text), height: 1},
			})
			break
		}
	}
	return regions
}

func findFieldMouseRegions(content string, fields []field) []mouseRegion {
	lines := strings.Split(ansi.Strip(content), "\n")
	regions := make([]mouseRegion, 0, len(fields))
	for index, field := range fields {
		for y, line := range lines {
			byteIndex := strings.Index(line, field.label)
			if byteIndex < 0 {
				continue
			}
			regions = append(regions, mouseRegion{
				target: mouseTarget{kind: mouseTargetField, index: index},
				bounds: mouseBounds{x: ansi.StringWidth(line[:byteIndex]), y: y, width: max(1, ansi.StringWidth(line)-ansi.StringWidth(line[:byteIndex])), height: 1},
			})
			break
		}
	}
	return regions
}

func mouseTargetAt(regions []mouseRegion, x, y int) mouseTarget {
	for index := len(regions) - 1; index >= 0; index-- {
		if regions[index].bounds.contains(x, y) {
			return regions[index].target
		}
	}
	return mouseTarget{}
}

func mouseHandler(regions []mouseRegion, hovered mouseTarget) func(tea.MouseMsg) tea.Cmd {
	return func(msg tea.MouseMsg) tea.Cmd {
		mouse := msg.Mouse()
		target := mouseTargetAt(regions, mouse.X, mouse.Y)
		event := mouseInteractionMsg{target: target, button: mouse.Button, x: mouse.X, y: mouse.Y, at: time.Now()}
		switch msg.(type) {
		case tea.MouseMotionMsg:
			if target == hovered {
				return nil
			}
			event.kind = mouseEventHover
		case tea.MouseClickMsg:
			event.kind = mouseEventClick
		case tea.MouseReleaseMsg:
			event.kind = mouseEventRelease
		case tea.MouseWheelMsg:
			event.kind = mouseEventWheel
		default:
			return nil
		}
		return func() tea.Msg { return event }
	}
}

func isConfirmationScreen(screen screen) bool {
	switch screen {
	case screenConfirm, screenConfirmDelete, screenConfirmSync, screenConfirmCapture,
		screenConfirmRemoveRemote, screenConfirmDisconnect, screenConfirmDiffPublish,
		screenConfirmDiffDiscard, screenConfirmEditorDiscard, screenConfirmApprove,
		screenConfirmReject, screenConfirmDiverged:
		return true
	default:
		return false
	}
}

func isFormScreen(screen screen) bool {
	switch screen {
	case screenCreate, screenClone, screenAddProject, screenNewProfile,
		screenRemoteChange, screenMigrate, screenUnlockPassword, screenEnrollRequest,
		screenImportRecovery, screenRecovery, screenRecoveryPrompt, screenAdoptClone,
		screenAdoptLink, screenProjectOptions:
		return true
	default:
		return false
	}
}
