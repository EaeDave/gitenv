package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/eaedave/gitenv/internal/vault"
)

func editorModelForEnv(t *testing.T, content []byte) (model, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := vault.LocalConfig{Projects: map[string]vault.LocalProject{"api": {Path: dir}}}
	return model{cfg: &cfg, screen: screenProfiles, selectedProject: "api", width: 80, height: 24}, dir
}

func editorWithBase(base, buffer string, trailing bool) model {
	editor := textarea.New()
	editor.CharLimit = 0
	editor.SetValue(buffer)
	return model{
		width:                 80,
		height:                24,
		screen:                screenEditor,
		editor:                editor,
		editorProject:         "millennium-api-docs",
		editorTrailingNewline: trailing,
		editorBase:            []byte(base),
		editorBaseProfile:     "prod",
		editorBaseAvailable:   true,
	}
}

func TestEditorRendersGitStyleDiffAgainstCapturedProfile(t *testing.T) {
	base := "DATABASE_URL=postgres://x\nAPP_AUTH_USER=bakihanma\n"
	m := editorWithBase(base, "DATABASE_URL=postgres://x\nAPP_AUTH_USER=bakihanma\ntest", false)
	view := m.renderEditor(80)
	if !strings.Contains(view, "Diff vs prod (captured)") {
		t.Fatalf("editor diff missing baseline title:\n%s", view)
	}
	if !strings.Contains(view, `"test"`) || !strings.Contains(view, "+") {
		t.Fatalf("added local line not shown as a diff addition:\n%s", view)
	}

	clean := editorWithBase(base, "DATABASE_URL=postgres://x\nAPP_AUTH_USER=bakihanma", false)
	if view := clean.renderEditor(80); !strings.Contains(view, "matches the captured profile") {
		t.Fatalf("unchanged buffer should report a clean match:\n%s", view)
	}
}

func openEditorModel(t *testing.T, content []byte) (model, string) {
	t.Helper()
	m, dir := editorModelForEnv(t, content)
	next, _ := m.openEditor("api", screenProfiles)
	m = next.(model)
	if m.screen != screenEditor {
		t.Fatalf("editor did not open: screen=%v err=%q", m.screen, m.errText)
	}
	return m, dir
}

func TestEditorPreservesBytesOnUneditedRoundTrip(t *testing.T) {
	longValue := strings.Repeat("x", 600)
	manyLines := strings.Repeat("KEY=value\n", 150)
	cases := map[string][]byte{
		"lf trailing":     []byte("API_KEY=old\n# DEBUG=true\nEMPTY=\n"),
		"crlf trailing":   []byte("API_KEY=old\r\n# DEBUG=true\r\n"),
		"no trailing":     []byte("API_KEY=old\nB=2"),
		"blank trailing":  []byte("API_KEY=old\n\n"),
		"leading blank":   []byte("\nAPI_KEY=old\n"),
		"long value line": []byte("TOKEN=" + longValue + "\n"),
		"many lines":      []byte(manyLines),
		"empty file":      {},
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			m, _ := openEditorModel(t, content)
			if got := m.editorBytes(); !bytes.Equal(got, content) {
				t.Fatalf("round-trip changed bytes:\n got %q\nwant %q", got, content)
			}
			if m.editorDirty() {
				t.Fatalf("unedited buffer reported dirty for %q", name)
			}
		})
	}
}

func TestEditorRefusesContentItCannotPreserve(t *testing.T) {
	cases := map[string]struct {
		content  []byte
		fragment string
	}{
		"tab":          {[]byte("API_KEY=a\tb\n"), "tab"},
		"control byte": {[]byte("API_KEY=a\x00b\n"), "control"},
		"lone cr":      {[]byte("API_KEY=old\rB=2\n"), "control"},
		"invalid utf8": {[]byte{0x41, 0x3d, 0xff, 0x0a}, "non-UTF-8"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m, dir := editorModelForEnv(t, tc.content)
			next, _ := m.openEditor("api", screenProfiles)
			m = next.(model)
			if m.screen == screenEditor {
				t.Fatalf("editor opened on unpreservable content %q", name)
			}
			if !strings.Contains(m.errText, tc.fragment) {
				t.Fatalf("error %q missing fragment %q", m.errText, tc.fragment)
			}
			got, err := os.ReadFile(filepath.Join(dir, ".env"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tc.content) {
				t.Fatalf("refused open mutated file: %q", got)
			}
		})
	}
}

func typeIntoEditor(m model, text string) model {
	m2, _ := m.editorKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(model)
	for _, r := range text {
		next, _ := m.editorKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(model)
	}
	return m
}

func TestEditorSavesEditsWithFidelity(t *testing.T) {
	m, dir := openEditorModel(t, []byte("API_KEY=old\r\n"))
	m = typeIntoEditor(m, "NEW=1")
	if !m.editorDirty() {
		t.Fatal("edited buffer not reported dirty")
	}
	if view := m.renderEditor(80); !strings.Contains(view, "no captured profile") {
		t.Fatalf("editor without a captured baseline should say so:\n%s", view)
	}

	saved, cmd := m.editorKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = saved.(model)
	if cmd == nil || m.screen != screenProfiles || m.info != ".env saved" {
		t.Fatalf("save did not return to profiles: screen=%v info=%q", m.screen, m.info)
	}
	if m.editorProject != "" || m.editorRaw != nil {
		t.Fatalf("editor state not cleared after save: %#v", m)
	}
	got, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "API_KEY=old\r\nNEW=1\r\n"; string(got) != want {
		t.Fatalf("saved bytes = %q, want %q", got, want)
	}
}

func TestEditorNoOpSaveWritesNothing(t *testing.T) {
	m, dir := openEditorModel(t, []byte("API_KEY=old\n"))
	info, err := os.Stat(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	saved, cmd := m.editorKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = saved.(model)
	if cmd != nil || m.screen != screenProfiles || m.info != "no changes to save" {
		t.Fatalf("no-op save misbehaved: screen=%v info=%q cmd=%v", m.screen, m.info, cmd)
	}
	after, err := os.Stat(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(info.ModTime()) {
		t.Fatal("no-op save rewrote the file")
	}
}

func TestEditorEscConfirmsBeforeDiscardingChanges(t *testing.T) {
	original := []byte("API_KEY=old\n")
	m, dir := openEditorModel(t, original)
	m = typeIntoEditor(m, "NEW=1")

	prompted, _ := m.editorKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = prompted.(model)
	if m.screen != screenConfirmEditorDiscard {
		t.Fatalf("dirty esc did not prompt: screen=%v", m.screen)
	}
	kept, _ := m.confirmEditorDiscardKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = kept.(model)
	if m.screen != screenEditor || !m.editorDirty() {
		t.Fatalf("declining discard lost edits: screen=%v", m.screen)
	}
	prompted, _ = m.editorKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = prompted.(model)
	discarded, _ := m.confirmEditorDiscardKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = discarded.(model)
	if m.screen != screenProfiles || m.editorProject != "" {
		t.Fatalf("confirmed discard did not close editor: %#v", m)
	}
	got, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("discard wrote to disk: %q", got)
	}
}
