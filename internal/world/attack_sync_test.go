package world

import (
	"reflect"
	"testing"
	"time"

	"openmir2/internal/storage"
)

type attackSyncCall struct {
	kind  string
	magic uint16
	level byte
	train int
	delay time.Duration
}

type recordingAttackSyncer struct {
	calls []attackSyncCall
}

func (s *recordingAttackSyncer) UpdateClient(storage.Character) {
	s.calls = append(s.calls, attackSyncCall{kind: "update"})
}

func (s *recordingAttackSyncer) SendActionOK() {
	s.calls = append(s.calls, attackSyncCall{kind: "action"})
}

func (s *recordingAttackSyncer) SendWinExp(int, int) {
	s.calls = append(s.calls, attackSyncCall{kind: "exp"})
}

func (s *recordingAttackSyncer) SendLevelUp(storage.Character) {
	s.calls = append(s.calls, attackSyncCall{kind: "level-up"})
}

func (s *recordingAttackSyncer) SendHealthSpellChanged(storage.Character) {
	s.calls = append(s.calls, attackSyncCall{kind: "health"})
}

func (s *recordingAttackSyncer) SendCharacterHitChanges(hit CharacterHit) {
	if len(hit.DeletedItems) > 0 {
		s.calls = append(s.calls, attackSyncCall{kind: "delete"})
	}
	if hit.FeatureChanged {
		s.calls = append(s.calls, attackSyncCall{kind: "feature"})
	}
	for range hit.Durability {
		s.calls = append(s.calls, attackSyncCall{kind: "durability"})
	}
	if hit.FeatureChanged {
		s.calls = append(s.calls, attackSyncCall{kind: "status"})
	}
}

func (s *recordingAttackSyncer) BroadcastCharacterHit(storage.Character, uint16) {
	s.calls = append(s.calls, attackSyncCall{kind: "character-hit"})
}

func (s *recordingAttackSyncer) BroadcastCharacterStruck(CharacterHit) {
	s.calls = append(s.calls, attackSyncCall{kind: "character-struck"})
}

func (s *recordingAttackSyncer) BroadcastCharacterNameColor(storage.Character) {
	s.calls = append(s.calls, attackSyncCall{kind: "name-color"})
}

func (s *recordingAttackSyncer) BroadcastHitImpact(AttackResult) {
	s.calls = append(s.calls, attackSyncCall{kind: "monster-hit"})
}

func (s *recordingAttackSyncer) SendSkillExp(magicID uint16, level byte, train int, delay time.Duration) {
	s.calls = append(s.calls, attackSyncCall{kind: "skill-exp", magic: magicID, level: level, train: train, delay: delay})
}

func TestApplyAttackSyncSendsSkillExpAfterAttackResults(t *testing.T) {
	syncer := &recordingAttackSyncer{}
	result := AttackResult{
		SkillExp:      true,
		SkillMagicID:  27,
		SkillLevel:    2,
		SkillTrain:    11,
		SkillExpDelay: 3 * time.Second,
		MonsterID:     "monster-1",
		Damage:        1,
	}

	ApplyAttackSync(syncer, result, 1)

	got := make([]string, 0, len(syncer.calls))
	for _, call := range syncer.calls {
		got = append(got, call.kind)
	}
	want := []string{"update", "character-hit", "monster-hit", "action", "skill-exp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("call order = %v, want %v", got, want)
	}
	last := syncer.calls[len(syncer.calls)-1]
	if last.magic != result.SkillMagicID || last.level != result.SkillLevel || last.train != result.SkillTrain || last.delay != result.SkillExpDelay {
		t.Fatalf("skill exp call = %+v, want magic=%d level=%d train=%d delay=%s", last, result.SkillMagicID, result.SkillLevel, result.SkillTrain, result.SkillExpDelay)
	}
}

func TestApplyAttackSyncBroadcastsNewAttackerNameColorAfterStrike(t *testing.T) {
	syncer := &recordingAttackSyncer{}
	ApplyAttackSync(syncer, AttackResult{
		Character:     storage.Character{ID: "attacker", PKFlag: true},
		CharacterHits: []CharacterHit{{Character: storage.Character{ID: "target"}, Damage: 1, AttackerNameColorChanged: true}},
	}, 1)
	got := make([]string, 0, len(syncer.calls))
	for _, call := range syncer.calls {
		got = append(got, call.kind)
	}
	want := []string{"update", "character-hit", "character-struck", "action", "name-color"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("call order = %v, want %v", got, want)
	}
}

func TestApplyAttackSyncSendsSkillExpAfterLevelUp(t *testing.T) {
	syncer := &recordingAttackSyncer{}
	ApplyAttackSync(syncer, AttackResult{SkillExp: true, SkillLevelUp: true, Damage: 1}, 1)
	if len(syncer.calls) == 0 || syncer.calls[len(syncer.calls)-1].kind != "skill-exp" {
		t.Fatalf("calls = %v, want skill-exp after level-up", syncer.calls)
	}
}

func TestApplyAttackSyncSendsAttackBeforeExperience(t *testing.T) {
	syncer := &recordingAttackSyncer{}
	ApplyAttackSync(syncer, AttackResult{Experience: 10, CurrentExp: 20}, 1)
	got := make([]string, 0, len(syncer.calls))
	for _, call := range syncer.calls {
		got = append(got, call.kind)
	}
	want := []string{"update", "character-hit", "action", "exp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("call order = %v, want %v", got, want)
	}
}

func TestApplyAttackSyncSkipsZeroDamageMonsterImpact(t *testing.T) {
	syncer := &recordingAttackSyncer{}
	ApplyAttackSync(syncer, AttackResult{MonsterID: "monster-1"}, 1)
	for _, call := range syncer.calls {
		if call.kind == "monster-hit" {
			t.Fatal("zero-damage attack emitted monster impact")
		}
	}
}

func TestApplyAttackSyncOrdersAttackBeforeDelayedCharacterImpact(t *testing.T) {
	syncer := &recordingAttackSyncer{}
	result := AttackResult{
		CharacterHits: []CharacterHit{{Damage: 1, Durability: []SpellDurability{{Slot: 1}}}},
	}

	ApplyAttackSync(syncer, result, 1)

	got := make([]string, 0, len(syncer.calls))
	for _, call := range syncer.calls {
		got = append(got, call.kind)
	}
	want := []string{"update", "durability", "character-hit", "character-struck", "action"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("call order = %v, want %v", got, want)
	}
}

func TestApplyAttackSyncSkipsZeroDamageCharacterImpact(t *testing.T) {
	syncer := &recordingAttackSyncer{}
	ApplyAttackSync(syncer, AttackResult{CharacterHits: []CharacterHit{{Damage: 0}}}, 1)
	for _, call := range syncer.calls {
		if call.kind == "character-struck" {
			t.Fatal("zero-damage attack emitted character impact")
		}
	}
}
