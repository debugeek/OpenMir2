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

func TestApplyQueuedRecovery(t *testing.T) {
	ch := storage.Character{
		Level:            1,
		HP:               10,
		MaxHP:            100,
		MP:               5,
		MaxMP:            40,
		IncHealth:        20,
		IncSpell:         30,
		IncHealthSpellAt: time.Unix(10, 0).UnixMilli(),
	}
	next, changed := ApplyQueuedRecovery(ch, time.Unix(11, 0))
	if !changed {
		t.Fatal("ApplyQueuedRecovery() changed = false, want true")
	}
	if next.HP != 15 || next.MP != 10 {
		t.Fatalf("ApplyQueuedRecovery() HP/MP = %d/%d, want 15/10", next.HP, next.MP)
	}
	if next.IncHealth != 15 || next.IncSpell != 25 {
		t.Fatalf("ApplyQueuedRecovery() queued = %d/%d, want 15/25", next.IncHealth, next.IncSpell)
	}
}
