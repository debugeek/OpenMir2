package core

import (
	"testing"

	"openmir2/internal/storage"
)

func TestApplyVitalDelta(t *testing.T) {
	tests := []struct {
		name      string
		ch        storage.Character
		hpDelta   int
		mpDelta   int
		wantHP    int
		wantMP    int
		wantDead  bool
		wantDelta bool
	}{
		{
			name:      "heal clamps to max",
			ch:        storage.Character{HP: 10, MP: 8, MaxHP: 20, MaxMP: 15},
			hpDelta:   15,
			mpDelta:   10,
			wantHP:    20,
			wantMP:    15,
			wantDead:  false,
			wantDelta: true,
		},
		{
			name:      "damage marks dead",
			ch:        storage.Character{HP: 5, MP: 7, MaxHP: 20, MaxMP: 15},
			hpDelta:   -9,
			mpDelta:   0,
			wantHP:    0,
			wantMP:    7,
			wantDead:  true,
			wantDelta: true,
		},
		{
			name:      "zero delta keeps state",
			ch:        storage.Character{HP: 9, MP: 3, MaxHP: 20, MaxMP: 15},
			hpDelta:   0,
			mpDelta:   0,
			wantHP:    9,
			wantMP:    3,
			wantDead:  false,
			wantDelta: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyVitalDelta(tt.ch, tt.hpDelta, tt.mpDelta)
			if got.Character.HP != tt.wantHP || got.Character.MP != tt.wantMP {
				t.Fatalf("vitals = %d/%d, want %d/%d", got.Character.HP, got.Character.MP, tt.wantHP, tt.wantMP)
			}
			if got.Dead != tt.wantDead {
				t.Fatalf("Dead = %v, want %v", got.Dead, tt.wantDead)
			}
			if got.Changed != tt.wantDelta {
				t.Fatalf("Changed = %v, want %v", got.Changed, tt.wantDelta)
			}
		})
	}
}

func TestSetVitals(t *testing.T) {
	got := SetVitals(storage.Character{HP: 3, MP: 4, MaxHP: 12, MaxMP: 11}, 99, -3)
	if got.Character.HP != 12 || got.Character.MP != 0 {
		t.Fatalf("vitals = %d/%d, want 12/0", got.Character.HP, got.Character.MP)
	}
}
