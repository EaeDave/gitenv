package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/eaedave/gitenv/internal/app"
	"github.com/eaedave/gitenv/internal/vault"
)

func divergedModel(report *app.DivergenceReport) model {
	return model{
		cfg:             &vault.LocalConfig{VaultPath: "/vault"},
		diverged:        report,
		divergedChoices: map[string]app.DivergenceChoice{},
	}
}

func hasAction(items []divergedMenuItem, action divergedAction) bool {
	for _, item := range items {
		if item.action == action {
			return true
		}
	}
	return false
}

func TestDivergedMenuHidesReviewWhenNothingConflicts(t *testing.T) {
	// One profile changed only locally: nothing to decide, so no review row.
	oneSided := divergedModel(&app.DivergenceReport{
		LocalCommits:  1,
		RemoteCommits: 1,
		Profiles:      []app.DivergedProfile{{Project: "api", Profile: "dev", ChangedLocally: true}},
	})
	if hasAction(oneSided.divergedMenuItems(), divergeReview) {
		t.Fatal("review row appeared with no both-sides conflict")
	}

	conflicting := divergedModel(&app.DivergenceReport{
		Profiles: []app.DivergedProfile{{Project: "api", Profile: "dev", ChangedLocally: true, ChangedRemotely: true}},
	})
	if !hasAction(conflicting.divergedMenuItems(), divergeReview) {
		t.Fatal("review row missing despite a both-sides conflict")
	}
}

func TestDivergedProfilesToggleUpdatesChoices(t *testing.T) {
	m := divergedModel(&app.DivergenceReport{
		Profiles: []app.DivergedProfile{{Project: "api", Profile: "dev", ChangedLocally: true, ChangedRemotely: true}},
	})
	m.screen = screenDivergedProfiles
	m.menuCursor = 0

	next, _ := m.divergedProfilesKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	got := next.(model)
	if got.divergedChoices["api/dev"] != app.DivergenceKeepMine {
		t.Fatalf("toggle did not switch to keep-mine: %#v", got.divergedChoices)
	}

	next, _ = got.divergedProfilesKey(tea.KeyPressMsg{Code: tea.KeyRight})
	got = next.(model)
	if got.divergedChoices["api/dev"] != app.DivergenceTakeRemote {
		t.Fatalf("second toggle did not switch back to take-remote: %#v", got.divergedChoices)
	}
}

func TestConfirmDivergedYStartsOperation(t *testing.T) {
	m := divergedModel(&app.DivergenceReport{
		Profiles: []app.DivergedProfile{{Project: "api", Profile: "dev", ChangedLocally: true, ChangedRemotely: true}},
	})
	m.screen = screenConfirmDiverged
	m.divergedCursor = 0 // the resolve row

	next, cmd := m.confirmDivergedKey(tea.KeyPressMsg{Code: 'y', Text: "y"})
	got := next.(model)
	if cmd == nil {
		t.Fatal("y on confirm did not start an operation")
	}
	if !got.busy || got.screen != screenProjects {
		t.Fatalf("y did not enter the busy projects state: busy=%v screen=%v", got.busy, got.screen)
	}

	cancel, cmd := m.confirmDivergedKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	cancelled := cancel.(model)
	if cmd != nil || cancelled.screen != screenDiverged {
		t.Fatalf("non-y did not cancel back to the menu: cmd=%v screen=%v", cmd, cancelled.screen)
	}
}

func TestRenderConfirmDivergedMentionsBackup(t *testing.T) {
	m := divergedModel(&app.DivergenceReport{
		Profiles: []app.DivergedProfile{{Project: "api", Profile: "dev", ChangedLocally: true, ChangedRemotely: true}},
	})

	items := m.divergedMenuItems()
	for index, item := range items {
		if item.action != divergeResolve && item.action != divergeDiscard {
			continue
		}
		m.divergedCursor = index
		out := m.renderConfirmDiverged(80)
		if !strings.Contains(out, "backup") {
			t.Fatalf("confirm text for action %d does not mention the backup: %s", item.action, out)
		}
	}
}

// TestResolveNeverDecidesAnUnseenConflict pins the rule that a both-sided
// conflict is never resolved behind the user's back. Choosing "keep my changes"
// while a conflict is undecided must open the review screen instead of resolving,
// and the review must show an explicit side for every conflict so the list the
// user reads is exactly what the resolution applies.
func TestResolveNeverDecidesAnUnseenConflict(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault"}
	report := app.DivergenceReport{
		LocalCommits:  1,
		RemoteCommits: 1,
		Profiles: []app.DivergedProfile{
			{Project: "api", Profile: "dev", ChangedLocally: true, ChangedRemotely: true},
			{Project: "api", Profile: "prod", ChangedLocally: true},
		},
	}
	m := model{cfg: &cfg, screen: screenDiverged, diverged: &report}

	// The resolve row is first; entering it with an undecided conflict reviews.
	next, cmd := m.divergedKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := next.(model)
	if cmd != nil {
		t.Fatal("resolving an unseen conflict started an operation")
	}
	if got.screen != screenDivergedProfiles {
		t.Fatalf("resolve with an undecided conflict did not open the review: %v", got.screen)
	}
	if got.info == "" {
		t.Fatal("the user was sent to review with no explanation")
	}
	// Every conflict now carries an explicit, rendered decision.
	if len(got.divergedChoices) != 1 {
		t.Fatalf("review did not make each conflict explicit: %#v", got.divergedChoices)
	}
	if got.divergedChoices["api/dev"] != app.DivergenceTakeRemote {
		t.Fatalf("seeded default is not the safe side: %#v", got.divergedChoices)
	}

	// What the review renders must match what will be applied.
	view := got.View().Content
	if !strings.Contains(view, "api/dev") && !strings.Contains(view, "dev") {
		t.Fatalf("review does not name the conflicted environment:\n%s", view)
	}

	// Toggling to keep-mine then resolving proceeds, carrying that exact choice.
	toggled, _ := got.divergedProfilesKey(tea.KeyPressMsg{Code: tea.KeyRight})
	got = toggled.(model)
	if got.divergedChoices["api/dev"] != app.DivergenceKeepMine {
		t.Fatalf("toggle did not record keep-mine: %#v", got.divergedChoices)
	}
	back, _ := got.divergedProfilesKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	got = back.(model)
	if got.screen != screenDiverged {
		t.Fatalf("leaving the review did not return to the menu: %v", got.screen)
	}
	got.divergedCursor = 0
	proceed, cmd := got.divergedKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("confirmation was skipped")
	}
	if proceed.(model).screen != screenConfirmDiverged {
		t.Fatalf("a fully reviewed resolution did not reach confirmation: %v", proceed.(model).screen)
	}
	if proceed.(model).divergedChoices["api/dev"] != app.DivergenceKeepMine {
		t.Fatal("the reviewed choice was lost on the way to confirmation")
	}
}
