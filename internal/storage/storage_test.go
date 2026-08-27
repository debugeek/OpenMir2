package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
