package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestRemoveGuildMemberOnlyUpdatesGuildForOfflineCharacter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	member, err := store.InsertCharacter(Character{Name: "Offline", Account: "test", GuildID: "g1", GuildRank: 0, GuildAllianceID: "g2", GuildAllianceIDs: []string{"g2"}})
	if err != nil {
		t.Fatalf("InsertCharacter() error = %v", err)
	}
	if err := store.SaveGuild(Guild{ID: "g1", Ranks: []GuildRank{{Number: 0, Members: []string{"Offline"}}}}); err != nil {
		t.Fatalf("SaveGuild() error = %v", err)
	}
	updated, removed, err := store.RemoveGuildMember("g1", "Offline")
	if err != nil || !removed {
		t.Fatalf("RemoveGuildMember() = (%+v, %t, %v), want removed", updated, removed, err)
	}
	if !reflect.DeepEqual(updated, Character{}) {
		t.Fatalf("updated member = %+v, want no runtime character update", updated)
	}
	loaded, ok := store.Character(member.ID)
	if !ok || loaded.GuildID != "g1" || loaded.GuildAllianceID != "g2" || len(loaded.GuildAllianceIDs) != 1 || loaded.GuildAllianceIDs[0] != "g2" {
		t.Fatalf("stored member = %+v (found=%t), want original guild fields", loaded, ok)
	}
	guild, ok := store.Guild("g1")
	if !ok || len(guild.Ranks[0].Members) != 0 {
		t.Fatalf("stored guild = %+v (found=%t), want member removed", guild, ok)
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

func TestCharacterObjectOrderIsRuntimeOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	character, err := store.InsertCharacter(Character{Account: "test", Name: "runtime-order", Class: "warrior", ObjectOrder: 42})
	if err != nil {
		t.Fatalf("InsertCharacter() error = %v", err)
	}
	if character.ObjectOrder != 42 {
		t.Fatalf("in-memory ObjectOrder = %d, want 42", character.ObjectOrder)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen Open() error = %v", err)
	}
	if got, ok := reopened.Character(character.ID); !ok || got.ObjectOrder != 0 {
		t.Fatalf("reopened ObjectOrder = %d (found=%t), want runtime zero value", got.ObjectOrder, ok)
	}
}

func TestUpdateGuildAlliancePersistsOfflineMembers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	first, err := store.InsertCharacter(Character{Account: "test", Name: "alliance-first", GuildID: "guild-a", GuildAllianceID: ""})
	if err != nil {
		t.Fatalf("InsertCharacter(first) error = %v", err)
	}
	second, err := store.InsertCharacter(Character{Account: "test", Name: "alliance-second", GuildID: "guild-a", GuildAllianceID: ""})
	if err != nil {
		t.Fatalf("InsertCharacter(second) error = %v", err)
	}
	if err := store.UpdateGuildAlliance("guild-a", "guild-b"); err != nil {
		t.Fatalf("UpdateGuildAlliance() error = %v", err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	for _, id := range []string{first.ID, second.ID} {
		ch, ok := reopened.Character(id)
		if !ok || ch.GuildAllianceID != "guild-b" {
			t.Fatalf("character %s alliance = %q (found=%t), want guild-b", id, ch.GuildAllianceID, ok)
		}
	}
}

func TestSaveGuildAlliancePairDoesNotPersistOfflineMemberFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	member, err := store.InsertCharacter(Character{Name: "alliance-offline", GuildID: "guild-a", GuildAllianceID: "old", GuildAllianceIDs: []string{"old"}})
	if err != nil {
		t.Fatalf("InsertCharacter() error = %v", err)
	}
	if err := store.SaveGuildAlliancePair(Guild{ID: "guild-a"}, Guild{ID: "guild-b"}, []string{"guild-b"}, []string{"guild-a"}); err != nil {
		t.Fatalf("SaveGuildAlliancePair() error = %v", err)
	}
	stored, ok := store.Character(member.ID)
	if !ok || stored.GuildAllianceID != "old" || len(stored.GuildAllianceIDs) != 1 || stored.GuildAllianceIDs[0] != "old" {
		t.Fatalf("offline member after alliance update = %+v, want unchanged fields", stored)
	}
}

func TestDeleteCharacterKeepsReferenceNameTombstone(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	created, err := store.InsertCharacter(Character{Account: "test", Name: "deleted-name"})
	if err != nil {
		t.Fatalf("InsertCharacter() error = %v", err)
	}
	deleted, err := store.DeleteCharacter("test", "deleted-name")
	if err != nil || !deleted {
		t.Fatalf("DeleteCharacter() = (%t, %v), want success", deleted, err)
	}
	if len(store.Characters("test")) != 0 {
		t.Fatal("deleted character remained in the account list")
	}
	if !store.CharacterNameExists("deleted-name") {
		t.Fatal("deleted character name was released")
	}
	if _, err := store.InsertCharacter(Character{Account: "test", Name: "deleted-name"}); err == nil {
		t.Fatal("deleted character name was reusable")
	}
	if deleted, err := store.DeleteCharacter("test", "deleted-name"); err != nil || !deleted {
		t.Fatalf("second DeleteCharacter() = (%t, %v), want success", deleted, err)
	}
	stored, ok := store.Character(created.ID)
	if !ok || !stored.Deleted || stored.Selected {
		t.Fatalf("stored tombstone = %+v, want deleted and unselected", stored)
	}
}

func TestDisbandGuildClearsAllianceSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.SaveGuild(Guild{ID: "guild-a"}); err != nil {
		t.Fatalf("SaveGuild() error = %v", err)
	}
	member, err := store.InsertCharacter(Character{Account: "test", Name: "disband-alliance", GuildID: "guild-a", GuildAllianceID: "guild-b", GuildAllianceIDs: []string{"guild-b", "guild-c"}})
	if err != nil {
		t.Fatalf("InsertCharacter() error = %v", err)
	}
	if _, err := store.DisbandGuild("guild-a"); err != nil {
		t.Fatalf("DisbandGuild() error = %v", err)
	}
	updated, ok := store.Character(member.ID)
	if !ok || updated.GuildID != "" || updated.GuildAllianceID != "" || len(updated.GuildAllianceIDs) != 0 {
		t.Fatalf("disbanded member = %+v, want no guild or alliance", updated)
	}
}

func TestDeleteGuildPreservesCharacterSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.SaveGuild(Guild{ID: "guild-delete", Ranks: []GuildRank{{Number: 1, Name: "会长", Members: []string{"Leader"}}}}); err != nil {
		t.Fatalf("SaveGuild() error = %v", err)
	}
	leader, err := store.InsertCharacter(Character{Account: "test", Name: "Leader", GuildID: "guild-delete", GuildRank: 1, GuildRankName: "会长", GuildNotice: "notice"})
	if err != nil {
		t.Fatalf("InsertCharacter() error = %v", err)
	}
	if err := store.DeleteGuild("guild-delete"); err != nil {
		t.Fatalf("DeleteGuild() error = %v", err)
	}
	if _, ok := store.Guild("guild-delete"); ok {
		t.Fatal("deleted guild remained")
	}
	stored, ok := store.Character(leader.ID)
	if !ok || stored.GuildID != "guild-delete" || stored.GuildRank != 1 || stored.GuildRankName != "会长" || stored.GuildNotice != "notice" {
		t.Fatalf("character snapshot = %+v, want unchanged guild fields", stored)
	}
}

func TestAddGuildWarPersistsSymmetricRelation(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.SaveGuild(Guild{ID: "guild-a"}); err != nil {
		t.Fatalf("SaveGuild(a) error = %v", err)
	}
	if err := store.SaveGuild(Guild{ID: "guild-b"}); err != nil {
		t.Fatalf("SaveGuild(b) error = %v", err)
	}
	if err := store.AddGuildWar("guild-a", "guild-b"); err != nil {
		t.Fatalf("AddGuildWar() error = %v", err)
	}
	if !store.GuildAtWar("guild-a", "guild-b") || !store.GuildAtWar("guild-b", "guild-a") {
		t.Fatal("guild war relation is not symmetric")
	}
	guild, ok := store.Guild("guild-a")
	if !ok || !reflect.DeepEqual(guild.WarOrder, []string{"guild-b"}) {
		t.Fatalf("guild war order = %v, want [guild-b]", guild.WarOrder)
	}
	if err := store.AddGuildWar("guild-a", "guild-b"); err == nil {
		t.Fatal("duplicate guild war should fail")
	}
}

func TestAddGuildWarAllowsReferenceSelfRelation(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.SaveGuild(Guild{ID: "guild-self"}); err != nil {
		t.Fatalf("SaveGuild() error = %v", err)
	}
	if err := store.AddGuildWar("guild-self", "guild-self", time.Hour); err != nil {
		t.Fatalf("AddGuildWar() error = %v, want reference self relation", err)
	}
	if !store.GuildAtWar("guild-self", "guild-self") {
		t.Fatal("self guild war relation was not persisted")
	}
	expired, err := store.ExpireGuildWars(time.Now().Add(2 * time.Hour))
	if err != nil || len(expired) != 1 {
		t.Fatalf("ExpireGuildWars() = %v, %v; want one self-war expiry", expired, err)
	}
	if store.GuildAtWar("guild-self", "guild-self") {
		t.Fatal("expired self guild war relation remained")
	}
}

func TestGuildWarExpiryRemovesBothRelationsAndDuplicateRequestResetsExpiry(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	for _, id := range []string{"guild-a", "guild-b"} {
		if err := store.SaveGuild(Guild{ID: id}); err != nil {
			t.Fatalf("SaveGuild(%s) error = %v", id, err)
		}
	}
	started := time.Now()
	if err := store.AddGuildWar("guild-a", "guild-b", time.Hour); err != nil {
		t.Fatalf("AddGuildWar() error = %v", err)
	}
	if expired, err := store.ExpireGuildWars(started.Add(30 * time.Minute)); err != nil || len(expired) != 0 {
		t.Fatalf("ExpireGuildWars(before expiry) error = %v", err)
	}
	if !store.GuildAtWar("guild-a", "guild-b") {
		t.Fatal("guild war expired before configured duration")
	}
	if err := store.AddGuildWar("guild-a", "guild-b", time.Hour); err != nil {
		t.Fatalf("duplicate AddGuildWar() error = %v", err)
	}
	if expired, err := store.ExpireGuildWars(time.Now().Add(30 * time.Minute)); err != nil || len(expired) != 0 {
		t.Fatalf("ExpireGuildWars(after original expiry) error = %v", err)
	}
	if !store.GuildAtWar("guild-a", "guild-b") {
		t.Fatal("duplicate request did not reset guild war expiry")
	}
	if expired, err := store.ExpireGuildWars(time.Now().Add(2 * time.Hour)); err != nil || len(expired) != 2 {
		t.Fatalf("ExpireGuildWars(final) error = %v", err)
	}
	if store.GuildAtWar("guild-a", "guild-b") || store.GuildAtWar("guild-b", "guild-a") {
		t.Fatal("expired guild war relation remained on one side")
	}
}

func TestExpireWeaponUpgradesClearsOldState(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	ch, err := store.InsertCharacter(Character{
		Account: "test", Name: "expired-upgrade", Class: "warrior",
		WeaponUpgrade: &WeaponUpgradeState{StartedAt: time.Now().Add(-9 * 24 * time.Hour).UnixMilli()},
	})
	if err != nil {
		t.Fatalf("InsertCharacter() error = %v", err)
	}
	expired, err := store.ExpireWeaponUpgrades(time.Now().Add(-8 * 24 * time.Hour))
	if err != nil || len(expired) != 1 || expired[0] != ch.ID {
		t.Fatalf("ExpireWeaponUpgrades() = ids:%v err:%v, want %s", expired, err, ch.ID)
	}
	updated, ok := store.Character(ch.ID)
	if !ok || updated.WeaponUpgrade != nil {
		t.Fatalf("expired upgrade = %+v (found=%t), want nil", updated.WeaponUpgrade, ok)
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

func TestSaveCharacterPairRestoresBothCharactersOnPersistenceFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	first, err := store.InsertCharacter(Character{Account: "test", Name: "pair-first", Class: "warrior", HP: 100})
	if err != nil {
		t.Fatalf("InsertCharacter(first) error = %v", err)
	}
	second, err := store.InsertCharacter(Character{Account: "test", Name: "pair-second", Class: "warrior", HP: 100})
	if err != nil {
		t.Fatalf("InsertCharacter(second) error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	first.HP = 1
	second.HP = 2
	if err := store.SaveCharacterPair(first, second); err == nil {
		t.Fatal("SaveCharacterPair() error = nil, want persistence failure")
	}
	gotFirst, firstOK := store.Character(first.ID)
	gotSecond, secondOK := store.Character(second.ID)
	if !firstOK || !secondOK || gotFirst.HP != 100 || gotSecond.HP != 100 {
		t.Fatalf("pair after failure = first:%+v second:%+v, want both HP 100", gotFirst, gotSecond)
	}
}

func TestSetCharacterRuntimeDoesNotPersistToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	character, err := store.InsertCharacter(Character{Account: "test", Name: "runtime-only", Class: "warrior", HP: 100})
	if err != nil {
		t.Fatalf("InsertCharacter() error = %v", err)
	}
	character.HP = 1
	if err := store.SetCharacterRuntime(character); err != nil {
		t.Fatalf("SetCharacterRuntime() error = %v", err)
	}
	if got, ok := store.Character(character.ID); !ok || got.HP != 1 {
		t.Fatalf("runtime character = %+v (found=%t), want HP 1", got, ok)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen Open() error = %v", err)
	}
	if got, ok := reopened.Character(character.ID); !ok || got.HP != 100 {
		t.Fatalf("reopened character = %+v (found=%t), want persisted HP 100", got, ok)
	}
}

func TestSaveCharacterBatchRestoresAllCharactersOnPersistenceFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	first, err := store.InsertCharacter(Character{Account: "test", Name: "batch-first", Class: "warrior", HP: 100})
	if err != nil {
		t.Fatalf("InsertCharacter(first) error = %v", err)
	}
	second, err := store.InsertCharacter(Character{Account: "test", Name: "batch-second", Class: "warrior", HP: 100})
	if err != nil {
		t.Fatalf("InsertCharacter(second) error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	first.HP = 1
	second.HP = 2
	if err := store.SaveCharacterBatch([]Character{first, second}); err == nil {
		t.Fatal("SaveCharacterBatch() error = nil, want persistence failure")
	}
	gotFirst, firstOK := store.Character(first.ID)
	gotSecond, secondOK := store.Character(second.ID)
	if !firstOK || !secondOK || gotFirst.HP != 100 || gotSecond.HP != 100 {
		t.Fatalf("batch after failure = first:%+v second:%+v, want both HP 100", gotFirst, gotSecond)
	}
}

func TestSaveGuildAlliancePairRestoresMemoryOnPersistenceFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.SaveGuild(Guild{ID: "guild-a", Alliances: []string{"guild-old"}, Alliance: "guild-old"}); err != nil {
		t.Fatalf("SaveGuild(guild-a) error = %v", err)
	}
	if err := store.SaveGuild(Guild{ID: "guild-b"}); err != nil {
		t.Fatalf("SaveGuild(guild-b) error = %v", err)
	}
	original, err := store.InsertCharacter(Character{Name: "alliance-member", GuildID: "guild-a", GuildAllianceID: "guild-old", GuildAllianceIDs: []string{"guild-old"}})
	if err != nil {
		t.Fatalf("InsertCharacter() error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := store.SaveGuildAlliancePair(Guild{ID: "guild-a"}, Guild{ID: "guild-b"}, []string{"guild-b"}, []string{"guild-a"}); err == nil {
		t.Fatal("SaveGuildAlliancePair() error = nil, want persistence failure")
	}
	guild, ok := store.Guild("guild-a")
	if !ok || len(guild.Alliances) != 1 || guild.Alliances[0] != "guild-old" {
		t.Fatalf("guild-a after rollback = %+v (found=%t), want original alliance", guild, ok)
	}
	member, ok := store.Character(original.ID)
	if !ok || member.GuildAllianceID != "guild-old" || len(member.GuildAllianceIDs) != 1 || member.GuildAllianceIDs[0] != "guild-old" {
		t.Fatalf("member after rollback = %+v (found=%t), want original alliance", member, ok)
	}
}

func TestSaveGuildAlliancePairBestEffortKeepsMemoryOnPersistenceFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.SaveGuild(Guild{ID: "guild-a"}); err != nil {
		t.Fatalf("SaveGuild(guild-a) error = %v", err)
	}
	if err := store.SaveGuild(Guild{ID: "guild-b"}); err != nil {
		t.Fatalf("SaveGuild(guild-b) error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := store.SaveGuildAlliancePairBestEffort(Guild{ID: "guild-a"}, Guild{ID: "guild-b"}, []string{"guild-b"}, []string{"guild-a"}); err == nil {
		t.Fatal("SaveGuildAlliancePairBestEffort() error = nil, want persistence failure")
	}
	guild, ok := store.Guild("guild-a")
	if !ok || !reflect.DeepEqual(guild.Alliances, []string{"guild-b"}) || guild.Alliance != "guild-b" {
		t.Fatalf("guild-a after best-effort failure = %+v (found=%t), want updated alliance", guild, ok)
	}
}

func TestRemoveGuildMemberRestoresMemoryOnPersistenceFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.SaveGuild(Guild{ID: "guild-a", Ranks: []GuildRank{{Number: 1, Name: "会长", Members: []string{"Leader"}}, {Number: 0, Name: "成员", Members: []string{"Offline"}}}}); err != nil {
		t.Fatalf("SaveGuild() error = %v", err)
	}
	member, err := store.InsertCharacter(Character{Name: "Offline", GuildID: "guild-a", GuildRank: 0, GuildRankName: "成员", GuildNotice: "notice", GuildAllianceID: "guild-b", GuildAllianceIDs: []string{"guild-b"}})
	if err != nil {
		t.Fatalf("InsertCharacter() error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if _, removed, err := store.RemoveGuildMember("guild-a", "Offline"); err == nil || removed {
		t.Fatalf("RemoveGuildMember() = removed:%t err:%v, want persistence failure", removed, err)
	}
	guild, ok := store.Guild("guild-a")
	if !ok || len(guild.Ranks) != 2 || len(guild.Ranks[1].Members) != 1 || guild.Ranks[1].Members[0] != "Offline" {
		t.Fatalf("guild after rollback = %+v (found=%t), want member retained", guild, ok)
	}
	got, ok := store.Character(member.ID)
	if !ok || got.GuildID != "guild-a" || got.GuildRank != 0 || got.GuildRankName != "成员" || got.GuildAllianceID != "guild-b" {
		t.Fatalf("character after rollback = %+v (found=%t), want original guild fields", got, ok)
	}
}

func TestRemoveGuildMemberBestEffortKeepsRemovalOnPersistenceFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.SaveGuild(Guild{ID: "guild-best-effort-member", Ranks: []GuildRank{{Members: []string{"Member"}}}}); err != nil {
		t.Fatalf("SaveGuild() error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	removed, err := store.RemoveGuildMemberBestEffort("guild-best-effort-member", "Member")
	if !removed || err == nil {
		t.Fatalf("RemoveGuildMemberBestEffort() = (%t, %v), want removal with persistence failure", removed, err)
	}
	guild, ok := store.Guild("guild-best-effort-member")
	if !ok || len(guild.Ranks) != 1 || len(guild.Ranks[0].Members) != 0 {
		t.Fatalf("guild after best-effort removal = %+v (found=%t), want member removed", guild, ok)
	}
}

func TestUpdateGuildRanksBestEffortRetainsMemoryOnPersistenceFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.SaveGuild(Guild{ID: "guild-ranks", Ranks: []GuildRank{{Number: 1, Name: "旧", Members: []string{"Leader"}}}}); err != nil {
		t.Fatalf("SaveGuild() error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := store.UpdateGuildRanksBestEffort("guild-ranks", []GuildRank{{Number: 1, Name: "新", Members: []string{"Leader"}}}); err == nil {
		t.Fatal("UpdateGuildRanksBestEffort() error = nil, want persistence failure")
	}
	guild, ok := store.Guild("guild-ranks")
	if !ok || guild.Ranks[0].Name != "新" {
		t.Fatalf("guild after best-effort failure = %+v (found=%t), want new in-memory rank", guild, ok)
	}
}

func TestUpdateGuildNoticeBestEffortRetainsMemoryOnPersistenceFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.SaveGuild(Guild{ID: "guild-notice", Notice: "旧"}); err != nil {
		t.Fatalf("SaveGuild() error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := store.UpdateGuildNoticeBestEffort("guild-notice", "新"); err == nil {
		t.Fatal("UpdateGuildNoticeBestEffort() error = nil, want persistence failure")
	}
	guild, ok := store.Guild("guild-notice")
	if !ok || guild.Notice != "新" {
		t.Fatalf("guild after best-effort failure = %+v (found=%t), want new in-memory notice", guild, ok)
	}
}

func TestDisbandGuildRestoresMemoryOnPersistenceFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.SaveGuild(Guild{ID: "guild-a", Ranks: []GuildRank{{Number: 1, Name: "会长", Members: []string{"Leader", "Member"}}}}); err != nil {
		t.Fatalf("SaveGuild() error = %v", err)
	}
	member, err := store.InsertCharacter(Character{Name: "Member", GuildID: "guild-a", GuildRank: 0})
	if err != nil {
		t.Fatalf("InsertCharacter() error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if _, err := store.DisbandGuild("guild-a"); err == nil {
		t.Fatal("DisbandGuild() error = nil, want persistence failure")
	}
	if guild, ok := store.Guild("guild-a"); !ok || len(guild.Ranks) != 1 || len(guild.Ranks[0].Members) != 2 {
		t.Fatalf("guild after rollback = %+v (found=%t), want original guild", guild, ok)
	}
	got, ok := store.Character(member.ID)
	if !ok || got.GuildID != "guild-a" {
		t.Fatalf("character after rollback = %+v (found=%t), want original guild", got, ok)
	}
}

func TestDeleteGuildBestEffortKeepsMemoryChangeOnPersistenceFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.SaveGuild(Guild{ID: "guild-best-effort"}); err != nil {
		t.Fatalf("SaveGuild() error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := store.DeleteGuildBestEffort("guild-best-effort"); err == nil {
		t.Fatal("DeleteGuildBestEffort() error = nil, want persistence failure")
	}
	if _, ok := store.Guild("guild-best-effort"); ok {
		t.Fatal("guild remained in memory after best-effort deletion")
	}
}

func TestExpireWeaponUpgradesRestoresMemoryOnPersistenceFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	original, err := store.InsertCharacter(Character{Name: "expired-rollback", WeaponUpgrade: &WeaponUpgradeState{StartedAt: time.Now().Add(-time.Hour).UnixMilli()}})
	if err != nil {
		t.Fatalf("InsertCharacter() error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if _, err := store.ExpireWeaponUpgrades(time.Now()); err == nil {
		t.Fatal("ExpireWeaponUpgrades() error = nil, want persistence failure")
	}
	got, ok := store.Character(original.ID)
	if !ok || got.WeaponUpgrade == nil {
		t.Fatalf("character after rollback = %+v (found=%t), want upgrade retained", got, ok)
	}
}

func TestInsertCharacterRestoresMemoryOnPersistenceFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if _, err := store.InsertCharacter(Character{Name: "insert-rollback"}); err == nil {
		t.Fatal("InsertCharacter() error = nil, want persistence failure")
	}
	if _, ok := store.Character("char-1"); ok {
		t.Fatal("failed character remained in memory")
	}
	if got, err := store.InsertCharacter(Character{Name: "insert-after-rollback"}); err == nil || got.ID != "" {
		t.Fatalf("second InsertCharacter() = %+v, err:%v, want same persistence failure without ID commit", got, err)
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

func TestCharacterItemSkillHotkeysPersist(t *testing.T) {
	input := Character{ItemSkillHotkeys: map[string]byte{"火球术": '3'}}
	b, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var output Character
	if err := json.Unmarshal(b, &output); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if output.ItemSkillHotkeys["火球术"] != '3' {
		t.Fatalf("item skill hotkeys = %+v, want 火球术:'3'", output.ItemSkillHotkeys)
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
		IncHealth:   11, IncSpell: 22, IncHealing: 33, TargetID: "runtime-target", Sitting: true, MapMoveAt: expires,
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
	if disk.StatusTimeFormat != characterStatusTimeFormat || disk.DefenceUpUntil <= 0 || disk.DefenceUpUntil > 30000 || disk.MagDefenceUpUntil <= 0 || disk.MagDefenceUpUntil > 30000 || disk.ParalyzedUntil <= 0 || disk.ParalyzedUntil > 30000 || disk.BubbleDefenceLevel != 0 || disk.IncHealth != 11 || disk.IncSpell != 22 || disk.IncHealing != 33 || disk.TargetID != "" || disk.Sitting || disk.MapMoveAt != 0 || disk.ThrustingDisabled || disk.HalfMoonDisabled || disk.AdminMode || disk.StoneMode || disk.PKFlag || disk.FreePKArea || disk.IncHealthSpellAt != 0 || disk.PoisonHealthLevel != 0 || disk.PoisonArmorLevel != 0 || disk.PoisonHealthStartAt != 0 || disk.PoisonHealthTickAt != 0 || disk.ShowHPUntil != 0 {
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

func TestCharacterTemporaryAbilitiesDoNotPersist(t *testing.T) {
	input := Character{
		ExtraAbil:      [6]uint16{3, 4},
		ExtraAbilTimes: [6]int64{100, 200},
	}
	b, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(b), "extra_abil") {
		t.Fatalf("temporary abilities persisted in JSON: %s", b)
	}
	var output Character
	if err := json.Unmarshal([]byte(`{"extra_abil":[9],"extra_abil_times":[10]}`), &output); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if output.ExtraAbil != [6]uint16{} || output.ExtraAbilTimes != [6]int64{} {
		t.Fatalf("temporary abilities restored from JSON: %+v/%+v", output.ExtraAbil, output.ExtraAbilTimes)
	}
}
