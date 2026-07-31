package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"k8s.io/klog/v2"
)

// resyncInterval is a belt-and-braces re-read of the token file. inotify is
// not guaranteed: events are dropped when the kernel queue overflows, and
// some volume types deliver nothing at all. Rotation is infrequent, so
// polling this slowly costs nothing and turns a missed event into a bounded
// delay rather than an outage lasting until the pod restarts.
const resyncInterval = 30 * time.Second

type TokenLoader struct {
	mu    sync.RWMutex
	path  string
	token string
}

func NewTokenLoader(path string) (*TokenLoader, error) {
	tl := &TokenLoader{path: path}
	if err := tl.reload(); err != nil {
		return nil, err
	}

	// The watcher is created and registered SYNCHRONOUSLY, before returning.
	// Starting it inside the goroutine meant any write landing between the
	// constructor returning and w.Add() running was missed permanently:
	// there is no event for something that changed before the watch existed,
	// and nothing re-read the file afterwards.
	w, err := fsnotify.NewWatcher()
	if err != nil {
		// A missing watcher must not be fatal - the resync loop still picks
		// rotations up, just less promptly.
		klog.Errorf("fsnotify watcher: %v (falling back to %s polling)", err, resyncInterval)
		go tl.resyncLoop()
		return tl, nil
	}

	// Watch the DIRECTORY, not the file. An inotify watch follows the inode,
	// so it goes deaf the moment the path is replaced rather than written in
	// place - which is exactly how Kubernetes rotates a projected
	// serviceaccount token or a mounted secret: it writes a new timestamped
	// directory and atomically swaps the `..data` symlink. Watching the file
	// therefore survived at most one rotation and then silently stopped
	// updating, leaving the process pinned to a credential that eventually
	// expires. The parent directory's inode is stable across that swap.
	dir := filepath.Dir(path)
	if err := w.Add(dir); err != nil {
		klog.Errorf("watch %s: %v (falling back to %s polling)", dir, err, resyncInterval)
		w.Close()
		go tl.resyncLoop()
		return tl, nil
	}

	go tl.watch(w)
	go tl.resyncLoop()
	return tl, nil
}

// NewFakeTokenLoader constructs a TokenLoader with a fixed token (test helper).
func NewFakeTokenLoader(t string) *TokenLoader {
	return &TokenLoader{token: t}
}

func (t *TokenLoader) Token() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.token
}

func (t *TokenLoader) reload() error {
	b, err := os.ReadFile(t.path)
	if err != nil {
		return fmt.Errorf("read token: %w", err)
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		// Refuse to install an empty token. Rotation is not atomic from the
		// reader's point of view on every volume type, and briefly observing
		// a truncated file must not replace a working credential with "".
		return fmt.Errorf("read token: %s is empty", t.path)
	}
	t.mu.Lock()
	t.token = token
	t.mu.Unlock()
	return nil
}

func (t *TokenLoader) watch(w *fsnotify.Watcher) {
	defer w.Close()

	base := filepath.Base(t.path)
	for ev := range w.Events {
		// Directory-level events arrive for every child. Take the token file
		// itself and the `..data` symlink swap Kubernetes performs; ignore
		// the rest of the churn in the volume.
		name := filepath.Base(ev.Name)
		if name != base && name != "..data" {
			continue
		}
		if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename|fsnotify.Chmod) == 0 {
			continue
		}
		if err := t.reload(); err != nil {
			// Mid-rotation the path can be absent for an instant. The next
			// event and the resync loop both retry, so this is not an error
			// state.
			klog.V(4).Infof("token reload after %s: %v", ev.Op, err)
			continue
		}
		klog.Info("token reloaded")
	}
}

func (t *TokenLoader) resyncLoop() {
	for range time.Tick(resyncInterval) {
		before := t.Token()
		if err := t.reload(); err != nil {
			klog.V(4).Infof("token resync: %v", err)
			continue
		}
		if t.Token() != before {
			klog.Info("token reloaded (resync)")
		}
	}
}
