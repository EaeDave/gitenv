package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const syncFetchTimeout = 8 * time.Second

type SyncState string

const (
	SyncChecking    SyncState = "checking"
	SyncSynced      SyncState = "synced"
	SyncLocalAhead  SyncState = "local_ahead"
	SyncRemoteAhead SyncState = "remote_ahead"
	SyncDiverged    SyncState = "diverged"
	SyncNoRemote    SyncState = "no_remote"
	SyncOffline     SyncState = "offline"
	SyncAuthError   SyncState = "auth_error"
	SyncError       SyncState = "error"
)

type SyncStatus struct {
	State     SyncState
	Ahead     int
	Behind    int
	Dirty     bool
	CheckedAt time.Time
	Detail    string
}

// InspectSync fetches remote refs and compares them with HEAD without merging,
// checking out files, staging changes, or modifying the working tree.
func InspectSync(root string) SyncStatus {
	status := SyncStatus{State: SyncChecking, CheckedAt: time.Now()}
	if !HasRemote(root, "origin") {
		status.State = SyncNoRemote
		return status
	}
	status.Dirty = workingTreeDirty(root)
	if _, err := runWithTimeout(root, syncFetchTimeout, "fetch", "--quiet", "origin"); err != nil {
		status.State, status.Detail = classifyRemoteError(err)
		return status
	}
	if !hasCommit(root) || !hasUpstream(root) {
		status.Ahead = commitCount(root)
		status.State = syncState(status.Ahead, 0, status.Dirty)
		return status
	}
	ahead, behind, err := countAheadBehind(root)
	if err != nil {
		status.State = SyncError
		status.Detail = RedactURL(err.Error())
		return status
	}
	status.Ahead, status.Behind = ahead, behind
	status.State = syncState(ahead, behind, status.Dirty)
	return status
}

func syncState(ahead, behind int, dirty bool) SyncState {
	switch {
	case (ahead > 0 && behind > 0) || (dirty && behind > 0):
		return SyncDiverged
	case ahead > 0 || dirty:
		return SyncLocalAhead
	case behind > 0:
		return SyncRemoteAhead
	default:
		return SyncSynced
	}
}

func countAheadBehind(root string) (int, int, error) {
	output, err := run(root, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if err != nil {
		return 0, 0, fmt.Errorf("compare local and remote revisions: %w", err)
	}
	fields := strings.Fields(output)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("compare local and remote revisions: expected two counts, got %q", strings.TrimSpace(output))
	}
	ahead, aheadErr := strconv.Atoi(fields[0])
	behind, behindErr := strconv.Atoi(fields[1])
	if aheadErr != nil || behindErr != nil {
		return 0, 0, fmt.Errorf("compare local and remote revisions: invalid counts %q", strings.TrimSpace(output))
	}
	return ahead, behind, nil
}

func workingTreeDirty(root string) bool {
	status, err := Status(root)
	return err != nil || status != ""
}

func hasCommit(root string) bool {
	_, err := run(root, "rev-parse", "--verify", "HEAD")
	return err == nil
}

func commitCount(root string) int {
	if !hasCommit(root) {
		return 0
	}
	output, err := run(root, "rev-list", "--count", "HEAD")
	if err != nil {
		return 0
	}
	count, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil {
		return 0
	}
	return count
}

func classifyRemoteError(err error) (SyncState, string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return SyncOffline, "remote check timed out"
	}
	detail := strings.ToLower(err.Error())
	switch {
	case strings.Contains(detail, "authentication failed"),
		strings.Contains(detail, "permission denied"),
		strings.Contains(detail, "could not read username"),
		strings.Contains(detail, "repository not found"):
		return SyncAuthError, RedactURL(err.Error())
	case strings.Contains(detail, "could not resolve host"),
		strings.Contains(detail, "network is unreachable"),
		strings.Contains(detail, "connection timed out"),
		strings.Contains(detail, "connection refused"),
		strings.Contains(detail, "unable to access"):
		return SyncOffline, RedactURL(err.Error())
	default:
		return SyncError, RedactURL(err.Error())
	}
}
