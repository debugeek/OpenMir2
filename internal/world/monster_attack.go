package world

import (
	"math/rand"
	"time"

	"openmir2/internal/protocol/mir176"
	"openmir2/internal/storage"
	"openmir2/internal/world/core"
)

func (w *World) attackLocked(ch storage.Character, mon *Monster, attackIdent uint16, blockers ...storage.Character) (AttackResult, error) {
	return w.attackLockedWithDamageIdent(ch, mon, attackIdent, attackIdent, blockers...)
}

func (w *World) attackLockedWithDamageIdent(ch storage.Character, mon *Monster, attackIdent, damageIdent uint16, blockers ...storage.Character) (AttackResult, error) {
	damage := w.characterAttackDamageLocked(ch, mon, damageIdent)
	if damage < 0 {
		damage = 0
	}
	result, err := w.attackMonsterWithDamageLocked(ch, mon, damage, blockers...)
	if err != nil {
		return AttackResult{}, err
	}
	if result.Damage <= 0 {
		return result, nil
	}
	points, ok := meleeSkillTrainPoints(w.rand, attackIdent)
	if !ok {
		return result, nil
	}
	updated, changed, levelUp := w.trainMeleeSkillLocked(result.Character, attackIdent, points)
	if changed {
		result.Character = updated
		result.SkillChanged = true
		result.SkillExp = true
		skillID, skillOK := meleeSkillIDForAttackIdent(attackIdent)
		if skillOK {
			result.SkillMagicID, _ = w.MagicIDByName(skillID)
		}
		if skillOK {
			if trained, _, ok := updated.Skills.Get(skillID); ok {
				result.SkillLevel = trained.Level
				result.SkillTrain = trained.Train
			}
		}
		result.SkillExpDelay = 3 * time.Second
		if levelUp {
			result.SkillExpDelay = 800 * time.Millisecond
		}
		if err := w.store.SaveCharacter(updated); err != nil {
			return AttackResult{}, err
		}
	}
	return result, nil
}

func (w *World) attackMonsterWithDamageLocked(ch storage.Character, mon *Monster, damage int, blockers ...storage.Character) (AttackResult, error) {
	return w.attackMonsterWithDamageModeLocked(ch, mon, damage, true, blockers...)
}

func (w *World) attackMonsterDirectDamageLocked(ch storage.Character, mon *Monster, damage int, blockers ...storage.Character) (AttackResult, error) {
	if mon != nil && w.monsterSpeedPointLocked(mon) > 0 && w.characterHitPointLocked(ch) < w.rand.Intn(w.monsterSpeedPointLocked(mon)) {
		return AttackResult{
			MonsterID: mon.ID, Damage: 0, MonsterHP: mon.HP, MonsterMaxHP: mon.MaxHP,
			MonsterRaceImg: mon.RaceImg, MonsterWeapon: mon.MonsterWeapon, MonsterAppr: mon.Appr,
			MonsterX: mon.X, MonsterY: mon.Y, MonsterDir: mon.Dir, MonsterStatus: MonsterStatus(*mon, time.Now()), Character: ch,
			ImpactDelay: 500 * time.Millisecond,
		}, nil
	}
	now := time.Now()
	if monsterPoisonArmorActive(mon, now) {
		damage = referenceRound(float64(damage) * poisonDamageMultiplier(true))
	}
	if damage < 0 {
		damage = 0
	}
	change := core.ApplyHPDelta(mon.HP, mon.MaxHP, -damage)
	mon.HP = change.HP
	mon.TargetCharacterID = ch.ID
	mon.TargetFocusAt = now
	mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
	if mon.HP <= 0 {
		mon.PendingDeath = true
		mon.DeathHitterID = ch.ID
	}
	result := AttackResult{
		MonsterID: mon.ID, Connected: true, Damage: damage, MonsterHP: mon.HP, MonsterMaxHP: mon.MaxHP,
		MonsterRaceImg: mon.RaceImg, MonsterWeapon: mon.MonsterWeapon, MonsterAppr: mon.Appr,
		MonsterX: mon.X, MonsterY: mon.Y, MonsterDir: mon.Dir, MonsterStatus: MonsterStatus(*mon, now),
		MonsterHealthChanged: mon.ShowHPUntil > 0, Character: ch, ImpactDelay: 500 * time.Millisecond,
	}
	if mon.Race >= 50 {
		w.monsterStruckByCharacterLocked(mon, ch, blockers, now)
	}
	return result, nil
}

func (w *World) attackMonsterWithDamageModeLocked(ch storage.Character, mon *Monster, damage int, applyDefense bool, blockers ...storage.Character) (AttackResult, error) {
	now := time.Now()
	if damage < 0 {
		damage = 0
	}
	if monsterPoisonArmorActive(mon, now) {
		damage = referenceRound(float64(damage) * poisonDamageMultiplier(true))
		if damage < 0 {
			damage = 0
		}
	}
	if applyDefense {
		defenceBonus, magicDefenceBonus := activeMonsterProtectionBuffs(mon, now)
		if mon.UseMagic && magicDefenceBonus > 0 {
			damage -= magicDefenceBonus
		}
		if !mon.UseMagic && defenceBonus > 0 {
			damage -= defenceBonus
		}
	}
	if damage < 0 {
		damage = 0
	}
	hp := core.ApplyHPDelta(mon.HP, mon.MaxHP, -damage)
	mon.HP = hp.HP
	mon.TargetCharacterID = ch.ID
	mon.TargetFocusAt = now
	mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
	result := AttackResult{
		MonsterID:      mon.ID,
		Damage:         damage,
		MonsterHP:      hp.HP,
		MonsterMaxHP:   mon.MaxHP,
		MonsterRaceImg: mon.RaceImg,
		MonsterWeapon:  mon.MonsterWeapon,
		MonsterAppr:    mon.Appr,
		MonsterX:       mon.X,
		MonsterY:       mon.Y,
		MonsterDir:     mon.Dir,
		MonsterStatus:  MonsterStatus(*mon, now),
	}
	if hp.Dead {
		summoned := mon.MasterID != ""
		w.removeMonsterLocked(mon, !summoned)
		delay := mon.Spawn.RespawnSeconds
		if !summoned && delay > 0 {
			mon.RespawnAt = now.Add(time.Duration(delay) * time.Second)
		}
		var expGained int
		var leveled bool
		var err error
		if !summoned {
			ch, _, expGained, leveled, err = gainExperienceLocked(w, ch, mon.Experience)
			if err != nil {
				return AttackResult{}, err
			}
			result.Experience = expGained
			result.CurrentExp = ch.Experience
			result.LevelUp = leveled
		}
		result.Dead = true
		if mon.Animal {
			mon.RunAwayMode = true
			mon.TargetCharacterID = ch.ID
			mon.TargetFocusAt = now
			mon.TargetX, mon.TargetY = fleePointForMonster(mon, ch)
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
		}
		if !summoned {
			result.Drops = w.rollDropsLocked(mon, ch.ID, blockers...)
		}
	}
	result.Character = ch
	return result, w.store.SaveCharacter(ch)
}

func (w *World) monsterPhysicalDamageAfterDefenseLocked(mon *Monster, damage int) int {
	if mon == nil || damage <= 0 {
		return 0
	}
	damage -= mon.Defense
	defenceBonus, _ := activeMonsterProtectionBuffs(mon, time.Now())
	if defenceBonus > 0 {
		damage -= defenceBonus
	}
	if damage < 0 {
		return 0
	}
	return damage
}

func (w *World) attackMonsterWithMagicDamageLocked(ch storage.Character, mon *Monster, damage int, blockers ...storage.Character) (AttackResult, error) {
	return w.attackMonsterMagicDamageLocked(ch, mon, damage, true, blockers...)
}

func (w *World) attackMonsterWithImmediateMagicDamageLocked(ch storage.Character, mon *Monster, damage int, blockers ...storage.Character) (AttackResult, error) {
	w.monsterMagicStruckLocked(mon, time.Now())
	damage = w.monsterMagicDamageAfterDefenseLocked(mon, damage)
	if damage <= 0 {
		return AttackResult{
			MonsterID: mon.ID, Damage: 0, MonsterHP: mon.HP, MonsterMaxHP: mon.MaxHP, Magic: true,
			MonsterRaceImg: mon.RaceImg, MonsterWeapon: mon.MonsterWeapon, MonsterAppr: mon.Appr,
			MonsterX: mon.X, MonsterY: mon.Y, MonsterDir: mon.Dir, MonsterStatus: MonsterStatus(*mon, time.Now()), Character: ch,
		}, nil
	}
	return w.applyMonsterMagicDamageLocked(ch, mon, damage, false, blockers...)
}

func (w *World) applyMonsterMagicStrikeLocked(ch storage.Character, mon *Monster, damage int) (AttackResult, error) {
	w.monsterMagicStruckLocked(mon, time.Now())
	damage = w.monsterMagicDamageAfterDefenseLocked(mon, damage)
	if damage <= 0 {
		return AttackResult{
			MonsterID: mon.ID, Damage: 0, MonsterHP: mon.HP, MonsterMaxHP: mon.MaxHP, Magic: true,
			MonsterRaceImg: mon.RaceImg, MonsterWeapon: mon.MonsterWeapon, MonsterAppr: mon.Appr,
			MonsterX: mon.X, MonsterY: mon.Y, MonsterDir: mon.Dir, MonsterStatus: MonsterStatus(*mon, time.Now()), Character: ch,
		}, nil
	}
	return w.applyMonsterMagicDamageLocked(ch, mon, damage, false)
}

func (w *World) attackMonsterMagicDamageLocked(ch storage.Character, mon *Monster, damage int, setTarget bool, blockers ...storage.Character) (AttackResult, error) {
	damage = w.monsterMagicDamageAfterDefenseLocked(mon, damage)
	if damage < 0 {
		damage = 0
	}
	if damage == 0 {
		return AttackResult{
			MonsterID: mon.ID, Damage: 0, MonsterHP: mon.HP, MonsterMaxHP: mon.MaxHP, Magic: true,
			MonsterRaceImg: mon.RaceImg, MonsterWeapon: mon.MonsterWeapon, MonsterAppr: mon.Appr,
			MonsterX: mon.X, MonsterY: mon.Y, MonsterDir: mon.Dir, MonsterStatus: MonsterStatus(*mon, time.Now()), Character: ch,
		}, nil
	}
	return w.applyMonsterMagicDamageLocked(ch, mon, damage, setTarget, blockers...)
}

func (w *World) monsterMagicDamageAfterDefenseLocked(mon *Monster, damage int) int {
	if damage < 0 {
		damage = 0
	}
	magicDefenseMax := mon.MagicDefenseMax
	if magicDefenseMax < mon.MagicDefense {
		magicDefenseMax = mon.MagicDefense
	}
	if magicDefenseMax > 0 {
		damage -= mon.MagicDefense
		if magicDefenseMax > mon.MagicDefense {
			damage -= w.rand.Intn(magicDefenseMax - mon.MagicDefense + 1)
		}
	}
	if damage < 0 {
		damage = 0
	}
	return damage
}

func (w *World) applyMonsterMagicDamageLocked(ch storage.Character, mon *Monster, damage int, setTarget bool, blockers ...storage.Character) (AttackResult, error) {
	now := time.Now()
	if damage == 0 {
		return AttackResult{
			MonsterID: mon.ID, Damage: 0, MonsterHP: mon.HP, MonsterMaxHP: mon.MaxHP, Magic: true,
			MonsterRaceImg: mon.RaceImg, MonsterWeapon: mon.MonsterWeapon, MonsterAppr: mon.Appr,
			MonsterX: mon.X, MonsterY: mon.Y, MonsterDir: mon.Dir, MonsterStatus: MonsterStatus(*mon, now), Character: ch,
		}, nil
	}
	hp := core.ApplyHPDelta(mon.HP, mon.MaxHP, -damage)
	mon.HP = hp.HP
	if setTarget {
		mon.TargetCharacterID = ch.ID
		mon.TargetFocusAt = now
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
	}
	result := AttackResult{
		MonsterID:            mon.ID,
		Damage:               damage,
		MonsterHP:            hp.HP,
		MonsterMaxHP:         mon.MaxHP,
		MonsterMP:            mon.MP,
		MonsterMaxMP:         mon.MaxMP,
		MonsterRaceImg:       mon.RaceImg,
		MonsterWeapon:        mon.MonsterWeapon,
		MonsterAppr:          mon.Appr,
		MonsterX:             mon.X,
		MonsterY:             mon.Y,
		MonsterDir:           mon.Dir,
		MonsterStatus:        MonsterStatus(*mon, now),
		Magic:                true,
		MonsterHealthChanged: mon.ShowHPUntil > 0,
	}
	if hp.Dead {
		mon.PendingDeath = true
		mon.DeathHitterID = ch.ID
	}
	result.Character = ch
	return result, w.store.SaveCharacter(ch)
}

func (w *World) killMonsterWithDamageLocked(ch storage.Character, mon *Monster, damage int, blockers ...storage.Character) (AttackResult, error) {
	now := time.Now()
	noKiller := ch.ID == ""
	if noKiller {
		ch.MapID = mon.MapID
		ch.X = mon.X
		ch.Y = mon.Y
	}
	if damage < 0 {
		damage = 0
	}
	if monsterPoisonArmorActive(mon, now) {
		damage = referenceRound(float64(damage) * poisonDamageMultiplier(true))
		if damage < 0 {
			damage = 0
		}
	}
	defenceBonus, magicDefenceBonus := activeMonsterProtectionBuffs(mon, now)
	if mon.UseMagic && magicDefenceBonus > 0 {
		damage -= magicDefenceBonus
	}
	if !mon.UseMagic && defenceBonus > 0 {
		damage -= defenceBonus
	}
	if damage < 0 {
		damage = 0
	}
	hp := core.ApplyHPDelta(mon.HP, mon.MaxHP, -mon.HP)
	mon.HP = hp.HP
	mon.TargetCharacterID = ch.ID
	mon.TargetFocusAt = now
	mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
	result := AttackResult{
		MonsterID:      mon.ID,
		Damage:         damage,
		MonsterHP:      hp.HP,
		MonsterMaxHP:   mon.MaxHP,
		MonsterRaceImg: mon.RaceImg,
		MonsterWeapon:  mon.MonsterWeapon,
		MonsterAppr:    mon.Appr,
		MonsterX:       mon.X,
		MonsterY:       mon.Y,
		MonsterMapID:   mon.MapID,
		MonsterDir:     mon.Dir,
		MonsterStatus:  MonsterStatus(*mon, now),
	}
	summoned := mon.MasterID != ""
	w.removeMonsterLocked(mon, !summoned)
	delay := mon.Spawn.RespawnSeconds
	if !summoned && delay > 0 {
		mon.RespawnAt = now.Add(time.Duration(delay) * time.Second)
	}
	var expGained int
	var leveled bool
	var err error
	if !summoned && !noKiller {
		ch, _, expGained, leveled, err = gainExperienceLocked(w, ch, mon.Experience)
		if err != nil {
			return AttackResult{}, err
		}
		result.Experience = expGained
		result.CurrentExp = ch.Experience
		result.LevelUp = leveled
	}
	result.Dead = true
	if mon.Animal {
		mon.RunAwayMode = true
		mon.TargetCharacterID = ch.ID
		mon.TargetFocusAt = now
		mon.TargetX, mon.TargetY = fleePointForMonster(mon, ch)
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
	}
	if !summoned && !noKiller {
		result.Drops = w.rollDropsLocked(mon, ch.ID, blockers...)
	}
	result.Character = ch
	if noKiller {
		return result, nil
	}
	return result, w.store.SaveCharacter(ch)
}

func (w *World) characterAttackDamageLocked(ch storage.Character, mon *Monster, attackIdent uint16) int {
	if mon != nil {
		hit := w.characterHitPointLocked(ch)
		speed := w.monsterSpeedPointLocked(mon)
		if speed > 0 && hit < w.rand.Intn(speed) {
			return 0
		}
	}
	stats := w.combatStatsLocked(ch)
	minAttack := 3 + max(ch.Level, 1) + stats.DC
	maxAttack := 3 + max(ch.Level, 1) + stats.DCMax
	if maxAttack < minAttack {
		maxAttack = minAttack
	}
	damage := minAttack
	if maxAttack > minAttack {
		damage += w.rand.Intn(maxAttack - minAttack + 1)
	}
	damage += w.warriorHitBonusLocked(ch, attackIdent, damage)
	if mon == nil {
		return damage
	}
	return damage - mon.Defense
}

func (w *World) characterHitPointLocked(ch storage.Character) int {
	stats := w.combatStatsLocked(ch)
	hit := 5 + stats.Hit + int(ch.BonusAbil.Hit)
	if ch.ExtraAbil[3] > 0 {
		hit += int(ch.ExtraAbil[3])
	}
	if hit < 0 {
		hit = 0
	}
	return hit
}

func (w *World) characterSpeedPointLocked(ch storage.Character) int {
	stats := w.combatStatsLocked(ch)
	speed := int(SubAbilitySpeed(ch.Class)) + stats.Speed + int(ch.BonusAbil.Speed)
	if ch.ExtraAbil[3] > 0 {
		speed += int(ch.ExtraAbil[3])
	}
	if speed < 1 {
		speed = 1
	}
	return speed
}

func (w *World) monsterHitPointLocked(mon *Monster) int {
	if mon == nil {
		return 0
	}
	if mon.Hit < 0 {
		return 0
	}
	return mon.Hit
}

func (w *World) monsterSpeedPointLocked(mon *Monster) int {
	if mon == nil {
		return 1
	}
	if mon.Speed < 1 {
		return 1
	}
	return mon.Speed
}

func (w *World) warriorHitBonusLocked(ch storage.Character, attackIdent uint16, baseDamage int) int {
	bonus := 0
	switch attackIdent {
	case mir176.CMPowerHit:
		if state, _, ok := ch.Skills.Get("攻杀剑术"); ok {
			bonus += 5 + int(state.Level)
		}
	case mir176.CMLongHit:
		if !ch.ThrustingDisabled {
			if state, _, ok := ch.Skills.Get("刺杀剑术"); ok {
				if _, ok := w.data.Skills["刺杀剑术"]; ok {
					bonus += referenceRound(float64(baseDamage) / float64(spellTrainLevel+2) * float64(state.Level+2))
				}
			}
		}
	case mir176.CMWideHit:
		if !ch.HalfMoonDisabled {
			if state, _, ok := ch.Skills.Get("半月弯刀"); ok {
				if _, ok := w.data.Skills["半月弯刀"]; ok {
					bonus += referenceRound(float64(baseDamage) / float64(spellTrainLevel+10) * float64(state.Level+2))
				}
			}
		}
	case mir176.CMFireHit:
		if state, _, ok := ch.Skills.Get("烈火剑法"); ok {
			firePct := 40 + int(state.Level)*40
			bonus += referenceRound(float64(baseDamage) * float64(firePct) / 100.0)
		}
	}
	return bonus
}

func (w *World) trainMeleeSkillLocked(ch storage.Character, attackIdent uint16, points int) (storage.Character, bool, bool) {
	skillID, ok := meleeSkillIDForAttackIdent(attackIdent)
	if !ok {
		return ch, false, false
	}
	skill, ok := w.data.Skills[skillID]
	if !ok {
		return ch, false, false
	}
	state, idx, ok := ch.Skills.Get(skillID)
	if !ok || state.Level >= 3 {
		return ch, false, false
	}
	if ch.Level < skillNeedLevel(skill, state.Level) {
		return ch, false, false
	}
	previousLevel := state.Level
	state.Train = minInt(65535, state.Train+points)
	levelUp := w.advanceSkillTrainingLocked(skill, &state)
	ch.Skills[idx] = state
	return ch, true, levelUp || state.Level > previousLevel
}

func meleeSkillTrainPoints(r *rand.Rand, attackIdent uint16) (int, bool) {
	if r == nil {
		return 0, false
	}
	switch attackIdent {
	case mir176.CMHit:
		return r.Intn(3) + 1, true
	case mir176.CMPowerHit, mir176.CMLongHit, mir176.CMWideHit, mir176.CMFireHit:
		return 1, true
	default:
		return 0, false
	}
}

func meleeSkillIDForAttackIdent(attackIdent uint16) (string, bool) {
	switch attackIdent {
	case mir176.CMHit:
		return "基本剑术", true
	case mir176.CMPowerHit:
		return "攻杀剑术", true
	case mir176.CMLongHit:
		return "刺杀剑术", true
	case mir176.CMWideHit:
		return "半月弯刀", true
	case mir176.CMFireHit:
		return "烈火剑法", true
	default:
		return "", false
	}
}

func (w *World) attackCharacterWithDamageLocked(caster storage.Character, target storage.Character, damage int) (storage.Character, CharacterHit, error) {
	return w.attackCharacterWithDamageModeLocked(caster, target, damage, true)
}

func (w *World) attackCharacterDirectDamageLocked(caster storage.Character, target storage.Character, damage int) (storage.Character, CharacterHit, error) {
	return w.attackCharacterWithDamageModeLocked(caster, target, damage, false)
}

func (w *World) characterPhysicalDamageAfterDefenseLocked(target *storage.Character, damage int) int {
	if target == nil || damage <= 0 {
		return 0
	}
	stats := w.combatStatsLocked(*target)
	armor := stats.AC
	defenceBonus, _, _, _ := activeProtectionBuffs(*target, time.Now())
	low := armor & 0xFF
	high := (armor >> 8) & 0xFF
	if defenceBonus > 0 {
		high = minInt(255, high+defenceBonus)
	}
	if high < low {
		high = low
	}
	if high > low {
		damage -= low + w.rand.Intn(high-low+1)
	} else {
		damage -= low
	}
	if damage <= 0 {
		return 0
	}
	return applyCharacterMagicBubbleLocked(target, damage, time.Now())
}

func (w *World) attackCharacterWithDamageModeLocked(caster storage.Character, target storage.Character, damage int, applyDefense bool) (storage.Character, CharacterHit, error) {
	now := time.Now()
	if damage < 1 {
		damage = 1
	}
	hitPoint := w.characterHitPointLocked(caster)
	speed := w.characterSpeedPointLocked(target)
	connected := true
	if speed > 0 && hitPoint < w.rand.Intn(speed) {
		damage = 0
		connected = false
	}
	if applyDefense {
		stats := w.combatStatsLocked(target)
		armor := stats.AC
		low := armor & 0xFF
		high := (armor >> 8) & 0xFF
		if high < low {
			high = low
		}
		if damage > 0 && high > low {
			damage -= low + w.rand.Intn(high-low+1)
		} else if damage > 0 {
			damage -= low
		}
		if damage > 0 {
			damage = applyCharacterMagicBubbleLocked(&target, damage, now)
		}
	}
	if damage < 0 {
		damage = 0
	}
	target, damage, durability, featureChanged := w.applyCharacterStruckLocked(target, damage)
	change := core.ApplyVitalDelta(target, -damage, 0)
	target = change.Character
	resultHit := CharacterHit{
		Character:      target,
		Connected:      connected,
		Damage:         damage,
		Durability:     durability,
		FeatureChanged: featureChanged,
		AttackerID:     caster.ID,
		AttackerX:      caster.X,
		AttackerY:      caster.Y,
		Dead:           change.Dead,
	}
	return target, resultHit, w.store.SaveCharacter(target)
}

func (w *World) applyCharacterStruckLocked(target storage.Character, damage int) (storage.Character, int, []SpellDurability, bool) {
	if damage <= 0 {
		return target, damage, nil, false
	}
	nDam := w.rand.Intn(10) + 5
	if characterPoisonArmorActive(target, time.Now()) {
		nDam = referenceRound(float64(nDam) * poisonDamageMultiplier(true))
		damage = referenceRound(float64(damage) * poisonDamageMultiplier(true))
	}
	durability := make([]SpellDurability, 0)
	featureChanged := false
	applyDurability := func(slot int) {
		item, ok := target.EquippedItems[slot]
		if !ok || item.ItemID == "" {
			return
		}
		oldDisplay := int(item.Dura / 1000)
		if int(item.Dura) <= nDam {
			item.Dura = 0
			item.ItemID = ""
			if slot == SlotDress {
				featureChanged = true
			}
		} else {
			item.Dura -= uint16(nDam)
		}
		target.EquippedItems[slot] = item
		if oldDisplay != int(item.Dura/1000) {
			durability = append(durability, SpellDurability{Slot: slot, Dura: item.Dura, DuraMax: item.DuraMax})
		}
	}
	applyDurability(SlotDress)
	for slot := 0; slot < useSlotCount; slot++ {
		if w.rand.Intn(8) != 0 {
			continue
		}
		applyDurability(slot)
	}
	return target, damage, durability, featureChanged
}

func (w *World) monsterAttackCharacterLocked(mon *Monster, ch storage.Character) (storage.Character, CharacterHit, error) {
	damage := mon.MinAttack
	if mon.MaxAttack > mon.MinAttack {
		damage += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
	}
	if damage < 1 {
		damage = 1
	}
	return w.monsterAttackCharacterWithDamageLocked(mon, ch, damage)
}

func (w *World) monsterAttackCharacterWithDamageLocked(mon *Monster, ch storage.Character, damage int) (storage.Character, CharacterHit, error) {
	if damage < 1 {
		damage = 1
	}
	now := time.Now()
	stats := w.combatStatsLocked(ch)
	defenceBonus, magicDefenceBonus, bubbleLevel, bubbleActive := activeProtectionBuffs(ch, now)
	monsterDefenceBonus, monsterMagicDefenceBonus := activeMonsterProtectionBuffs(mon, now)
	armor := stats.AC
	if mon.UseMagic {
		armor = stats.MAC
	}
	low := armor & 0xFF
	high := (armor >> 8) & 0xFF
	if mon.UseMagic && (magicDefenceBonus > 0 || monsterMagicDefenceBonus > 0) {
		magicDefenceBonus += monsterMagicDefenceBonus
		high = minInt(255, high+magicDefenceBonus)
	}
	if !mon.UseMagic && (defenceBonus > 0 || monsterDefenceBonus > 0) {
		defenceBonus += monsterDefenceBonus
		high = minInt(255, high+defenceBonus)
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
		damage = referenceRound(float64(damage) * float64(int(bubbleLevel)+2) * 8.0 / 100.0)
		if damage < 0 {
			damage = 0
		}
		remaining := time.Until(time.Unix(0, ch.BubbleDefenceUntil))
		if remaining > 3*time.Second {
			remaining -= 3 * time.Second
		} else {
			remaining = time.Second
		}
		ch.BubbleDefenceUntil = now.Add(remaining).UnixNano()
	}
	if characterPoisonArmorActive(ch, now) {
		damage = referenceRound(float64(damage) * poisonDamageMultiplier(true))
		if damage < 0 {
			damage = 0
		}
	}
	change := core.ApplyVitalDelta(ch, -damage, 0)
	ch = change.Character
	dead := false
	if change.Dead {
		dead = true
		mon.TargetCharacterID = ""
		mon.TargetFocusAt = time.Time{}
		mon.NextSearchAt = time.Now()
	}
	hit := CharacterHit{
		Character:       ch,
		Damage:          damage,
		AttackerID:      mon.ID,
		AttackerRaceImg: mon.RaceImg,
		AttackerAppr:    mon.Appr,
		AttackerX:       mon.X,
		AttackerY:       mon.Y,
		Dead:            dead,
	}
	return ch, hit, w.store.SaveCharacter(ch)
}
