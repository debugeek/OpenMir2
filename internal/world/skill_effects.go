package world

import (
	"fmt"
	"sort"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/npc"
	"openmir2/internal/storage"
	"openmir2/internal/world/core"
)

func (w *World) magCanHitTargetLocked(mapID string, x, y, targetX, targetY int) bool {
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
	}
	return false
}

const (
	skillExplosionRadius = 1
	skillLightningRange  = 8
)

type spellAreaTarget struct {
	Character *storage.Character
	Monster   *Monster
	Trainer   *npc.Entity
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
	return w.spellAreaTargetsWithLifeFilterLocked(players, mapID, x, y, radius, true)
}

func (w *World) hostileSpellAreaTargetsLocked(players []storage.Character, mapID string, x, y, radius int) []spellAreaTarget {
	targets := w.spellAreaTargetsLocked(players, mapID, x, y, radius)
	for _, entity := range w.npcEntitiesInMapOrdered(mapID, true) {
		if !npc.IsTrainer(entity) || entity.Hidden || abs(entity.X-x) > radius || abs(entity.Y-y) > radius {
			continue
		}
		targets = append(targets, spellAreaTarget{Trainer: &entity})
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

func (w *World) spellAreaTargetsWithLifeFilterLocked(players []storage.Character, mapID string, x, y, radius int, requireAlive bool) []spellAreaTarget {
	if radius < 0 {
		return nil
	}
	targets := make([]spellAreaTarget, 0, len(players)+len(w.monsters))
	for i := range players {
		target := players[i]
		if target.ID == "" || target.MapID != mapID || target.Ghost || (requireAlive && w.characterDeadLocked(target)) || abs(target.X-x) > radius || abs(target.Y-y) > radius {
			continue
		}
		targets = append(targets, spellAreaTarget{Character: &target})
	}
	monsters := make([]*Monster, 0, len(w.monsters))
	for _, target := range w.monsters {
		if target == nil || target.ID == "" || (requireAlive && !target.Alive) || target.Ghost || target.MapID != mapID || abs(target.X-x) > radius || abs(target.Y-y) > radius {
			continue
		}
		monsters = append(monsters, target)
	}
	sort.SliceStable(monsters, func(i, j int) bool {
		return monsterSnapshotBefore(*monsters[i], *monsters[j])
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
	if target.Trainer != nil {
		return target.Trainer.X, target.Trainer.Y, target.Trainer.ID
	}
	return target.Monster.X, target.Monster.Y, target.Monster.ID
}

func (target spellAreaTarget) ObjectOrder() uint64 {
	if target.Character != nil {
		return target.Character.ObjectOrder
	}
	if target.Trainer != nil {
		return uint64(target.Trainer.ReferenceOrder)
	}
	return target.Monster.ObjectOrder
}

func spellAreaTargetBefore(a, b spellAreaTarget) bool {
	aOrder, bOrder := a.ObjectOrder(), b.ObjectOrder()
	if aOrder != 0 || bOrder != 0 {
		if aOrder == 0 {
			return false
		}
		if bOrder == 0 {
			return true
		}
		if aOrder != bOrder {
			return aOrder < bOrder
		}
	}
	_, _, aID := a.CharacterOrMonster()
	_, _, bID := b.CharacterOrMonster()
	return aID < bID
}

func (w *World) charactersInRadiusLocked(players []storage.Character, mapID string, x, y, radius int) []storage.Character {
	if radius < 0 {
		return nil
	}
	affected := make([]storage.Character, 0, 8)
	seen := map[string]struct{}{}
	for _, target := range players {
		if target.ID == "" || target.MapID != mapID || w.characterDeadLocked(target) || target.Ghost {
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
	sort.SliceStable(affected, func(i, j int) bool {
		if affected[i].X != affected[j].X {
			return affected[i].X < affected[j].X
		}
		if affected[i].Y != affected[j].Y {
			return affected[i].Y < affected[j].Y
		}
		return characterObjectOrderBefore(affected[i], affected[j])
	})
	return affected
}

func (w *World) movingObjectAtPointLocked(players []storage.Character, mapID string, x, y int) spellAreaTarget {
	return w.movingObjectAtPointWithTrainerLocked(players, mapID, x, y, true)
}

func (w *World) movingObjectAtPointForMonsterLocked(players []storage.Character, mapID string, x, y int) spellAreaTarget {
	return w.movingObjectAtPointWithTrainerLocked(players, mapID, x, y, false)
}

func (w *World) movingObjectAtPointWithTrainerLocked(players []storage.Character, mapID string, x, y int, includeTrainer bool) spellAreaTarget {
	var selected spellAreaTarget
	for i := range players {
		target := players[i]
		if target.ID == "" || target.MapID != mapID || w.characterDeadLocked(target) || target.Ghost || target.ObserverMode || target.X != x || target.Y != y {
			continue
		}
		candidate := spellAreaTarget{Character: &target}
		if selected.Character == nil && selected.Monster == nil || movingObjectBefore(candidate, selected) {
			selected = candidate
		}
	}
	for _, target := range w.monsters {
		if target == nil || target.ID == "" || !target.Alive || target.Ghost || target.MapID != mapID || target.X != x || target.Y != y {
			continue
		}
		candidate := spellAreaTarget{Monster: target}
		if selected.Character == nil && selected.Monster == nil || movingObjectBefore(candidate, selected) {
			selected = candidate
		}
	}
	if includeTrainer {
		for _, target := range w.npcEntitiesInMapOrdered(mapID, true) {
			if !npc.IsTrainer(target) || target.Hidden || target.X != x || target.Y != y {
				continue
			}
			candidate := spellAreaTarget{Trainer: &target}
			if selected.Character == nil && selected.Monster == nil && selected.Trainer == nil || movingObjectBefore(candidate, selected) {
				selected = candidate
			}
		}
	}
	return selected
}

func movingObjectBefore(a, b spellAreaTarget) bool {
	aOrder, bOrder := a.ObjectOrder(), b.ObjectOrder()
	return aOrder != 0 && bOrder != 0 && aOrder < bOrder
}

func (w *World) isProperMonsterTargetLocked(caster storage.Character, players []storage.Character, mon *Monster) bool {
	if mon == nil || !mon.Alive || mon.Ghost || mon.AdminMode || mon.StoneMode {
		return false
	}
	if mon.MasterID == "" {
		return true
	}
	if mon.MasterID == caster.ID {
		return caster.AttackMode == 0
	}
	master, ok := w.characterByIDLocked(players, mon.MasterID)
	if !ok || w.isSafeZoneLocked(caster) || w.isSafeZoneLocked(storage.Character{MapID: mon.MapID, X: mon.X, Y: mon.Y}) {
		return false
	}
	return w.isAttackCharacterTargetLocked(caster, master)
}

func (w *World) isProperDelayedMonsterPoisonTargetLocked(caster storage.Character, players []storage.Character, mon *Monster) bool {
	if mon == nil || !mon.Alive || mon.Ghost || mon.AdminMode || mon.StoneMode {
		return false
	}
	if mon.MasterID == "" {
		return true
	}
	if mon.MasterID == caster.ID {
		return false
	}
	master, ok := w.characterByIDLocked(players, mon.MasterID)
	if !ok || w.isSafeZoneLocked(caster) || w.isSafeZoneLocked(storage.Character{MapID: mon.MapID, X: mon.X, Y: mon.Y}) {
		return false
	}
	return master.TargetID == caster.ID || caster.TargetID == master.ID
}

func (w *World) isProperMonsterAreaTargetLocked(caster storage.Character, players []storage.Character, mon *Monster) bool {
	return w.isProperMonsterTargetLocked(caster, players, mon)
}

func (w *World) isProperCharacterTargetLocked(caster, target storage.Character) bool {
	if caster.ID == "" || target.ID == "" || caster.ID == target.ID || caster.MapID != target.MapID || w.characterDeadLocked(target) || target.AdminMode || target.Ghost || target.StoneMode {
		return false
	}
	if !w.isAttackCharacterTargetLocked(caster, target) {
		return false
	}
	return w.isProtectedCharacterTargetLocked(caster, target)
}

func (w *World) isProperCharacterAreaTargetLocked(caster, target storage.Character) bool {
	return w.isProperCharacterTargetLocked(caster, target)
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
		if !w.gameplay.Combat.NonPKServer && w.guildWarArea(caster) && w.guildWarArea(target) && charactersShareAlliance(caster, target) {
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
		if combat.MapMoveProtectMS > 0 {
			now := time.Now()
			protect := time.Duration(combat.MapMoveProtectMS) * time.Millisecond
			if recentCharacterMove(caster, now, protect) || recentCharacterMove(target, now, protect) {
				return false
			}
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
	for _, startPoint := range mapData.StartPoints {
		if abs(ch.X-startPoint.X) <= combat.SafeZoneSize && abs(ch.Y-startPoint.Y) <= combat.SafeZoneSize {
			return true
		}
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
		itemEntry, ok := w.activeEquippedItemLocked(target, slot)
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
		itemEntry, ok := w.activeEquippedItemLocked(target, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[itemEntry.ItemID]
		if !ok {
			continue
		}
		if (slot == SlotDress || slot == SlotWeapon || slot == SlotRightHand) && item.AniCount == 139 {
			return true
		}
		if slot != SlotDress && slot != SlotWeapon && slot != SlotRightHand && item.Shape == 139 {
			return true
		}
	}
	return false
}

func (w *World) characterHasParalysisRingLocked(ch storage.Character) bool {
	for slot := 0; slot < useSlotCount; slot++ {
		entry, ok := w.activeEquippedItemLocked(ch, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok {
			continue
		}
		if item.AniCount == 113 && (slot == SlotDress || slot == SlotWeapon || slot == SlotRightHand) {
			return true
		}
		if item.Shape == 113 && slot != SlotDress && slot != SlotWeapon && slot != SlotRightHand {
			return true
		}
		if (slot == SlotDress || slot == SlotWeapon || slot == SlotRightHand) &&
			(item.AniCount == 150 || item.AniCount == 151 || item.AniCount == 152 || item.AniCount == 153 || item.AniCount == 157 || item.AniCount == 161) {
			return true
		}
		if slot != SlotDress && slot != SlotWeapon && slot != SlotRightHand && (item.Shape == 150 || item.Shape == 151 || item.Shape == 152 || item.Shape == 153 || item.Shape == 157 || item.Shape == 161) {
			return true
		}
	}
	return false
}

func (w *World) characterAntiPoisonLocked(ch storage.Character) int {
	antiPoison := ch.AntiPoison
	for slot := 0; slot < useSlotCount; slot++ {
		entry, ok := w.activeEquippedItemLocked(ch, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok {
			continue
		}
		display := UpgradeClientItemForDisplay(item, entry, false)
		antiPoison += display.ToxAvoid
		if display.StdMode == 23 {
			antiPoison += display.Stats.AcMax
		}
	}
	if antiPoison < 0 {
		return 0
	}
	return antiPoison
}

func (w *World) tryApplyParalysisRingToCharacterLocked(attacker storage.Character, target *storage.Character) bool {
	return w.tryApplyParalysisRingToCharacterAtLocked(attacker, target, time.Now())
}

func (w *World) tryApplyParalysisRingToCharacterAtLocked(attacker storage.Character, target *storage.Character, now time.Time) bool {
	if target == nil || !w.characterHasParalysisRingLocked(attacker) || w.characterCannotParalyzeLocked(*target) {
		return false
	}
	if w.rand.Intn(w.characterAntiPoisonLocked(*target)+5) != 0 {
		return false
	}
	target.ParalyzedUntil = now.Add(5 * time.Second).UnixNano()
	w.resetCharacterStatusClockLocked(target.ID, statusClockParalyzed, target.ParalyzedUntil, now)
	return true
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
	return w.applyPreparedCharacterMagicDamageAtLocked(caster, target, damage, time.Now())
}

func (w *World) applyPreparedCharacterMagicDamageAtLocked(caster storage.Character, target storage.Character, damage int, now time.Time) (storage.Character, CharacterHit, error) {
	canMarkCasterPK := w.isProperCharacterTargetLocked(caster, target)
	canSetCasterPK := canMarkCasterPK && w.canSetCharacterPKFlagLocked(target, caster)
	magicShield := w.characterHasMagicShieldLocked(target) && !w.characterMagicShieldBlockedLocked(target)
	target, damage, durability, deletedItems, featureChanged := w.applyCharacterStruckAtLocked(target, damage, now)
	hpDamage := w.applyCharacterMagicShieldStateLocked(&target, damage, magicShield)
	change := core.ApplyVitalDelta(target, -hpDamage, 0)
	target = change.Character
	if change.Dead {
		w.deferCharacterDeathLocked(target)
	}
	if damage > 0 {
		target.HealthTick = 0
		target.SpellTick = 0
		if canMarkCasterPK {
			w.setCharacterLastHitterAtLocked(&target, caster.ID, now)
		}
		if canSetCasterPK {
			caster.PKFlag = true
			caster.PKFlagUntil = now.Add(60 * time.Second).UnixNano()
		}
	}
	hit := CharacterHit{
		Character:      target,
		Magic:          true,
		Damage:         hpDamage,
		StruckDamage:   damage,
		Durability:     durability,
		DeletedItems:   deletedItems,
		FeatureChanged: featureChanged,
		AttackerID:     caster.ID,
		AttackerActor:  CharacterActorID(caster),
		AttackerX:      caster.X,
		AttackerY:      caster.Y,
		Dead:           change.Dead,
		DeathDeferred:  change.Dead,
	}
	return target, hit, nil
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

func (w *World) applyCharacterMagicShieldLocked(target *storage.Character, damage int) int {
	if target == nil {
		return maxInt(damage, 0)
	}
	active := w.characterHasMagicShieldLocked(*target) && !w.characterMagicShieldBlockedLocked(*target)
	return w.applyCharacterMagicShieldStateLocked(target, damage, active)
}

func (w *World) applyCharacterMagicShieldStateLocked(target *storage.Character, damage int, active bool) int {
	if target == nil || !active || damage <= 0 || target.MP <= 0 {
		return maxInt(damage, 0)
	}
	shieldCost := referenceRound(float64(damage) * 1.5)
	if int(target.MP) >= shieldCost {
		target.MP -= shieldCost
		return 0
	}
	remaining := shieldCost - int(target.MP)
	target.MP = 0
	return referenceRound(float64(remaining) / 1.5)
}

func (w *World) characterHasMagicShieldLocked(target storage.Character) bool {
	for slot := 0; slot < useSlotCount; slot++ {
		entry, ok := w.activeEquippedItemLocked(target, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok {
			continue
		}
		if item.AniCount == 118 && (slot == SlotDress || slot == SlotWeapon || slot == SlotRightHand) {
			return true
		}
		if item.Shape == 118 && slot != SlotDress && slot != SlotWeapon && slot != SlotRightHand {
			return true
		}
		if (slot == SlotDress || slot == SlotWeapon || slot == SlotRightHand) &&
			(item.AniCount == 150 || item.AniCount == 154 || item.AniCount == 155 || item.AniCount == 156 || item.AniCount == 158 || item.AniCount == 162) {
			return true
		}
		if slot != SlotDress && slot != SlotWeapon && slot != SlotRightHand && (item.Shape == 150 || item.Shape == 154 || item.Shape == 155 || item.Shape == 156 || item.Shape == 158 || item.Shape == 162) {
			return true
		}
	}
	return false
}

func (w *World) characterMagicShieldBlockedLocked(target storage.Character) bool {
	for slot := 0; slot < useSlotCount; slot++ {
		entry, ok := w.activeEquippedItemLocked(target, slot)
		if !ok {
			continue
		}
		item, ok := w.data.Items[entry.ItemID]
		if !ok {
			continue
		}
		if (slot == SlotDress || slot == SlotWeapon || slot == SlotRightHand) && item.AniCount == 143 {
			return true
		}
		if slot != SlotDress && slot != SlotWeapon && slot != SlotRightHand && item.Shape == 143 {
			return true
		}
	}
	return false
}

func (w *World) characterMagicDamageAfterDefenseLocked(target storage.Character, damage int, now time.Time) int {
	if damage < 0 {
		damage = 0
	}
	stats := w.combatStatsLocked(target)
	armor := stats.MAC
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
	return damage
}

func (w *World) castLightningLineSkillLocked(result *SkillCastResult, ch storage.Character, skill data.StdSkill, state storage.SkillState, players []storage.Character, targetX, targetY, targetID int32, steps int, undeadAttack bool, now time.Time) (storage.Character, bool, error) {
	if targetID != 0 {
		result.MagicTargetID = targetID
	}
	if int(targetX) == ch.X && int(targetY) == ch.Y {
		return ch, false, fmt.Errorf("no valid target")
	}
	if _, ok := w.data.Maps[ch.MapID]; !ok {
		return ch, false, fmt.Errorf("map %s not found", ch.MapID)
	}
	dir := direction(ch.X, ch.Y, int(targetX), int(targetY))
	mapData := w.data.Maps[ch.MapID]
	firstX, firstY, ok := spellLineNextPosition(mapData, ch.X, ch.Y, dir, 1)
	if !ok {
		return ch, false, nil
	}
	damage := w.spellMonsterDamageLocked(ch, skill, state)
	lineDamage := damage
	endX, endY, _ := spellLineNextPosition(mapData, ch.X, ch.Y, dir, steps)
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
				appliedDamage := lineDamage
				if undeadAttack {
					lineDamage = referenceRound(float64(lineDamage) * 1.5)
					appliedDamage = lineDamage
				}
				w.enqueuePendingSpellLocked(pendingSpell{DueAt: now.Add(spellDelayMagic), CasterID: ch.ID, TargetMonsterID: mon.ID, SingleMagicStrike: true, TargetX: sx, TargetY: sy, TargetRange: 1, Damage: appliedDamage})
				hitCount++
				hitMonsters[mon.ID] = struct{}{}
			}
		} else if areaTarget.Character != nil {
			target := *areaTarget.Character
			if w.isProperCharacterTargetLocked(ch, target) && w.characterMagicHitAllowedLocked(target) {
				if _, seen := hitCharacters[target.ID]; !seen {
					appliedDamage := lineDamage
					if undeadAttack {
						lineDamage = referenceRound(float64(lineDamage) * 1.5)
						appliedDamage = lineDamage
					}
					w.enqueuePendingSpellLocked(pendingSpell{DueAt: now.Add(spellDelayMagic), CasterID: ch.ID, TargetCharacterID: target.ID, CharacterDamage: true, SingleMagicStrike: true, TargetX: sx, TargetY: sy, TargetRange: 1, Damage: appliedDamage})
					hitCount++
					hitCharacters[target.ID] = struct{}{}
				}
			}
		} else if areaTarget.Trainer != nil && w.canPlayerTargetTrainerLocked(*areaTarget.Trainer) {
			w.enqueuePendingSpellLocked(pendingSpell{DueAt: now.Add(spellDelayMagic), CasterID: ch.ID, TargetTrainerID: areaTarget.Trainer.ID, TargetX: sx, TargetY: sy, TargetRange: 1, Damage: lineDamage})
			hitCount++
		}
		if sx == endX && sy == endY {
			break
		}
		nextDir := direction(sx, sy, endX, endY)
		nextX, nextY, ok := spellLineNextPosition(mapData, sx, sy, nextDir, 1)
		if !ok {
			break
		}
		sx, sy = nextX, nextY
	}
	result.Character = ch
	return ch, hitCount > 0, nil
}

func spellLineNextPosition(mapData data.StdMap, x, y, dir, steps int) (int, int, bool) {
	if steps <= 0 || dir < 0 || dir >= len(dirOffsets) {
		return x, y, false
	}
	nextX := x + dirOffsets[dir][0]*steps
	nextY := y + dirOffsets[dir][1]*steps
	if nextX < 0 || nextY < 0 || nextX >= mapData.Width || nextY >= mapData.Height {
		return x, y, false
	}
	return nextX, nextY, true
}

func (w *World) castExplosionSkillLocked(result *SkillCastResult, ch storage.Character, skill data.StdSkill, state storage.SkillState, targetX, targetY int, players []storage.Character, now time.Time) (storage.Character, bool, error) {
	if _, ok := w.data.Maps[ch.MapID]; !ok {
		return ch, false, fmt.Errorf("map %s not found", ch.MapID)
	}
	damage := w.spellMonsterDamageLocked(ch, skill, state)
	validTarget := false
	hitMonsters := map[string]struct{}{}
	hitCharacters := map[string]struct{}{}
	for _, areaTarget := range w.hostileSpellAreaTargetsLocked(players, ch.MapID, targetX, targetY, skillExplosionRadius) {
		if areaTarget.Monster != nil {
			mon := areaTarget.Monster
			if !w.isProperMonsterAreaTargetLocked(ch, players, mon) {
				continue
			}
			validTarget = true
			if _, seen := hitMonsters[mon.ID]; seen {
				continue
			}
			setCharacterTargetLocked(&ch, mon.ID, now)
			w.enqueuePendingSpellLocked(pendingSpell{DueAt: now, CasterID: ch.ID, TargetMonsterID: mon.ID, SingleMagicStrike: true, Damage: damage})
			hitMonsters[mon.ID] = struct{}{}
			continue
		}
		if areaTarget.Trainer != nil && w.canPlayerTargetTrainerLocked(*areaTarget.Trainer) {
			w.enqueuePendingSpellLocked(pendingSpell{DueAt: now, CasterID: ch.ID, TargetTrainerID: areaTarget.Trainer.ID, Damage: damage})
			validTarget = true
			continue
		}
		target := *areaTarget.Character
		if !w.isProperCharacterAreaTargetLocked(ch, target) {
			continue
		}
		validTarget = true
		if _, seen := hitCharacters[target.ID]; seen {
			continue
		}
		setCharacterTargetLocked(&ch, target.ID, now)
		w.enqueuePendingSpellLocked(pendingSpell{DueAt: now, CasterID: ch.ID, TargetCharacterID: target.ID, CharacterDamage: true, SingleMagicStrike: true, Damage: damage})
		hitCharacters[target.ID] = struct{}{}
	}
	result.Character = ch
	return ch, validTarget, nil
}

func (w *World) castElectricBlizzardSkillLocked(result *SkillCastResult, ch storage.Character, skill data.StdSkill, state storage.SkillState, targetX, targetY int, players []storage.Character, now time.Time) (storage.Character, bool, error) {
	const radius = 2
	validTarget := false
	damage := w.spellMonsterDamageLocked(ch, skill, state)
	for _, areaTarget := range w.hostileSpellAreaTargetsLocked(players, ch.MapID, ch.X, ch.Y, radius) {
		if areaTarget.Monster != nil {
			mon := areaTarget.Monster
			if !w.isProperMonsterAreaTargetLocked(ch, players, mon) {
				continue
			}
			pendingDamage := damage
			if mon.Undead <= 0 {
				pendingDamage /= 10
			}
			w.enqueuePendingSpellLocked(pendingSpell{DueAt: now, CasterID: ch.ID, TargetMonsterID: mon.ID, SingleMagicStrike: true, TargetX: mon.X, TargetY: mon.Y, TargetRange: -1, Damage: pendingDamage})
			validTarget = true
			continue
		}
		if areaTarget.Trainer != nil && w.canPlayerTargetTrainerLocked(*areaTarget.Trainer) {
			w.enqueuePendingSpellLocked(pendingSpell{DueAt: now, CasterID: ch.ID, TargetTrainerID: areaTarget.Trainer.ID, Damage: damage})
			validTarget = true
			continue
		}
		target := *areaTarget.Character
		if !w.isProperCharacterAreaTargetLocked(ch, target) {
			continue
		}
		validTarget = true
		w.enqueuePendingSpellLocked(pendingSpell{DueAt: now, CasterID: ch.ID, TargetCharacterID: target.ID, SingleMagicStrike: true, CharacterDamage: true, TargetX: target.X, TargetY: target.Y, TargetRange: -1, Damage: damage / 10})
	}
	result.Character = ch
	return ch, validTarget, nil
}
