package app

import (
	"testing"
	"time"

	gitops "github.com/eaedave/gitenv/internal/git"
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

// TestRejectDeviceEnrollmentPublishesRemoval exercises the app boundary: pull,
// remove, commit, and push must leave both the manifest and sync state clean.
func TestRejectDeviceEnrollmentPublishesRemoval(t *testing.T) {
	cfg, root := newVaultForRemote(t)
	bare := root + "/remote.git"
	initBareRepo(t, bare)
	if err := ConfigureVaultRemote(cfg, bare); err != nil {
		t.Fatal(err)
	}
	_, request, err := vault.CreateEnrollmentRequest("unknown-laptop")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.AddEnrollmentRequest(cfg.VaultPath, request); err != nil {
		t.Fatal(err)
	}
	commitVault(t, cfg.VaultPath, "pending request")
	if err := PushExisting(cfg); err != nil {
		t.Fatal(err)
	}

	if err := RejectDeviceEnrollment(cfg, request.ID); err != nil {
		t.Fatal(err)
	}
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.EnrollmentRequests) != 0 {
		t.Fatalf("rejected request remains: %#v", manifest.EnrollmentRequests)
	}
	status := InspectSync(cfg)
	if status.State != gitops.SyncSynced || status.Dirty {
		t.Fatalf("rejection was not fully published: %#v", status)
	}
}

// TestRejectedRequesterCanRequestAgain verifies the requesting computer does not
// stay wedged forever after another device removes its request.
func TestRejectedRequesterCanRequestAgain(t *testing.T) {
	cfg, root := newVaultForRemote(t)
	bare := root + "/remote.git"
	initBareRepo(t, bare)
	if err := ConfigureVaultRemote(cfg, bare); err != nil {
		t.Fatal(err)
	}
	commitVault(t, cfg.VaultPath, "initial")
	if err := PushExisting(cfg); err != nil {
		t.Fatal(err)
	}
	request, err := RequestDeviceEnrollment(&cfg, "new-laptop")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.RejectEnrollmentRequest(cfg.VaultPath, request.ID); err != nil {
		t.Fatal(err)
	}
	if err := Push(cfg); err != nil {
		t.Fatal(err)
	}

	err = ActivateDeviceEnrollment(&cfg, request.ID, vault.StoreIdentitySession)
	if err == nil || cfg.PendingEnrollmentID != "" {
		t.Fatalf("rejected requester stayed pending: id=%q err=%v", cfg.PendingEnrollmentID, err)
	}
	if _, err := RequestDeviceEnrollment(&cfg, "new-laptop"); err != nil {
		t.Fatalf("requester could not try again after rejection: %v", err)
	}
}
