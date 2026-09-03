package world

import (
	"testing"

	"openmir2/internal/storage"
)

func TestBaseWarriorLevel1(t *testing.T) {
	got := Base("warrior", 1)
	want := LevelAbilities{
		MaxHP: 19, MaxMP: 15,
		DC: 1, DCMax: 1,
		MaxWeight: 50, MaxWearWeight: 15, MaxHandWeight: 12,
	}
	if got != want {
		t.Fatalf("Base(warrior, 1) = %+v, want %+v", got, want)
	}
}

func TestBaseWizardLevel1(t *testing.T) {
	got := Base("wizard", 1)
	want := LevelAbilities{
		MaxHP: 16, MaxMP: 18,
		DC: 0, DCMax: 1,
		MC: 0, MCMax: 1,
		MaxWeight: 50, MaxWearWeight: 15, MaxHandWeight: 12,
	}
	if got != want {
		t.Fatalf("Base(wizard, 1) = %+v, want %+v", got, want)
	}
}

func TestBaseTaoistLevel1(t *testing.T) {
	got := Base("taoist", 1)
	want := LevelAbilities{
		MaxHP: 17, MaxMP: 13,
		DC: 0, DCMax: 1,
		SC: 0, SCMax: 1,
		MAC: 0, MACMax: 1,
		MaxWeight: 50, MaxWearWeight: 15, MaxHandWeight: 12,
	}
	if got != want {
		t.Fatalf("Base(taoist, 1) = %+v, want %+v", got, want)
	}
}

func TestBaseUnknownClassFallsBackToWarrior(t *testing.T) {
	if got, want := Base("nonsense", 5), Base("warrior", 5); got != want {
		t.Fatalf("Base(nonsense, 5) = %+v, want fallback to warrior = %+v", got, want)
	}
}

func TestBaseLevelHigherThanOneIncreasesMaxHP(t *testing.T) {
	l1 := Base("warrior", 1)
	l10 := Base("warrior", 10)
	if l10.MaxHP <= l1.MaxHP {
		t.Fatalf("Base(warrior, 10).MaxHP = %d, want > Base(warrior, 1).MaxHP = %d", l10.MaxHP, l1.MaxHP)
	}
}

func TestTemporaryCombatBonusesUseReferenceOffset(t *testing.T) {
	world := &World{}
	ch := storage.Character{
		Class:     "warrior",
		Level:     1,
		ExtraAbil: [7]uint16{3, 4, 5},
	}

	stats := world.AbilityStats(ch)
	base := Base(ch.Class, ch.Level)
	if got, want := highByte(stats.DC), highByte(PackWord(base.DC, base.DCMax, 0, 0))+5; got != want {
		t.Fatalf("temporary DC = %d, want %d", got, want)
	}
	if got, want := highByte(stats.MC), highByte(PackWord(base.MC, base.MCMax, 0, 0))+6; got != want {
		t.Fatalf("temporary MC = %d, want %d", got, want)
	}
	if got, want := highByte(stats.SC), highByte(PackWord(base.SC, base.SCMax, 0, 0))+7; got != want {
		t.Fatalf("temporary SC = %d, want %d", got, want)
	}
	combat := world.CombatStats(ch)
	if combat.DCMax != 5 || combat.MCMax != 6 || combat.SCMax != 7 {
		t.Fatalf("temporary combat stats = %+v, want DC/MC/SC max 5/6/7", combat)
	}
}

func highByte(word int) int {
	return (word >> 8) & 0xFF
}

func TestBaseNonPositiveLevelTreatedAsOne(t *testing.T) {
	if got, want := Base("warrior", 0), Base("warrior", 1); got != want {
		t.Fatalf("Base(warrior, 0) = %+v, want same as level 1 = %+v", got, want)
	}
}

func TestPackWordCombinesAndClampsToByte(t *testing.T) {
	if got, want := PackWord(1, 1, 1, 2), 2|3<<8; got != want {
		t.Fatalf("PackWord(1,1,1,2) = %d, want %d", got, want)
	}
	if got, want := PackWord(200, 200, 100, 100), 255|255<<8; got != want {
		t.Fatalf("PackWord(200,200,100,100) = %d, want %d (clamped to byte)", got, want)
	}
	if got, want := PackWord(1, 1, -5, -5), 0|0<<8; got != want {
		t.Fatalf("PackWord(1,1,-5,-5) = %d, want %d (clamped to 0)", got, want)
	}
}
