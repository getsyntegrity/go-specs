package specs

import (
	"sync"
	"testing"
)

// SnapshotMatcher defines the function signature used for snapshot assertions.
type SnapshotMatcher func(testing.TB, any)

var (
	//nolint:unused // Retained for registered snapshot extensions.
	defaultSnapshotMatcher SnapshotMatcher
	snapshotMatcherLock    sync.RWMutex
)

// RegisterSnapshotMatcher allows extensions to provide snapshot matching. Pass nil to clear the handler.
func RegisterSnapshotMatcher(fn SnapshotMatcher) {
	snapshotMatcherLock.Lock()
	defer snapshotMatcherLock.Unlock()
	defaultSnapshotMatcher = fn
}

//nolint:unused // Retained for registered snapshot extensions.
func currentSnapshotMatcher() SnapshotMatcher {
	snapshotMatcherLock.RLock()
	defer snapshotMatcherLock.RUnlock()
	return defaultSnapshotMatcher
}

//nolint:unused // Retained for registered snapshot extensions.
func enforceSnapshotMatcher(t testing.TB, handler SnapshotMatcher, actual any) {
	if handler == nil {
		t.Fatalf("snapshot matcher not registered; import github.com/pablogore/go-specs/snapshots and register if using custom matcher")
		return
	}
	handler(t, actual)
}
