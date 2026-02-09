package dialog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// Loader loads and optionally hot-reloads dialog definitions from YAML files.
type Loader struct {
	dir string

	mu      sync.RWMutex
	dialogs map[string]*StateMachine
}

// NewLoader creates a new dialog loader for the given directory.
func NewLoader(dir string) *Loader {
	return &Loader{
		dir:     dir,
		dialogs: make(map[string]*StateMachine),
	}
}

// LoadAll loads all .yaml and .yml files from the configured directory.
func (l *Loader) LoadAll() (map[string]*StateMachine, error) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, fmt.Errorf("read dialog dir %q: %w", l.dir, err)
	}

	result := make(map[string]*StateMachine)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(l.dir, entry.Name())
		sm, err := l.loadFile(path)
		if err != nil {
			return nil, fmt.Errorf("load %q: %w", path, err)
		}
		result[sm.Dialog().Name] = sm
	}

	l.mu.Lock()
	l.dialogs = result
	l.mu.Unlock()

	return result, nil
}

// Get returns a loaded state machine by dialog name.
func (l *Loader) Get(name string) (*StateMachine, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	sm, ok := l.dialogs[name]
	return sm, ok
}

// All returns all loaded state machines.
func (l *Loader) All() map[string]*StateMachine {
	l.mu.RLock()
	defer l.mu.RUnlock()
	result := make(map[string]*StateMachine, len(l.dialogs))
	for k, v := range l.dialogs {
		result[k] = v
	}
	return result
}

func (l *Loader) loadFile(path string) (*StateMachine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var d Dialog
	if err := yaml.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}

	if d.Name == "" {
		d.Name = filepath.Base(path)
	}

	sm := NewStateMachine(&d)
	if err := sm.Validate(); err != nil {
		return nil, err
	}

	return sm, nil
}

// WatchAndReload starts watching the dialog directory for changes and reloads.
// This blocks until the done channel is closed.
func (l *Loader) WatchAndReload(done <-chan struct{}) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(l.dir); err != nil {
		return fmt.Errorf("watch dir %q: %w", l.dir, err)
	}

	for {
		select {
		case <-done:
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				ext := filepath.Ext(event.Name)
				if ext == ".yaml" || ext == ".yml" {
					l.LoadAll()
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			return err
		}
	}
}
