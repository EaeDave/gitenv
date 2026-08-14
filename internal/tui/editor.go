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

	"github.com/eaedave/gitenv/internal/app"
	"github.com/eaedave/gitenv/internal/vault"
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
	m.editorReturn = screenProjects
}
