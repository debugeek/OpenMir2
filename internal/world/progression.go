package world

import (
	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func canLearnSkill(ch storage.Character, skill data.StdSkill) bool {
	job := 0
	switch NormalizeClass(ch.Class) {
	case "wizard":
		job = 1
	case "taoist":
		job = 2
	}
	if skill.Job != 99 && skill.Job != job {
		return false
	}
	return ch.Level >= skill.TrainLevel1
}

func hasSkill(ch storage.Character, skillID string) bool {
	for _, learned := range ch.Skills {
		if learned == skillID {
			return true
		}
	}
	return false
}

func (w *World) RequiredExperience(level int) int {
	if level <= 0 {
		level = 1
	}
	return level * w.gameplay.Progression.RequiredExperiencePerLevel
}

func gainExperienceLocked(w *World, ch storage.Character, exp int) (storage.Character, string, int, bool, error) {
	if exp <= 0 {
		return ch, "", 0, false, nil
	}
	ch.Experience += exp
	gained := exp
	leveled := false
	for {
		required := w.RequiredExperience(ch.Level)
		if ch.Experience < required {
			break
		}
		ch.Experience -= required
		ch.Level++
		leveled = true
	}
	if leveled {
		base := Base(ch.Class, ch.Level)
		ch.MaxHP = base.MaxHP
		ch.HP = base.MaxHP
		ch.MaxMP = base.MaxMP
		ch.MP = base.MaxMP
	}
	return ch, "", gained, leveled, nil
}
