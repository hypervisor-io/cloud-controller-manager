package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTokenLoader_LoadAndReload(t *testing.T) {
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
		t.Errorf("got %q, want token-v1", got)
	}

	if err := os.WriteFile(path, []byte("token-v2"), 0600); err != nil {
		t.Fatal(err)
	}
	// Allow fsnotify a moment
	time.Sleep(200 * time.Millisecond)
	if got := tl.Token(); got != "token-v2" {
		t.Errorf("after reload got %q, want token-v2", got)
	}
}
