package data

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"openmir2/internal/npc"
)

func LoadConfigsWithReport(dir string) (StdBundle, StdLoadReport, error) {
	var report StdLoadReport
	items, itemOrder, err := loadConfigItems(filepath.Join(dir, "items"))
	if err != nil {
		return StdBundle{}, report, err
	}
	skills, err := loadConfigSkills(filepath.Join(dir, "skills"))
	if err != nil {
		return StdBundle{}, report, err
	}
	maps, spawnRecords, err := loadConfigMaps(filepath.Join(dir, "maps"))
	if err != nil {
		return StdBundle{}, report, err
	}
	monsters, drops, err := loadConfigMonsters(filepath.Join(dir, "monsters"), items, &report)
	if err != nil {
		return StdBundle{}, report, err
	}
	spawns := loadConfigSpawns(spawnRecords, monsters, &report)
	makeItems, err := loadConfigMakeItems(filepath.Join(dir, "items_make.json"))
	if err != nil {
		return StdBundle{}, report, err
	}
	npcs, err := loadConfigNPCs(filepath.Join(dir, "npcs"))
	if err != nil {
		return StdBundle{}, report, err
	}
	b := StdBundle{
		Items:     items,
		ItemOrder: append([]string(nil), itemOrder...),
		Skills:    skills,
		Monsters:  monsters,
		Drops:     drops,
		Maps:      maps,
		Spawns:    spawns,
		MakeItems: makeItems,
		NPCs:      npcs,
	}
	if err := b.Validate(); err != nil {
		return StdBundle{}, report, err
	}
	return b, report, nil
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

func loadConfigNPCs(dir string) (npc.Library, error) {
	lib, err := npc.Load(dir)
	if err != nil {
		return npc.Library{}, err
	}
	return lib, nil
}

func loadConfigMakeItems(path string) (map[string][]StdMakeIngredient, error) {
	var recipes map[string][]StdMakeIngredient
	if err := loadJSON(path, &recipes); err != nil {
		return nil, err
	}
	if recipes == nil {
		recipes = map[string][]StdMakeIngredient{}
	}
	return recipes, nil
}

type monsterAttributesConfig struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	Race               int               `json:"race"`
	RaceImg            int               `json:"race_img"`
	Appr               int               `json:"appr"`
	Level              int               `json:"level"`
	Undead             int               `json:"undead"`
	CoolEye            int               `json:"cool_eye"`
	Animal             bool              `json:"animal"`
	FleeOnSight        bool              `json:"flee_on_sight"`
	UseMagic           bool              `json:"use_magic"`
	Hidden             bool              `json:"hidden"`
	FixedHideMode      bool              `json:"fixed_hide_mode"`
	StoneMode          bool              `json:"stone_mode"`
	FirstRevealPending bool              `json:"first_reveal_pending"`
	AttackMax          int               `json:"attack_max"`
	ViewRange          int               `json:"view_range"`
	LeashRange         int               `json:"leash_range"`
	SearchNoTargetMS   int               `json:"search_no_target_ms"`
	SearchHasTargetMS  int               `json:"search_has_target_ms"`
	Experience         int               `json:"experience"`
	HP                 int               `json:"hp"`
	MP                 int               `json:"mp"`
	Defense            int               `json:"defense"`
	MagicDefense       int               `json:"magic_defense"`
	MinAttack          int               `json:"min_attack"`
	MaxAttack          int               `json:"max_attack"`
	MagicAttack        int               `json:"magic_attack"`
	TaoAttack          int               `json:"tao_attack"`
	Speed              int               `json:"speed"`
	Hit                int               `json:"hit"`
	WalkSpeedMS        int               `json:"walk_speed_ms"`
	WalkStep           int               `json:"walk_step"`
	WalkWait           int               `json:"walk_wait"`
	AttackIntervalMS   int               `json:"attack_interval_ms"`
	Drops              []dropEntryConfig `json:"drops"`
}

type dropEntryConfig struct {
	ItemID   string `json:"item_id"`
	Chance   string `json:"chance"`
	MinCount int    `json:"min_count"`
	MaxCount int    `json:"max_count"`
}

type spawnRecord struct {
	File     string
	MapID    string
	Index    int
	StdSpawn StdMapSpawn
}

func loadConfigItems(dir string) (map[string]StdItem, []string, error) {
	var manifest []string
	if err := loadJSON(filepath.Join(dir, "items.json"), &manifest); err != nil {
		return nil, nil, err
	}
	items, err := loadConfigItemsFromManifest(dir, manifest)
	if err != nil {
		return nil, nil, err
	}
	return items, manifest, nil
}

func loadConfigSkills(dir string) (map[string]StdSkill, error) {
	files, err := jsonFiles(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]StdSkill{}
	for _, path := range files {
		var skill StdSkill
		if err := loadJSON(path, &skill); err != nil {
			return nil, err
		}
		if skill.ID == "" {
			return nil, fmt.Errorf("%s: skill id is required", path)
		}
		if skill.Name == "" {
			return nil, fmt.Errorf("%s: skill name is required", path)
		}
		if _, ok := out[skill.ID]; ok {
			return nil, fmt.Errorf("duplicate skill id %s", skill.ID)
		}
		out[skill.ID] = skill
	}
	return out, nil
}

func loadConfigMaps(dir string) (map[string]StdMap, []spawnRecord, error) {
	files, err := jsonFiles(dir)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]StdMap{}
	var spawnRecords []spawnRecord
	for _, path := range files {
		var mp StdMap
		if err := loadJSON(path, &mp); err != nil {
			return nil, nil, err
		}
		if mp.ID == "" {
			return nil, nil, fmt.Errorf("%s: map id is required", path)
		}
		if _, ok := out[mp.ID]; ok {
			return nil, nil, fmt.Errorf("duplicate map id %s", mp.ID)
		}
		if mp.Connections == nil {
			mp.Connections = []StdMapConnection{}
		}
		if mp.Spawns == nil {
			mp.Spawns = []StdMapSpawn{}
		}
		for i, sp := range mp.Spawns {
			if sp.MonsterID == "" {
				return nil, nil, fmt.Errorf("%s: spawn monster_id is required", path)
			}
			spawnRecords = append(spawnRecords, spawnRecord{
				File:     path,
				MapID:    mp.ID,
				Index:    i,
				StdSpawn: sp,
			})
		}
		if mp.StartPoints == nil {
			mp.StartPoints = []StdStartPoint{}
		}
		out[mp.ID] = mp
	}
	return out, spawnRecords, nil
}

func loadConfigMonsters(dir string, items map[string]StdItem, report *StdLoadReport) (map[string]StdMonster, map[string]StdDropTable, error) {
	files, err := jsonFiles(dir)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]StdMonster{}
	drops := map[string]StdDropTable{}
	for _, path := range files {
		var cfg monsterAttributesConfig
		if err := loadJSON(path, &cfg); err != nil {
			return nil, nil, err
		}
		if cfg.ID == "" {
			return nil, nil, fmt.Errorf("%s: monster id is required", path)
		}
		if _, ok := out[cfg.ID]; ok {
			return nil, nil, fmt.Errorf("duplicate monster id %s", cfg.ID)
		}
		out[cfg.ID] = StdMonster{
			ID:                 cfg.ID,
			Name:               cfg.Name,
			Race:               cfg.Race,
			RaceImg:            cfg.RaceImg,
			Appr:               cfg.Appr,
			Level:              cfg.Level,
			Undead:             cfg.Undead,
			CoolEye:            cfg.CoolEye,
			Animal:             cfg.Animal,
			FleeOnSight:        cfg.FleeOnSight,
			UseMagic:           cfg.UseMagic,
			Hidden:             cfg.Hidden,
			FixedHideMode:      cfg.FixedHideMode,
			StoneMode:          cfg.StoneMode,
			FirstRevealPending: cfg.FirstRevealPending,
			AttackMax:          cfg.AttackMax,
			ViewRange:          cfg.ViewRange,
			LeashRange:         cfg.LeashRange,
			SearchNoTargetMS:   cfg.SearchNoTargetMS,
			SearchHasTargetMS:  cfg.SearchHasTargetMS,
			HP:                 cfg.HP,
			MP:                 cfg.MP,
			MinAttack:          cfg.MinAttack,
			MaxAttack:          cfg.MaxAttack,
			Defense:            cfg.Defense,
			MagicDefense:       cfg.MagicDefense,
			MagicAttack:        cfg.MagicAttack,
			TaoAttack:          cfg.TaoAttack,
			Experience:         cfg.Experience,
			Speed:              cfg.Speed,
			Hit:                cfg.Hit,
			WalkSpeedMS:        cfg.WalkSpeedMS,
			WalkStep:           cfg.WalkStep,
			WalkWait:           cfg.WalkWait,
			AttackIntervalMS:   cfg.AttackIntervalMS,
		}
		if len(cfg.Drops) > 0 {
			table := StdDropTable{ID: cfg.ID}
			for _, entry := range cfg.Drops {
				if _, ok := items[entry.ItemID]; !ok {
					report.Skipped = append(report.Skipped, StdLoadSkip{
						Kind:   "drop_entry",
						File:   path,
						ID:     cfg.ID + " -> " + entry.ItemID,
						Reason: "missing item",
					})
					continue
				}
				chance, err := parseChance(entry.Chance)
				if err != nil {
					return nil, nil, fmt.Errorf("%s: %w", path, err)
				}
				table.Entries = append(table.Entries, StdDropEntry{
					ItemID:   entry.ItemID,
					Chance:   chance,
					MinCount: entry.MinCount,
					MaxCount: entry.MaxCount,
				})
			}
			drops[cfg.ID] = table
		}
	}
	return out, drops, nil
}

func loadConfigSpawns(records []spawnRecord, monsters map[string]StdMonster, report *StdLoadReport) []StdSpawn {
	var out []StdSpawn
	for _, rec := range records {
		if _, ok := monsters[rec.StdSpawn.MonsterID]; !ok {
			report.Skipped = append(report.Skipped, StdLoadSkip{
				Kind:   "spawn",
				File:   rec.File,
				MapID:  rec.MapID,
				ID:     fmt.Sprintf("%s:%03d:%s", rec.MapID, rec.Index, rec.StdSpawn.MonsterID),
				Reason: "missing monster attributes",
			})
			continue
		}
		out = append(out, StdSpawn{
			ID:             fmt.Sprintf("%s:%03d:%s", rec.MapID, rec.Index, rec.StdSpawn.MonsterID),
			MapID:          rec.MapID,
			MonsterID:      rec.StdSpawn.MonsterID,
			X:              rec.StdSpawn.X,
			Y:              rec.StdSpawn.Y,
			Range:          rec.StdSpawn.Range,
			Count:          rec.StdSpawn.Count,
			RespawnSeconds: rec.StdSpawn.RespawnSeconds,
			MissionGenRate: rec.StdSpawn.MissionGenRate,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func jsonFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		out = append(out, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(out)
	return out, nil
}

func jsonFilesRecursive(dir string) ([]string, error) {
	var out []string
	if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(d.Name()) != ".json" || d.Name() == "items.json" {
			return nil
		}
		out = append(out, path)
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func loadConfigItemsFromManifest(dir string, manifest []string) (map[string]StdItem, error) {
	files, err := jsonFilesRecursive(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]StdItem{}
	seen := map[string]struct{}{}
	for _, path := range files {
		var item StdItem
		if err := loadJSON(path, &item); err != nil {
			return nil, err
		}
		name := item.Name
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		if name == "" {
			return nil, fmt.Errorf("%s: item name is required", path)
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("duplicate item id %s", name)
		}
		item.ID = name
		item.Name = name
		if item.Kind == "" {
			return nil, fmt.Errorf("%s: item kind is required", path)
		}
		out[item.ID] = item
		seen[name] = struct{}{}
	}
	for _, name := range manifest {
		if name == "" {
			continue
		}
		if _, ok := out[name]; !ok {
			return nil, fmt.Errorf("missing item file for %s", name)
		}
	}
	return out, nil
}

func parseChance(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("chance is required")
	}
	if before, after, ok := strings.Cut(value, "/"); ok {
		n, err := strconv.ParseFloat(strings.TrimSpace(before), 64)
		if err != nil {
			return 0, fmt.Errorf("invalid chance %q", value)
		}
		d, err := strconv.ParseFloat(strings.TrimSpace(after), 64)
		if err != nil || d == 0 {
			return 0, fmt.Errorf("invalid chance %q", value)
		}
		return n / d, nil
	}
	chance, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid chance %q", value)
	}
	return chance, nil
}
