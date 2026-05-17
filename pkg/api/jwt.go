package api

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"k8s.io/klog/v2"
)

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
	go tl.watch()
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
	t.mu.Lock()
	t.token = strings.TrimSpace(string(b))
	t.mu.Unlock()
	return nil
}

func (t *TokenLoader) watch() {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		klog.Errorf("fsnotify watcher: %v", err)
		return
	}
	defer w.Close()
	if err := w.Add(t.path); err != nil {
		klog.Errorf("watch %s: %v", t.path, err)
		return
	}
	for ev := range w.Events {
		if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
			if err := t.reload(); err != nil {
				klog.Errorf("token reload: %v", err)
			} else {
				klog.Info("token reloaded")
			}
		}
	}
}
