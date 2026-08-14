package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/eaedave/gitenv/internal/app"
	"github.com/eaedave/gitenv/internal/vault"
)

// divergenceReportMsg carries the result of inspecting a diverged vault off the
// update loop. The lead's Update stores report into m.diverged, resets
// m.divergedChoices/m.divergedCursor and switches to screenDiverged, or shows
// err. report is nil when err is set.
type divergenceReportMsg struct {
	report *app.DivergenceReport
	err    error
}

// divergedAction names what a screenDiverged menu row does when chosen.
type divergedAction int

const (
	divergeResolve divergedAction = iota
	divergeReview
	divergeDiscard
	divergeBack
)

type divergedMenuItem struct {
	label  string
	action divergedAction
}

// divergedMenuItems builds the resolution menu. The review row appears only
// when at least one profile changed on both sides, because that is the only
// case where the user has a per-profile decision to make.
//
// The resolve row keeps a plain label because it can no longer mislead: when any
// both-sided conflict is still undecided, choosing it opens the review screen
// instead of resolving. A user therefore never receives the shared copy's value
// for a profile they never looked at.
func (m model) divergedMenuItems() []divergedMenuItem {
	conflicts := len(m.divergedConflicts())
	items := []divergedMenuItem{{"Keep my changes and rebuild on top of the remote", divergeResolve}}
	if conflicts > 0 {
		items = append(items, divergedMenuItem{
			fmt.Sprintf("Review the %d %s that changed on both sides", conflicts, pluralize(conflicts, "environment", "environments")),
			divergeReview,
		})
	}
	items = append(items, divergedMenuItem{"Discard my unpublished vault changes", divergeDiscard})
	items = append(items, divergedMenuItem{"Back", divergeBack})
	return items
}

// undecidedConflicts counts both-sided profiles the user has not explicitly
// decided about yet, which are the ones that will fall back to the shared copy.
func (m model) undecidedConflicts() int {
	undecided := 0
	for _, profile := range m.divergedConflicts() {
		if _, chosen := m.divergedChoices[profile.Project+"/"+profile.Profile]; !chosen {
			undecided++
		}
	}
	return undecided
}

// divergedConflicts returns the both-sides-changed profiles, or nil when no
// report is loaded.
func (m model) divergedConflicts() []app.DivergedProfile {
	if m.diverged == nil {
		return nil
	}
	return m.diverged.Conflicted()
}

// divergedCmd inspects the vault off the update loop and reports the result.
func divergedCmd(cfg *vault.LocalConfig) tea.Cmd {
	return func() tea.Msg {
		report, err := app.InspectDivergence(*cfg)
		if err != nil {
			return divergenceReportMsg{err: err}
		}
		return divergenceReportMsg{report: &report}
	}
}

// resolveDivergedCmd runs the keep-mine resolution and names the backup ref in
// the success line so the user knows where their previous vault went.
func resolveDivergedCmd(cfg *vault.LocalConfig, choices map[string]app.DivergenceChoice) tea.Cmd {
	return func() tea.Msg {
		backup, err := app.ResolveDivergence(cfg, choices)
		if err != nil {
			return operationMsg{err: err}
		}
		return operationMsg{info: "vault rebuilt on the remote; your previous vault is saved as " + backup}
	}
}

// discardDivergedCmd throws away local vault commits and names the backup ref.
func discardDivergedCmd(cfg *vault.LocalConfig) tea.Cmd {
	return func() tea.Msg {
		backup, err := app.DiscardLocalVaultChanges(cfg)
		if err != nil {
			return operationMsg{err: err}
		}
		return operationMsg{info: "vault reset to the remote; your previous vault is saved as " + backup}
	}
}

func (m model) divergedKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	items := m.divergedMenuItems()
	switch key.String() {
	case "esc", "q":
		m.screen = screenProjects
	case "up", "k":
		m.divergedCursor = max(0, m.divergedCursor-1)
	case "down", "j":
		m.divergedCursor = min(len(items)-1, m.divergedCursor+1)
	case "enter":
		if m.divergedCursor < 0 || m.divergedCursor >= len(items) {
			return m, nil
		}
		switch items[m.divergedCursor].action {
		case divergeResolve:
			// Never resolve a both-sided conflict the user has not seen. Picking
			// "keep my changes" and silently receiving the shared copy's value is
			// a data surprise, even though the backup ref makes it recoverable.
			if m.undecidedConflicts() > 0 {
				m.seedDivergedChoices()
				m.menuCursor = 0
				m.screen = screenDivergedProfiles
				m.info = "choose a side for each environment that changed in both places"
				return m, nil
			}
			m.screen = screenConfirmDiverged
		case divergeDiscard:
			m.screen = screenConfirmDiverged
		case divergeReview:
			m.seedDivergedChoices()
			m.menuCursor = 0
			m.screen = screenDivergedProfiles
		case divergeBack:
			m.screen = screenProjects
		}
	}
	return m, nil
}

func (m model) divergedProfilesKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	conflicts := m.divergedConflicts()
	switch key.String() {
	case "esc", "enter":
		m.screen = screenDiverged
	case "up", "k":
		m.menuCursor = max(0, m.menuCursor-1)
	case "down", "j":
		m.menuCursor = min(max(0, len(conflicts)-1), m.menuCursor+1)
	case "left", "right", "space":
		if m.menuCursor >= 0 && m.menuCursor < len(conflicts) {
			m.toggleDivergedChoice(conflicts[m.menuCursor])
		}
	}
	return m, nil
}

func (m model) confirmDivergedKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.String() == "y" || key.String() == "Y" {
		m.screen, m.busy = screenProjects, true
		if m.pendingDivergedAction() == divergeDiscard {
			return m, discardDivergedCmd(m.cfg)
		}
		return m, resolveDivergedCmd(m.cfg, m.divergedChoices)
	}
	m.screen, m.info = screenDiverged, "cancelled"
	return m, nil
}

// pendingDivergedAction reads which resolution the confirm screen is confirming
// from the menu row still under the cursor. Only resolve and discard rows reach
// the confirm screen, so any other value falls back to the safer resolve path.
func (m model) pendingDivergedAction() divergedAction {
	items := m.divergedMenuItems()
	if m.divergedCursor >= 0 && m.divergedCursor < len(items) {
		if action := items[m.divergedCursor].action; action == divergeDiscard {
			return divergeDiscard
		}
	}
	return divergeResolve
}

// toggleDivergedChoice flips a conflicted profile between keeping the local
// version and taking the remote. A missing entry means take-remote, so the
// first toggle switches to keep-mine.
func (m *model) toggleDivergedChoice(profile app.DivergedProfile) {
	if m.divergedChoices == nil {
		m.divergedChoices = map[string]app.DivergenceChoice{}
	}
	key := profile.Project + "/" + profile.Profile
	if m.divergedChoices[key] == app.DivergenceKeepMine {
		m.divergedChoices[key] = app.DivergenceTakeRemote
	} else {
		m.divergedChoices[key] = app.DivergenceKeepMine
	}
}

// seedDivergedChoices makes every conflict explicit the moment the review screen
// opens, so what the list shows is exactly what the resolution will do. Without
// this, a user could open the review, change nothing, and be sent straight back
// by the undecided-conflict guard with no way to accept the defaults.
func (m *model) seedDivergedChoices() {
	if m.divergedChoices == nil {
		m.divergedChoices = map[string]app.DivergenceChoice{}
	}
	for _, profile := range m.divergedConflicts() {
		key := profile.Project + "/" + profile.Profile
		if _, chosen := m.divergedChoices[key]; !chosen {
			m.divergedChoices[key] = app.DivergenceTakeRemote
		}
	}
}

// divergedChoice reports the current per-profile decision, defaulting to
// take-remote for any profile the user has not toggled.
func (m model) divergedChoice(profile app.DivergedProfile) app.DivergenceChoice {
	if choice, ok := m.divergedChoices[profile.Project+"/"+profile.Profile]; ok {
		return choice
	}
	return app.DivergenceTakeRemote
}
