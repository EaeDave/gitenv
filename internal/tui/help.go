package tui

import tea "charm.land/bubbletea/v2"

// keyBinding is one row of a screen's authoritative keymap: the literal keys the
// handler listens for, and what pressing them does.
type keyBinding struct{ keys, action string }

// globalKeymap lists the bindings that work on every screen. They live apart
// from keymapFor so each screen's help does not have to repeat them, and so
// neither goes undocumented the way ctrl+c and U were.
func globalKeymap() []keyBinding {
	return []keyBinding{
		{"mouse", "hover for feedback; click actions; wheel scrolls lists"},
		{"ctrl+c", "quit immediately, from anywhere"},
		{"U", "install the available gitenv update (when one is offered)"},
	}
}

// keymapFor returns the complete keymap for a screen, derived from what each
// key handler in keys.go genuinely binds. It is the single source of truth for
// the in-app help, so a binding here that keys.go does not implement is a lie:
// keep the two in lockstep.
func keymapFor(s screen) []keyBinding {
	switch s {
	case screenProjects:
		return []keyBinding{
			{"↑↓/jk", "move between projects"},
			{"pgup/pgdn", "move one page"},
			{"home/end", "jump to first / last project"},
			{"/", "fuzzy-search projects"},
			{"enter", "open, adopt, or add the selected project"},
			{"a", "add current project"},
			{"c", "capture .env into a profile"},
			{"o", "project options (env file, line endings)"},
			{"f", "find local clones to link"},
			{"v", "view unpublished vault changes"},
			{"s", "sync (publish or pull)"},
			{"g", "sync repository settings"},
			{"b", "save recovery key"},
			{"d", "devices & approvals"},
			{"r", "refresh"},
			{"?", "this help"},
			{"q", "quit"},
		}
	case screenProfiles:
		return []keyBinding{
			{"↑↓/jk", "move between profiles"},
			{"enter", "apply profile to disk"},
			{"c", "capture .env into active profile"},
			{"n", "capture .env as a new profile"},
			{"e", "edit the .env in-app"},
			{"d", "remove profile"},
			{"o", "project options (env file, line endings)"},
			{"v", "view unpublished vault changes"},
			{"s", "sync (publish or pull)"},
			{"p", "all projects"},
			{"r", "refresh"},
			{"?", "this help"},
			{"esc/q", "back"},
		}
	case screenSyncDiff:
		return []keyBinding{
			{"tab", "next environment"},
			{"shift+tab", "previous environment"},
			{"x", "reveal / hide values"},
			{"e", "edit the .env in-app"},
			{"p", "publish selected environment"},
			{"d", "discard selected change"},
			{"↑↓/jk", "scroll"},
			{"pgup/ctrl+b", "page up"},
			{"pgdn/ctrl+f/space", "page down"},
			{"home/g", "jump to top"},
			{"end/G", "jump to bottom"},
			{"?", "this help"},
			{"esc/q", "back"},
		}
	case screenDevices:
		return []keyBinding{
			{"↑↓/jk", "select device or request"},
			{"enter", "approve pending request"},
			{"x", "reject pending request"},
			{"r", "reload"},
			{"?", "this help"},
			{"esc/q", "back"},
		}
	case screenDiverged:
		return []keyBinding{
			{"↑↓/jk", "move between options"},
			{"enter", "choose resolution"},
			{"?", "this help"},
			{"esc/q", "back"},
		}
	default:
		return nil
	}
}

// helpReturnScreen is where the help screen sends the user back to. It falls
// back to the project list when the origin was never set (the zero value,
// screenOnboarding) or would loop back onto help itself.
func (m model) helpReturnScreen() screen {
	if m.helpReturn == screenOnboarding || m.helpReturn == screenHelp {
		return screenProjects
	}
	return m.helpReturn
}

// helpScreenChromeHeight is the vertical space the header, help line, position
// indicator, and their separators consume, subtracted from the terminal height
// to size the scroll window.
const helpScreenChromeHeight = 10

// helpPageSize is how many content lines fit on screen. A zero (or absent)
// terminal height means "unbounded": render everything and never scroll, which
// is also what tests that never send a WindowSizeMsg get.
func (m model) helpPageSize() int {
	if m.height <= 0 {
		return 0
	}
	return max(4, m.height-helpScreenChromeHeight)
}

// helpMaxOffset is the furthest the scroll offset may travel before the last
// line sits at the bottom of the window.
func helpMaxOffset(lineCount, pageSize int) int {
	if pageSize <= 0 {
		return 0
	}
	return max(0, lineCount-pageSize)
}

// clampHelpOffset keeps a scroll offset within [0, maxOffset], mirroring the
// diff viewer's clampSyncDiffOffset rather than inventing a second scheme.
func clampHelpOffset(offset, lineCount, pageSize int) int {
	return min(max(0, offset), helpMaxOffset(lineCount, pageSize))
}

// helpKey drives the help screen: any exit key returns to the originating
// screen, and the arrow/paging keys scroll when the content is taller than the
// terminal (a no-op when it already fits).
func (m model) helpKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	lines := m.helpLines(availableWidth(m.width))
	pageSize := m.helpPageSize()
	m.helpOffset = clampHelpOffset(m.helpOffset, len(lines), pageSize)
	switch key.String() {
	case "esc", "q", "?", "enter":
		m.screen = m.helpReturnScreen()
		m.helpOffset = 0
	case "up", "k":
		m.helpOffset = max(0, m.helpOffset-1)
	case "down", "j":
		m.helpOffset = min(helpMaxOffset(len(lines), pageSize), m.helpOffset+1)
	case "pgup", "ctrl+b":
		m.helpOffset = max(0, m.helpOffset-pageSize)
	case "pgdown", "ctrl+f", "space":
		m.helpOffset = min(helpMaxOffset(len(lines), pageSize), m.helpOffset+pageSize)
	}
	return m, nil
}
