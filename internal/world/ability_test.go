package world

import "testing"

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
