package app

import (
	"testing"
	"time"

	"github.com/eaedave/gitenv/internal/vault"
)

// TestPendingApprovalsExcludesOwnRequest is the guard for the whole design: a
// computer must never see its own pending request as something it can approve.
func TestPendingApprovalsExcludesOwnRequest(t *testing.T) {
	cfg := vault.LocalConfig{PendingEnrollmentID: "mine"}
	manifest := vault.Manifest{EnrollmentRequests: []vault.EnrollmentRequest{
		{ID: "mine", Name: "this-laptop"},
		{ID: "theirs", Name: "desktop"},
	}}

	pending := PendingApprovals(cfg, manifest)

	if len(pending) != 1 {
		t.Fatalf("expected exactly the other computer's request, got %d: %#v", len(pending), pending)
	}
	if pending[0].ID != "theirs" {
		t.Fatalf("own request was not excluded: %#v", pending)
	}
}

// TestPendingApprovalsSortedOldestFirst verifies the oldest request surfaces
// first, so the queue is processed in the order devices asked.
func TestPendingApprovalsSortedOldestFirst(t *testing.T) {
	now := time.Now()
	manifest := vault.Manifest{EnrollmentRequests: []vault.EnrollmentRequest{
		{ID: "b", Name: "newer", CreatedAt: now},
		{ID: "a", Name: "older", CreatedAt: now.Add(-2 * time.Hour)},
	}}

	pending := PendingApprovals(vault.LocalConfig{}, manifest)

	if len(pending) != 2 || pending[0].ID != "a" || pending[1].ID != "b" {
		t.Fatalf("requests not sorted oldest first: %#v", pending)
	}
}

// TestEnrolledDevicesSortedByName verifies the roster reads the same on every
// computer regardless of manifest order.
func TestEnrolledDevicesSortedByName(t *testing.T) {
	manifest := vault.Manifest{Devices: []vault.Device{
		{ID: "2", Name: "zeta"},
		{ID: "1", Name: "alpha"},
	}}

	devices := EnrolledDevices(manifest)

	if len(devices) != 2 || devices[0].Name != "alpha" || devices[1].Name != "zeta" {
		t.Fatalf("devices not sorted by name: %#v", devices)
	}
}
