package world

import (
	"fmt"
	"math"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
	"openmir2/internal/world/core"
)

const (
	skillExplosionRadius      = 1
	skillElectricBlizzardSize = 2
	skillLightningRange       = 8
)

func (w *World) charactersInRadiusLocked(players []storage.Character, mapID string, x, y, radius int) []storage.Character {
	if radius < 0 {
		radius = 0
	}
	affected := make([]storage.Character, 0, 8)
	seen := map[string]struct{}{}
	for _, target := range players {
		if target.ID == "" || target.MapID != mapID || target.HP <= 0 {
			continue
		}
		if abs(target.X-x) > radius || abs(target.Y-y) > radius {
			continue
		}
		if _, ok := seen[target.ID]; ok {
			continue
		}
		seen[target.ID] = struct{}{}
		affected = append(affected, target)
	}
	return affected
}

func (w *World) spellCharacterDamageWithPowerLocked(caster storage.Character, target storage.Character, damage int) (storage.Character, CharacterHit, error) {
	now := time.Now()
	if damage < 1 {
		damage = 1
	}
	stats := w.combatStatsLocked(target)
	_, magicDefenceBonus, bubbleLevel, bubbleActive := activeProtectionBuffs(target, now)
	armor := stats.MAC
	low := armor & 0xFF
	high := (armor >> 8) & 0xFF
	if magicDefenceBonus > 0 {
		high = minInt(255, high+magicDefenceBonus)
	}
	if high < low {
		high = low
	}
	if high > low {
		damage -= low + w.rand.Intn(high-low+1)
	} else {
		damage -= low
	}
	if damage < 0 {
		damage = 0
	}
	if damage > 0 && bubbleActive {
		damage = int(math.Round(float64(damage) * float64(int(bubbleLevel)+2) * 8.0 / 100.0))
		if damage < 0 {
			damage = 0
		}
		remaining := time.Until(time.Unix(0, target.BubbleDefenceUntil))
		if remaining > 3*time.Second {
			remaining -= 3 * time.Second
		} else {
			remaining = time.Second
		}
		target.BubbleDefenceUntil = now.Add(remaining).UnixNano()
	}
	change := core.ApplyVitalDelta(target, -damage, 0)
	target = change.Character
	hit := CharacterHit{
		Character:  target,
		Damage:     damage,
		AttackerID: caster.ID,
		AttackerX:  caster.X,
		AttackerY:  caster.Y,
		Dead:       change.Dead,
	}
	return target, hit, w.store.SaveCharacter(target)
}

func (w *World) castLightningLineSkillLocked(result *SkillCastResult, ch storage.Character, skill data.StdSkill, state storage.SkillState, players []storage.Character, targetX, targetY, targetID int32) (storage.Character, error) {
	if int(targetX) == ch.X && int(targetY) == ch.Y {
		return ch, fmt.Errorf("no valid target")
	}
	if _, ok := w.data.Maps[ch.MapID]; !ok {
		return ch, fmt.Errorf("map %s not found", ch.MapID)
	}
	dir := direction(ch.X, ch.Y, int(targetX), int(targetY))
	dx, dy := dirOffsets[dir][0], dirOffsets[dir][1]
	damage := w.spellMonsterDamageLocked(ch, skill, state)
	monsterHits := make([]AttackResult, 0, skillLightningRange)
	characterHits := make([]CharacterHit, 0, skillLightningRange)
	hitMonsters := map[string]struct{}{}
	hitCharacters := map[string]struct{}{}
	sx, sy := ch.X, ch.Y
	for i := 0; i < skillLightningRange; i++ {
		sx += dx
		sy += dy
		mon := w.monsterAtPointLocked(ch.MapID, sx, sy, 1)
		if mon != nil {
			if _, seen := hitMonsters[mon.ID]; !seen {
				applied := damage
				if mon.Undead > 0 {
					applied = int(math.Round(float64(applied) * 1.5))
					if applied < 1 {
						applied = 1
					}
				}
				attackResult, err := w.attackMonsterWithMagicDamageLocked(ch, mon, applied)
				if err != nil {
					return ch, err
				}
				ch = attackResult.Character
				monsterHits = append(monsterHits, attackResult)
				hitMonsters[mon.ID] = struct{}{}
			}
		}
		target, ok := w.characterAtPointLocked(players, ch.MapID, sx, sy, targetID)
		if ok {
			if _, seen := hitCharacters[target.ID]; !seen {
				_, hit, err := w.spellCharacterDamageWithPowerLocked(ch, target, damage)
				if err != nil {
					return ch, err
				}
				characterHits = append(characterHits, hit)
				hitCharacters[target.ID] = struct{}{}
			}
		}
	}
	if len(monsterHits) > 0 {
		result.MonsterHit = &monsterHits[0]
		result.MonsterHits = monsterHits
		last := monsterHits[len(monsterHits)-1]
		result.Experience = 0
		for _, hit := range monsterHits {
			result.Experience += hit.Experience
			if hit.LevelUp {
				result.LevelUp = true
			}
		}
		result.CurrentExp = last.CurrentExp
	}
	if len(characterHits) > 0 {
		result.CharacterHits = characterHits
	}
	result.Character = ch
	return ch, nil
}

func (w *World) castExplosionSkillLocked(result *SkillCastResult, ch storage.Character, skill data.StdSkill, state storage.SkillState, targetX, targetY int, players []storage.Character) (storage.Character, error) {
	if _, ok := w.data.Maps[ch.MapID]; !ok {
		return ch, fmt.Errorf("map %s not found", ch.MapID)
	}
	damage := w.spellMonsterDamageLocked(ch, skill, state)
	monsterHits := make([]AttackResult, 0, 8)
	characterHits := make([]CharacterHit, 0, 8)
	hitMonsters := map[string]struct{}{}
	hitCharacters := map[string]struct{}{}
	for _, mon := range w.monstersInRadiusLocked(ch.MapID, targetX, targetY, skillExplosionRadius) {
		if _, seen := hitMonsters[mon.ID]; seen {
			continue
		}
		attackResult, err := w.attackMonsterWithMagicDamageLocked(ch, mon, damage)
		if err != nil {
			return ch, err
		}
		ch = attackResult.Character
		monsterHits = append(monsterHits, attackResult)
		hitMonsters[mon.ID] = struct{}{}
	}
	for _, target := range w.charactersInRadiusLocked(players, ch.MapID, targetX, targetY, skillExplosionRadius) {
		if _, seen := hitCharacters[target.ID]; seen {
			continue
		}
		_, hit, err := w.spellCharacterDamageWithPowerLocked(ch, target, damage)
		if err != nil {
			return ch, err
		}
		characterHits = append(characterHits, hit)
		hitCharacters[target.ID] = struct{}{}
	}
	if len(monsterHits) > 0 {
		result.MonsterHit = &monsterHits[0]
		result.MonsterHits = monsterHits
		last := monsterHits[len(monsterHits)-1]
		for _, hit := range monsterHits {
			result.Experience += hit.Experience
			if hit.LevelUp {
				result.LevelUp = true
			}
		}
		result.CurrentExp = last.CurrentExp
	}
	if len(characterHits) > 0 {
		result.CharacterHits = characterHits
	}
	result.Character = ch
	return ch, nil
}

func (w *World) castElectricBlizzardSkillLocked(result *SkillCastResult, ch storage.Character, skill data.StdSkill, state storage.SkillState, players []storage.Character) (storage.Character, error) {
	damage := w.spellMonsterDamageLocked(ch, skill, state)
	monsterHits := make([]AttackResult, 0, 8)
	characterHits := make([]CharacterHit, 0, 8)
	hitMonsters := map[string]struct{}{}
	hitCharacters := map[string]struct{}{}
	for _, mon := range w.monstersInRadiusLocked(ch.MapID, ch.X, ch.Y, skillElectricBlizzardSize) {
		if _, seen := hitMonsters[mon.ID]; seen {
			continue
		}
		applied := damage
		if mon.Undead <= 0 {
			applied = maxInt(1, damage/10)
		}
		attackResult, err := w.attackMonsterWithMagicDamageLocked(ch, mon, applied)
		if err != nil {
			return ch, err
		}
		ch = attackResult.Character
		monsterHits = append(monsterHits, attackResult)
		hitMonsters[mon.ID] = struct{}{}
	}
	for _, target := range w.charactersInRadiusLocked(players, ch.MapID, ch.X, ch.Y, skillElectricBlizzardSize) {
		if _, seen := hitCharacters[target.ID]; seen {
			continue
		}
		_, hit, err := w.spellCharacterDamageWithPowerLocked(ch, target, maxInt(1, damage/10))
		if err != nil {
			return ch, err
		}
		characterHits = append(characterHits, hit)
		hitCharacters[target.ID] = struct{}{}
	}
	if len(monsterHits) > 0 {
		result.MonsterHit = &monsterHits[0]
		result.MonsterHits = monsterHits
		last := monsterHits[len(monsterHits)-1]
		for _, hit := range monsterHits {
			result.Experience += hit.Experience
			if hit.LevelUp {
				result.LevelUp = true
			}
		}
		result.CurrentExp = last.CurrentExp
	}
	if len(characterHits) > 0 {
		result.CharacterHits = characterHits
	}
	result.Character = ch
	return ch, nil
}
