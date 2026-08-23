package npc

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

func Load(dir string) (Library, error) {
	lib := Library{
		Entities: map[string]Entity{},
		Scripts:  map[string]Script{},
	}
	if dir == "" {
		return lib, nil
	}
	files, err := jsonFiles(dir)
	if err != nil {
		return Library{}, err
	}
	for _, path := range files {
		cfg, err := loadFile(path)
		if err != nil {
			return Library{}, err
		}
		if cfg.ID == "" {
			return Library{}, fmt.Errorf("%s: npc id is required", path)
		}
		cfg.Kind = NormalizeKind(cfg.Kind)
		if _, ok := lib.Entities[cfg.ID]; ok {
			return Library{}, fmt.Errorf("duplicate npc id %s", cfg.ID)
		}
		if len(cfg.Labels) > 0 {
			if _, ok := lib.Scripts[cfg.ID]; ok {
				return Library{}, fmt.Errorf("duplicate npc script id %s", cfg.ID)
			}
			cfg.ScriptID = cfg.ID
			lib.Scripts[cfg.ID] = Script{
				ID:     cfg.ID,
				Labels: cfg.Labels,
			}
		}
		lib.Entities[cfg.ID] = cfg.Entity
	}
	if err := lib.Validate(); err != nil {
		return Library{}, err
	}
	return lib, nil
}

type fileConfig struct {
	Entity
	Labels map[string]Label `json:"labels,omitempty"`
}

func loadFile(path string) (fileConfig, error) {
	var cfg fileConfig
	if err := loadJSON(path, &cfg); err != nil {
		return fileConfig{}, err
	}
	if cfg.Labels == nil {
		cfg.Labels = map[string]Label{}
	}
	return cfg, nil
}

func loadJSON(path string, out any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func jsonFiles(dir string) ([]string, error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	files := make([]string, 0)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(d.Name()) != ".json" {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}
