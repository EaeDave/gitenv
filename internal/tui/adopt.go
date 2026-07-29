package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eaedave/gitenv/internal/app"
	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/vault"
)

// discoveryTimeout bounds a filesystem scan. A scan runs inside a tea.Cmd so it
// never blocks the UI, but it must still terminate: a pathological directory
// tree could otherwise walk indefinitely.
const discoveryTimeout = 90 * time.Second

// cloneTimeout bounds a clone. Ten minutes is generous on purpose — a large
// repository over a slow link is legitimately slow — and the TUI stays
// responsive because the clone runs inside a tea.Cmd, not on the update loop.
const cloneTimeout = 10 * time.Minute

// discoveryMsg reports the result of a repository scan keyed by canonical
// identity. found is the same shape RunDiscovery persists to the cache.
type discoveryMsg struct {
	found map[string][]string
	err   error
}

// adoptMsg reports the result of adopting a project. outcome is zero for a
// pure link (no clone happened); its URL is already redacted by CloneRepository.
type adoptMsg struct {
	project string
	outcome gitops.CloneOutcome
	err     error
}

// discoverCmd scans the filesystem for clones and refreshes the discovery cache.
func discoverCmd(cfg *vault.LocalConfig) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), discoveryTimeout)
		defer cancel()
		found, err := app.RunDiscovery(ctx, cfg)
		return discoveryMsg{found: found, err: err}
	}
}

// cloneAdoptCmd clones the recorded repository into dest, then links and applies
// the env file. The redacted outcome names the method that actually succeeded.
func cloneAdoptCmd(cfg *vault.LocalConfig, name, dest string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), cloneTimeout)
		defer cancel()
		outcome, err := app.CloneAndAdopt(ctx, cfg, name, dest, "")
		return adoptMsg{project: name, outcome: outcome, err: err}
	}
}

// linkAdoptCmd links an already-present clone at path and applies the env file.
func linkAdoptCmd(cfg *vault.LocalConfig, name, path string) tea.Cmd {
	return func() tea.Msg {
		if err := app.AdoptProject(cfg, name, path, ""); err != nil {
			return adoptMsg{project: name, err: err}
		}
		return adoptMsg{project: name}
	}
}

// projectNames extracts the ordered names of project states so cursor
// arithmetic and the existing name-driven helpers keep working unchanged.
func projectNames(states []app.ProjectState) []string {
	names := make([]string, len(states))
	for index, state := range states {
		names[index] = state.Name
	}
	return names
}

// selectedProjectState returns the state under the projects cursor.
func (m model) selectedProjectState() (app.ProjectState, bool) {
	if m.projectCursor < 0 || m.projectCursor >= len(m.projectStates) {
		return app.ProjectState{}, false
	}
	return m.projectStates[m.projectCursor], true
}

// projectStateByName finds a project state by name, used by the adopt screens
// which track the project by name rather than by a cursor that could move.
func (m model) projectStateByName(name string) (app.ProjectState, bool) {
	for _, state := range m.projectStates {
		if state.Name == name {
			return state, true
		}
	}
	return app.ProjectState{}, false
}

// pluralize picks the singular or plural form for a count.
func pluralize(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
