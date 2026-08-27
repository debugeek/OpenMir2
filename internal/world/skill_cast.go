package world

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
	"openmir2/internal/world/core"
)

const defaultTurnUndeadLevel = 50
const defaultMagicAttackRange = 8
const defaultTamingCount = 5
const defaultTamingHPRate = 100

type SkillCastResult struct {
	Character          storage.Character
	SkillID            string
	ManaCost           int
	CooldownMS         int
	Experience         int
	CurrentExp         int
	LevelUp            bool
	SkillChanged       bool
	MonsterHit         *AttackResult
	MonsterHits        []AttackResult
	MonsterActions     []MonsterAction
	CharacterHits      []CharacterHit
	AffectedCharacters []storage.Character
	AffectedMonsters   []Monster
	SummonedMonsters   []Monster
}

func (w *World) SpellCost(skill data.StdSkill, state storage.SkillState) int {
	if skill.Spell <= 0 {
		return 0
	}
	trainLevel := skill.TrainLevel1
	if trainLevel < 0 {
		trainLevel = 0
	}
	cost := int(math.Round(float64(skill.Spell) / float64(trainLevel+1) * float64(int(state.Level)+1)))
	if cost < 0 {
		return 0
	}
	return cost
}

func (w *World) CastSkill(ch storage.Character, skillID string, targetX, targetY int, targetID int32) (SkillCastResult, error) {
	return w.CastSkillWithPlayers(ch, skillID, targetX, targetY, targetID, nil)
}

func (w *World) CastSkillWithPlayers(ch storage.Character, skillID string, targetX, targetY int, targetID int32, players []storage.Character) (SkillCastResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.normalizeEquippedItemsLocked(&ch)
	state, idx, ok := ch.Skills.Get(skillID)
	if !ok {
		return SkillCastResult{}, fmt.Errorf("skill %s not learned", skillID)
	}
	skill, ok := w.data.Skills[skillID]
	if !ok {
		return SkillCastResult{}, fmt.Errorf("skill %s not found", skillID)
	}
	if state.Locked {
		return SkillCastResult{}, fmt.Errorf("skill %s is locked", skillID)
	}
	now := time.Now()
	if state.LastCastAt > 0 && skill.Delay > 0 {
		lastCast := time.UnixMilli(state.LastCastAt)
		if elapsed := now.Sub(lastCast); elapsed < time.Duration(skill.Delay)*time.Millisecond {
			return SkillCastResult{}, fmt.Errorf("skill %s is cooling down", skillID)
		}
	}
	cost := w.SpellCost(skill, state)
	if ch.MP < cost {
		return SkillCastResult{}, fmt.Errorf("not enough mp")
	}
	if ch.MapID == "" {
		return SkillCastResult{}, fmt.Errorf("character has no current map")
	}
	if targetX < 0 || targetY < 0 {
		return SkillCastResult{}, fmt.Errorf("invalid spell target")
	}
	if abs(ch.X-targetX) > defaultMagicAttackRange || abs(ch.Y-targetY) > defaultMagicAttackRange {
		return SkillCastResult{}, fmt.Errorf("spell target out of range")
	}

	ch.MP -= cost
	start := ch
	result := SkillCastResult{
		Character:  ch,
		SkillID:    skillID,
		ManaCost:   cost,
		CooldownMS: skill.Delay,
	}
	var err error
	skillTrained := false
	poisonApplied := false
	fireWallCreated := 0

	switch skillID {
	case "火球术", "大火球":
		mon := w.monsterAtPointLocked(ch.MapID, targetX, targetY, 1)
		if mon == nil {
			return SkillCastResult{}, fmt.Errorf("no valid monster target")
		}
		damage := w.spellMonsterDamageLocked(ch, skill, state)
		attackResult, err := w.attackMonsterWithDamageLocked(ch, mon, damage)
		if err != nil {
			return SkillCastResult{}, err
		}
		ch = attackResult.Character
		result.Character = ch
		result.MonsterHit = &attackResult
		result.Experience = attackResult.Experience
		result.CurrentExp = attackResult.CurrentExp
		result.LevelUp = attackResult.LevelUp
		skillTrained = true
	case "冰咆哮":
		targets := w.monstersInRadiusLocked(ch.MapID, targetX, targetY, 1)
		if len(targets) == 0 {
			return SkillCastResult{}, fmt.Errorf("no valid monster target")
		}
		damage := w.spellMonsterDamageLocked(ch, skill, state)
		hits := make([]AttackResult, 0, len(targets))
		totalExp := 0
		levelUp := false
		for _, mon := range targets {
			attackResult, err := w.attackMonsterWithDamageLocked(ch, mon, damage)
			if err != nil {
				return SkillCastResult{}, err
			}
			ch = attackResult.Character
			hits = append(hits, attackResult)
			totalExp += attackResult.Experience
			if attackResult.LevelUp {
				levelUp = true
			}
		}
		if len(hits) > 0 {
			result.MonsterHit = &hits[0]
			result.MonsterHits = hits
			last := hits[len(hits)-1]
			result.Experience = totalExp
			result.CurrentExp = last.CurrentExp
			result.LevelUp = levelUp
			skillTrained = true
		}
		result.Character = ch
	case "治愈术":
		heal := w.spellHealAmountLocked(ch, skill, state)
		target, ok := w.characterAtPointLocked(players, ch.MapID, targetX, targetY, targetID)
		if !ok {
			if mon := w.monsterAtPointLocked(ch.MapID, targetX, targetY, 1); mon != nil {
				if !w.isFriendlySummonedMonsterLocked(ch, players, mon) {
					return SkillCastResult{}, fmt.Errorf("no valid healing target")
				}
				change := core.ApplyVitalDelta(storage.Character{HP: mon.HP, MaxHP: mon.MaxHP}, heal, 0)
				if !change.Changed {
					return SkillCastResult{}, fmt.Errorf("no valid healing target")
				}
				mon.HP = change.Character.HP
				result.AffectedMonsters = []Monster{*mon}
				skillTrained = true
				result.Character = ch
				break
			}
			target = ch
		} else if !w.isProperFriendLocked(ch, target) {
			return SkillCastResult{}, fmt.Errorf("no valid healing target")
		}
		change := core.ApplyVitalDelta(target, heal, 0)
		target = change.Character
		if target.ID == ch.ID {
			ch = target
			result.Character = ch
		} else {
			result.AffectedCharacters = []storage.Character{target}
		}
		skillTrained = change.Changed
	case "群体治疗术":
		heal := w.spellHealAmountLocked(ch, skill, state)
		affected, affectedMonsters, changed, err := w.groupHealCharactersLocked(ch, players, targetX, targetY, heal)
		if err != nil {
			return SkillCastResult{}, err
		}
		if len(affected) == 0 && len(affectedMonsters) == 0 && !changed {
			return SkillCastResult{}, fmt.Errorf("no valid healing targets")
		}
		result.AffectedCharacters = affected
		result.AffectedMonsters = affectedMonsters
		for _, target := range affected {
			if target.ID == ch.ID {
				ch = target
				result.Character = target
				break
			}
		}
		skillTrained = true
	case "神圣战甲术":
		affected, affectedMonsters, err := w.groupDefenceCharactersLocked(ch, skill, state, players, targetX, targetY, false)
		if err != nil {
			return SkillCastResult{}, err
		}
		if len(affected) == 0 && len(affectedMonsters) == 0 {
			return SkillCastResult{}, fmt.Errorf("no valid defence targets")
		}
		result.AffectedCharacters = affected
		result.AffectedMonsters = affectedMonsters
		for _, target := range affected {
			if target.ID == ch.ID {
				ch = target
				result.Character = target
				break
			}
		}
		skillTrained = true
	case "幽灵盾":
		affected, affectedMonsters, err := w.groupDefenceCharactersLocked(ch, skill, state, players, targetX, targetY, true)
		if err != nil {
			return SkillCastResult{}, err
		}
		if len(affected) == 0 && len(affectedMonsters) == 0 {
			return SkillCastResult{}, fmt.Errorf("no valid defence targets")
		}
		result.AffectedCharacters = affected
		result.AffectedMonsters = affectedMonsters
		for _, target := range affected {
			if target.ID == ch.ID {
				ch = target
				result.Character = target
				break
			}
		}
		skillTrained = true
	case "魔法盾":
		if ch.BubbleDefenceUntil > now.UnixNano() {
			return SkillCastResult{}, fmt.Errorf("skill %s is already active", skillID)
		}
		duration := w.magicShieldDurationLocked(ch, skill, state)
		if duration < time.Second {
			duration = time.Second
		}
		ch.BubbleDefenceUntil = now.Add(duration).UnixNano()
		ch.BubbleDefenceLevel = state.Level
		result.Character = ch
		skillTrained = true
	case "圣言术":
		mon := w.monsterAtPointLocked(ch.MapID, targetX, targetY, 1)
		if mon == nil {
			return SkillCastResult{}, fmt.Errorf("no valid monster target")
		}
		if mon.Undead <= 0 {
			return SkillCastResult{}, fmt.Errorf("no valid undead target")
		}
		casterLevel := maxInt(ch.Level, 1)
		if mon.Level >= defaultTurnUndeadLevel {
			result.Character = ch
			break
		}
		if w.rand.Intn(2)+(casterLevel-1) <= mon.Level {
			result.Character = ch
			break
		}
		chance := (int(state.Level) * 8) - int(state.Level) + 15 + (casterLevel - mon.Level)
		if chance < 0 {
			chance = 0
		}
		if w.rand.Intn(100) < chance {
			attackResult, err := w.killMonsterWithDamageLocked(ch, mon, mon.HP)
			if err != nil {
				return SkillCastResult{}, err
			}
			ch = attackResult.Character
			result.Character = ch
			result.MonsterHit = &attackResult
			result.Experience = attackResult.Experience
			result.CurrentExp = attackResult.CurrentExp
			result.LevelUp = attackResult.LevelUp
			skillTrained = true
		} else {
			result.Character = ch
		}
	case "隐身术":
		duration := w.stealthDurationLocked(ch, skill, state, 30)
		if characterTransparentActive(ch, now) {
			return SkillCastResult{}, fmt.Errorf("skill %s is already active", skillID)
		}
		setCharacterTransparentLocked(&ch, now.Add(duration))
		w.breakNearbyMonsterTargetsForStealthLocked(ch)
		result.Character = ch
		skillTrained = true
	case "集体隐身术":
		duration := w.stealthDurationLocked(ch, skill, state, 30)
		affected := w.stealthAffectedTargetsLocked(ch, players, targetX, targetY)
		if len(affected) == 0 {
			return SkillCastResult{}, fmt.Errorf("no valid stealth targets")
		}
		for i := range affected {
			if characterTransparentActive(affected[i], now) {
				continue
			}
			if setCharacterTransparentLocked(&affected[i], now.Add(duration)) {
				w.breakNearbyMonsterTargetsForStealthLocked(affected[i])
				result.AffectedCharacters = append(result.AffectedCharacters, affected[i])
				if err := w.store.SaveCharacter(affected[i]); err != nil {
					return SkillCastResult{}, err
				}
			}
			if affected[i].ID == ch.ID {
				ch = affected[i]
				result.Character = ch
			}
		}
		if len(result.AffectedCharacters) == 0 {
			return SkillCastResult{}, fmt.Errorf("no valid stealth targets")
		}
		skillTrained = true
	case "瞬息移动":
		if w.rand.Intn(11) >= int(state.Level)*2+4 {
			result.Character = ch
			break
		}
		next, err := w.homeTeleportRandomCharacterLocked(ch)
		if err != nil {
			return SkillCastResult{}, err
		}
		ch = next
		result.Character = ch
		skillTrained = true
	case "心灵启示":
		target, ok := w.characterAtPointLocked(players, ch.MapID, targetX, targetY, targetID)
		if !ok {
			return SkillCastResult{}, fmt.Errorf("no valid inspection target")
		}
		if target.ShowHPOpenAt > now.UnixNano() || target.ShowHPUntil > now.UnixNano() {
			result.Character = ch
			break
		}
		if w.rand.Intn(6) > int(state.Level)+3 {
			result.Character = ch
			break
		}
		duration := w.showHealthDurationLocked(ch, skill, state)
		target.ShowHPOpenAt = now.Add(1500 * time.Millisecond).UnixNano()
		target.ShowHPUntil = target.ShowHPOpenAt + duration.Nanoseconds()
		result.AffectedCharacters = []storage.Character{target}
		skillTrained = true
	case "施毒术":
		poisonItem, ok := w.consumePoisonPowderLocked(&ch)
		if !ok {
			return SkillCastResult{}, fmt.Errorf("no valid poison target")
		}
		poisonStd, ok := w.data.Items[poisonItem.ItemID]
		if !ok {
			return SkillCastResult{}, fmt.Errorf("item %s not found", poisonItem.ItemID)
		}
		applyHealthPoison := poisonStd.Shape == 1
		applyArmorPoison := poisonStd.Shape == 2
		if !applyHealthPoison && !applyArmorPoison {
			return SkillCastResult{}, fmt.Errorf("no valid poison target")
		}
		basePower := poisonBaseHealthPowerGrey
		if applyArmorPoison {
			basePower = poisonBaseHealthPowerYellow
		}
		poisonPower := w.poisonSpellPowerLocked(ch, skill, state, basePower)
		duration := poisonEffectDuration(poisonPower)
		poisonLevel := poisonLevelFromPower(maxInt(1, int(math.Round(float64(int(state.Level)+1)/3.0*float64(poisonPower)/10.0))))
		if mon := w.monsterAtPointLocked(ch.MapID, targetX, targetY, 1); mon != nil {
			if applyHealthPoison {
				setMonsterHealthPoisonLocked(mon, poisonLevel, now.Add(duration), ch.ID, now)
			}
			if applyArmorPoison {
				setMonsterArmorPoisonLocked(mon, now.Add(duration))
			}
			result.Character = ch
			poisonApplied = true
			skillTrained = true
			break
		}
		target, ok := w.characterAtPointLocked(players, ch.MapID, targetX, targetY, targetID)
		if !ok {
			return SkillCastResult{}, fmt.Errorf("no valid poison target")
		}
		if !poisonArmorChanceOK(w.rand, w.poisonAvoidanceLocked(target)) {
			result.Character = ch
			break
		}
		if applyHealthPoison {
			setCharacterHealthPoisonLocked(&target, poisonLevel, now.Add(duration))
		}
		if applyArmorPoison {
			setCharacterArmorPoisonLocked(&target, now.Add(duration))
		}
		result.AffectedCharacters = []storage.Character{target}
		if target.ID == ch.ID {
			ch = target
			result.Character = target
		}
		poisonApplied = true
		skillTrained = true
	case "灵魂火符":
		if mon := w.monsterAtPointLocked(ch.MapID, targetX, targetY, 1); mon != nil {
			if w.rand.Intn(10) < maxInt(0, minInt(10, mon.MagicDefense)) {
				result.Character = ch
				break
			}
			damage := w.spellSpiritDamageLocked(ch, skill, state)
			attackResult, err := w.attackMonsterWithDamageLocked(ch, mon, damage)
			if err != nil {
				return SkillCastResult{}, err
			}
			ch = attackResult.Character
			result.Character = ch
			result.MonsterHit = &attackResult
			result.Experience = attackResult.Experience
			result.CurrentExp = attackResult.CurrentExp
			result.LevelUp = attackResult.LevelUp
			skillTrained = true
			break
		}
		target, ok := w.characterAtPointLocked(players, ch.MapID, targetX, targetY, targetID)
		if !ok {
			return SkillCastResult{}, fmt.Errorf("no valid target")
		}
		if w.rand.Intn(10) < maxInt(0, minInt(10, w.combatStatsLocked(target).MAC)) {
			result.Character = ch
			break
		}
		_, hit, err := w.spellCharacterDamageWithPowerLocked(ch, target, w.spellSpiritDamageLocked(ch, skill, state))
		if err != nil {
			return SkillCastResult{}, err
		}
		result.Character = ch
		result.CharacterHits = []CharacterHit{hit}
		skillTrained = true
	case "雷电术":
		if mon := w.monsterAtPointLocked(ch.MapID, targetX, targetY, 1); mon != nil {
			damage := w.spellMonsterDamageLocked(ch, skill, state)
			if mon.Undead > 0 {
				damage = int(math.Round(float64(damage) * 1.5))
				if damage < 1 {
					damage = 1
				}
			}
			attackResult, err := w.attackMonsterWithDamageLocked(ch, mon, damage)
			if err != nil {
				return SkillCastResult{}, err
			}
			ch = attackResult.Character
			result.Character = ch
			result.MonsterHit = &attackResult
			result.Experience = attackResult.Experience
			result.CurrentExp = attackResult.CurrentExp
			result.LevelUp = attackResult.LevelUp
			skillTrained = true
			break
		}
		target, ok := w.characterAtPointLocked(players, ch.MapID, targetX, targetY, targetID)
		if !ok {
			return SkillCastResult{}, fmt.Errorf("no valid target")
		}
		_, hit, err := w.spellCharacterDamageLocked(ch, target, skill, state)
		if err != nil {
			return SkillCastResult{}, err
		}
		result.Character = ch
		result.CharacterHits = []CharacterHit{hit}
		skillTrained = true
	case "疾光电影":
		ch, err = w.castLightningLineSkillLocked(&result, ch, skill, state, players, int32(targetX), int32(targetY), targetID)
		if err != nil {
			return SkillCastResult{}, err
		}
		skillTrained = len(result.MonsterHits) > 0 || len(result.CharacterHits) > 0
	case "爆裂火焰":
		ch, err = w.castExplosionSkillLocked(&result, ch, skill, state, targetX, targetY, players)
		if err != nil {
			return SkillCastResult{}, err
		}
		skillTrained = len(result.MonsterHits) > 0 || len(result.CharacterHits) > 0
	case "地狱雷光":
		ch, err = w.castElectricBlizzardSkillLocked(&result, ch, skill, state, players)
		if err != nil {
			return SkillCastResult{}, err
		}
		skillTrained = len(result.MonsterHits) > 0 || len(result.CharacterHits) > 0
	case "抗拒火环":
		ch, err = w.castPushAroundSkillLocked(&result, ch, state, players)
		if err != nil {
			return SkillCastResult{}, err
		}
		skillTrained = len(result.AffectedCharacters) > 0 || len(result.MonsterActions) > 0
	case "野蛮冲撞":
		ch, err = w.castChargeSkillLocked(&result, ch, state, players, targetX, targetY)
		if err != nil {
			return SkillCastResult{}, err
		}
		skillTrained = ch.X != start.X || ch.Y != start.Y || len(result.AffectedCharacters) > 0 || len(result.MonsterActions) > 0
	case "火墙":
		fireWallCreated = w.castFireWallLocked(ch, skill, state, targetX, targetY, now)
		result.Character = ch
		skillTrained = fireWallCreated > 0
	case "召唤骷髅", "召唤神兽":
		templateID := "骷髅"
		if skillID == "召唤神兽" {
			templateID = "神兽"
		}
		if w.countActiveSummonedMonstersLocked(ch.ID, "", now) > 0 {
			return SkillCastResult{}, fmt.Errorf("skill %s is already active", skillID)
		}
		summoned, err := w.summonMonsterNearCharacterLocked(ch, players, templateID, 10*24*time.Hour)
		if err != nil {
			return SkillCastResult{}, err
		}
		if summoned != nil {
			result.SummonedMonsters = []Monster{*summoned}
		}
		result.Character = ch
		skillTrained = len(result.SummonedMonsters) > 0
	case "诱惑之光":
		target := w.monsterAtPointLocked(ch.MapID, targetX, targetY, 1)
		if target == nil {
			return SkillCastResult{}, fmt.Errorf("no valid taming target")
		}
		if target.Undead > 0 {
			return SkillCastResult{}, fmt.Errorf("no valid taming target")
		}
		if target.Level > ch.Level+2 {
			return SkillCastResult{}, fmt.Errorf("no valid taming target")
		}
		if w.countActiveSummonedMonstersLocked(ch.ID, "", now) >= defaultTamingCount {
			return SkillCastResult{}, fmt.Errorf("too many controlled monsters")
		}
		hpGate := target.MaxHP / defaultTamingHPRate
		if hpGate <= 2 {
			hpGate = 2
		} else {
			hpGate *= 2
		}
		if ch.Level+w.rand.Intn(20)+int(state.Level)*5 > target.Level+10 {
			if w.rand.Intn(hpGate) != 0 {
				result.Character = ch
				break
			}
			if target.MasterID != "" && target.MasterID != ch.ID {
				target.HP = maxInt(1, target.HP/10)
			}
			duration := time.Duration(w.rand.Intn(maxInt(ch.Level, 1))+60*(ch.Level/10)+int(state.Level)*20) * time.Minute
			if duration < time.Minute {
				duration = time.Minute
			}
			target.TargetCharacterID = ""
			target.TargetFocusAt = time.Time{}
			target.NextSearchAt = now
			target.RunAwayMode = false
			target.MasterID = ch.ID
			target.MasterExpiresAt = now.Add(duration)
			result.AffectedMonsters = []Monster{*target}
			skillTrained = true
		}
		result.Character = ch
	case "地狱火":
		if targetX == ch.X && targetY == ch.Y {
			return SkillCastResult{}, fmt.Errorf("no valid target")
		}
		_, ok := w.data.Maps[ch.MapID]
		if !ok {
			return SkillCastResult{}, fmt.Errorf("map %s not found", ch.MapID)
		}
		dir := direction(ch.X, ch.Y, targetX, targetY)
		dx, dy := dirOffsets[dir][0], dirOffsets[dir][1]
		damage := w.spellMonsterDamageLocked(ch, skill, state)
		monsterHits := make([]AttackResult, 0, 5)
		characterHits := make([]CharacterHit, 0, 5)
		hitMonsters := map[string]struct{}{}
		hitCharacters := map[string]struct{}{}
		sx, sy := ch.X, ch.Y
		for i := 0; i < 5; i++ {
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
					attackResult, err := w.attackMonsterWithDamageLocked(ch, mon, applied)
					if err != nil {
						return SkillCastResult{}, err
					}
					ch = attackResult.Character
					monsterHits = append(monsterHits, attackResult)
					hitMonsters[mon.ID] = struct{}{}
				}
			}
			target, ok := w.characterAtPointLocked(players, ch.MapID, sx, sy, 0)
			if ok {
				if _, seen := hitCharacters[target.ID]; !seen {
					_, hit, err := w.spellCharacterDamageLocked(ch, target, skill, state)
					if err != nil {
						return SkillCastResult{}, err
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
			}
			result.CurrentExp = last.CurrentExp
			for _, hit := range monsterHits {
				if hit.LevelUp {
					result.LevelUp = true
					break
				}
			}
		}
		if len(characterHits) > 0 {
			result.CharacterHits = characterHits
		}
		result.Character = ch
		skillTrained = len(monsterHits) > 0 || len(characterHits) > 0
	case "困魔咒":
		radius := maxInt(1, int(state.Level))
		targets := w.monstersInRadiusLocked(ch.MapID, targetX, targetY, radius)
		if len(targets) == 0 {
			return SkillCastResult{}, fmt.Errorf("no valid monster target")
		}
		combat := w.combatStatsLocked(ch)
		damage := w.spellScaledPowerLocked(skill, state)
		low := combat.DC
		high := combat.DCMax
		if high < low {
			high = low
		}
		if high > low {
			damage += low + w.rand.Intn(high-low+1)
		} else {
			damage += low
		}
		if damage < 1 {
			damage = 1
		}
		hits := make([]AttackResult, 0, len(targets))
		totalExp := 0
		levelUp := false
		for _, mon := range targets {
			attackResult, err := w.attackMonsterWithDamageLocked(ch, mon, damage)
			if err != nil {
				return SkillCastResult{}, err
			}
			ch = attackResult.Character
			hits = append(hits, attackResult)
			totalExp += attackResult.Experience
			if attackResult.LevelUp {
				levelUp = true
			}
		}
		if len(hits) > 0 {
			result.MonsterHit = &hits[0]
			result.MonsterHits = hits
			last := hits[len(hits)-1]
			result.Experience = totalExp
			result.CurrentExp = last.CurrentExp
			result.LevelUp = levelUp
			skillTrained = true
		}
		result.Character = ch
	default:
		return SkillCastResult{}, fmt.Errorf("skill %s effect not implemented", skillID)
	}

	if skillID == "施毒术" {
		skillTrained = poisonApplied
	}
	state.LastCastAt = now.UnixMilli()
	if skillTrained {
		points := magicTrainPointsForSkill(w.rand)
		if w.applySkillTrainingLocked(ch.Level, skill, &state, points) {
			result.SkillChanged = true
		}
	}
	ch.Skills[idx] = state
	result.Character = ch
	if result.Character.ID != "" {
		if err := w.store.SaveCharacter(result.Character); err != nil {
			return SkillCastResult{}, err
		}
	}
	return result, nil
}

func (w *World) advanceSkillTrainingLocked(skill data.StdSkill, state *storage.SkillState) bool {
	if skill.TrainLevel1 <= 0 || state == nil {
		return false
	}
	if state.Level >= 3 || state.Train < skill.TrainLevel1 {
		if state.Train < 0 {
			state.Train = 0
		}
		return false
	}
	state.Train -= skill.TrainLevel1
	state.Level++
	if state.Train < 0 {
		state.Train = 0
	}
	return true
}

func (w *World) applySkillTrainingLocked(charLevel int, skill data.StdSkill, state *storage.SkillState, points int) bool {
	if points <= 0 {
		return false
	}
	if state == nil || state.Locked || state.Level >= 3 {
		return false
	}
	if skill.NeedLevel1 <= 0 || charLevel < skill.NeedLevel1 {
		return false
	}
	state.Train = minInt(65535, state.Train+points)
	w.advanceSkillTrainingLocked(skill, state)
	return true
}

func magicTrainPointsForSkill(r *rand.Rand) int {
	if r == nil {
		return 1
	}
	return r.Intn(3) + 1
}

func (w *World) countActiveSummonedMonstersLocked(masterID, templateID string, now time.Time) int {
	count := 0
	for _, mon := range w.monsters {
		if mon == nil || !mon.Alive || mon.MasterID != masterID {
			continue
		}
		if templateID != "" && mon.TemplateID != templateID {
			continue
		}
		if !mon.MasterExpiresAt.IsZero() && !now.Before(mon.MasterExpiresAt) {
			continue
		}
		count++
	}
	return count
}

func (w *World) groupHealCharactersLocked(caster storage.Character, players []storage.Character, targetX, targetY, heal int) ([]storage.Character, []Monster, bool, error) {
	affected := make([]storage.Character, 0, 8)
	affectedMonsters := make([]Monster, 0, 8)
	if heal < 1 {
		heal = 1
	}
	playerByID := make(map[string]storage.Character, len(players))
	for _, target := range players {
		if target.ID == "" {
			continue
		}
		playerByID[target.ID] = target
	}
	changed := false
	for _, target := range players {
		if target.MapID != caster.MapID || target.HP <= 0 {
			continue
		}
		if abs(target.X-targetX) > 1 || abs(target.Y-targetY) > 1 {
			continue
		}
		if !w.isProperFriendLocked(caster, target) {
			continue
		}
		change := core.ApplyVitalDelta(target, heal, 0)
		if !change.Changed {
			continue
		}
		target = change.Character
		affected = append(affected, target)
		if err := w.store.SaveCharacter(target); err != nil {
			return nil, nil, false, err
		}
		changed = true
	}
	for _, mon := range w.monsters {
		if mon == nil || !mon.Alive || mon.MapID != caster.MapID || mon.HP <= 0 {
			continue
		}
		if abs(mon.X-targetX) > 1 || abs(mon.Y-targetY) > 1 {
			continue
		}
		master, ok := playerByID[mon.MasterID]
		if !ok || !w.isProperFriendLocked(caster, master) {
			continue
		}
		hp := core.ApplyVitalDelta(storage.Character{HP: mon.HP, MaxHP: mon.MaxHP}, heal, 0)
		if !hp.Changed {
			continue
		}
		mon.HP = hp.Character.HP
		affectedMonsters = append(affectedMonsters, *mon)
		changed = true
	}
	return affected, affectedMonsters, changed, nil
}

func (w *World) groupDefenceDurationLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) time.Duration {
	combat := w.combatStatsLocked(ch)
	nInt := 60
	trainLevel := skill.TrainLevel1
	if trainLevel < 0 {
		trainLevel = 0
	}
	d10 := float64(nInt) / 3.0
	d18 := float64(nInt) - d10
	duration := int(math.Round(d18/float64(trainLevel+1)*float64(int(state.Level)+1) + d10))
	low := combat.SC
	high := combat.SCMax
	if high < low {
		high = low
	}
	if high > low {
		duration += (low + w.rand.Intn(high-low+1)) * 10
	} else {
		duration += low * 10
	}
	if duration < 1 {
		duration = 1
	}
	return time.Duration(duration) * time.Second
}

func (w *World) groupDefenceCharactersLocked(caster storage.Character, skill data.StdSkill, state storage.SkillState, players []storage.Character, targetX, targetY int, magic bool) ([]storage.Character, []Monster, error) {
	affected := make([]storage.Character, 0, 8)
	affectedMonsters := make([]Monster, 0, 8)
	duration := w.groupDefenceDurationLocked(caster, skill, state)
	now := time.Now()
	expires := now.Add(duration).UnixNano()
	for _, target := range players {
		if target.ID == "" || target.MapID != caster.MapID || target.HP <= 0 {
			continue
		}
		if abs(target.X-targetX) > 3 || abs(target.Y-targetY) > 3 {
			continue
		}
		if !w.isProperFriendLocked(caster, target) {
			continue
		}
		next := target
		changed := false
		if magic {
			if next.MagDefenceUpUntil < expires {
				next.MagDefenceUpUntil = expires
				changed = true
			}
		} else {
			if next.DefenceUpUntil < expires {
				next.DefenceUpUntil = expires
				changed = true
			}
		}
		if !changed {
			continue
		}
		affected = append(affected, next)
		if err := w.store.SaveCharacter(next); err != nil {
			return nil, nil, err
		}
	}
	for _, mon := range w.monstersInRadiusLocked(caster.MapID, targetX, targetY, 3) {
		if mon == nil || !w.isFriendlySummonedMonsterLocked(caster, players, mon) {
			continue
		}
		changed := false
		if magic {
			if mon.MagDefenceUpUntil < expires {
				mon.MagDefenceUpUntil = expires
				changed = true
			}
		} else {
			if mon.DefenceUpUntil < expires {
				mon.DefenceUpUntil = expires
				changed = true
			}
		}
		if changed {
			affectedMonsters = append(affectedMonsters, *mon)
		}
	}
	return affected, affectedMonsters, nil
}

func (w *World) monstersInRadiusLocked(mapID string, x, y, radius int) []*Monster {
	if radius < 0 {
		radius = 0
	}
	out := make([]*Monster, 0, 8)
	for _, mon := range w.monsters {
		if mon == nil || !mon.Alive || mon.MapID != mapID {
			continue
		}
		if abs(mon.X-x) > radius || abs(mon.Y-y) > radius {
			continue
		}
		out = append(out, mon)
	}
	sort.Slice(out, func(i, j int) bool {
		di := abs(out[i].X-x) + abs(out[i].Y-y)
		dj := abs(out[j].X-x) + abs(out[j].Y-y)
		if di != dj {
			return di < dj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (w *World) isProperFriendLocked(a, b storage.Character) bool {
	if a.ID == "" || b.ID == "" {
		return false
	}
	if a.ID == b.ID {
		return true
	}
	if a.AttackMode == 0 || a.AttackMode == 1 {
		return true
	}
	if a.GroupOwnerID == "" || b.GroupOwnerID == "" {
		return false
	}
	if a.GroupOwnerID == b.ID || b.GroupOwnerID == a.ID {
		return true
	}
	return a.GroupOwnerID == b.GroupOwnerID
}

func (w *World) isFriendlySummonedMonsterLocked(caster storage.Character, players []storage.Character, mon *Monster) bool {
	if mon == nil || mon.MasterID == "" {
		return false
	}
	if mon.MasterID == caster.ID {
		return true
	}
	master, ok := w.characterByIDLocked(players, mon.MasterID)
	if !ok {
		return false
	}
	return w.isProperFriendLocked(caster, master)
}

func (w *World) characterByIDLocked(players []storage.Character, id string) (storage.Character, bool) {
	for _, ch := range players {
		if ch.ID == id {
			return ch, true
		}
	}
	return storage.Character{}, false
}

func (w *World) spellMonsterDamageLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) int {
	return w.spellDamageLocked(ch, skill, state)
}

func (w *World) spellSpiritDamageLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) int {
	combat := w.combatStatsLocked(ch)
	damage := w.spellScaledPowerLocked(skill, state)
	low := combat.SC
	high := combat.SCMax
	if high < low {
		high = low
	}
	if high > low {
		damage += low + w.rand.Intn(high-low+1)
	} else {
		damage += low
	}
	if damage < 1 {
		damage = 1
	}
	return damage
}

func (w *World) spellDamageLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) int {
	combat := w.combatStatsLocked(ch)
	damage := w.spellScaledPowerLocked(skill, state)
	low := combat.MC
	high := combat.MCMax
	if high < low {
		high = low
	}
	if high > low {
		damage += low + w.rand.Intn(high-low+1)
	} else {
		damage += low
	}
	if damage < 1 {
		damage = 1
	}
	return damage
}

func (w *World) spellCharacterDamageLocked(caster storage.Character, target storage.Character, skill data.StdSkill, state storage.SkillState) (storage.Character, CharacterHit, error) {
	damage := w.spellDamageLocked(caster, skill, state)
	return w.spellCharacterDamageWithPowerLocked(caster, target, damage)
}

func (w *World) spellHealAmountLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) int {
	combat := w.combatStatsLocked(ch)
	heal := w.spellScaledPowerLocked(skill, state)
	low := combat.SC
	high := combat.SCMax
	if high < low {
		high = low
	}
	if high > low {
		heal += low*2 + w.rand.Intn(high-low+1)
	} else {
		heal += low * 2
	}
	if heal < 1 {
		heal = 1
	}
	return heal
}

func (w *World) spellScaledPowerLocked(skill data.StdSkill, state storage.SkillState) int {
	base := skill.Power
	if base <= 0 {
		base = 1
	}
	maxPower := skill.MaxPower
	if maxPower < base {
		maxPower = base
	}
	roll := base
	if maxPower > base {
		roll += w.rand.Intn(maxPower - base + 1)
	}
	trainLevel := skill.TrainLevel1
	if trainLevel < 0 {
		trainLevel = 0
	}
	power := int(math.Round(float64(roll) / float64(trainLevel+1) * float64(int(state.Level)+1)))
	if power < 1 {
		power = 1
	}
	return power
}

func (w *World) magicShieldDurationLocked(ch storage.Character, skill data.StdSkill, state storage.SkillState) time.Duration {
	combat := w.combatStatsLocked(ch)
	roll := combat.MC
	if high := combat.MCMax; high > roll {
		roll += w.rand.Intn(high - roll + 1)
	}
	roll += 15
	if roll < 1 {
		roll = 1
	}
	scaled := w.spellScaledPowerLocked(data.StdSkill{
		Power:       roll,
		MaxPower:    roll,
		TrainLevel1: skill.TrainLevel1,
	}, state)
	if scaled < 1 {
		scaled = 1
	}
	return time.Duration(scaled) * time.Second
}
