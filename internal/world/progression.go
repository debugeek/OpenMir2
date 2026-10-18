package world

import (
	"math"

	"openmir2/internal/config"
	"openmir2/internal/data"
	"openmir2/internal/storage"
	"openmir2/internal/world/core"
)

var groupExperienceBonus = [...]float64{1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9, 2, 2.1, 2.2}

const maxCharacterLevel = 65535

func monsterExperienceForCharacter(combat config.CombatSettings, characterLevel, monsterLevel, baseExperience int) int {
	if baseExperience <= 0 {
		return baseExperience
	}
	if combat.HighLevelKillMonFixExp || characterLevel < monsterLevel+10 {
		return baseExperience
	}
	result := baseExperience - referenceRound(float64(baseExperience)/15*float64(characterLevel-(monsterLevel+10)))
	if result <= 0 {
		return 1
	}
	return result
}

func (w *World) gainMonsterExperienceLocked(killer storage.Character, baseExperience int, players map[string]storage.Character) ([]SpellExperience, storage.Character, error) {
	if baseExperience <= 0 || killer.ID == "" {
		return nil, killer, nil
	}
	owner, ok := players[killer.GroupOwnerID]
	if killer.GroupOwnerID == "" || !ok {
		updated, _, gained, levelUp, err := gainExperienceLocked(w, killer, baseExperience)
		if err != nil {
			return nil, killer, err
		}
		return []SpellExperience{{CharacterID: updated.ID, Experience: gained, CurrentExp: updated.Experience, LevelUp: levelUp, Character: updated}}, updated, nil
	}
	ids := owner.GroupMembers
	if len(ids) == 0 {
		ids = []string{owner.ID}
	}
	recipients := make([]storage.Character, 0, len(ids))
	for _, id := range ids {
		member, exists := players[id]
		if !exists || member.ID == "" || member.HP <= 0 || member.MapID != killer.MapID || abs(member.X-killer.X) > 12 || abs(member.Y-killer.Y) > 12 {
			continue
		}
		recipients = append(recipients, member)
	}
	levelSum := 0
	for _, member := range recipients {
		levelSum += member.Level
	}
	if len(recipients) <= 1 || levelSum <= 0 {
		updated, _, gained, levelUp, err := gainExperienceLocked(w, killer, baseExperience)
		if err != nil {
			return nil, killer, err
		}
		return []SpellExperience{{CharacterID: updated.ID, Experience: gained, CurrentExp: updated.Experience, LevelUp: levelUp, Character: updated}}, updated, nil
	}
	sharedExperience := float64(baseExperience)
	if len(recipients) < len(groupExperienceBonus) {
		sharedExperience *= groupExperienceBonus[len(recipients)]
	}
	result := make([]SpellExperience, 0, len(recipients))
	updatedKiller := killer
	for _, member := range recipients {
		share := sharedExperience / float64(len(recipients))
		if !w.gameplay.Combat.HighLevelGroupFixExp && levelSum > 0 {
			share = sharedExperience * float64(member.Level) / float64(levelSum)
		}
		updated, _, gained, levelUp, err := gainExperienceLocked(w, member, referenceRound(share))
		if err != nil {
			return nil, killer, err
		}
		players[updated.ID] = updated
		if updated.ID == killer.ID {
			updatedKiller = updated
		}
		result = append(result, SpellExperience{CharacterID: updated.ID, Experience: gained, CurrentExp: updated.Experience, LevelUp: levelUp, Character: updated})
	}
	return result, updatedKiller, nil
}

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
	exp = referenceRound(float64(characterRate) / 100 * float64(exp))
	exp = referenceRound(float64(mapRate) / 100 * float64(exp))
	if exp <= 0 {
		exp = 1
	}
	ch.Experience += exp
	addBodyLuck(&ch, float64(exp)*0.002)
	gained := exp
	leveled := false
	required := w.RequiredExperience(ch.Level)
	if required > 0 && ch.Experience >= required {
		ch.Experience -= required
		if ch.Level < maxCharacterLevel {
			ch.Level++
		}
		addBodyLuck(&ch, 100)
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

func addBodyLuck(ch *storage.Character, delta float64) {
	if ch == nil {
		return
	}
	if delta > 0 && ch.BodyLuck >= 25000 || delta < 0 && ch.BodyLuck <= -50000 {
		return
	}
	ch.BodyLuck += delta
	if ch.BodyLuck > 25000 {
		ch.BodyLuck = 25000
	}
	if ch.BodyLuck < -50000 {
		ch.BodyLuck = -50000
	}
	ch.BodyLuckLevel = int(math.Trunc(ch.BodyLuck / 5000))
	if ch.BodyLuckLevel > 5 {
		ch.BodyLuckLevel = 5
	}
	if ch.BodyLuckLevel < -10 {
		ch.BodyLuckLevel = -10
	}
}
