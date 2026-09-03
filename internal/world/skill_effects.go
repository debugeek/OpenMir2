package world

import (
	"fmt"
	"sort"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
	"openmir2/internal/world/core"
)

func (w *World) magCanHitTargetLocked(mapID string, x, y, targetX, targetY int) bool {
	if x == targetX && y == targetY {
		return true
	}
	mapData, ok := w.data.Maps[mapID]
	if !ok {
		return false
	}
	distance := abs(x-targetX) + abs(y-targetY)
	for i := 0; i < 13; i++ {
		dir := direction(x, y, targetX, targetY)
		nextX := x + dirOffsets[dir][0]
		nextY := y + dirOffsets[dir][1]
		if !mapData.Walkable(nextX, nextY) {
			return false
		}
		x, y = nextX, nextY
		if x == targetX && y == targetY {
			return true
		}
		if abs(x-targetX)+abs(y-targetY) > distance {
			return true
		}
		distance = abs(x-targetX) + abs(y-targetY)
	}
	return false
}

const (
	skillExplosionRadius      = 1
	skillElectricBlizzardSize = 2
	skillLightningRange       = 8
)

type spellAreaTarget struct {
	Character *storage.Character
	Monster   *Monster
}

func playersWithCaster(players []storage.Character, caster storage.Character) []storage.Character {
	if caster.ID == "" {
		return players
	}
	for _, player := range players {
		if player.ID == caster.ID {
			return players
		}
	}
	return append(append([]storage.Character(nil), players...), caster)
}

func orderSpellAffectedTargets(characters []storage.Character, monsters []Monster) []SpellAffectedTarget {
	targets := make([]spellAreaTarget, 0, len(characters)+len(monsters))
	for i := range characters {
		target := characters[i]
		targets = append(targets, spellAreaTarget{Character: &target})
	}
	for i := range monsters {
		target := monsters[i]
		targets = append(targets, spellAreaTarget{Monster: &target})
	}
	sort.SliceStable(targets, func(i, j int) bool {
		ix, iy, _ := targets[i].CharacterOrMonster()
		jx, jy, _ := targets[j].CharacterOrMonster()
		if ix != jx {
			return ix < jx
		}
		if iy != jy {
			return iy < jy
		}
		return spellAreaTargetBefore(targets[i], targets[j])
	})
	ordered := make([]SpellAffectedTarget, 0, len(targets))
	for _, target := range targets {
		ordered = append(ordered, SpellAffectedTarget{Character: target.Character, Monster: target.Monster})
	}
	return ordered
}

func (w *World) spellAreaTargetsLocked(players []storage.Character, mapID string, x, y, radius int) []spellAreaTarget {
	return w.spellAreaTargetsWithDeadLocked(players, mapID, x, y, radius, false)
}

func (w *World) spellAreaTargetsIncludingDeadLocked(players []storage.Character, mapID string, x, y, radius int) []spellAreaTarget {
	return w.spellAreaTargetsWithDeadLocked(players, mapID, x, y, radius, true)
}

func (w *World) spellAreaTargetsWithDeadLocked(players []storage.Character, mapID string, x, y, radius int, includeDead bool) []spellAreaTarget {
	targets := make([]spellAreaTarget, 0, len(players)+len(w.monsters))
	for i := range players {
		target := players[i]
		if target.ID == "" || target.MapID != mapID || (!includeDead && target.HP <= 0) || abs(target.X-x) > radius || abs(target.Y-y) > radius {
			continue
		}
		targets = append(targets, spellAreaTarget{Character: &target})
	}
	monsters := make([]*Monster, 0, len(w.monsters))
	for _, target := range w.monsters {
		if target == nil || (!includeDead && !target.Alive) || target.MapID != mapID || abs(target.X-x) > radius || abs(target.Y-y) > radius {
			continue
		}
		monsters = append(monsters, target)
	}
	sort.SliceStable(monsters, func(i, j int) bool {
		if monsters[i].ObjectOrder == 0 || monsters[j].ObjectOrder == 0 {
			return idSeq(monsters[i].ID) < idSeq(monsters[j].ID)
		}
		return monsters[i].ObjectOrder < monsters[j].ObjectOrder
	})
	for _, target := range monsters {
		targets = append(targets, spellAreaTarget{Monster: target})
	}
	sort.SliceStable(targets, func(i, j int) bool {
		ix, iy, _ := targets[i].CharacterOrMonster()
		jx, jy, _ := targets[j].CharacterOrMonster()
		if ix != jx {
			return ix < jx
		}
		if iy != jy {
			return iy < jy
		}
		return spellAreaTargetBefore(targets[i], targets[j])
	})
	return targets
}

func (target spellAreaTarget) CharacterOrMonster() (int, int, string) {
	if target.Character != nil {
		return target.Character.X, target.Character.Y, target.Character.ID
	}
	return target.Monster.X, target.Monster.Y, target.Monster.ID
}

func (target spellAreaTarget) ObjectOrder() uint64 {
	if target.Character != nil {
		return target.Character.ObjectOrder
	}
	return target.Monster.ObjectOrder
}

func spellAreaTargetBefore(a, b spellAreaTarget) bool {
	aOrder, bOrder := a.ObjectOrder(), b.ObjectOrder()
	if aOrder == 0 || bOrder == 0 {
		return false
	}
	return aOrder < bOrder
}

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

func characterAtExactPointLocked(players []storage.Character, mapID string, x, y int) (storage.Character, bool) {
	for _, target := range players {
		if target.ID == "" || target.MapID != mapID || target.HP <= 0 || target.X != x || target.Y != y {
			continue
		}
		return target, true
	}
	return storage.Character{}, false
}

func (w *World) movingObjectAtPointLocked(players []storage.Character, mapID string, x, y int) spellAreaTarget {
	var selected spellAreaTarget
	selectedOrder := uint64(0)
	for i := range players {
		target := players[i]
		if target.ID == "" || target.MapID != mapID || target.HP <= 0 || target.X != x || target.Y != y {
			continue
		}
		candidate := spellAreaTarget{Character: &target}
		order := candidate.ObjectOrder()
		if selected.Character == nil && selected.Monster == nil || order != 0 && selectedOrder != 0 && order < selectedOrder {
			selected = candidate
			selectedOrder = order
		}
	}
	for _, target := range w.monsters {
		if target == nil || !target.Alive || target.MapID != mapID || target.X != x || target.Y != y {
			continue
		}
		candidate := spellAreaTarget{Monster: target}
		order := candidate.ObjectOrder()
		if selected.Character == nil && selected.Monster == nil || order != 0 && selectedOrder != 0 && order < selectedOrder {
			selected = candidate
			selectedOrder = order
		}
	}
	return selected
}

func (w *World) isProperMonsterTargetLocked(caster storage.Character, players []storage.Character, mon *Monster) bool {
	if mon == nil || !mon.Alive || mon.AdminMode || mon.StoneMode {
		return false
	}
	if mon.MasterID == "" {
		return true
	}
	if mon.MasterID == caster.ID {
		return caster.AttackMode == 0
	}
	master, ok := w.characterByIDLocked(players, mon.MasterID)
	if !ok || w.isSafeZoneLocked(caster) || w.isSafeZoneLocked(storage.Character{MapID: mon.MapID}) {
		return false
	}
	return w.isAttackCharacterTargetLocked(caster, master)
}

func (w *World) isProperCharacterTargetLocked(caster, target storage.Character) bool {
	if caster.ID == "" || target.ID == "" || caster.ID == target.ID || caster.MapID != target.MapID || target.HP <= 0 || target.AdminMode || target.StoneMode {
		return false
	}
	if !w.isAttackCharacterTargetLocked(caster, target) {
		return false
	}
	return w.isProtectedCharacterTargetLocked(caster, target)
}

func (w *World) isAttackCharacterTargetLocked(caster, target storage.Character) bool {
	switch caster.AttackMode {
	case 1:
		return false
	case 2:
		if !w.gameplay.Combat.NonPKServer && w.isProperFriendLocked(caster, target) {
			return false
		}
	case 3:
		if !w.gameplay.Combat.NonPKServer && caster.GuildID != "" && caster.GuildID == target.GuildID {
			return false
		}
		if !w.gameplay.Combat.NonPKServer && caster.GuildWarArea && target.GuildWarArea && caster.GuildAllianceID != "" && caster.GuildAllianceID == target.GuildAllianceID {
			return false
		}
	case 4:
		if w.gameplay.Combat.NonPKServer {
			break
		}
		casterPK, targetPK := characterPKLevel(caster), characterPKLevel(target)
		if casterPK >= 2 {
			if targetPK >= 2 {
				return false
			}
		} else if targetPK < 2 {
			return false
		}
	default:
	}
	return true
}

func characterPKLevel(ch storage.Character) int {
	return ch.PKPoint / 100
}

func (w *World) isProtectedCharacterTargetLocked(caster, target storage.Character) bool {
	if w.isSafeZoneLocked(caster) || w.isSafeZoneLocked(target) {
		return false
	}
	if !target.FreePKArea {
		combat := w.gameplay.Combat
		casterPK, targetPK := characterPKLevel(caster), characterPKLevel(target)
		if combat.PKLevelProtect {
			if caster.Level > combat.PKProtectLevel && !target.PKFlag && target.Level <= combat.PKProtectLevel && targetPK < 2 {
				return false
			}
			if caster.Level <= combat.PKProtectLevel && !target.PKFlag && target.Level > combat.PKProtectLevel && targetPK < 2 {
				return false
			}
		}
		if casterPK >= 2 && caster.Level > combat.RedPKProtectLevel && target.Level <= combat.RedPKProtectLevel && targetPK < 2 {
			return false
		}
		if caster.Level <= combat.RedPKProtectLevel && casterPK < 2 && targetPK >= 2 && target.Level > combat.RedPKProtectLevel {
			return false
		}
	}
	if combat := w.gameplay.Combat.MapMoveProtectMS; combat > 0 {
		now := time.Now()
		protect := time.Duration(combat) * time.Millisecond
		if recentCharacterMove(caster, now, protect) || recentCharacterMove(target, now, protect) {
			return false
		}
	}
	return true
}

func recentCharacterMove(ch storage.Character, now time.Time, protect time.Duration) bool {
	if ch.MapMoveAt == 0 {
		return false
	}
	movedAt := time.Unix(0, ch.MapMoveAt)
	return !now.Before(movedAt) && now.Sub(movedAt) < protect
}

func (w *World) isSafeZoneLocked(ch storage.Character) bool {
	mapData, ok := w.data.Maps[ch.MapID]
	if !ok {
		return false
	}
	if mapData.Safe {
		return true
	}
	combat := w.gameplay.Combat
	if ch.MapID == combat.RedHomeMap && abs(ch.X-combat.RedHomeX) <= combat.SafeZoneSize && abs(ch.Y-combat.RedHomeY) <= combat.SafeZoneSize {
		return true
	}
	return false
}

func (w *World) monsterMagicHitAllowedLocked(mon *Monster) bool {
	if mon == nil {
		return false
	}
	return w.rand.Intn(10) >= mon.AntiMagic
}

func (w *World) characterMagicHitAllowedLocked(target storage.Character) bool {
	antiMagic := 1
	for slot := 0; slot < useSlotCount; slot++ {
		itemEntry, ok := w.equippedItemLocked(target, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[itemEntry.ItemID]
		if !ok {
			continue
		}
		antiMagic += UpgradeClientItemForDisplay(item, itemEntry, false).MgAvoid
	}
	return w.rand.Intn(10) >= antiMagic
}

func (w *World) characterCannotParalyzeLocked(target storage.Character) bool {
	for slot := 0; slot < useSlotCount; slot++ {
		itemEntry, ok := w.equippedItemLocked(target, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[itemEntry.ItemID]
		if ok && (item.AniCount == 139 || item.Shape == 139) {
			return true
		}
	}
	return false
}

func (w *World) spellCharacterDamageWithPowerLocked(caster storage.Character, target storage.Character, damage int) (storage.Character, CharacterHit, error) {
	now := time.Now()
	target, damage = w.prepareCharacterMagicDamageLocked(target, damage, now)
	return w.applyPreparedCharacterMagicDamageLocked(caster, target, damage)
}

func (w *World) prepareCharacterMagicDamageLocked(target storage.Character, damage int, now time.Time) (storage.Character, int) {
	damage = w.characterMagicDamageAfterDefenseLocked(target, damage, now)
	damage = applyCharacterMagicBubbleLocked(&target, damage, now)
	return target, damage
}

func (w *World) applyPreparedCharacterMagicDamageLocked(caster storage.Character, target storage.Character, damage int) (storage.Character, CharacterHit, error) {
	target, damage, durability, featureChanged := w.applyCharacterStruckLocked(target, damage)
	change := core.ApplyVitalDelta(target, -damage, 0)
	target = change.Character
	if damage > 0 {
		target.SpellTick = 0
	}
	hit := CharacterHit{
		Character:      target,
		Magic:          true,
		Damage:         damage,
		Durability:     durability,
		FeatureChanged: featureChanged,
		AttackerID:     caster.ID,
		AttackerX:      caster.X,
		AttackerY:      caster.Y,
		Dead:           change.Dead,
	}
	return target, hit, w.store.SaveCharacter(target)
}

func applyCharacterMagicBubbleLocked(target *storage.Character, damage int, now time.Time) int {
	if target == nil || damage <= 0 {
		return maxInt(damage, 0)
	}
	_, _, bubbleLevel, bubbleActive := activeProtectionBuffs(*target, now)
	if !bubbleActive {
		return damage
	}
	damage = referenceRound(float64(damage) * float64(int(bubbleLevel)+2) * 8.0 / 100.0)
	if damage < 0 {
		damage = 0
	}
	remaining := time.Unix(0, target.BubbleDefenceUntil).Sub(now)
	if remaining > 3*time.Second {
		remaining -= 3 * time.Second
	} else {
		remaining = time.Second
	}
	target.BubbleDefenceUntil = now.Add(remaining).UnixNano()
	return damage
}

func (w *World) characterMagicDamageAfterDefenseLocked(target storage.Character, damage int, now time.Time) int {
	if damage < 0 {
		damage = 0
	}
	stats := w.combatStatsLocked(target)
	_, magicDefenceBonus, _, _ := activeProtectionBuffs(target, now)
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
	return damage
}

func (w *World) castLightningLineSkillLocked(result *SkillCastResult, ch storage.Character, skill data.StdSkill, state storage.SkillState, players []storage.Character, targetX, targetY, targetID int32, steps int, undeadAttack bool, now time.Time) (storage.Character, bool, error) {
	if int(targetX) == ch.X && int(targetY) == ch.Y {
		return ch, false, fmt.Errorf("no valid target")
	}
	if _, ok := w.data.Maps[ch.MapID]; !ok {
		return ch, false, fmt.Errorf("map %s not found", ch.MapID)
	}
	damage := w.spellMonsterDamageLocked(ch, skill, state)
	lineDamage := damage
	dir := direction(ch.X, ch.Y, int(targetX), int(targetY))
	firstX := ch.X + dirOffsets[dir][0]
	firstY := ch.Y + dirOffsets[dir][1]
	if !w.data.Maps[ch.MapID].Walkable(firstX, firstY) {
		return ch, false, nil
	}
	endX, endY := ch.X, ch.Y
	for i := 0; i < steps; i++ {
		nextX := endX + dirOffsets[dir][0]
		nextY := endY + dirOffsets[dir][1]
		if !w.data.Maps[ch.MapID].Walkable(nextX, nextY) {
			break
		}
		endX, endY = nextX, nextY
	}
	result.MagicFireTargetX = endX
	result.MagicFireTargetY = endY
	result.MagicFireTargetSet = true
	sx, sy := firstX, firstY
	hitCount := 0
	hitMonsters := map[string]struct{}{}
	hitCharacters := map[string]struct{}{}
	for i := 0; i < 13; i++ {
		areaTarget := w.movingObjectAtPointLocked(players, ch.MapID, sx, sy)
		if areaTarget.Monster != nil {
			mon := areaTarget.Monster
			if _, seen := hitMonsters[mon.ID]; !seen && w.isProperMonsterTargetLocked(ch, players, mon) && w.monsterMagicHitAllowedLocked(mon) {
				if undeadAttack {
					lineDamage = referenceRound(float64(lineDamage) * 1.5)
				}
				w.pendingSpells = append(w.pendingSpells, pendingSpell{DueAt: now.Add(spellDelayMagic), CasterID: ch.ID, TargetMonsterID: mon.ID, TargetX: sx, TargetY: sy, Damage: lineDamage, SingleMagicStrike: true})
				hitCount++
				hitMonsters[mon.ID] = struct{}{}
			}
		} else if areaTarget.Character != nil {
			target := *areaTarget.Character
			if w.isProperCharacterTargetLocked(ch, target) && w.characterMagicHitAllowedLocked(target) {
				if _, seen := hitCharacters[target.ID]; !seen {
					if undeadAttack {
						lineDamage = referenceRound(float64(lineDamage) * 1.5)
					}
					w.pendingSpells = append(w.pendingSpells, pendingSpell{DueAt: now.Add(spellDelayMagic), CasterID: ch.ID, TargetCharacterID: target.ID, CharacterDamage: true, TargetX: sx, TargetY: sy, Damage: lineDamage, SingleMagicStrike: true})
					hitCount++
					hitCharacters[target.ID] = struct{}{}
				}
			}
		}
		if sx == endX && sy == endY {
			break
		}
		nextDir := direction(sx, sy, endX, endY)
		nextX := sx + dirOffsets[nextDir][0]
		nextY := sy + dirOffsets[nextDir][1]
		if !w.data.Maps[ch.MapID].Walkable(nextX, nextY) {
			break
		}
		sx, sy = nextX, nextY
	}
	result.Character = ch
	return ch, hitCount > 0, nil
}

func (w *World) castExplosionSkillLocked(result *SkillCastResult, ch storage.Character, skill data.StdSkill, state storage.SkillState, targetX, targetY int, players []storage.Character) (storage.Character, bool, error) {
	if _, ok := w.data.Maps[ch.MapID]; !ok {
		return ch, false, fmt.Errorf("map %s not found", ch.MapID)
	}
	damage := w.spellMonsterDamageLocked(ch, skill, state)
	validTarget := false
	hitMonsters := map[string]struct{}{}
	hitCharacters := map[string]struct{}{}
	for _, areaTarget := range w.spellAreaTargetsLocked(players, ch.MapID, targetX, targetY, skillExplosionRadius) {
		if areaTarget.Monster != nil {
			mon := areaTarget.Monster
			if !w.isProperMonsterTargetLocked(ch, players, mon) {
				continue
			}
			validTarget = true
			if _, seen := hitMonsters[mon.ID]; seen {
				continue
			}
			ch.TargetID = mon.ID
			hit, err := w.attackMonsterWithImmediateMagicDamageLocked(ch, mon, damage, players...)
			if err != nil {
				return ch, false, err
			}
			if hit.Damage > 0 {
				result.Impacts = append(result.Impacts, SpellImpact{MonsterHit: &hit})
			}
			hitMonsters[mon.ID] = struct{}{}
			continue
		}
		target := *areaTarget.Character
		if !w.isProperCharacterTargetLocked(ch, target) {
			continue
		}
		validTarget = true
		if _, seen := hitCharacters[target.ID]; seen {
			continue
		}
		ch.TargetID = target.ID
		updated, hit, err := w.spellCharacterDamageWithPowerLocked(ch, target, damage)
		if err != nil {
			return ch, false, err
		}
		if updated.ID != "" {
			result.Impacts = append(result.Impacts, SpellImpact{CharacterHit: &hit})
		}
		hitCharacters[target.ID] = struct{}{}
	}
	result.Character = ch
	return ch, validTarget, nil
}

func (w *World) castElectricBlizzardSkillLocked(result *SkillCastResult, ch storage.Character, skill data.StdSkill, state storage.SkillState, players []storage.Character) (storage.Character, bool, error) {
	damage := w.spellMonsterDamageLocked(ch, skill, state)
	validTarget := false
	hitMonsters := map[string]struct{}{}
	hitCharacters := map[string]struct{}{}
	for _, areaTarget := range w.spellAreaTargetsLocked(players, ch.MapID, ch.X, ch.Y, skillElectricBlizzardSize) {
		if areaTarget.Monster != nil {
			mon := areaTarget.Monster
			if !w.isProperMonsterTargetLocked(ch, players, mon) {
				continue
			}
			validTarget = true
			if _, seen := hitMonsters[mon.ID]; seen {
				continue
			}
			applied := damage
			if mon.Undead <= 0 {
				applied = damage / 10
			}
			hit, err := w.attackMonsterWithImmediateMagicDamageLocked(ch, mon, applied, players...)
			if err != nil {
				return ch, false, err
			}
			if hit.Damage > 0 {
				result.Impacts = append(result.Impacts, SpellImpact{MonsterHit: &hit})
			}
			hitMonsters[mon.ID] = struct{}{}
			continue
		}
		target := *areaTarget.Character
		if !w.isProperCharacterTargetLocked(ch, target) {
			continue
		}
		validTarget = true
		if _, seen := hitCharacters[target.ID]; seen {
			continue
		}
		applied := damage / 10
		if applied <= 0 {
			continue
		}
		_, hit, err := w.spellCharacterDamageWithPowerLocked(ch, target, applied)
		if err != nil {
			return ch, false, err
		}
		if hit.Damage > 0 {
			result.Impacts = append(result.Impacts, SpellImpact{CharacterHit: &hit})
		}
		hitCharacters[target.ID] = struct{}{}
	}
	result.Character = ch
	return ch, validTarget, nil
}
