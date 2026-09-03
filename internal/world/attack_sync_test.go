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

func (s *recordingAttackSyncer) SendDurability(storage.Character, SpellDurability) {
	s.calls = append(s.calls, attackSyncCall{kind: "durability"})
}

func (s *recordingAttackSyncer) BroadcastCharacterHit(storage.Character, uint16) {
	s.calls = append(s.calls, attackSyncCall{kind: "character-hit"})
}

func (s *recordingAttackSyncer) BroadcastCharacterStruck(CharacterHit) {
	s.calls = append(s.calls, attackSyncCall{kind: "character-struck"})
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
		CharacterHits: []CharacterHit{{Durability: []SpellDurability{{Slot: 1}}}},
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
