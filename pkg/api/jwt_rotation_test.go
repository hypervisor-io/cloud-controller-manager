package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitForToken polls until the loader reports want, or the deadline passes.
// Polling rather than a fixed sleep keeps the test fast in the common case
// and tolerant of inotify delivery jitter.
func waitForToken(t *testing.T, tl *TokenLoader, want string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if tl.Token() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("token never became %q (still %q after %s)", want, tl.Token(), within)
}

// A Kubernetes projected serviceaccount token is rotated by writing a new
// timestamped directory and atomically swapping the `..data` symlink, which
// REPLACES the token path rather than writing it in place.
//
// The original implementation called w.Add(path) on the file itself. An
// inotify watch follows the inode, so it went deaf as soon as the path was
// replaced: the first rotation was observed and every rotation after it was
// silently missed, leaving the CCM authenticating with a credential that
// eventually expires. Reverting jwt.go's w.Add(dir) to w.Add(path) fails
// this test on the SECOND rotation while still passing the first, which is
// exactly the shape of the bug.
func TestTokenLoader_SurvivesRepeatedAtomicReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("token-v1"), 0600); err != nil {
		t.Fatal(err)
	}

	tl, err := NewTokenLoader(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := tl.Token(); got != "token-v1" {
		t.Fatalf("initial load: got %q, want token-v1", got)
	}

	// Three consecutive rotations. One is not enough to catch the defect.
	for _, want := range []string{"token-v2", "token-v3", "token-v4"} {
		staging := filepath.Join(dir, want+".staging")
		if err := os.WriteFile(staging, []byte(want), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(staging, path); err != nil {
			t.Fatal(err)
		}
		waitForToken(t, tl, want, 5*time.Second)
	}
}

// An in-place rewrite must also be picked up. This is the path the original
// TestTokenLoader_LoadAndReload exercised, and it failed for a second,
// independent reason: watch() registered the watcher inside the goroutine,
// so a write that landed before w.Add() ran produced no event and was never
// reconciled. NewTokenLoader now registers the watch before returning.
func TestTokenLoader_ReloadsOnInPlaceWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("token-v1"), 0600); err != nil {
		t.Fatal(err)
	}

	tl, err := NewTokenLoader(path)
	if err != nil {
		t.Fatal(err)
	}

	// Written immediately, with no settling delay, to keep the constructor
	// race in scope.
	if err := os.WriteFile(path, []byte("token-v2"), 0600); err != nil {
		t.Fatal(err)
	}
	waitForToken(t, tl, "token-v2", 5*time.Second)
}

// A truncated or empty file observed mid-rotation must never replace a good
// credential: authenticating with "" fails every request until the next
// event, and on a volume that delivers no further events, forever.
func TestTokenLoader_IgnoresEmptyToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("token-v1"), 0600); err != nil {
		t.Fatal(err)
	}

	tl, err := NewTokenLoader(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("   \n"), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)

	if got := tl.Token(); got != "token-v1" {
		t.Fatalf("empty file replaced a live token: got %q, want token-v1 retained", got)
	}
}

// The constructor must still fail loudly when the token is absent at start:
// booting with no credential at all is a configuration error, not something
// to paper over with the resync loop.
func TestTokenLoader_ConstructorFailsWhenMissing(t *testing.T) {
	if _, err := NewTokenLoader(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected an error for a missing token file, got nil")
	}
}
