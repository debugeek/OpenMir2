package world

import (
	"testing"

	"openmir2/internal/storage"
)

type equipSyncCall struct {
	kind string
}

type recordingEquipSyncer struct {
	calls []equipSyncCall
}

func (s *recordingEquipSyncer) SendWeightChanged(storage.Character) {
	s.calls = append(s.calls, equipSyncCall{kind: "weight"})
}

func (s *recordingEquipSyncer) UpdateClient(storage.Character) {
	s.calls = append(s.calls, equipSyncCall{kind: "update"})
}

func (s *recordingEquipSyncer) SendBagAddItem(storage.Character, storage.UserItem) {
	s.calls = append(s.calls, equipSyncCall{kind: "item"})
}

func (s *recordingEquipSyncer) SendSkillAdded(storage.Character, storage.SkillState) {
	s.calls = append(s.calls, equipSyncCall{kind: "skill-add"})
}

func (s *recordingEquipSyncer) SendSkillRemoved(storage.Character, string) {
	s.calls = append(s.calls, equipSyncCall{kind: "skill-remove"})
}

func (s *recordingEquipSyncer) SendEquipSuccess(storage.Character, uint16) {
	s.calls = append(s.calls, equipSyncCall{kind: "ok"})
}

func (s *recordingEquipSyncer) SendAbilityOnly(storage.Character) {
	s.calls = append(s.calls, equipSyncCall{kind: "ability"})
}

func (s *recordingEquipSyncer) SendSubAbility(storage.Character) {
	s.calls = append(s.calls, equipSyncCall{kind: "sub-ability"})
}

func (s *recordingEquipSyncer) SendFeatureChanged(storage.Character) {
	s.calls = append(s.calls, equipSyncCall{kind: "feature"})
}

func TestApplyEquipSyncSendsSwappedItemBeforeAbilityRefresh(t *testing.T) {
	syncer := &recordingEquipSyncer{}
	ApplyEquipSync(syncer, EquipResult{
		Character:     storage.Character{ID: "character"},
		HasSwappedOut: true,
		SwappedOut:    storage.UserItem{ItemID: "old-item"},
	}, 1)
	got := make([]string, 0, len(syncer.calls))
	for _, call := range syncer.calls {
		got = append(got, call.kind)
	}
	want := []string{"update", "weight", "item", "ability", "sub-ability", "ok", "feature"}
	if len(got) != len(want) {
		t.Fatalf("call order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("call order = %v, want %v", got, want)
		}
	}
}

func TestApplyUnequipSyncSendsRemovedItemBeforeAbilityRefresh(t *testing.T) {
	syncer := &recordingEquipSyncer{}
	ApplyUnequipSync(syncer, UnequipResult{
		Character:      storage.Character{ID: "character"},
		HasRemovedItem: true,
		RemovedItem:    storage.UserItem{ItemID: "old-item"},
	}, 1)
	got := make([]string, 0, len(syncer.calls))
	for _, call := range syncer.calls {
		got = append(got, call.kind)
	}
	want := []string{"update", "weight", "item", "ability", "sub-ability", "ok", "feature"}
	if len(got) != len(want) {
		t.Fatalf("call order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("call order = %v, want %v", got, want)
		}
	}
}
