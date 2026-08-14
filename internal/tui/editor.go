package tui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/eaedave/gitenv/internal/app"
	"github.com/eaedave/gitenv/internal/vault"
)

const editorChromeHeight = 18

// inlineEditableEnv rejects .env content the built-in editor cannot round-trip
// byte-for-byte. The textarea sanitizer replaces tabs, drops control characters
// and collapses lone carriage returns, so we refuse those inputs instead of
// silently corrupting a real .env.
func inlineEditableEnv(raw []byte) error {
	if !utf8.Valid(raw) {
		return errors.New("this .env contains non-UTF-8 bytes; the built-in editor cannot preserve it")
	}
	normalized := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	for _, r := range string(normalized) {
		switch {
		case r == '\n':
		case r == '\t':
			return errors.New("this .env uses tab characters the built-in editor cannot preserve")
		case r == utf8.RuneError:
			return errors.New("this .env contains an invalid character the built-in editor cannot preserve")
		case unicode.IsControl(r):
			return errors.New("this .env contains control characters the built-in editor cannot preserve")
		}
	}
	return nil
}

func (m model) openEditor(project string, back screen) (tea.Model, tea.Cmd) {
	if _, ok := m.cfg.Projects[project]; !ok {
		m.errText = "project is not linked on this computer"
		return m, nil
	}
	raw, err := app.ReadLocalEnv(*m.cfg, project)
	if err != nil {
		m.errText = safeError(err)
		return m, nil
	}
	base, profile, available, _ := app.ReadActiveProfileEnv(*m.cfg, project)
	return m.openEditorContent(project, "", raw, base, profile, available, back)
}

func (m model) openCaptureEditor() (tea.Model, tea.Cmd) {
	projectPath, err := m.captureProjectPath(m.pendingProject, m.pendingCapture)
	if err != nil {
		m.errText = err.Error()
		return m, nil
	}
	envPath := filepath.Join(projectPath, ".env")
	raw, err := os.ReadFile(envPath)
	if err != nil {
		m.errText = safeError(fmt.Errorf("read project .env: %w", err))
		return m, nil
	}
	base, available, err := captureBaseline(m.cfg, m.pendingProject, m.pendingProfile)
	if err != nil {
		m.errText = safeError(err)
		return m, nil
	}
	return m.openEditorContent(m.pendingProject, envPath, raw, base, m.pendingProfile, available, screenConfirmCapture)
}

func (m model) openEditorContent(project, path string, raw, base []byte, baseProfile string, baseAvailable bool, back screen) (tea.Model, tea.Cmd) {
	if err := inlineEditableEnv(raw); err != nil {
		m.errText = err.Error()
		return m, nil
	}

	crlf := bytes.Contains(raw, []byte("\r\n"))
	normalized := string(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")))
	trailing := strings.HasSuffix(normalized, "\n")
	buffer := normalized
	if trailing {
		buffer = strings.TrimSuffix(buffer, "\n")
	}

	editor := textarea.New()
	editor.SetStyles(textarea.DefaultStyles(m.isDark))
	editor.Prompt = "  "
	editor.ShowLineNumbers = true
	editor.CharLimit = 0
	editor.SetValue(buffer)
	editor.Focus()

	m.editor = editor
	m.editorProject = project
	m.editorPath = path
	m.editorRaw = raw
	m.editorBase = base
	m.editorBaseProfile = baseProfile
	m.editorBaseAvailable = baseAvailable
	m.editorCRLF = crlf
	m.editorTrailingNewline = trailing
	m.editorReturn = back
	m.editorTopLine = 0
	m.editorHorizontalOffset = 0
	m.screen = screenEditor
	m = m.applyEditorSize()
	m = m.ensureEditorCursorVisible()
	return m, textarea.Blink
}

func (m model) applyEditorSize() model {
	if m.screen != screenEditor {
		return m
	}
	m.editor.SetWidth(max(20, availableWidth(m.width)-4))
	m.editor.SetHeight(m.editorViewportHeight())
	return m.ensureEditorCursorVisible()
}

// editorBytes reconstructs the on-disk representation from the buffer,
// restoring the original newline style and trailing-newline state so that an
// unedited buffer is byte-identical to the file it was loaded from.
func (m model) editorBytes() []byte {
	out := m.editor.Value()
	if m.editorTrailingNewline {
		out += "\n"
	}
	if m.editorCRLF {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	return []byte(out)
}

func (m model) editorDirty() bool {
	return !bytes.Equal(m.editorBytes(), m.editorRaw)
}

func (m model) ensureEditorCursorVisible() model {
	height := m.editorViewportHeight()
	line := m.editor.Line()
	if line < m.editorTopLine {
		m.editorTopLine = line
	} else if line >= m.editorTopLine+height {
		m.editorTopLine = line - height + 1
	}

	lines := strings.Split(m.editor.Value(), "\n")
	if line < 0 || line >= len(lines) {
		return m
	}
	runes := []rune(lines[line])
	column := min(max(0, m.editor.Column()), len(runes))
	if column < m.editorHorizontalOffset {
		m.editorHorizontalOffset = column
	}
	contentWidth := max(2, m.editorContentWidth()-1)
	for m.editorHorizontalOffset < column && runeSliceWidth(runes[m.editorHorizontalOffset:column]) >= contentWidth {
		m.editorHorizontalOffset++
	}
	if column < contentWidth {
		m.editorHorizontalOffset = 0
	}
	return m
}

func runeSliceWidth(runes []rune) int {
	width := 0
	for _, character := range runes {
		width += max(1, ansi.StringWidth(string(character)))
	}
	return width
}

func (m *model) moveEditorCursor(line, visualColumn int) {
	line = min(max(0, line), max(0, m.editor.LineCount()-1))
	for m.editor.Line() < line {
		m.editor.CursorDown()
	}
	for m.editor.Line() > line {
		m.editor.CursorUp()
	}
	lines := strings.Split(m.editor.Value(), "\n")
	if line >= len(lines) {
		return
	}
	runes := []rune(lines[line])
	column := m.editorHorizontalOffset
	used := 0
	for column < len(runes) {
		characterWidth := max(1, ansi.StringWidth(string(runes[column])))
		if used+characterWidth > max(0, visualColumn) {
			break
		}
		used += characterWidth
		column++
	}
	m.editor.SetCursorColumn(column)
	*m = m.ensureEditorCursorVisible()
}

func (m *model) scrollEditor(lines int) {
	maximum := max(0, m.editor.LineCount()-m.editorViewportHeight())
	m.editorTopLine = min(maximum, max(0, m.editorTopLine+lines))
}

func (m model) editorKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+s":
		return m.saveEditor()
	case "esc":
		if m.editorDirty() {
			m.screen = screenConfirmEditorDiscard
			return m, nil
		}
		return m.closeEditor("")
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(key)
	m = m.ensureEditorCursorVisible()
	return m, cmd
}

func (m model) saveEditor() (tea.Model, tea.Cmd) {
	if !m.editorDirty() {
		return m.closeEditor("no changes to save")
	}
	data := m.editorBytes()
	project := m.editorProject
	var err error
	if m.editorPath != "" {
		err = vault.WriteAtomic(m.editorPath, data, 0o600)
	} else {
		err = app.WriteLocalEnv(*m.cfg, project, data)
	}
	if err != nil {
		m.errText = safeError(err)
		return m, nil
	}
	back := m.editorReturn
	if back == screenConfirmCapture {
		profile, intent := m.pendingProfile, m.pendingCapture
		m.clearEditor()
		m.screen = back
		m.info = ".env saved — preview refreshed"
		return m.requestCapturePreview(project, profile, intent)
	}
	m.clearEditor()
	m.screen = back
	m.info = ".env saved"
	return m, tea.Batch(loadCmd(m.cfg, m.cwd), inspectSyncCmd(m.cfg))
}

func (m model) confirmEditorDiscardKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.String() == "y" || key.String() == "Y" {
		return m.closeEditor("changes discarded")
	}
	m.screen = screenEditor
	return m, textarea.Blink
}

func (m model) closeEditor(info string) (tea.Model, tea.Cmd) {
	back := m.editorReturn
	m.clearEditor()
	m.screen = back
	if info != "" {
		m.info = info
	}
	return m, nil
}

func (m *model) clearEditor() {
	m.editor = textarea.Model{}
	m.editorProject = ""
	m.editorPath = ""
	m.editorRaw = nil
	m.editorBase = nil
	m.editorBaseProfile = ""
	m.editorBaseAvailable = false
	m.editorCRLF = false
	m.editorTrailingNewline = false
	m.editorTopLine = 0
	m.editorHorizontalOffset = 0
	m.editorReturn = screenProjects
}
