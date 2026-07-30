package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eaedave/gitenv/internal/vault"
)

// twoPendingModel builds a devices screen holding two pending approvals whose
// ids are distinctive strings, so a test can assert those ids never leak into
// the rendered output.
func twoPendingModel() model {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	return model{
		cfg:    &cfg,
		screen: screenDevices,
		pendingApprovals: []vault.EnrollmentRequest{
			{ID: "req-secret-id-0001", Name: "work-laptop", CreatedAt: time.Now().Add(-2 * time.Hour)},
			{ID: "req-secret-id-0002", Name: "home-desktop", CreatedAt: time.Now().Add(-30 * time.Minute)},
		},
	}
}

// TestLowercaseDOpensDevices pins the user-facing shortcut: routine navigation
// must not require Shift. Uppercase D was the original regression.
func TestLowercaseDOpensDevices(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	m := model{cfg: &cfg, screen: screenProjects}

	next, cmd := m.projectsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	got := next.(model)
	if cmd != nil || got.screen != screenDevices {
		t.Fatalf("lowercase d did not open devices: screen=%v cmd=%v", got.screen, cmd)
	}
}

// TestDevicesRendersNamesNeverIDs is the regression guard for the whole design:
// device names appear, request ids never do.
func TestDevicesRendersNamesNeverIDs(t *testing.T) {
	m := twoPendingModel()

	out := m.renderDevices(80)

	if !strings.Contains(out, "work-laptop") || !strings.Contains(out, "home-desktop") {
		t.Fatalf("pending device names not rendered:\n%s", out)
	}
	for _, id := range []string{"req-secret-id-0001", "req-secret-id-0002"} {
		if strings.Contains(out, id) {
			t.Fatalf("request id %q leaked into the devices screen:\n%s", id, out)
		}
	}
}

// TestEnterOnPendingRequestConfirms verifies enter on a pending request opens
// the approval confirmation, and the prompt names the device.
func TestEnterOnPendingRequestConfirms(t *testing.T) {
	m := twoPendingModel()
	m.approvalCursor = 1

	next, cmd := m.devicesKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if cmd != nil {
		t.Fatalf("opening the confirmation must not start a command")
	}
	if got.screen != screenConfirmApprove {
		t.Fatalf("enter did not reach the approval confirmation: screen=%v", got.screen)
	}
	prompt := got.renderConfirmApprove(80)
	if !strings.Contains(prompt, "home-desktop") {
		t.Fatalf("approval prompt did not name the device:\n%s", prompt)
	}
}

// TestConfirmApproveStartsOperationOnlyOnYes verifies y starts the (slow)
// re-encrypt-and-push through opCmd with the spinner on, while any other key
// cancels back to the roster without touching the vault.
func TestConfirmApproveStartsOperationOnlyOnYes(t *testing.T) {
	m := twoPendingModel()
	m.screen = screenConfirmApprove
	m.approvalCursor = 0

	next, cmd := m.confirmApproveKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	got := next.(model)
	if cmd == nil || !got.busy {
		t.Fatalf("y did not start an approval operation: cmd=%v busy=%v", cmd, got.busy)
	}
	if got.screen != screenDevices {
		t.Fatalf("approval should return to the devices roster: screen=%v", got.screen)
	}

	next, cmd = m.confirmApproveKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	got = next.(model)
	if cmd != nil || got.busy {
		t.Fatalf("cancelling must not start an operation: cmd=%v busy=%v", cmd, got.busy)
	}
	if got.screen != screenDevices || got.info != "cancelled" {
		t.Fatalf("cancel did not return to the roster: screen=%v info=%q", got.screen, got.info)
	}
}

// TestDevicesEmptyStateRendersAndEnterIsNoop verifies that with no pending
// requests the screen still renders (and explains where requests come from),
// and enter does nothing rather than opening a dead confirmation.
func TestDevicesEmptyStateRendersAndEnterIsNoop(t *testing.T) {
	cfg := vault.LocalConfig{VaultPath: "/vault", Projects: map[string]vault.LocalProject{}}
	m := model{cfg: &cfg, screen: screenDevices}

	out := m.renderDevices(80)
	if !strings.Contains(out, "unlock screen") {
		t.Fatalf("empty state did not explain where requests come from:\n%s", out)
	}

	next, cmd := m.devicesKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if cmd != nil {
		t.Fatalf("enter with no pending request must not start a command")
	}
	if got.screen != screenDevices {
		t.Fatalf("enter with no pending request should stay put: screen=%v", got.screen)
	}
}
