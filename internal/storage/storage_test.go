package storage

import (
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
