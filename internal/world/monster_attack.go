package world

import (
	"fmt"
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
	damage := w.characterAttackPowerLocked(ch, damageIdent)
	oldTargetCharacterID := mon.TargetCharacterID
	oldTargetMonsterID := mon.TargetMonsterID
	oldTargetFocusAt := mon.TargetFocusAt
	oldNextSearchAt := mon.NextSearchAt
	oldLastHitterID := mon.LastHitterID
	oldLastHitterAt := mon.LastHitterAt
	result, err := w.attackMonsterWithBaseDamageModeLocked(ch, mon, damage, false, blockers...)
	if err != nil {
		return AttackResult{}, err
	}
	if result.Damage <= 0 {
		mon.TargetCharacterID = oldTargetCharacterID
		mon.TargetMonsterID = oldTargetMonsterID
		mon.TargetFocusAt = oldTargetFocusAt
		mon.NextSearchAt = oldNextSearchAt
	}
	if !result.Connected {
		return result, nil
	}
	if result.Damage > 0 {
		mon.TargetCharacterID = oldTargetCharacterID
		mon.TargetMonsterID = oldTargetMonsterID
		mon.TargetFocusAt = oldTargetFocusAt
		mon.NextSearchAt = oldNextSearchAt
		mon.LastHitterID = oldLastHitterID
		mon.LastHitterAt = oldLastHitterAt
		result.ImpactDelay = 200 * time.Millisecond
		if err := w.applyMeleeTrainingLocked(&result, attackIdent); err != nil {
			return AttackResult{}, err
		}
	}
	if result.WeaponDamageEligible {
		durability, deletedItems, featureChanged := w.applyWeaponDamageLocked(&result.Character)
		result.Durability = append(result.Durability, durability...)
		result.DeletedItems = append(result.DeletedItems, deletedItems...)
		result.FeatureChanged = result.FeatureChanged || featureChanged
	}
	if result.Damage > 0 {
		w.enqueueCharacterMonsterStruckLocked(mon, result.Character, blockers, time.Now())
	}
	return result, nil
}

func (w *World) applyMeleeTrainingLocked(result *AttackResult, attackIdent uint16) error {
	if result == nil {
		return nil
	}
	training := []uint16{mir176.CMHit}
	if attackIdent != mir176.CMHit {
		training = append(training, attackIdent)
	}
	for _, ident := range training {
		skillID, ok := meleeSkillIDForAttackIdent(ident)
		if !ok {
			continue
		}
		state, _, ok := result.Character.Skills.Get(skillID)
		if !ok {
			continue
		}
		skill, ok := w.data.Skills[skillID]
		if !ok || state.Level >= 3 || result.Character.Level < skillNeedLevel(skill, state.Level) {
			continue
		}
		points, ok := meleeSkillTrainPoints(w.rand, ident)
		if !ok {
			continue
		}
		points *= w.skillTrainingMultiplierLocked(result.Character)
		updated, changed, levelUp := w.trainMeleeSkillLocked(result.Character, ident, points)
		if !changed {
			continue
		}
		result.Character = updated
		result.SkillChanged = true
		result.SkillLevelUp = result.SkillLevelUp || levelUp
		trained, _, ok := updated.Skills.Get(skillID)
		if !ok {
			continue
		}
		magicID, ok := w.MagicIDByName(skillID)
		if !ok {
			continue
		}
		delay := 3 * time.Second
		if levelUp {
			delay = 800 * time.Millisecond
		}
		if result.SkillMagicID == 0 {
			result.SkillMagicID = magicID
			result.SkillLevel = trained.Level
			result.SkillTrain = trained.Train
			result.SkillExpDelay = delay
		}
		result.SkillExperiences = append(result.SkillExperiences, AttackSkillExperience{
			MagicID: magicID, Level: trained.Level, Train: trained.Train, Delay: delay,
		})
	}
	result.SkillExp = len(result.SkillExperiences) > 0
	return nil
}

func (w *World) attackMonsterWithDamageLocked(ch storage.Character, mon *Monster, damage int, blockers ...storage.Character) (AttackResult, error) {
	return w.attackMonsterWithDamageModeLocked(ch, mon, damage, true, blockers...)
}

func (w *World) attackMonsterWithPoisonDamageLocked(ch storage.Character, mon *Monster, damage int) (AttackResult, error) {
	result, err := w.attackMonsterWithDamageModeAndMeatLocked(ch, mon, damage, false, false, false, true, false)
	if err == nil && result.Damage > 0 {
		w.decayMonsterMeatQualityLocked(mon, 1)
	}
	return result, err
}

func (w *World) attackMonsterDirectDamageLocked(ch storage.Character, mon *Monster, damage int, blockers ...storage.Character) (AttackResult, error) {
	return w.attackMonsterDirectDamageModeLocked(ch, mon, damage, true, true, blockers...)
}

func (w *World) attackMonsterSecondaryDamageLocked(ch storage.Character, mon *Monster, damage int, blockers ...storage.Character) (AttackResult, error) {
	return w.attackMonsterDirectDamageModeLocked(ch, mon, damage, true, true, blockers...)
}

func (w *World) attackMonsterDirectDamageModeLocked(ch storage.Character, mon *Monster, damage int, updateTarget, applyMonsterEffects bool, blockers ...storage.Character) (AttackResult, error) {
	return w.attackMonsterDirectDamageModeForActorLocked(ch, mon, damage, updateTarget, applyMonsterEffects, true, blockers...)
}

func (w *World) attackMonsterDirectDamageModeForActorLocked(ch storage.Character, mon *Monster, damage int, updateTarget, applyMonsterEffects, attackerIsCharacter bool, blockers ...storage.Character) (AttackResult, error) {
	var oldLastHitterID string
	var oldLastHitterAt time.Time
	if mon != nil {
		oldLastHitterID = mon.LastHitterID
		oldLastHitterAt = mon.LastHitterAt
	}
	if !attackerIsCharacter && mon != nil && w.isSafeZoneLocked(ch) && w.isSafeZoneLocked(storage.Character{MapID: mon.MapID, X: mon.X, Y: mon.Y}) {
		return AttackResult{
			MonsterID: mon.ID, MonsterMapID: mon.MapID, Damage: 0, MonsterHP: mon.HP, MonsterMaxHP: mon.MaxHP,
			MonsterRaceImg: mon.RaceImg, MonsterWeapon: mon.MonsterWeapon, MonsterAppr: mon.Appr,
			MonsterX: mon.X, MonsterY: mon.Y, MonsterDir: mon.Dir, MonsterStatus: MonsterStatus(*mon, time.Now()), Character: ch,
		}, nil
	}
	if mon != nil && w.monsterSpeedPointLocked(mon) > 0 && w.rand.Intn(w.monsterSpeedPointLocked(mon)) >= w.characterHitPointLocked(ch) {
		return AttackResult{
			MonsterID: mon.ID, MonsterMapID: mon.MapID, Damage: 0, MonsterHP: mon.HP, MonsterMaxHP: mon.MaxHP,
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
	w.recordCastleDamageLocked(mon, now)
	deferAnimalTargetUpdate := updateTarget && applyMonsterEffects && mon.Race >= 50
	if updateTarget && !deferAnimalTargetUpdate {
		mon.TargetCharacterID = ch.ID
		mon.TargetFocusAt = now
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
	}
	if damage > 0 {
		if updateTarget && mon.Race < 50 {
			w.monsterMagicDamageRecoveryResetLocked(mon)
		}
		if updateTarget {
			w.setMonsterLastHitterAtLocked(mon, ch.ID, now)
		}
	}
	if mon.HP <= 0 {
		mon.PendingDeath = true
		mon.DeathHitterID = ch.ID
	}
	result := AttackResult{
		MonsterID: mon.ID, MonsterMapID: mon.MapID, Connected: true, Damage: damage, MonsterHP: mon.HP, MonsterMaxHP: mon.MaxHP,
		MonsterRaceImg: mon.RaceImg, MonsterWeapon: mon.MonsterWeapon, MonsterAppr: mon.Appr,
		MonsterX: mon.X, MonsterY: mon.Y, MonsterDir: mon.Dir, MonsterStatus: MonsterStatus(*mon, now),
		MonsterHealthChanged: mon.ShowHPUntil > 0, Character: ch, ImpactDelay: 500 * time.Millisecond,
	}
	if damage > 0 && applyMonsterEffects && mon.Race >= 50 {
		if attackerIsCharacter {
			mon.LastHitterID = oldLastHitterID
			mon.LastHitterAt = oldLastHitterAt
			w.enqueueCharacterMonsterStruckLocked(mon, ch, blockers, now)
		} else {
			ch = w.monsterStruckByCharacterLocked(mon, ch, blockers, now)
		}
		result.Character = ch
	}
	return result, nil
}

func (w *World) enqueueCharacterMonsterStruckLocked(mon *Monster, ch storage.Character, blockers []storage.Character, now time.Time) {
	if mon == nil {
		return
	}
	struckCharacter := ch
	w.enqueuePendingMonsterAttackLocked(pendingMonsterAttack{
		DueAt:           now.Add(200 * time.Millisecond),
		MonsterID:       mon.ID,
		StruckCharacter: &struckCharacter,
		StruckPlayers:   append([]storage.Character(nil), blockers...),
		MonsterTargets:  []pendingMonsterTarget{{ID: mon.ID, Monster: true, StruckOnly: true}},
	})
}

func (w *World) attackMonsterWithBaseDamageLocked(ch storage.Character, mon *Monster, damage int, blockers ...storage.Character) (AttackResult, error) {
	return w.attackMonsterWithBaseDamageModeLocked(ch, mon, damage, true, blockers...)
}

func (w *World) attackMonsterWithBaseDamageModeLocked(ch storage.Character, mon *Monster, damage int, applyStruck bool, blockers ...storage.Character) (AttackResult, error) {
	if mon != nil && w.monsterHitPointLocked(mon) > 0 && w.monsterSpeedPointLocked(mon) > 0 && w.characterHitPointLocked(ch) < w.rand.Intn(w.monsterSpeedPointLocked(mon)) {
		return AttackResult{
			MonsterID: mon.ID, MonsterMapID: mon.MapID, Damage: 0, MonsterHP: mon.HP, MonsterMaxHP: mon.MaxHP,
			MonsterRaceImg: mon.RaceImg, MonsterWeapon: mon.MonsterWeapon, MonsterAppr: mon.Appr,
			MonsterX: mon.X, MonsterY: mon.Y, MonsterDir: mon.Dir, MonsterStatus: MonsterStatus(*mon, time.Now()), Character: ch,
		}, nil
	}
	if damage < 0 {
		damage = 0
	}
	if mon != nil {
		damage = w.monsterPhysicalDamageAfterDefenseLocked(mon, damage)
		if damage > 0 && mon.Undead > 0 {
			damage += w.combatStatsLocked(ch).Undead
		}
	}
	weaponDamageEligible := damage > 0 && mon != nil && !mon.UseMagic
	oldTargetCharacterID := mon.TargetCharacterID
	oldTargetMonsterID := mon.TargetMonsterID
	oldTargetFocusAt := mon.TargetFocusAt
	oldNextSearchAt := mon.NextSearchAt
	result, err := w.attackMonsterWithDamageModeAndMeatLocked(ch, mon, damage, false, false, true, true, true, blockers...)
	if err != nil {
		return result, err
	}
	result.WeaponDamageEligible = weaponDamageEligible
	if result.Damage <= 0 {
		mon.TargetCharacterID = oldTargetCharacterID
		mon.TargetMonsterID = oldTargetMonsterID
		mon.TargetFocusAt = oldTargetFocusAt
		mon.NextSearchAt = oldNextSearchAt
	}
	if result.Damage > 0 && applyStruck {
		ch = w.monsterStruckByCharacterLocked(mon, ch, blockers, time.Now())
		result.Character = ch
	}
	return result, nil
}

func (w *World) attackMonsterWithDamageModeLocked(ch storage.Character, mon *Monster, damage int, applyDefense bool, blockers ...storage.Character) (AttackResult, error) {
	return w.attackMonsterWithDamageModeAndMeatLocked(ch, mon, damage, applyDefense, true, true, true, true, blockers...)
}

func (w *World) attackMonsterWithDamageModeAndMeatLocked(ch storage.Character, mon *Monster, damage int, applyDefense, decayMeat, applyPoisonArmor, deferDeath, resetRecovery bool, blockers ...storage.Character) (AttackResult, error) {
	now := w.monsterActionTimeLocked()
	if damage < 0 {
		damage = 0
	}
	if applyPoisonArmor && monsterPoisonArmorActive(mon, now) {
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
	if damage > 0 && decayMeat {
		w.decayMonsterMeatQualityLocked(mon, 0)
	}
	hp := core.ApplyHPDelta(mon.HP, mon.MaxHP, -damage)
	mon.HP = hp.HP
	w.recordCastleDamageLocked(mon, now)
	deferAnimalTargetUpdate := mon.Race >= 50
	if !deferAnimalTargetUpdate {
		mon.TargetCharacterID = ch.ID
		mon.TargetFocusAt = now
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
	}
	if damage > 0 && resetRecovery {
		w.monsterMagicDamageRecoveryResetLocked(mon)
		w.setMonsterLastHitterAtLocked(mon, ch.ID, now)
	} else if damage > 0 {
		w.setMonsterLastHitterAtLocked(mon, ch.ID, now)
	}
	if damage > 0 && applyDefense && w.characterHasParalysisRingLocked(ch) && w.rand.Intn(mon.AntiPoison+5) == 0 {
		mon.ParalyzedUntil = now.Add(5 * time.Second)
		w.resetMonsterStatusClockLocked(mon.ID, statusClockParalyzed, mon.ParalyzedUntil, now)
	}
	result := AttackResult{
		MonsterID:      mon.ID,
		MonsterMapID:   mon.MapID,
		Connected:      true,
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
		Character:      ch,
	}
	if hp.Dead {
		if deferDeath {
			mon.PendingDeath = true
			mon.DeathHitterID = ch.ID
			result.Character = ch
			return result, nil
		}
		if mon.CastleWall {
			mon.Alive = false
			mon.DeathAt = now
			mon.PendingDeath = false
			w.castleWallSetMapFlagLocked(mon)
			result.Dead = true
			result.Character = ch
			return result, nil
		}
		summoned := mon.MasterID != ""
		if mon.Animal && !summoned {
			w.killAnimalLocked(mon)
		} else {
			w.removeMonsterLocked(mon, !summoned)
		}
		if !summoned && !mon.CastleDoor {
			w.scheduleMonsterRespawnLocked(mon, now)
		}
		var expGained int
		var leveled bool
		var err error
		if !summoned {
			monsterExperience := monsterExperienceForCharacter(w.gameplay.Combat, ch.Level, mon.Level, mon.Experience)
			ch, _, expGained, leveled, err = gainExperienceLocked(w, ch, monsterExperience)
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
		if !summoned && !mon.Animal && !mon.CastleDoor {
			result.Drops = w.rollDropsLocked(mon, ch.ID, ch.GroupOwnerID, blockers...)
		}
	}
	result.Character = ch
	return result, nil
}

func (w *World) monsterPhysicalDamageAfterDefenseLocked(mon *Monster, damage int) int {
	return w.monsterPhysicalDamageAfterDefenseAtLocked(mon, damage, w.monsterActionTimeLocked())
}

func (w *World) monsterPhysicalDamageAfterDefenseAtLocked(mon *Monster, damage int, now time.Time) int {
	if mon == nil || damage <= 0 {
		return 0
	}
	damage -= mon.Defense
	defenceBonus, _ := activeMonsterProtectionBuffs(mon, now)
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
	return w.attackMonsterWithImmediateMagicDamageModeLocked(ch, mon, damage, true, blockers...)
}

func (w *World) attackMonsterWithImmediateMagicDamageModeLocked(ch storage.Character, mon *Monster, damage int, applyAnimalDelay bool, blockers ...storage.Character) (AttackResult, error) {
	if applyAnimalDelay {
		w.monsterMagicStruckLocked(mon, time.Now())
	}
	damage = w.monsterMagicDamageAfterDefenseLocked(mon, damage)
	if damage <= 0 {
		return AttackResult{
			MonsterID: mon.ID, MonsterMapID: mon.MapID, Damage: 0, MonsterHP: mon.HP, MonsterMaxHP: mon.MaxHP, Magic: true,
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
			MonsterID: mon.ID, MonsterMapID: mon.MapID, Damage: 0, MonsterHP: mon.HP, MonsterMaxHP: mon.MaxHP, Magic: true,
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
			MonsterID: mon.ID, MonsterMapID: mon.MapID, Damage: 0, MonsterHP: mon.HP, MonsterMaxHP: mon.MaxHP, Magic: true,
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
	return w.applyMonsterMagicDamageAtLocked(ch, mon, damage, setTarget, time.Now(), blockers...)
}

func (w *World) applyMonsterMagicDamageAtLocked(ch storage.Character, mon *Monster, damage int, setTarget bool, now time.Time, blockers ...storage.Character) (AttackResult, error) {
	if damage == 0 {
		return AttackResult{
			MonsterID: mon.ID, MonsterMapID: mon.MapID, Damage: 0, MonsterHP: mon.HP, MonsterMaxHP: mon.MaxHP, Magic: true,
			MonsterRaceImg: mon.RaceImg, MonsterWeapon: mon.MonsterWeapon, MonsterAppr: mon.Appr,
			MonsterX: mon.X, MonsterY: mon.Y, MonsterDir: mon.Dir, MonsterStatus: MonsterStatus(*mon, now), Character: ch,
		}, nil
	}
	if monsterPoisonArmorActive(mon, now) {
		damage = referenceRound(float64(damage) * poisonDamageMultiplier(true))
	}
	w.monsterMagicDamageRecoveryResetLocked(mon)
	w.setMonsterLastHitterAtLocked(mon, ch.ID, now)
	hp := core.ApplyHPDelta(mon.HP, mon.MaxHP, -damage)
	mon.HP = hp.HP
	w.recordCastleDamageLocked(mon, now)
	w.decayMonsterMeatQualityLocked(mon, damage)
	if setTarget {
		mon.TargetCharacterID = ch.ID
		mon.TargetFocusAt = now
		mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
	}
	result := AttackResult{
		MonsterID:            mon.ID,
		MonsterMapID:         mon.MapID,
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
		MonsterHealthChanged: true,
	}
	if hp.Dead {
		mon.PendingDeath = true
		mon.DeathHitterID = ch.ID
	}
	result.Character = ch
	if _, isMonster := w.monsters[ch.ID]; isMonster {
		return result, nil
	}
	return result, nil
}

func (w *World) applyReferenceMonsterMagicDamageAtLocked(caster storage.Character, target *Monster, damage int, now time.Time) (AttackResult, error) {
	hit, err := w.applyMonsterMagicDamageAtLocked(caster, target, damage, false, now)
	if err == nil && hit.Damage > 0 {
		caster = w.monsterStruckByCharacterLocked(target, caster, nil, now)
		hit.Character = caster
	}
	return hit, err
}

func (w *World) applyReferenceMonsterVsMonsterDamageAtLocked(attacker, target *Monster, damage int, now time.Time) (AttackResult, error) {
	if attacker == nil || target == nil {
		return AttackResult{}, fmt.Errorf("monster struck target is missing")
	}
	lastAttackAt := target.LastAttackAt
	lastHitterID := target.LastHitterID
	lastHitterAt := target.LastHitterAt
	expHitterID := target.ExpHitterID
	expHitterAt := target.ExpHitterAt
	targetCharacterID := target.TargetCharacterID
	targetMonsterID := target.TargetMonsterID
	targetFocusAt := target.TargetFocusAt
	targetNextSearchAt := target.NextSearchAt
	targetHealthTick := target.HealthTick
	targetSpellTick := target.SpellTick
	targetPerHealth := target.PerHealth
	targetPerSpell := target.PerSpell
	meatQuality := target.MeatQuality
	holySeizeUntil := target.HolySeizeUntil
	pendingDeath := target.PendingDeath
	deathHitterID := target.DeathHitterID
	caster := storage.Character{ID: attacker.ID, MapID: attacker.MapID, X: attacker.X, Y: attacker.Y}
	hit, err := w.applyMonsterMagicDamageAtLocked(caster, target, damage, false, now)
	target.LastAttackAt = lastAttackAt
	target.LastHitterID = lastHitterID
	target.LastHitterAt = lastHitterAt
	target.ExpHitterID = expHitterID
	target.ExpHitterAt = expHitterAt
	target.TargetCharacterID = targetCharacterID
	target.TargetMonsterID = targetMonsterID
	target.TargetFocusAt = targetFocusAt
	target.NextSearchAt = targetNextSearchAt
	target.HealthTick = targetHealthTick
	target.SpellTick = targetSpellTick
	target.PerHealth = targetPerHealth
	target.PerSpell = targetPerSpell
	target.MeatQuality = meatQuality
	target.HolySeizeUntil = holySeizeUntil
	target.PendingDeath = pendingDeath
	target.DeathHitterID = deathHitterID
	if hit.Damage > 0 && hit.MonsterHP <= 0 {
		target.PendingDeath = true
		target.DeathHitterID = attacker.ID
	}
	return hit, err
}

func (w *World) enqueueReferenceMonsterStruckLocked(attacker, target *Monster, now time.Time, delay time.Duration) {
	if attacker == nil || target == nil || target.ID == "" {
		return
	}
	w.enqueuePendingMonsterAttackLocked(pendingMonsterAttack{
		DueAt: now.Add(delay), MonsterID: attacker.ID,
		MonsterTargets: []pendingMonsterTarget{{ID: target.ID, Monster: true, StruckOnly: true}},
	})
}

func (w *World) applyReferenceMonsterDamageAtLocked(caster storage.Character, target *Monster, damage int, now time.Time) (AttackResult, error) {
	magicDefense := target.MagicDefense
	magicDefenseMax := target.MagicDefenseMax
	meatQuality := target.MeatQuality
	holySeizeUntil := target.HolySeizeUntil
	lastAttackAt := target.LastAttackAt
	lastHitterID := target.LastHitterID
	lastHitterAt := target.LastHitterAt
	expHitterID := target.ExpHitterID
	expHitterAt := target.ExpHitterAt
	targetCharacterID := target.TargetCharacterID
	targetMonsterID := target.TargetMonsterID
	targetFocusAt := target.TargetFocusAt
	targetNextSearchAt := target.NextSearchAt
	targetHealthTick := target.HealthTick
	targetSpellTick := target.SpellTick
	targetPerHealth := target.PerHealth
	targetPerSpell := target.PerSpell
	target.MagicDefense = 0
	target.MagicDefenseMax = 0
	hit, err := w.applyReferenceMonsterMagicDamageAtLocked(caster, target, damage, now)
	target.MagicDefense = magicDefense
	target.MagicDefenseMax = magicDefenseMax
	target.MeatQuality = meatQuality
	target.HolySeizeUntil = holySeizeUntil
	target.LastAttackAt = lastAttackAt
	target.LastHitterID = lastHitterID
	target.LastHitterAt = lastHitterAt
	target.ExpHitterID = expHitterID
	target.ExpHitterAt = expHitterAt
	target.TargetCharacterID = targetCharacterID
	target.TargetMonsterID = targetMonsterID
	target.TargetFocusAt = targetFocusAt
	target.NextSearchAt = targetNextSearchAt
	target.HealthTick = targetHealthTick
	target.SpellTick = targetSpellTick
	target.PerHealth = targetPerHealth
	target.PerSpell = targetPerSpell
	return hit, err
}

func (w *World) applyReferenceDeferredMonsterMagicDamageAtLocked(caster storage.Character, target *Monster, damage int, now time.Time) (AttackResult, error) {
	magicDefense := target.MagicDefense
	magicDefenseMax := target.MagicDefenseMax
	lastHitterID := target.LastHitterID
	lastHitterAt := target.LastHitterAt
	expHitterID := target.ExpHitterID
	expHitterAt := target.ExpHitterAt
	targetCharacterID := target.TargetCharacterID
	targetMonsterID := target.TargetMonsterID
	targetFocusAt := target.TargetFocusAt
	targetNextSearchAt := target.NextSearchAt
	targetHealthTick := target.HealthTick
	targetSpellTick := target.SpellTick
	targetPerHealth := target.PerHealth
	targetPerSpell := target.PerSpell
	target.MagicDefense = 0
	target.MagicDefenseMax = 0
	hit, err := w.applyMonsterMagicDamageAtLocked(caster, target, damage, false, now)
	target.MagicDefense = magicDefense
	target.MagicDefenseMax = magicDefenseMax
	target.LastHitterID = lastHitterID
	target.LastHitterAt = lastHitterAt
	target.ExpHitterID = expHitterID
	target.ExpHitterAt = expHitterAt
	target.TargetCharacterID = targetCharacterID
	target.TargetMonsterID = targetMonsterID
	target.TargetFocusAt = targetFocusAt
	target.NextSearchAt = targetNextSearchAt
	target.HealthTick = targetHealthTick
	target.SpellTick = targetSpellTick
	target.PerHealth = targetPerHealth
	target.PerSpell = targetPerSpell
	return hit, err
}

func (w *World) killMonsterWithDamageLocked(ch storage.Character, mon *Monster, damage int, blockers ...storage.Character) (AttackResult, error) {
	return w.killMonsterWithDamageAtLocked(ch, mon, damage, time.Now(), blockers...)
}

func (w *World) killMonsterWithDamageAtLocked(ch storage.Character, mon *Monster, damage int, now time.Time, blockers ...storage.Character) (AttackResult, error) {
	result, _, err := w.killMonsterWithDamageAtAndPlayersLocked(ch, mon, damage, now, nil, blockers...)
	return result, err
}

func (w *World) killMonsterWithDamageAtAndPlayersLocked(ch storage.Character, mon *Monster, damage int, now time.Time, players map[string]storage.Character, blockers ...storage.Character) (AttackResult, []SpellExperience, error) {
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
	w.recordCastleDamageLocked(mon, now)
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
	if mon.CastleWall {
		mon.Alive = false
		mon.PendingDeath = false
		mon.DeathAt = now
		w.castleWallSetMapFlagLocked(mon)
		result.Dead = true
		result.Character = ch
		return result, nil, nil
	}
	summoned := mon.MasterID != ""
	if mon.Animal && !summoned {
		w.killAnimalLocked(mon)
	} else {
		w.removeMonsterLocked(mon, !summoned)
	}
	if !summoned {
		w.scheduleMonsterRespawnLocked(mon, now)
	}
	var expGained int
	var leveled bool
	var err error
	var experienceEvents []SpellExperience
	if !summoned && !noKiller && !mon.CastleDoor {
		monsterExperience := monsterExperienceForCharacter(w.gameplay.Combat, ch.Level, mon.Level, mon.Experience)
		if players != nil {
			experienceEvents, ch, err = w.gainMonsterExperienceLocked(ch, monsterExperience, players)
			for _, experience := range experienceEvents {
				if experience.CharacterID == ch.ID {
					expGained = experience.Experience
					leveled = experience.LevelUp
					break
				}
			}
		} else {
			ch, _, expGained, leveled, err = gainExperienceLocked(w, ch, monsterExperience)
		}
		if err != nil {
			return AttackResult{}, nil, err
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
	if !summoned && !mon.Animal && !mon.CastleDoor {
		result.Drops = w.rollDropsLocked(mon, ch.ID, ch.GroupOwnerID, blockers...)
	}
	result.Character = ch
	if noKiller {
		return result, experienceEvents, nil
	}
	return result, experienceEvents, nil
}

func (w *World) characterAttackDamageLocked(ch storage.Character, mon *Monster, attackIdent uint16) int {
	if mon != nil {
		hit := w.characterHitPointLocked(ch)
		speed := w.monsterSpeedPointLocked(mon)
		if speed > 0 && hit < w.rand.Intn(speed) {
			return 0
		}
	}
	damage := w.characterAttackPowerLocked(ch, attackIdent)
	if mon == nil {
		return damage
	}
	damage -= mon.Defense
	if damage < 0 {
		damage = 0
	}
	if damage > 0 && mon.Undead > 0 {
		damage += w.combatStatsLocked(ch).Undead
	}
	return damage
}

func (w *World) characterAttackPowerLocked(ch storage.Character, attackIdent uint16) int {
	stats := w.combatStatsLocked(ch)
	base := Base(ch.Class, ch.Level)
	minAttack := base.DC + stats.DC
	maxAttack := base.DCMax + stats.DCMax
	if maxAttack < minAttack {
		maxAttack = minAttack
	}
	damage := w.attackPowerLocked(stats, minAttack, maxAttack-minAttack)
	damage += w.warriorHitBonusLocked(ch, attackIdent, damage)
	return damage
}

func (w *World) characterHitPointLocked(ch storage.Character) int {
	stats := w.combatStatsLocked(ch)
	bonusHit, _ := w.bonusHitSpeedLocked(ch)
	hit := 5 + stats.Hit + bonusHit
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
	_, bonusSpeed := w.bonusHitSpeedLocked(ch)
	speed := int(SubAbilitySpeed(ch.Class)) + stats.Speed + bonusSpeed
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
		if !ch.PowerHitArmed {
			break
		}
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
		if !ch.FireHitArmed {
			break
		}
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
	state.Train += points
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
		if attackIdent == mir176.CMPowerHit {
			return r.Intn(3) + 1, true
		}
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
	return w.attackCharacterWithDamageModeLocked(caster, target, damage, true, true)
}

func (w *World) attackCharacterDirectDamageLocked(caster storage.Character, target storage.Character, damage int) (storage.Character, CharacterHit, error) {
	return w.attackCharacterWithDamageModeLocked(caster, target, damage, false, true)
}

func (w *World) attackCharacterDirectDamageAtLocked(caster storage.Character, target storage.Character, damage int, now time.Time) (storage.Character, CharacterHit, error) {
	return w.attackCharacterWithDamageModeAtLockedAndMark(caster, target, damage, false, true, true, true, now)
}

func (w *World) attackCharacterDirectDamageWithoutMagicShieldAtLocked(caster storage.Character, target storage.Character, damage int, now time.Time) (storage.Character, CharacterHit, error) {
	return w.attackCharacterWithDamageModeAtLockedAndMark(caster, target, damage, false, true, true, false, now)
}

func (w *World) attackCharacterSecondaryDamageLocked(caster storage.Character, target storage.Character, damage int) (storage.Character, CharacterHit, error) {
	return w.attackCharacterWithDamageModeAtLockedAndMark(caster, target, damage, false, false, false, true, time.Now())
}

func (w *World) attackCharacterDirectMagicDamageLocked(caster storage.Character, target storage.Character, damage int) (storage.Character, CharacterHit, error) {
	return w.attackCharacterWithDamageModeLocked(caster, target, damage, false, false)
}

func (w *World) attackCharacterDirectMagicDamageAtLocked(caster storage.Character, target storage.Character, damage int, now time.Time) (storage.Character, CharacterHit, error) {
	return w.attackCharacterWithDamageModeAtLocked(caster, target, damage, false, false, now)
}

func (w *World) characterPhysicalDamageAfterDefenseLocked(target *storage.Character, damage int) int {
	return w.characterPhysicalDamageAfterDefenseAtLocked(target, damage, time.Now())
}

func (w *World) characterPhysicalDamageAfterDefenseAtLocked(target *storage.Character, damage int, now time.Time) int {
	if target == nil || damage <= 0 {
		return 0
	}
	stats := w.combatStatsLocked(*target)
	armor := stats.AC
	low := armor & 0xFF
	high := (armor >> 8) & 0xFF
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
	return applyCharacterMagicBubbleLocked(target, damage, now)
}

func (w *World) attackCharacterWithDamageModeLocked(caster storage.Character, target storage.Character, damage int, applyDefense, applyAttackEffects bool) (storage.Character, CharacterHit, error) {
	return w.attackCharacterWithDamageModeAtLocked(caster, target, damage, applyDefense, applyAttackEffects, time.Now())
}

func (w *World) attackCharacterWithDamageModeAtLocked(caster storage.Character, target storage.Character, damage int, applyDefense, applyAttackEffects bool, now time.Time) (storage.Character, CharacterHit, error) {
	return w.attackCharacterWithDamageModeAtLockedAndMark(caster, target, damage, applyDefense, applyAttackEffects, true, true, now)
}

func (w *World) attackCharacterWithDamageModeAtLockedAndMark(caster storage.Character, target storage.Character, damage int, applyDefense, applyAttackEffects, markCaster, applyMagicShield bool, now time.Time) (storage.Character, CharacterHit, error) {
	canMarkCasterPK := markCaster && w.isProperCharacterTargetLocked(caster, target)
	canSetCasterPK := canMarkCasterPK && w.canSetCharacterPKFlagLocked(target, caster)
	attackerNameColorChanged := false
	if damage < 0 {
		damage = 0
	}
	hitPoint := w.characterHitPointLocked(caster)
	speed := w.characterSpeedPointLocked(target)
	connected := true
	if caster.Account != "" && speed > 0 && hitPoint < w.rand.Intn(speed) {
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
	weaponDamageEligible := connected && damage > 0
	if damage == 0 {
		connected = false
	}
	target, damage, durability, deletedItems, featureChanged := w.applyCharacterStruckAtLocked(target, damage, now)
	hpDamage := damage
	if applyMagicShield {
		hpDamage = w.applyCharacterMagicShieldLocked(&target, damage)
	}
	change := core.ApplyVitalDelta(target, -hpDamage, 0)
	target = change.Character
	if change.Dead {
		w.deferCharacterDeathLocked(target)
	}
	if applyAttackEffects && damage > 0 {
		w.tryApplyParalysisRingToCharacterAtLocked(caster, &target, now)
	}
	if damage > 0 && canMarkCasterPK {
		target.HealthTick = 0
		target.SpellTick = 0
		w.setCharacterLastHitterAtLocked(&target, caster.ID, now)
	}
	if damage > 0 && canSetCasterPK {
		attackerNameColorChanged = !caster.PKFlag
		caster.PKFlag = true
		caster.PKFlagUntil = now.Add(60 * time.Second).UnixNano()
	}
	resultHit := CharacterHit{
		Character:                target,
		Connected:                connected,
		WeaponDamageEligible:     weaponDamageEligible,
		Damage:                   damage,
		Durability:               durability,
		DeletedItems:             deletedItems,
		FeatureChanged:           featureChanged,
		AttackerID:               caster.ID,
		AttackerActor:            CharacterActorID(caster),
		AttackerX:                caster.X,
		AttackerY:                caster.Y,
		AttackerNameColorChanged: attackerNameColorChanged,
		ImpactDelay:              200 * time.Millisecond,
		Dead:                     change.Dead,
		DeathDeferred:            change.Dead,
	}
	return target, resultHit, nil
}

func (w *World) canSetCharacterPKFlagLocked(receiver, attacker storage.Character) bool {
	if characterPKLevel(receiver) >= 2 || characterPKLevel(attacker) >= 2 || receiver.PKFlag {
		return false
	}
	mapData, ok := w.data.Maps[receiver.MapID]
	return !ok || (!mapData.FightZone && !mapData.Fight3Zone)
}

func (w *World) mergeStoredCharacterPKFlagLocked(ch storage.Character) storage.Character {
	stored, ok := w.store.Character(ch.ID)
	if ok && stored.PKFlag {
		ch.PKFlag = true
		ch.PKFlagUntil = stored.PKFlagUntil
	}
	return ch
}

func (w *World) applyCharacterStruckLocked(target storage.Character, damage int) (storage.Character, int, []SpellDurability, []storage.UserItem, bool) {
	return w.applyCharacterStruckAtLocked(target, damage, time.Now())
}

func (w *World) applyCharacterStruckAtLocked(target storage.Character, damage int, now time.Time) (storage.Character, int, []SpellDurability, []storage.UserItem, bool) {
	if damage <= 0 {
		return target, damage, nil, nil, false
	}
	target.PerHealth--
	target.PerSpell--
	nDam := w.rand.Intn(10) + 5
	if characterPoisonArmorActive(target, now) {
		nDam = referenceRound(float64(nDam) * poisonDamageMultiplier(true))
		damage = referenceRound(float64(damage) * poisonDamageMultiplier(true))
	}
	durability := make([]SpellDurability, 0)
	deletedItems := make([]storage.UserItem, 0)
	featureChanged := false
	applyDurability := func(slot int) {
		item, ok := target.EquippedItems[slot]
		if !ok || item.ItemID == "" {
			return
		}
		itemSkillID := ""
		if standard, ok := w.data.Items[item.ItemID]; ok {
			itemSkillID = itemGrantedSkillIDAtSlot(standard, slot)
		}
		oldDisplay := referenceRound(float64(item.Dura) / 1000)
		if int(item.Dura) <= nDam {
			deletedItems = append(deletedItems, item)
			item.Dura = 0
			item.ItemID = ""
			delete(target.ItemSkillHotkeys, itemSkillID)
			featureChanged = true
		} else {
			item.Dura -= uint16(nDam)
		}
		target.EquippedItems[slot] = item
		if oldDisplay != referenceRound(float64(item.Dura)/1000) {
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
	return target, damage, durability, deletedItems, featureChanged
}

func (w *World) applyWeaponDamageLocked(ch *storage.Character) ([]SpellDurability, []storage.UserItem, bool) {
	if ch == nil || ch.ID == "" || ch.EquippedItems == nil {
		return nil, nil, false
	}
	weapon, ok := ch.EquippedItems[SlotWeapon]
	if !ok || weapon.ItemID == "" {
		return nil, nil, false
	}
	loss := w.rand.Intn(5) + 2
	if item, ok := w.data.Items[weapon.ItemID]; ok {
		loss -= item.Strong
	}
	if loss <= 0 {
		return nil, nil, false
	}
	oldDisplay := referenceRound(float64(weapon.Dura) / 1.03)
	deleted := make([]storage.UserItem, 0, 1)
	featureChanged := false
	if int(weapon.Dura) <= loss {
		deleted = append(deleted, weapon)
		weapon.Dura = 0
		weapon.ItemID = ""
		featureChanged = true
	} else {
		weapon.Dura -= uint16(loss)
	}
	ch.EquippedItems[SlotWeapon] = weapon
	if len(deleted) > 0 {
		update := SpellDurability{Slot: SlotWeapon, Dura: weapon.Dura, DuraMax: weapon.DuraMax}
		return []SpellDurability{update, update}, deleted, featureChanged
	}
	if oldDisplay == referenceRound(float64(weapon.Dura)/1.03) {
		return nil, deleted, featureChanged
	}
	return []SpellDurability{{Slot: SlotWeapon, Dura: weapon.Dura, DuraMax: weapon.DuraMax}}, deleted, featureChanged
}

func (w *World) monsterAttackCharacterLocked(mon *Monster, ch storage.Character) (storage.Character, CharacterHit, error) {
	return w.monsterAttackCharacterAtLocked(mon, ch, time.Now())
}

func (w *World) monsterAttackCharacterAtLocked(mon *Monster, ch storage.Character, now time.Time) (storage.Character, CharacterHit, error) {
	damage := mon.MinAttack
	if mon.MaxAttack > mon.MinAttack {
		damage += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
	}
	return w.monsterAttackCharacterWithDamageAtLocked(mon, ch, damage, now)
}

func (w *World) monsterAttackCharacterWithHitCheckAtLocked(mon *Monster, ch storage.Character, now time.Time) (storage.Character, CharacterHit, error) {
	damage := mon.MinAttack
	if mon.MaxAttack > mon.MinAttack {
		damage += w.rand.Intn(mon.MaxAttack - mon.MinAttack + 1)
	}
	if w.characterHitPointLocked(ch) > 0 {
		if speed := w.characterSpeedPointLocked(ch); speed > 0 && w.monsterHitPointLocked(mon) < w.rand.Intn(speed) {
			return ch, CharacterHit{Character: ch, AttackerID: mon.ID, AttackerRaceImg: mon.RaceImg, AttackerAppr: mon.Appr, AttackerX: mon.X, AttackerY: mon.Y}, nil
		}
	}
	return w.monsterAttackCharacterWithDamageAtLocked(mon, ch, damage, now)
}

func (w *World) monsterMagicAttackCharacterLocked(mon *Monster, ch storage.Character) (storage.Character, CharacterHit, error) {
	return w.monsterMagicAttackCharacterAtLocked(mon, ch, time.Now())
}

func (w *World) monsterMagicAttackCharacterAtLocked(mon *Monster, ch storage.Character, now time.Time) (storage.Character, CharacterHit, error) {
	damage := mon.MagicAttack
	maxDamage := mon.MagicAttackMax
	if damage <= 0 {
		damage = mon.MinAttack
		maxDamage = mon.MaxAttack
	}
	if maxDamage > damage {
		damage += w.rand.Intn(maxDamage - damage + 1)
	}
	oldMagic := mon.UseMagic
	mon.UseMagic = true
	updated, hit, err := w.monsterAttackCharacterWithDamageAtLocked(mon, ch, damage, now)
	mon.UseMagic = oldMagic
	hit.Magic = true
	hit.ImpactDelay = 200 * time.Millisecond
	return updated, hit, err
}

func (w *World) monsterMixedAttackCharacterLocked(mon *Monster, ch storage.Character) (storage.Character, CharacterHit, error) {
	return w.monsterMixedAttackCharacterAtLocked(mon, ch, time.Now())
}

func (w *World) monsterMixedAttackCharacterAtLocked(mon *Monster, ch storage.Character, now time.Time) (storage.Character, CharacterHit, error) {
	power := mon.MinAttack
	if mon.MaxAttack > power {
		power += w.rand.Intn(mon.MaxAttack - power + 1)
	}
	return w.monsterMixedAttackCharacterWithPowerAtLocked(mon, ch, power, now)
}

func (w *World) monsterMixedAttackCharacterWithPowerLocked(mon *Monster, ch storage.Character, power int) (storage.Character, CharacterHit, error) {
	return w.monsterMixedAttackCharacterWithPowerAtLocked(mon, ch, power, time.Now())
}

func (w *World) monsterMixedAttackCharacterWithPowerAtLocked(mon *Monster, ch storage.Character, power int, now time.Time) (storage.Character, CharacterHit, error) {
	physical := w.characterPhysicalDamageAfterDefenseAtLocked(&ch, power/2, now)
	magical := w.characterMagicDamageAfterDefenseLocked(ch, power/2, now)
	magical = applyCharacterMagicBubbleLocked(&ch, magical, now)
	if physical+magical <= 0 {
		hit := CharacterHit{Character: ch, AttackerID: mon.ID, AttackerRaceImg: mon.RaceImg, AttackerAppr: mon.Appr, AttackerX: mon.X, AttackerY: mon.Y, ImpactDelay: 200 * time.Millisecond}
		return ch, hit, nil
	}
	return w.attackCharacterDirectDamageWithoutMagicShieldAtLocked(storage.Character{ID: mon.ID}, ch, physical+magical, now)
}

func (w *World) monsterAttackCharacterWithDamageLocked(mon *Monster, ch storage.Character, damage int) (storage.Character, CharacterHit, error) {
	return w.monsterAttackCharacterWithDamageAtLocked(mon, ch, damage, time.Now())
}

func (w *World) monsterReferenceCharacterDamageAtLocked(mon *Monster, ch storage.Character, damage int, now time.Time) (storage.Character, CharacterHit, error) {
	lastHitterID := ch.LastHitterID
	lastHitterAt := ch.LastHitterAt
	expHitterID := ch.ExpHitterID
	expHitterAt := ch.ExpHitterAt
	healthTick := ch.HealthTick
	spellTick := ch.SpellTick
	perHealth := ch.PerHealth
	perSpell := ch.PerSpell
	updated, hit, err := w.monsterAttackCharacterWithDamageAtLocked(mon, ch, damage, now)
	if err != nil {
		return updated, hit, err
	}
	updated.LastHitterID = lastHitterID
	updated.LastHitterAt = lastHitterAt
	updated.ExpHitterID = expHitterID
	updated.ExpHitterAt = expHitterAt
	updated.HealthTick = healthTick
	updated.SpellTick = spellTick
	updated.PerHealth = perHealth
	updated.PerSpell = perSpell
	return updated, hit, nil
}

func (w *World) monsterReferenceMagicCharacterAtLocked(mon *Monster, ch storage.Character, now time.Time) (storage.Character, CharacterHit, error) {
	lastHitterID := ch.LastHitterID
	lastHitterAt := ch.LastHitterAt
	expHitterID := ch.ExpHitterID
	expHitterAt := ch.ExpHitterAt
	healthTick := ch.HealthTick
	spellTick := ch.SpellTick
	perHealth := ch.PerHealth
	perSpell := ch.PerSpell
	updated, hit, err := w.monsterMagicAttackCharacterAtLocked(mon, ch, now)
	if err != nil {
		return updated, hit, err
	}
	updated.LastHitterID = lastHitterID
	updated.LastHitterAt = lastHitterAt
	updated.ExpHitterID = expHitterID
	updated.ExpHitterAt = expHitterAt
	updated.HealthTick = healthTick
	updated.SpellTick = spellTick
	updated.PerHealth = perHealth
	updated.PerSpell = perSpell
	return updated, hit, nil
}

func (w *World) monsterAttackCharacterWithDamageAtLocked(mon *Monster, ch storage.Character, damage int, now time.Time) (storage.Character, CharacterHit, error) {
	stats := w.combatStatsLocked(ch)
	_, _, bubbleLevel, bubbleActive := activeProtectionBuffs(ch, now)
	armor := stats.AC
	if mon.UseMagic {
		armor = stats.MAC
	}
	low := armor & 0xFF
	high := (armor >> 8) & 0xFF
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
		remaining := time.Unix(0, ch.BubbleDefenceUntil).Sub(now)
		if remaining > 3*time.Second {
			remaining -= 3 * time.Second
		} else {
			remaining = time.Second
		}
		ch.BubbleDefenceUntil = now.Add(remaining).UnixNano()
	}
	var durability []SpellDurability
	var deletedItems []storage.UserItem
	var featureChanged bool
	ch, damage, durability, deletedItems, featureChanged = w.applyCharacterStruckAtLocked(ch, damage, now)
	hpDamage := w.applyCharacterMagicShieldLocked(&ch, damage)
	if damage > 0 {
		ch.SpellTick = 0
		ch.HealthTick = 0
		w.setCharacterLastHitterAtLocked(&ch, mon.ID, now)
	}
	change := core.ApplyVitalDelta(ch, -hpDamage, 0)
	ch = change.Character
	if change.Dead {
		w.deferCharacterDeathLocked(ch)
	}
	dead := false
	if change.Dead {
		dead = true
		mon.TargetCharacterID = ""
		mon.TargetX, mon.TargetY = -1, -1
		mon.TargetFocusAt = time.Time{}
		mon.NextSearchAt = w.actionNow
		if mon.NextSearchAt.IsZero() {
			mon.NextSearchAt = time.Now()
		}
	}
	hit := CharacterHit{
		Character:       ch,
		Damage:          damage,
		Durability:      durability,
		DeletedItems:    deletedItems,
		FeatureChanged:  featureChanged,
		AttackerID:      mon.ID,
		AttackerActor:   MonsterActorID(*mon),
		AttackerRaceImg: mon.RaceImg,
		AttackerAppr:    mon.Appr,
		AttackerX:       mon.X,
		AttackerY:       mon.Y,
		ImpactDelay:     200 * time.Millisecond,
		Dead:            dead,
		DeathDeferred:   dead,
	}
	return ch, hit, nil
}

func (w *World) archerAttackMonsterLocked(attacker, target *Monster, power int) (AttackResult, error) {
	if attacker == nil || target == nil {
		return AttackResult{}, fmt.Errorf("archer monster target is missing")
	}
	targetCharacterID := target.TargetCharacterID
	targetFocusAt := target.TargetFocusAt
	targetNextSearchAt := target.NextSearchAt
	caster := storage.Character{ID: attacker.ID, MapID: attacker.MapID, X: attacker.X, Y: attacker.Y}
	damage := w.monsterPhysicalDamageAfterDefenseLocked(target, power)
	hit, err := w.attackMonsterWithDamageModeAndMeatLocked(caster, target, damage, false, false, true, true, false)
	if err != nil {
		return AttackResult{}, err
	}
	target.TargetCharacterID = targetCharacterID
	target.TargetFocusAt = targetFocusAt
	target.NextSearchAt = targetNextSearchAt
	if hit.Damage > 0 {
		w.monsterStruckByMonsterLocked(target, attacker, w.monsterActionTimeLocked())
	}
	hit.Magic = false
	hit.Character = caster
	return hit, nil
}

func (w *World) archerGuardAttackMonsterLocked(attacker, target *Monster, power int, now time.Time) (AttackResult, error) {
	if attacker == nil || target == nil {
		return AttackResult{}, fmt.Errorf("archer guard monster target is missing")
	}
	caster := storage.Character{ID: attacker.ID, MapID: attacker.MapID, X: attacker.X, Y: attacker.Y}
	damage := w.monsterPhysicalDamageAfterDefenseAtLocked(target, power, now)
	hit, err := w.applyReferenceMonsterVsMonsterDamageAtLocked(attacker, target, damage, now)
	if err != nil {
		return AttackResult{}, err
	}
	if hit.Damage > 0 {
		target.LastHitterID = attacker.ID
		target.LastHitterAt = now
		target.ExpHitterID = ""
		target.ExpHitterAt = time.Time{}
		if hit.MonsterHP <= 0 {
			target.PendingDeath = true
			target.DeathHitterID = attacker.ID
		}
		w.enqueueReferenceMonsterStruckLocked(attacker, target, now, time.Duration(maxInt(abs(attacker.X-target.X), abs(attacker.Y-target.Y))*50+600)*time.Millisecond)
	}
	hit.Magic = false
	hit.Character = caster
	return hit, nil
}
