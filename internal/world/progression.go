package world

import (
	"math"
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
	if len(w.gameplay.Progression.LevelExperience) > 0 {
		if level > len(w.gameplay.Progression.LevelExperience) {
			return w.gameplay.Progression.LevelExperience[len(w.gameplay.Progression.LevelExperience)-1]
		}
		return w.gameplay.Progression.LevelExperience[level-1]
	}
	return 0
}

func gainExperienceLocked(w *World, ch storage.Character, exp int) (storage.Character, string, int, bool, error) {
	if exp <= 0 {
		return ch, "", 0, false, nil
	}
	globalMultiple := w.gameplay.Progression.ExperienceMultiple
	if globalMultiple <= 0 {
		globalMultiple = 1
	}
	characterMultiple := ch.ExperienceMultiple
	if characterMultiple <= 0 {
		characterMultiple = 1
	}
	characterRate := ch.ExperienceRate
	if characterRate <= 0 {
		characterRate = 100
	}
	mapRate := 100
	if mp, ok := w.data.Maps[ch.MapID]; ok && mp.ExperienceRate > 0 {
		mapRate = mp.ExperienceRate
	}
	exp = exp * globalMultiple
	exp = exp * characterMultiple
	exp = int(math.Round(float64(characterRate) / 100 * float64(exp)))
	exp = int(math.Round(float64(mapRate) / 100 * float64(exp)))
	if exp <= 0 {
		exp = 1
	}
	ch.Experience += exp
	gained := exp
	leveled := false
	for {
		required := w.RequiredExperience(ch.Level)
		if required <= 0 {
			break
		}
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
