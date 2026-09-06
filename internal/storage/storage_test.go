package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenSeedsDefaultTestAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !store.Authenticate("test", "test") {
		t.Fatalf("default test account did not authenticate")
	}
}

func TestOpenAddsDefaultTestAccountToExistingState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"accounts":{},"characters":{},"next_id":1}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !store.Authenticate("test", "test") {
		t.Fatalf("default test account did not authenticate")
	}
}

func TestOpenDoesNotOverwriteExistingTestAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"accounts":{"test":{"username":"test","password":"custom"}},"characters":{},"next_id":1}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !store.Authenticate("test", "custom") {
		t.Fatalf("existing test account password was overwritten")
	}
	if store.Authenticate("test", "test") {
		t.Fatalf("existing test account unexpectedly accepts default password")
	}
}

func TestInsertCharacterPersistsCharacterFields(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	ch, err := store.InsertCharacter(Character{
		Account:  "test",
		Name:     "tester",
		Class:    "warrior",
		Level:    1,
		HomeMap:  "0",
		HomeX:    0,
		HomeY:    0,
		MapID:    "0",
		X:        0,
		Y:        0,
		MaxHP:    19,
		HP:       19,
		MaxMP:    15,
		MP:       15,
		BagItems: []UserItem{{ItemID: "木剑"}},
	})
	if err != nil {
		t.Fatalf("InsertCharacter() error = %v", err)
	}
	if ch.HP != 19 || ch.MaxHP != 19 {
		t.Fatalf("HP/MaxHP = %d/%d, want 19/19", ch.HP, ch.MaxHP)
	}
	if ch.MP != 15 || ch.MaxMP != 15 {
		t.Fatalf("MP/MaxMP = %d/%d, want 15/15", ch.MP, ch.MaxMP)
	}
}

func TestSaveCharacterRestoresMemoryOnPersistenceFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	original, err := store.InsertCharacter(Character{
		Account: "test", Name: "rollback", Class: "wizard", MapID: "0", MaxHP: 100, HP: 100, MaxMP: 50, MP: 50,
	})
	if err != nil {
		t.Fatalf("InsertCharacter() error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	updated := original
	updated.HP = 1
	if err := store.SaveCharacter(updated); err == nil {
		t.Fatal("SaveCharacter() error = nil, want persistence failure")
	}
	got, ok := store.Character(original.ID)
	if !ok || got.HP != original.HP {
		t.Fatalf("in-memory character = %+v (found=%t), want original HP %d", got, ok, original.HP)
	}
}

func TestCharacterSkillsAcceptLegacyStringArray(t *testing.T) {
	var ch Character
	if err := json.Unmarshal([]byte(`{"skills":["火球术","治愈术"]}`), &ch); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got, want := len(ch.Skills), 2; got != want {
		t.Fatalf("len(Skills) = %d, want %d", got, want)
	}
	if ch.Skills[0].ID != "火球术" || ch.Skills[1].ID != "治愈术" {
		t.Fatalf("Skills = %+v, want legacy IDs preserved", ch.Skills)
	}
	if ch.Skills[0].Level != 0 || ch.Skills[0].Train != 0 || ch.Skills[0].Hotkey != 0 {
		t.Fatalf("legacy skill defaults = %+v, want zeroed state", ch.Skills[0])
	}
	b, err := json.Marshal(ch.Skills)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if string(b) != `[{"id":"火球术"},{"id":"治愈术"}]` {
		t.Fatalf("json.Marshal() = %s, want object array", string(b))
	}
}

func TestCharacterStatusTimesPersistAsRemainingMilliseconds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	expires := time.Now().Add(30 * time.Second).UnixNano()
	inserted, err := store.InsertCharacter(Character{
		Account: "test", Name: "status-time", Class: "warrior", MapID: "0", MaxHP: 1, HP: 1, MaxMP: 1, MP: 1,
		DefenceUpUntil: expires, MagDefenceUpUntil: expires, BubbleDefenceLevel: 3, BubbleDefenceUntil: expires, TransparentUntil: expires, ParalyzedUntil: expires,
		ShowHPUntil: expires,
		IncHealth:   11, IncSpell: 22, IncHealing: 33, SpellBlocked: true, TargetID: "runtime-target", Sitting: true, MapMoveAt: expires,
		ThrustingDisabled: true, HalfMoonDisabled: true, AdminMode: true, StoneMode: true, PKFlag: true, FreePKArea: true,
		IncHealthSpellAt:  time.Now().Add(-time.Minute).UnixMilli(),
		PoisonHealthLevel: 9,
		PoisonHealthUntil: expires, PoisonHealthStartAt: time.Now().UnixNano(), PoisonHealthTickAt: time.Now().UnixNano(),
		PoisonArmorLevel: 7, PoisonArmorUntil: expires, PoisonArmorStartAt: time.Now().UnixNano(),
	})
	if err != nil {
		t.Fatalf("InsertCharacter() error = %v", err)
	}
	var raw struct {
		Characters map[string]struct {
			DefenceUpUntil      int64  `json:"defence_up_until"`
			MagDefenceUpUntil   int64  `json:"mag_defence_up_until"`
			ParalyzedUntil      int64  `json:"paralyzed_until"`
			BubbleDefenceLevel  byte   `json:"bubble_defence_level"`
			IncHealth           int    `json:"inc_health"`
			IncSpell            int    `json:"inc_spell"`
			IncHealing          int    `json:"inc_healing"`
			SpellBlocked        bool   `json:"spell_blocked"`
			TargetID            string `json:"target_id"`
			Sitting             bool   `json:"sitting"`
			MapMoveAt           int64  `json:"map_move_at"`
			ThrustingDisabled   bool   `json:"thrusting_disabled"`
			HalfMoonDisabled    bool   `json:"half_moon_disabled"`
			AdminMode           bool   `json:"admin_mode"`
			StoneMode           bool   `json:"stone_mode"`
			PKFlag              bool   `json:"pk_flag"`
			FreePKArea          bool   `json:"free_pk_area"`
			IncHealthSpellAt    int64  `json:"inc_health_spell_at"`
			PoisonHealthLevel   byte   `json:"poison_health_level"`
			PoisonArmorLevel    byte   `json:"poison_armor_level"`
			PoisonHealthStartAt int64  `json:"poison_health_start_at"`
			PoisonHealthTickAt  int64  `json:"poison_health_tick_at"`
			ShowHPUntil         int64  `json:"show_hp_until"`
			StatusTimeFormat    string `json:"status_time_format"`
		} `json:"characters"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	disk := raw.Characters[inserted.ID]
	if disk.StatusTimeFormat != characterStatusTimeFormat || disk.DefenceUpUntil <= 0 || disk.DefenceUpUntil > 30000 || disk.MagDefenceUpUntil <= 0 || disk.MagDefenceUpUntil > 30000 || disk.ParalyzedUntil <= 0 || disk.ParalyzedUntil > 30000 || disk.BubbleDefenceLevel != 0 || disk.IncHealth != 11 || disk.IncSpell != 22 || disk.IncHealing != 33 || disk.SpellBlocked || disk.TargetID != "" || disk.Sitting || disk.MapMoveAt != 0 || disk.ThrustingDisabled || disk.HalfMoonDisabled || disk.AdminMode || disk.StoneMode || disk.PKFlag || disk.FreePKArea || disk.IncHealthSpellAt != 0 || disk.PoisonHealthLevel != 0 || disk.PoisonArmorLevel != 0 || disk.PoisonHealthStartAt != 0 || disk.PoisonHealthTickAt != 0 || disk.ShowHPUntil != 0 {
		t.Fatalf("persisted status = %+v, want remaining milliseconds", disk)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen Open() error = %v", err)
	}
	loaded, ok := reopened.Character(inserted.ID)
	if !ok || loaded.DefenceUpUntil <= time.Now().Add(28*time.Second).UnixNano() || loaded.DefenceUpUntil > time.Now().Add(31*time.Second).UnixNano() {
		t.Fatalf("loaded DefenceUpUntil = %d (found=%t), want about 30 seconds remaining", loaded.DefenceUpUntil, ok)
	}
	if loaded.MagDefenceUpUntil <= time.Now().UnixNano() || loaded.ParalyzedUntil <= time.Now().UnixNano() {
		t.Fatalf("loaded status deadlines = mag:%d paralysis:%d, want active remaining statuses", loaded.MagDefenceUpUntil, loaded.ParalyzedUntil)
	}
	if loaded.PoisonHealthLevel != 0 {
		t.Fatalf("loaded PoisonHealthLevel = %d, want reset runtime poison point", loaded.PoisonHealthLevel)
	}
	if loaded.BubbleDefenceLevel != 0 {
		t.Fatalf("loaded BubbleDefenceLevel = %d, want reset runtime shield level", loaded.BubbleDefenceLevel)
	}
	if loaded.BubbleDefenceUntil <= time.Now().UnixNano() {
		t.Fatalf("loaded BubbleDefenceUntil = %d, want remaining status time", loaded.BubbleDefenceUntil)
	}
	if loaded.BubbleDefenceActive == nil || *loaded.BubbleDefenceActive {
		t.Fatalf("loaded BubbleDefenceActive = %v, want inactive runtime shield", loaded.BubbleDefenceActive)
	}
	if loaded.TransparentUntil <= time.Now().UnixNano() {
		t.Fatalf("loaded TransparentUntil = %d, want remaining status time", loaded.TransparentUntil)
	}
	if loaded.TransparentHideMode == nil || *loaded.TransparentHideMode {
		t.Fatalf("loaded TransparentHideMode = %v, want inactive runtime hide mode", loaded.TransparentHideMode)
	}
	if loaded.IncHealthSpellAt != 0 {
		t.Fatalf("loaded IncHealthSpellAt = %d, want reset runtime recovery timer", loaded.IncHealthSpellAt)
	}
	if loaded.IncHealth != 11 || loaded.IncSpell != 22 || loaded.IncHealing != 33 {
		t.Fatalf("loaded recovery queues = %d/%d/%d, want 11/22/33", loaded.IncHealth, loaded.IncSpell, loaded.IncHealing)
	}
	if loaded.SpellBlocked {
		t.Fatal("loaded SpellBlocked = true, want runtime spell gate reset")
	}
	if loaded.TargetID != "" {
		t.Fatalf("loaded TargetID = %q, want runtime target reset", loaded.TargetID)
	}
	if loaded.Sitting {
		t.Fatal("loaded Sitting = true, want runtime sit state reset")
	}
	if loaded.MapMoveAt != 0 {
		t.Fatalf("loaded MapMoveAt = %d, want runtime map-move timer reset", loaded.MapMoveAt)
	}
	if loaded.ThrustingDisabled || loaded.HalfMoonDisabled {
		t.Fatalf("loaded attack toggles = %t/%t, want runtime toggles reset", loaded.ThrustingDisabled, loaded.HalfMoonDisabled)
	}
	if loaded.AdminMode || loaded.StoneMode || loaded.PKFlag || loaded.FreePKArea {
		t.Fatalf("loaded runtime flags = admin:%t stone:%t pk:%t free-pk:%t, want reset", loaded.AdminMode, loaded.StoneMode, loaded.PKFlag, loaded.FreePKArea)
	}
	if loaded.ShowHPUntil != 0 {
		t.Fatalf("loaded ShowHPUntil = %d, want reset runtime show-health state", loaded.ShowHPUntil)
	}
	if loaded.PoisonArmorLevel != 0 {
		t.Fatalf("loaded PoisonArmorLevel = %d, want reset runtime poison point", loaded.PoisonArmorLevel)
	}
}
