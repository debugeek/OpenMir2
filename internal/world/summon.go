package world

import (
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func (w *World) gainMonsterSlaveExpLocked(mon *Monster, level int) bool {
	if mon == nil || mon.MasterID == "" || level <= 0 {
		return false
	}
	settings := w.gameplay.Monster
	killCounts := settings.SlaveExpKillCounts
	thresholdBonus := 0
	if int(mon.SlaveExpLevel) < len(killCounts) {
		thresholdBonus = killCounts[mon.SlaveExpLevel]
	}
	threshold := mon.Level*settings.SlaveExpRate - mon.Level + settings.SlaveExpBase + thresholdBonus
	if threshold <= 0 {
		threshold = 1
	}
	mon.SlaveExpProgress += level
	if mon.SlaveExpProgress < threshold {
		return false
	}
	mon.SlaveExpProgress -= threshold
	if mon.SlaveExpLevel >= mon.SlaveMakeLevel*2+1 {
		return false
	}
	mon.SlaveExpLevel++
	w.applySummonedMonsterExpLevelLocked(mon, mon.SlaveExpLevel)
	return true
}

func (w *World) monsterIsSummonedLocked(mon *Monster) bool {
	return mon != nil && mon.MasterID != ""
}

func (w *World) summonedMonsterOwnerLocked(mon *Monster, players map[string]storage.Character, now time.Time) (storage.Character, bool, bool) {
	if mon == nil || mon.MasterID == "" {
		return storage.Character{}, false, false
	}
	master, ok := players[mon.MasterID]
	if ok {
		if master.Ghost {
			return master, true, true
		}
		if !mon.MasterExpiresAt.IsZero() && now.After(mon.MasterExpiresAt) {
			return storage.Character{}, false, false
		}
		return master, true, false
	}
	ghost, ok := w.disconnectedCharacters[mon.MasterID]
	if !ok || now.After(ghost.Until) {
		return storage.Character{}, false, false
	}
	return ghost.Character, true, true
}

func (w *World) summonedMonsterOwnerForTickLocked(mon *Monster, players map[string]storage.Character, now time.Time) (storage.Character, bool, bool) {
	if mon == nil || mon.MasterID == "" {
		return storage.Character{}, false, false
	}
	if master, ok := players[mon.MasterID]; ok {
		if master.Ghost {
			return master, true, true
		}
		return master, true, false
	}
	return w.summonedMonsterOwnerLocked(mon, players, now)
}

func (w *World) findClosestMonsterTargetExceptLocked(mon *Monster, players map[string]storage.Character, viewRange int, excludeID string, now time.Time) (storage.Character, bool) {
	var target storage.Character
	best := 999999
	for _, ch := range players {
		if ch.ID == excludeID || !w.monsterCanTargetSummonCharacterLocked(mon, ch) {
			continue
		}
		if abs(ch.X-mon.X) > viewRange || abs(ch.Y-mon.Y) > viewRange {
			continue
		}
		dist := abs(ch.X-mon.X) + abs(ch.Y-mon.Y)
		if dist < best || dist == best && monsterTargetOrderPreferred(ch, target) {
			best = dist
			target = ch
		}
	}
	if target.ID == "" {
		return storage.Character{}, false
	}
	return target, true
}

func (w *World) findClosestMonsterTargetAgainstMasterFriendsLocked(mon *Monster, players map[string]storage.Character, viewRange int, master storage.Character, now time.Time) (storage.Character, bool) {
	var target storage.Character
	best := 999999
	for _, ch := range players {
		if ch.ID == master.ID || !w.monsterCanTargetSummonCharacterLocked(mon, ch) {
			continue
		}
		if w.isProperFriendLocked(master, ch) {
			continue
		}
		if abs(ch.X-mon.X) > viewRange || abs(ch.Y-mon.Y) > viewRange {
			continue
		}
		dist := abs(ch.X-mon.X) + abs(ch.Y-mon.Y)
		if dist < best || dist == best && monsterTargetOrderPreferred(ch, target) {
			best = dist
			target = ch
		}
	}
	if target.ID == "" {
		return storage.Character{}, false
	}
	return target, true
}

func (w *World) monsterCanTargetSummonCharacterLocked(mon *Monster, ch storage.Character) bool {
	if !w.monsterCanTargetCharacterLocked(mon, ch) {
		return false
	}
	mapData, ok := w.data.Maps[ch.MapID]
	return ok && !mapData.Safe
}

func (w *World) monsterCanKeepSummonTargetLocked(mon *Monster, target, master storage.Character) bool {
	return !master.SlaveRelax && w.monsterCanKeepCharacterTargetLocked(mon, target) && w.monsterCanTargetSummonCharacterLocked(mon, target) && !w.isProperFriendLocked(master, target)
}

func (w *World) tickSummonedMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	return w.tickSummonedMonsterCoreLocked(mon, players, now, true)
}

func (w *World) tickSummonedMonsterBehaviorLocked(mon *Monster, players map[string]storage.Character, now time.Time) ([]MonsterAction, []CharacterHit, []storage.Character, error) {
	return w.tickSummonedMonsterCoreLocked(mon, players, now, false)
}

func (w *World) tickSummonedMonsterCoreLocked(mon *Monster, players map[string]storage.Character, now time.Time, finalizeOwner bool) (actions []MonsterAction, hits []CharacterHit, chars []storage.Character, err error) {
	master, ok, ghost := w.summonedMonsterOwnerForTickLocked(mon, players, now)
	if !ok {
		w.removeMonsterLocked(mon, false)
		return nil, nil, nil, nil
	}
	if finalizeOwner {
		defer func() {
			w.finalizeSummonedOwnerStateLocked(mon, master, ghost, now)
		}()
	}
	if !ghost {
		mon.MasterGhostSince = time.Time{}
	}
	if mon.TargetCharacterID == master.ID {
		mon.TargetCharacterID = ""
		mon.TargetFocusAt = time.Time{}
	}
	if mon.TargetCharacterID != "" {
		target, ok := players[mon.TargetCharacterID]
		if !ok || (!master.SlaveRelax && !w.monsterCanKeepSummonTargetLocked(mon, target, master)) || (master.SlaveRelax && !w.monsterCanTargetSummonCharacterLocked(mon, target)) {
			mon.TargetCharacterID = ""
			mon.TargetFocusAt = time.Time{}
			mon.TargetX, mon.TargetY = -1, -1
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		} else if target.X != mon.X || target.Y != mon.Y {
			if abs(target.X-mon.X) <= 1 && abs(target.Y-mon.Y) <= 1 {
				return w.tickMonsterTargetLocked(mon, players, now)
			}
			mon.TargetX, mon.TargetY = target.X, target.Y
		} else {
			mon.TargetX, mon.TargetY = target.X, target.Y
		}
	}
	if mon.TargetCharacterID != "" {
		if target, ok := players[mon.TargetCharacterID]; ok && target.X == mon.X && target.Y == mon.Y {
			mon.TargetX, mon.TargetY = target.X, target.Y
		}
	}
	if mon.TargetMonsterID != "" {
		target := w.monsters[mon.TargetMonsterID]
		if !monsterTargetAliveLocked(target) || target.ID == "" || target.ID == mon.ID || target.MapID != mon.MapID {
			_, _, _, err := w.tickMonsterTargetLocked(mon, players, now)
			if err != nil {
				return nil, nil, nil, err
			}
		} else if target.X != mon.X || target.Y != mon.Y {
			if abs(target.X-mon.X) <= 1 && abs(target.Y-mon.Y) <= 1 {
				return w.tickMonsterTargetLocked(mon, players, now)
			}
			mon.TargetX, mon.TargetY = target.X, target.Y
		} else {
			mon.TargetX, mon.TargetY = target.X, target.Y
		}
	}
	if master.SlaveRelax {
		return nil, nil, nil, nil
	}
	spaceMapID := master.MapID
	spaceX, spaceY := w.summonedMonsterBackPositionLocked(master)
	hasTarget := mon.TargetCharacterID != "" || mon.TargetMonsterID != ""
	if !hasTarget && (abs(mon.X-master.X) > 20 || abs(mon.Y-master.Y) > 20 || master.MapID != mon.MapID) {
		mon.TargetX, mon.TargetY = spaceX, spaceY
	}
	if mon.TargetCharacterID != "" {
		spaceX, spaceY = mon.TargetX, mon.TargetY
	} else if mon.TargetMonsterID != "" {
		if target := w.monsters[mon.TargetMonsterID]; target != nil {
			spaceX, spaceY = target.X, target.Y
		}
	}
	if abs(mon.X-master.X) > 20 || abs(mon.Y-master.Y) > 20 {
		previousMapID := mon.MapID
		previousX, previousY := mon.X, mon.Y
		moved := false
		if hasTarget {
			moved = w.spaceMoveSummonedMonsterLocked(mon, spaceMapID, spaceX, spaceY)
		} else {
			moved = w.recallSummonedMonsterNearCharacterLocked(mon, master, false)
		}
		if !moved {
			return nil, nil, nil, nil
		}
		action := w.monsterActionLocked(mon, MonsterActionSpaceMove)
		action.PreviousMapID = previousMapID
		action.PreviousX = previousX
		action.PreviousY = previousY
		return []MonsterAction{action}, nil, nil, nil
	}
	if master.MapID != mon.MapID {
		previousMapID, previousX, previousY := mon.MapID, mon.X, mon.Y
		moved := false
		if hasTarget {
			moved = w.spaceMoveSummonedMonsterLocked(mon, spaceMapID, spaceX, spaceY)
		} else {
			moved = w.recallSummonedMonsterNearCharacterLocked(mon, master, false)
		}
		if !moved {
			return nil, nil, nil, nil
		}
		action := w.monsterActionLocked(mon, MonsterActionSpaceMove)
		action.PreviousMapID = previousMapID
		action.PreviousX = previousX
		action.PreviousY = previousY
		return []MonsterAction{action}, nil, nil, nil
	}
	if mon.TargetCharacterID == "" && !now.Before(mon.NextSearchAt) {
		for _, preferredID := range []string{master.TargetID, master.LastHitterID} {
			target, found := players[preferredID]
			if !found || !w.monsterCanTargetSummonCharacterLocked(mon, target) || w.isProperFriendLocked(master, target) {
				continue
			}
			if abs(target.X-mon.X) > mon.ViewRange || abs(target.Y-mon.Y) > mon.ViewRange {
				continue
			}
			mon.TargetCharacterID = target.ID
			mon.TargetFocusAt = now
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
			break
		}
	}
	if mon.TargetCharacterID == "" && !now.Before(mon.NextSearchAt) {
		if target, ok := w.findClosestMonsterTargetAgainstMasterFriendsLocked(mon, players, mon.ViewRange, master, now); ok {
			mon.TargetCharacterID = target.ID
			mon.TargetFocusAt = now
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchHasTargetMSLocked(mon)) * time.Millisecond)
		} else {
			mon.NextSearchAt = now.Add(time.Duration(w.monsterSearchNoTargetMSLocked(mon)) * time.Millisecond)
		}
	}
	if !hasTarget {
		backX, backY := w.summonedMonsterBackPositionLocked(master)
		if abs(mon.TargetX-backX) > 1 || abs(mon.TargetY-backY) > 1 {
			mon.TargetX, mon.TargetY = backX, backY
			if abs(mon.X-backX) <= 2 && abs(mon.Y-backY) <= 2 && w.movingObjectAtLocked(players, mon.MapID, backX, backY, mon.ID) {
				mon.TargetX, mon.TargetY = mon.X, mon.Y
			}
		}
	}
	if mon.X == mon.TargetX && mon.Y == mon.TargetY {
		return nil, nil, nil, nil
	}
	if w.moveMonsterTowardPointLocked(mon, mon.TargetX, mon.TargetY, players) {
		mon.LastWalkAt = now
		return []MonsterAction{w.monsterActionLocked(mon, MonsterActionWalk)}, nil, nil, nil
	}
	return nil, nil, nil, nil
}

func (w *World) finalizeSummonedOwnerStateLocked(mon *Monster, master storage.Character, ghost bool, now time.Time) bool {
	return w.finalizeSummonedOwnerStateWithMasterTickLocked(mon, master, ghost, now, true)
}

func (w *World) finalizeSummonedOwnerStateWithMasterTickLocked(mon *Monster, master storage.Character, ghost bool, now time.Time, checkMasterTick bool) bool {
	if ghost {
		if mon.MasterGhostSince.IsZero() {
			if disconnected, ok := w.disconnectedCharacters[mon.MasterID]; ok && !disconnected.Since.IsZero() {
				mon.MasterGhostSince = disconnected.Since
			} else {
				mon.MasterGhostSince = now
			}
		}
		if now.Sub(mon.MasterGhostSince) <= time.Second {
			return false
		}
		w.vacateMonsterLocked(mon)
		mon.Ghost = true
		mon.GhostAt = now
		mon.MasterGhostSince = time.Time{}
		mon.TargetCharacterID = ""
		mon.TargetFocusAt = time.Time{}
		return false
	}
	mon.MasterGhostSince = time.Time{}
	if !w.characterDeadLocked(master) {
		mon.MasterDeadSince = time.Time{}
		if checkMasterTick && !mon.MasterTick.IsZero() && now.Sub(mon.MasterTick) > 12*time.Hour {
			mon.HP = 0
			mon.PendingDeath = true
			mon.DeathHitterID = ""
			return true
		}
		return false
	}
	if mon.MasterDeadSince.IsZero() {
		if deathAt, ok := w.characterDeathAt[master.ID]; ok && !deathAt.IsZero() {
			mon.MasterDeadSince = deathAt
		} else {
			mon.MasterDeadSince = now
		}
	}
	if now.Sub(mon.MasterDeadSince) <= time.Second {
		return false
	}
	mutiny := w.gameplay.Monster.MasterDieMutiny && master.LastHitterID != "" && w.gameplay.Monster.MasterDieMutinyRate > 0 && w.rand.Intn(w.gameplay.Monster.MasterDieMutinyRate) == 0
	if mutiny {
		if mon.BaseMaxHP <= 0 {
			mon.BaseMaxHP = mon.MaxHP
		}
		if mon.BaseMaxAttack <= 0 {
			mon.BaseMaxAttack = mon.MaxAttack
		}
		mon.MasterID = ""
		mon.MasterName = ""
		mon.MasterExpiresAt = time.Time{}
		mon.MasterDeadSince = time.Time{}
		mon.SlaveExpLevel = 8
		if mon.SlaveExpLevel < byte(len(w.gameplay.Monster.SlaveColors)) {
			mon.SlaveNameColor = w.gameplay.Monster.SlaveColors[mon.SlaveExpLevel]
			mon.SlaveColorSet = true
		}
		if mon.Race == 100 || mon.Race == 113 || mon.Race == 114 {
			bonus := referenceRound((float64(mon.SlaveExpLevel)*0.1 + 0.3) * 3 * float64(mon.SlaveExpLevel))
			mon.MaxHP = mon.BaseMaxHP + referenceRound((float64(mon.SlaveExpLevel)*0.1+0.3)*float64(mon.BaseMaxHP))*int(mon.SlaveExpLevel)
			mon.MaxAttack = mon.BaseMaxAttack + bonus
		} else {
			maxHP := mon.BaseMaxHP + referenceRound(float64(mon.BaseMaxHP)*0.15)*int(mon.SlaveExpLevel)
			limit := mon.BaseMaxHP + int(mon.SlaveExpLevel)*60
			if maxHP > limit {
				maxHP = limit
			}
			mon.MaxHP = maxHP
			mon.MaxAttack = mon.BaseMaxAttack + int(mon.SlaveExpLevel)*2
		}
		if w.gameplay.Monster.MasterDieMutinyPower > 0 {
			mon.MinAttack *= w.gameplay.Monster.MasterDieMutinyPower
			mon.MaxAttack *= w.gameplay.Monster.MasterDieMutinyPower
		}
		if w.gameplay.Monster.MasterDieMutinySpeed > 0 {
			mon.WalkSpeedMS /= w.gameplay.Monster.MasterDieMutinySpeed
		}
		return false
	}
	mon.HP = 0
	mon.PendingDeath = true
	mon.DeathHitterID = ""
	return false
}

func (w *World) finalizeSummonedOwnerStateAfterRunLocked(mon *Monster, players map[string]storage.Character, now time.Time) bool {
	if mon == nil || mon.MasterID == "" {
		return false
	}
	master, ok, ghost := w.summonedMonsterOwnerForTickLocked(mon, players, now)
	if ok {
		return w.finalizeSummonedOwnerStateLocked(mon, master, ghost, now)
	}
	return false
}

func (w *World) finalizeSummonedOwnerStateBeforeInheritedLocked(mon *Monster, players map[string]storage.Character, now time.Time) {
	if mon == nil || mon.MasterID == "" {
		return
	}
	master, ok, ghost := w.summonedMonsterOwnerForTickLocked(mon, players, now)
	if ok {
		w.finalizeSummonedOwnerStateWithMasterTickLocked(mon, master, ghost, now, false)
	}
}

func expireSummonedMonsterMasterTickLocked(mon *Monster, now time.Time) bool {
	if mon == nil || mon.MasterTick.IsZero() || now.Sub(mon.MasterTick) <= 12*time.Hour {
		return false
	}
	mon.HP = 0
	mon.PendingDeath = true
	mon.DeathHitterID = ""
	return true
}

func (w *World) spaceMoveSummonedMonsterLocked(mon *Monster, mapID string, x, y int) bool {
	if mon == nil || !mon.Alive {
		return false
	}
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return false
	}
	x, y, ok = w.referenceSpaceMovePositionLocked(mp, x, y)
	if !ok {
		return false
	}
	w.vacateMonsterLocked(mon)
	mon.MapID = mapID
	mon.X = x
	mon.Y = y
	w.occupyMonsterLocked(mon)
	return true
}

func (w *World) referenceSpaceMovePositionLocked(mp data.StdMap, x, y int) (int, int, bool) {
	stepX := 10
	if mp.Width < 80 {
		stepX = 3
	}
	stepY := 50
	if mp.Height < 150 {
		stepY = 15
		if mp.Height < 50 {
			stepY = 2
		}
	}
	for attempt := 0; attempt < 201; attempt++ {
		if mp.Walkable(x, y) {
			return x, y, true
		}
		if x < mp.Width-stepY-1 {
			x += stepX
			continue
		}
		if mp.Width <= 0 || mp.Height <= 0 {
			return 0, 0, false
		}
		x = w.rand.Intn(mp.Width)
		if y < mp.Height-stepY-1 {
			y += stepY
		} else {
			y = w.rand.Intn(mp.Height)
		}
	}
	return 0, 0, false
}

func (w *World) summonedMonsterBackPositionLocked(master storage.Character) (int, int) {
	x, y := master.X, master.Y
	off := dirOffsets[0]
	if master.Dir >= 0 && master.Dir < len(dirOffsets) {
		off = dirOffsets[master.Dir]
	}
	if mp, ok := w.data.Maps[master.MapID]; ok {
		backX, backY := master.X-off[0], master.Y-off[1]
		if backX >= 0 && backX < mp.Width {
			x = backX
		}
		if backY >= 0 && backY < mp.Height {
			y = backY
		}
	}
	return x, y
}

func (w *World) recallSummonedMonsterNearCharacterLocked(mon *Monster, master storage.Character, clearTarget bool) bool {
	if mon == nil || !mon.Alive || mon.MasterID != master.ID {
		return false
	}
	dir := master.Dir
	if dir < 0 || dir >= len(dirOffsets) {
		dir = 0
	}
	off := dirOffsets[dir]
	x, y := master.X-off[0], master.Y-off[1]
	return w.recallSummonedMonsterAtLocked(mon, master, x, y, clearTarget)
}

func (w *World) recallSummonedMonsterInFrontLocked(mon *Monster, master storage.Character, clearTarget bool) bool {
	if mon == nil || !mon.Alive || mon.MasterID != master.ID {
		return false
	}
	dir := master.Dir
	if dir < 0 || dir >= len(dirOffsets) {
		dir = 0
	}
	off := dirOffsets[dir]
	return w.recallSummonedMonsterAtLocked(mon, master, master.X+off[0], master.Y+off[1], clearTarget)
}

func (w *World) recallSummonedMonsterAtLocked(mon *Monster, master storage.Character, x, y int, clearTarget bool) bool {
	mp, ok := w.data.Maps[master.MapID]
	if !ok || !mp.Walkable(x, y) {
		return false
	}
	dir := master.Dir
	if dir < 0 || dir >= len(dirOffsets) {
		dir = 0
	}
	w.vacateMonsterLocked(mon)
	mon.MapID = master.MapID
	mon.X = x
	mon.Y = y
	w.occupyMonsterLocked(mon)
	mon.Dir = dir
	if clearTarget {
		mon.TargetCharacterID = ""
		mon.TargetX, mon.TargetY = -1, -1
		mon.TargetFocusAt = time.Time{}
		mon.NextSearchAt = time.Time{}
		mon.RunAwayMode = false
		mon.RunAwayUntil = time.Time{}
	}
	return true
}
