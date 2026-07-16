package tui

import (
	"bytes"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/eaedave/gitenv/internal/app"
)

const editorChromeHeight = 11

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
	editor.Prompt = "  "
	editor.ShowLineNumbers = true
	editor.CharLimit = 0
	editor.SetValue(buffer)
	editor.Focus()

	m.editor = editor
	m.editorProject = project
	m.editorRaw = raw
	m.editorCRLF = crlf
	m.editorTrailingNewline = trailing
	m.editorReturn = back
	m.screen = screenEditor
	m = m.applyEditorSize()
	return m, textarea.Blink
}

func (m model) applyEditorSize() model {
	if m.screen != screenEditor {
		return m
	}
	m.editor.SetWidth(max(20, m.width-4))
	m.editor.SetHeight(max(3, m.height-editorChromeHeight))
	return m
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

func (m model) editorKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	return m, cmd
}

func (m model) saveEditor() (tea.Model, tea.Cmd) {
	if !m.editorDirty() {
		return m.closeEditor("no changes to save")
	}
	data := m.editorBytes()
	project := m.editorProject
	if err := app.WriteLocalEnv(*m.cfg, project, data); err != nil {
		m.errText = safeError(err)
		return m, nil
	}
	back := m.editorReturn
	m.clearEditor()
	m.screen = back
	m.info = ".env saved"
	return m, tea.Batch(loadCmd(m.cfg, m.cwd), inspectSyncCmd(m.cfg))
}

func (m model) confirmEditorDiscardKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	m.editorRaw = nil
	m.editorCRLF = false
	m.editorTrailingNewline = false
	m.editorReturn = screenProjects
}
