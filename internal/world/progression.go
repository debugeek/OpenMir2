package world

import (
	"openmir2/internal/data"
	"openmir2/internal/storage"
	"openmir2/internal/world/core"
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
	return ch.Level >= skill.NeedLevel1
}

func hasSkill(ch storage.Character, skillID string) bool {
	return ch.Skills.Has(skillID)
}

func learnSkill(ch *storage.Character, skillID string) bool {
	return (&ch.Skills).Learn(skillID)
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
		ch.MaxMP = base.MaxMP
		ch = core.SetVitals(ch, base.MaxHP, base.MaxMP).Character
	}
	return ch, "", gained, leveled, nil
}
