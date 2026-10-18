package world

import (
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func (w *World) moveMonsterTowardPointLocked(mon *Monster, x, y int, players map[string]storage.Character) bool {
	now := w.actionNow
	if now.IsZero() {
		now = time.Now()
	}
	if w.monsterHolySeizeActiveLocked(mon, now) {
		return false
	}
	mp, ok := w.data.Maps[mon.MapID]
	if !ok {
		return false
	}
	if mon.X == x && mon.Y == y {
		return false
	}
	dir := walkDirection(mon.X, mon.Y, x, y)
	target := storage.Character{MapID: mon.MapID, X: x, Y: y}
	fallbackDirection := w.rand.Intn(3)
	if w.tryMoveMonsterLocked(mon, target, mp, dir, players) {
		return true
	}
	for range dirOffsets {
		if fallbackDirection != 0 {
			dir = (dir + 1) % len(dirOffsets)
		} else if dir > 0 {
			dir--
		} else {
			dir = len(dirOffsets) - 1
		}
		if w.tryMoveMonsterLocked(mon, target, mp, dir, players) {
			return true
		}
	}
	return false
}

func (w *World) tryMoveMonsterLocked(mon *Monster, target storage.Character, mp data.StdMap, dir int, players map[string]storage.Character) bool {
	mon.Dir = dir
	off := dirOffsets[dir]
	x, y := mon.X+off[0], mon.Y+off[1]
	if !mp.Walkable(x, y) || w.movingObjectAtLocked(players, mon.MapID, x, y, mon.ID) {
		return false
	}
	if mon.MasterID != "" {
		if master, ok := players[mon.MasterID]; ok && master.MapID == mon.MapID {
			masterDir := master.Dir
			if masterDir < 0 || masterDir >= len(dirOffsets) {
				masterDir = 0
			}
			masterOffset := dirOffsets[masterDir]
			if x == master.X+masterOffset[0] && y == master.Y+masterOffset[1] {
				return false
			}
		}
	}
	w.moveMonsterLocked(mon, x, y)
	w.applyMonsterGroundEventHitLocked(mon, players, w.monsterActionTimeLocked())
	return true
}

func (w *World) monsterDamageGroundEventAtLocked(mapID string, x, y int) bool {
	field, ok := w.fireFields[fireFieldKey{MapID: mapID, X: x, Y: y}]
	now := w.monsterActionTimeLocked()
	return ok && field.Damage > 0 && (field.ExpiresAt.IsZero() || !now.After(field.ExpiresAt))
}

func (w *World) applyMonsterGroundEventHitLocked(mon *Monster, players map[string]storage.Character, now time.Time) {
	field, ok := w.fireFields[fireFieldKey{MapID: mon.MapID, X: mon.X, Y: mon.Y}]
	if !ok || field.OwnerID == "" || field.Damage <= 0 || (!field.ExpiresAt.IsZero() && now.After(field.ExpiresAt)) {
		return
	}
	owner := field.Owner
	if owner.ID == "" {
		owner = storage.Character{ID: field.OwnerID, MapID: field.MapID, X: field.X, Y: field.Y}
	}
	if !w.isProperMonsterAreaTargetLocked(owner, charactersFromMap(players), mon) {
		return
	}
	damage := w.monsterMagicDamageAfterDefenseLocked(mon, field.Damage)
	hit, err := w.applyReferenceMonsterMagicDamageAtLocked(owner, mon, damage, now)
	if err == nil && hit.Damage > 0 {
		if len(w.monsterBehaviorGroundHits) == 0 {
			w.monsterBehaviorGroundHitStart = len(w.monsterBehaviorHits)
		}
		w.monsterBehaviorHits = append(w.monsterBehaviorHits, hit)
		w.monsterBehaviorGroundHits = append(w.monsterBehaviorGroundHits, hit)
	}
}

func (w *World) moveMonsterTowardLocked(mon *Monster, target storage.Character, dir int, players map[string]storage.Character) bool {
	now := w.actionNow
	if now.IsZero() {
		now = time.Now()
	}
	if w.monsterHolySeizeActiveLocked(mon, now) {
		return false
	}
	mp, ok := w.data.Maps[mon.MapID]
	if !ok {
		return false
	}
	fallbackDirection := w.rand.Intn(3)
	if w.tryMoveMonsterLocked(mon, target, mp, dir, players) {
		return true
	}
	for range dirOffsets {
		if fallbackDirection != 0 {
			dir = (dir + 1) % len(dirOffsets)
		} else if dir > 0 {
			dir--
		} else {
			dir = len(dirOffsets) - 1
		}
		if w.tryMoveMonsterLocked(mon, target, mp, dir, players) {
			return true
		}
	}
	return false
}

func (w *World) monsterWalkReadyLocked(mon *Monster, now time.Time) bool {
	if mon.LastWalkAt.IsZero() {
		mon.LastWalkAt = now.Add(-time.Duration(w.monsterWalkSpeedMSLocked(mon)+1) * time.Millisecond)
	} else if now.Before(mon.LastWalkAt) {
		return false
	}
	if mon.WalkWaitLocked {
		if mon.WalkWait <= 0 || now.Sub(mon.WalkWaitTick) > time.Duration(mon.WalkWait)*time.Millisecond {
			mon.WalkWaitLocked = false
		} else {
			return false
		}
	}
	if now.Sub(mon.LastWalkAt) <= time.Duration(w.monsterWalkSpeedMSLocked(mon))*time.Millisecond {
		return false
	}
	mon.LastWalkAt = now
	mon.WalkCount++
	if mon.WalkCount > mon.WalkStep {
		mon.WalkCount = 0
		mon.WalkWaitLocked = true
		mon.WalkWaitTick = now
	}
	return true
}

func (w *World) referenceSpecialWalkReadyLocked(mon *Monster, now time.Time) bool {
	if mon.LastWalkAt.IsZero() {
		mon.LastWalkAt = now.Add(-time.Duration(w.monsterWalkSpeedMSLocked(mon)+1) * time.Millisecond)
	} else if now.Before(mon.LastWalkAt) {
		return false
	}
	if now.Sub(mon.LastWalkAt) <= time.Duration(w.monsterWalkSpeedMSLocked(mon))*time.Millisecond {
		return false
	}
	mon.LastWalkAt = now
	return true
}

func (w *World) referenceSpecialWalkReadyInclusiveLocked(mon *Monster, now time.Time) bool {
	if mon.LastWalkAt.IsZero() {
		mon.LastWalkAt = now.Add(-time.Duration(w.monsterWalkSpeedMSLocked(mon)) * time.Millisecond)
	} else if now.Before(mon.LastWalkAt) {
		return false
	}
	if now.Sub(mon.LastWalkAt) < time.Duration(w.monsterWalkSpeedMSLocked(mon))*time.Millisecond {
		return false
	}
	mon.LastWalkAt = now
	return true
}

func (w *World) referenceStoneRunReadyLocked(mon *Monster, now time.Time) bool {
	speed := time.Duration(w.monsterWalkSpeedMSLocked(mon)) * time.Millisecond
	if mon.LastWalkAt.IsZero() {
		mon.LastWalkAt = now.Add(-speed)
	}
	return now.Sub(mon.LastWalkAt) >= speed
}

func (w *World) monsterWalkReadyInclusiveLocked(mon *Monster, now time.Time) bool {
	if mon.LastWalkAt.IsZero() {
		mon.LastWalkAt = now.Add(-time.Duration(w.monsterWalkSpeedMSLocked(mon)) * time.Millisecond)
	} else if now.Before(mon.LastWalkAt) {
		return false
	}
	if mon.WalkWaitLocked {
		if mon.WalkWait <= 0 || now.Sub(mon.WalkWaitTick) > time.Duration(mon.WalkWait)*time.Millisecond {
			mon.WalkWaitLocked = false
		} else {
			return false
		}
	}
	if now.Sub(mon.LastWalkAt) < time.Duration(w.monsterWalkSpeedMSLocked(mon))*time.Millisecond {
		return false
	}
	mon.LastWalkAt = now
	mon.WalkCount++
	if mon.WalkCount > mon.WalkStep {
		mon.WalkCount = 0
		mon.WalkWaitLocked = true
		mon.WalkWaitTick = now
	}
	return true
}

func (w *World) wanderMonsterLocked(mon *Monster, players map[string]storage.Character) (MonsterAction, bool) {
	if w.monsterTraceIntn("wander.roll", 20) != 0 {
		return MonsterAction{}, false
	}
	if w.monsterTraceIntn("wander.turn_roll", 4) == 1 {
		mon.Dir = w.monsterTraceIntn("wander.direction", len(dirOffsets))
		return w.monsterActionLocked(mon, MonsterActionTurn), true
	}
	mp, ok := w.data.Maps[mon.MapID]
	if !ok {
		return MonsterAction{}, false
	}
	off := dirOffsets[mon.Dir]
	x, y := mon.X+off[0], mon.Y+off[1]
	if !mp.Walkable(x, y) || w.movingObjectAtLocked(players, mon.MapID, x, y, mon.ID) {
		return MonsterAction{}, false
	}
	w.moveMonsterLocked(mon, x, y)
	w.applyMonsterGroundEventHitLocked(mon, players, w.monsterActionTimeLocked())
	return w.monsterActionLocked(mon, MonsterActionWalk), true
}
