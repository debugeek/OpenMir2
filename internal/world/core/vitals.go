package core

import "openmir2/internal/storage"

type VitalChange struct {
	Character storage.Character
	Changed   bool
	Dead      bool
}

type HPChange struct {
	HP      int
	Changed bool
	Dead    bool
}

func ApplyHPDelta(hp, maxHP, delta int) HPChange {
	next := clampInt(hp+delta, 0, maxHP)
	return HPChange{
		HP:      next,
		Changed: next != hp,
		Dead:    hp > 0 && next == 0,
	}
}

func ApplyVitalDelta(ch storage.Character, hpDelta, mpDelta int) VitalChange {
	next := ch
	hp := ApplyHPDelta(next.HP, next.MaxHP, hpDelta)
	next.HP = hp.HP
	next.MP = clampInt(next.MP+mpDelta, 0, next.MaxMP)
	return VitalChange{
		Character: next,
		Changed:   hp.Changed || next.MP != ch.MP,
		Dead:      hp.Dead,
	}
}

func SetVitals(ch storage.Character, hp, mp int) VitalChange {
	next := ch
	hpChange := ApplyHPDelta(ch.HP, next.MaxHP, hp-ch.HP)
	next.HP = hpChange.HP
	next.MP = clampInt(mp, 0, next.MaxMP)
	return VitalChange{
		Character: next,
		Changed:   hpChange.Changed || next.MP != ch.MP,
		Dead:      hpChange.Dead,
	}
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
