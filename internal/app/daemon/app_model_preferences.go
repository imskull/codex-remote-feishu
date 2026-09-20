package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type modelPreferencesRuntimeState struct {
	path  string
	saved map[string]state.ModelConfigRecord
}

type modelPreferencesFile struct {
	Version int                                `json:"version"`
	Entries map[string]state.ModelConfigRecord `json:"entries"`
}

func (a *App) configureModelPreferencesLocked(stateDir string) {
	if stateDir == "" {
		return
	}
	path := filepath.Join(stateDir, "model-preferences.json")
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		log.Printf("load model preferences failed: %v", err)
		return
	}
	if err == nil {
		var stored modelPreferencesFile
		if err := json.Unmarshal(raw, &stored); err != nil {
			log.Printf("load model preferences failed: %v", err)
			return
		}
		if stored.Version != 1 {
			log.Printf("unsupported model preferences version: %d", stored.Version)
			return
		}
		a.service.MaterializeModelPreferences(stored.Entries)
	}
	a.modelPreferences = modelPreferencesRuntimeState{path: path, saved: a.service.ModelPreferences()}
}

func (a *App) syncModelPreferencesLocked() {
	if a.modelPreferences.path == "" {
		return
	}
	entries := a.service.ModelPreferences()
	if maps.Equal(entries, a.modelPreferences.saved) {
		return
	}
	if err := saveModelPreferences(a.modelPreferences.path, entries); err != nil {
		log.Printf("save model preferences failed: %v", err)
		return
	}
	a.modelPreferences.saved = entries
}

func saveModelPreferences(path string, entries map[string]state.ModelConfigRecord) error {
	raw, err := json.MarshalIndent(modelPreferencesFile{Version: 1, Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), "model-preferences-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace model preferences: %w", err)
	}
	return nil
}
