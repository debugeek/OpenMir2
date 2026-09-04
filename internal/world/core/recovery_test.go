package core

import (
	"testing"
	"time"

	"openmir2/internal/storage"
)

func TestQueueRecovery(t *testing.T) {
	ch := storage.Character{IncHealth: 3, IncSpell: 4}
	next := QueueRecovery(ch, 7, 11)
	if next.IncHealth != 10 || next.IncSpell != 15 {
		t.Fatalf("QueueRecovery() = %d/%d, want 10/15", next.IncHealth, next.IncSpell)
	}
}

func TestQueueHealing(t *testing.T) {
	ch := storage.Character{HP: 20, MaxHP: 100, IncHealing: 4}
	next := QueueHealing(ch, 9)
	if next.IncHealing != 13 {
		t.Fatalf("QueueHealing() = %d, want 13", next.IncHealing)
	}
}

func TestQueueHealingMatchesMessageConsumerForFullAndDeadTargets(t *testing.T) {
	for _, ch := range []storage.Character{
		{HP: 100, MaxHP: 100, IncHealing: 4},
		{HP: 0, MaxHP: 100, IncHealing: 4},
	} {
		next := QueueHealing(ch, 9)
		if next.IncHealing != 13 {
			t.Fatalf("QueueHealing(%+v) = %d, want 13", ch, next.IncHealing)
		}
	}
	if next := QueueHealing(storage.Character{IncHealing: 299}, 9); next.IncHealing != 300 {
		t.Fatalf("QueueHealing() cap = %d, want 300", next.IncHealing)
	}
}

func TestApplyQueuedRecovery(t *testing.T) {
	ch := storage.Character{
		Level:            1,
		HP:               10,
		MaxHP:            100,
		MP:               5,
		MaxMP:            40,
		IncHealth:        20,
		IncSpell:         30,
		IncHealing:       5,
		IncHealthSpellAt: time.Unix(10, 0).UnixMilli(),
	}
	next, changed := ApplyQueuedRecovery(ch, time.Unix(11, 0))
	if !changed {
		t.Fatal("ApplyQueuedRecovery() changed = false, want true")
	}
	if next.HP != 20 || next.MP != 10 {
		t.Fatalf("ApplyQueuedRecovery() HP/MP = %d/%d, want 20/10", next.HP, next.MP)
	}
	if next.IncHealth != 15 || next.IncSpell != 25 || next.IncHealing != 0 {
		t.Fatalf("ApplyQueuedRecovery() queued = %d/%d/%d, want 15/25/0", next.IncHealth, next.IncSpell, next.IncHealing)
	}
}

func TestApplyQueuedRecoveryUsesInitialReferenceAmounts(t *testing.T) {
	ch := storage.Character{
		Level:            20,
		HP:               1,
		MaxHP:            100,
		MP:               1,
		MaxMP:            100,
		IncHealth:        20,
		IncSpell:         20,
		IncHealthSpellAt: time.Unix(10, 0).UnixMilli(),
	}

	next, changed := ApplyQueuedRecovery(ch, time.Unix(11, 0))
	if !changed {
		t.Fatal("ApplyQueuedRecovery() changed = false, want true")
	}
	if next.HP != 6 || next.MP != 6 {
		t.Fatalf("ApplyQueuedRecovery() HP/MP = %d/%d, want 6/6", next.HP, next.MP)
	}
	if next.IncHealth != 15 || next.IncSpell != 15 {
		t.Fatalf("ApplyQueuedRecovery() queued = %d/%d, want 15/15", next.IncHealth, next.IncSpell)
	}
	if next.PerHealth != 7 || next.PerSpell != 7 {
		t.Fatalf("ApplyQueuedRecovery() per amounts = %d/%d, want 7/7", next.PerHealth, next.PerSpell)
	}
}
