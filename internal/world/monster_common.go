package world

import (
	"time"

	"openmir2/internal/storage"
)

func (w *World) monsterActionLocked(mon *Monster, kind MonsterActionKind) MonsterAction {
	now := w.actionNow
	if now.IsZero() {
		now = time.Now()
	}
	return MonsterAction{MonsterID: mon.ID, ActorID: MonsterActorID(*mon), Name: mon.Name, RaceImg: mon.RaceImg, MonsterWeapon: mon.MonsterWeapon, Appr: mon.Appr, Light: mon.Light, MapID: mon.MapID, X: mon.X, Y: mon.Y, Dir: mon.Dir, Status: MonsterStatus(*mon, now), Kind: kind}
}

func (w *World) monsterTraceIntn(label string, n int) int {
	value := w.rand.Intn(n)
	if w.monsterTraceCurrent != nil {
		w.monsterTraceCurrent.Random = append(w.monsterTraceCurrent.Random, MonsterRandomTrace{Label: label, Limit: n, Value: value})
	}
	return value
}

func monsterTraceState(mon *Monster) MonsterTraceState {
	state := MonsterTraceState{
		MapID:             mon.MapID,
		X:                 mon.X,
		Y:                 mon.Y,
		Dir:               mon.Dir,
		TargetCharacterID: mon.TargetCharacterID,
		TargetX:           mon.TargetX,
		TargetY:           mon.TargetY,
		RunAwayMode:       mon.RunAwayMode,
		Alive:             mon.Alive,
		Hidden:            mon.Hidden,
		FixedHideMode:     mon.FixedHideMode,
		AdminMode:         mon.AdminMode,
		WalkWaitLocked:    mon.WalkWaitLocked,
		WalkCount:         mon.WalkCount,
		DupMode:           mon.DupMode,
	}
	if !mon.LastWalkAt.IsZero() {
		state.LastWalkAtMS = mon.LastWalkAt.UnixMilli()
	}
	if !mon.WalkWaitTick.IsZero() {
		state.WalkWaitTickMS = mon.WalkWaitTick.UnixMilli()
	}
	if !mon.ThinkAt.IsZero() {
		state.ThinkAtMS = mon.ThinkAt.UnixMilli()
	}
	return state
}

func monsterTraceDecision(trace *MonsterTickTrace) string {
	if len(trace.Actions) == 0 {
		return "wait"
	}
	if trace.StateAfter.RunAwayMode {
		return "flee"
	}
	if trace.StateAfter.TargetCharacterID != "" {
		if trace.Actions[0].Kind == MonsterActionHit {
			return "attack"
		}
		return "chase"
	}
	switch trace.Actions[0].Kind {
	case MonsterActionTurn:
		return "wander.turn"
	case MonsterActionWalk:
		return "wander.walk"
	default:
		return "action"
	}
}

func (w *World) playerAtLocked(players map[string]storage.Character, mapID string, x, y int) bool {
	for _, ch := range players {
		if ch.MapID == mapID && !w.characterDeadLocked(ch) && !ch.ObserverMode && !ch.Ghost && ch.X == x && ch.Y == y {
			return true
		}
	}
	return false
}

func (w *World) movingObjectAtLocked(players map[string]storage.Character, mapID string, x, y int, exceptID string) bool {
	if !w.castleMapWalkableLocked(mapID, x, y) {
		return true
	}
	if w.monsterAtLocked(mapID, x, y, exceptID) || w.playerAtLocked(players, mapID, x, y) {
		return true
	}
	for _, entity := range w.npcEntitiesInMapOrdered(mapID, true) {
		if entity.MapID == mapID && entity.X == x && entity.Y == y {
			return true
		}
	}
	return false
}

func (w *World) monsterAtLocked(mapID string, x, y int, exceptID string) bool {
	return w.monsterAtLockedWithRun(mapID, x, y, exceptID, false)
}

func (w *World) monsterAtLockedWithRun(mapID string, x, y int, exceptID string, allowOverlap bool) bool {
	if allowOverlap {
		return false
	}
	cell := monsterPosition{MapID: mapID, X: x, Y: y}
	if members := w.monsterCells[cell]; len(members) > 0 {
		for id := range members {
			if id == exceptID {
				continue
			}
			mon := w.monsters[id]
			if mon != nil && mon.Alive && !mon.Ghost && !mon.FixedHideMode && !(mon.CastleDoor && mon.StoneMode) && !mon.CastleWall {
				return true
			}
		}
		return false
	}
	id, ok := w.occupied[cell]
	if !ok || id == exceptID {
		return false
	}
	mon := w.monsters[id]
	return mon != nil && mon.Alive && !mon.Ghost && !mon.FixedHideMode && !(mon.CastleDoor && mon.StoneMode) && !mon.CastleWall
}

func (w *World) monsterAtLockedWithRunFlags(mapID string, x, y int, exceptID string, allowMonsters, allowGuards bool) bool {
	cell := monsterPosition{MapID: mapID, X: x, Y: y}
	ids := w.monsterCells[cell]
	if len(ids) == 0 {
		if id, ok := w.occupied[cell]; ok {
			ids = map[string]struct{}{id: {}}
		}
	}
	for id := range ids {
		if id == exceptID {
			continue
		}
		mon := w.monsters[id]
		if mon == nil || !mon.Alive || mon.Ghost || mon.FixedHideMode || mon.CastleDoor && mon.StoneMode || mon.CastleWall {
			continue
		}
		if mon.Race == 81 || mon.Race == 112 || mon.GuardDirection > 0 {
			if !allowGuards {
				return true
			}
			continue
		}
		if !allowMonsters {
			return true
		}
	}
	return false
}

func (w *World) thinkMonsterLocked(mon *Monster, players map[string]storage.Character, now time.Time) bool {
	if !mon.ThinkAt.IsZero() && now.Sub(mon.ThinkAt) <= 3*time.Second {
		if !mon.DupMode {
			return false
		}
	} else {
		mon.ThinkAt = now
		w.clearInvalidMonsterTargetLocked(mon, players, now)
		count := 0
		if mon.Alive && !mon.Ghost && !mon.FixedHideMode && !(mon.CastleDoor && mon.StoneMode) && !mon.CastleWall {
			count = 1
		}
		w.ensureMonsterCellRegistryLocked()
		cell := monsterPosition{MapID: mon.MapID, X: mon.X, Y: mon.Y}
		for otherID := range w.monsterCells[cell] {
			other := w.monsters[otherID]
			if other != nil && other.ID != mon.ID && other.Alive && !other.Ghost && !other.FixedHideMode && !(other.CastleDoor && other.StoneMode) && !other.CastleWall {
				count++
			}
		}
		for _, player := range players {
			if !w.characterDeadLocked(player) && !player.ObserverMode && !player.Ghost && player.MapID == mon.MapID && player.X == mon.X && player.Y == mon.Y {
				count++
			}
		}
		if count >= 2 {
			mon.DupMode = true
		}
	}
	if !mon.DupMode {
		return false
	}
	if w.monsterHolySeizeActiveLocked(mon, now) {
		return false
	}
	dir := w.monsterTraceIntn("think.dup_direction", len(dirOffsets))
	mon.Dir = dir
	off := dirOffsets[dir]
	nx, ny := mon.X+off[0], mon.Y+off[1]
	mp, ok := w.data.Maps[mon.MapID]
	if !ok || !mp.Walkable(nx, ny) || !w.castleMapWalkableLocked(mon.MapID, nx, ny) || w.movingObjectAtLocked(players, mon.MapID, nx, ny, mon.ID) {
		return false
	}
	w.moveMonsterLocked(mon, nx, ny)
	w.applyMonsterGroundEventHitLocked(mon, players, now)
	mon.DupMode = false
	return true
}

func (w *World) monsterHolySeizeActiveLocked(mon *Monster, now time.Time) bool {
	return mon != nil && !mon.HolySeizeUntil.IsZero() && !now.After(mon.HolySeizeUntil)
}

func (w *World) monsterAtPointLocked(mapID string, x, y, radius int) *Monster {
	if radius < 0 {
		return nil
	}
	w.ensureMonsterCellRegistryLocked()
	var found *Monster
	bestDist := -1
	for xx := x - radius; xx <= x+radius; xx++ {
		for yy := y - radius; yy <= y+radius; yy++ {
			for id := range w.monsterCells[monsterPosition{MapID: mapID, X: xx, Y: yy}] {
				mon := w.monsters[id]
				if mon == nil || mon.ID == "" || !mon.Alive || mon.Ghost || mon.MapID != mapID {
					continue
				}
				dx := abs(mon.X - x)
				dy := abs(mon.Y - y)
				if dx > radius || dy > radius {
					continue
				}
				dist := dx + dy
				if found == nil || dist < bestDist || (dist == bestDist && monsterObjectOrderBefore(mon, found)) {
					found = mon
					bestDist = dist
				}
			}
		}
	}
	return found
}

func monsterObjectOrderBefore(candidate, current *Monster) bool {
	if candidate.ObjectOrder != 0 || current.ObjectOrder != 0 {
		if candidate.ObjectOrder == 0 {
			return false
		}
		if current.ObjectOrder == 0 {
			return true
		}
		if candidate.ObjectOrder != current.ObjectOrder {
			return candidate.ObjectOrder < current.ObjectOrder
		}
	}
	return candidate.ID < current.ID
}

func (w *World) occupyMonsterLocked(mon *Monster) {
	w.indexMonsterCellLocked(mon)
	if mon.Alive && !mon.Ghost {
		cell := monsterPosition{MapID: mon.MapID, X: mon.X, Y: mon.Y}
		if id, ok := w.occupied[cell]; !ok || w.monsters[id] == nil || !w.monsters[id].Alive || w.monsters[id].Ghost {
			w.occupied[cell] = mon.ID
		}
	}
}

func (w *World) vacateMonsterLocked(mon *Monster) {
	w.unindexMonsterCellLocked(mon)
	cell := monsterPosition{MapID: mon.MapID, X: mon.X, Y: mon.Y}
	if id, ok := w.occupied[cell]; !ok || id == mon.ID {
		delete(w.occupied, cell)
		for id := range w.monsterCells[cell] {
			candidate := w.monsters[id]
			if candidate != nil && candidate.Alive && !candidate.Ghost {
				w.occupied[cell] = id
				break
			}
		}
	}
}

func (w *World) indexMonsterCellLocked(mon *Monster) {
	if mon == nil || mon.ID == "" {
		return
	}
	if w.monsterCells == nil {
		w.monsterCells = map[monsterPosition]map[string]struct{}{}
	}
	if w.monsterCellByID == nil {
		w.monsterCellByID = map[string]monsterPosition{}
	}
	w.unindexMonsterCellLocked(mon)
	cell := monsterPosition{MapID: mon.MapID, X: mon.X, Y: mon.Y}
	members := w.monsterCells[cell]
	if members == nil {
		members = map[string]struct{}{}
		w.monsterCells[cell] = members
	}
	members[mon.ID] = struct{}{}
	w.monsterCellByID[mon.ID] = cell
}

func (w *World) unindexMonsterCellLocked(mon *Monster) {
	if mon == nil || w.monsterCellByID == nil {
		return
	}
	cell, ok := w.monsterCellByID[mon.ID]
	if !ok {
		return
	}
	delete(w.monsterCellByID, mon.ID)
	if members := w.monsterCells[cell]; members != nil {
		delete(members, mon.ID)
		if len(members) == 0 {
			delete(w.monsterCells, cell)
		}
	}
}

func (w *World) moveMonsterLocked(mon *Monster, x, y int) {
	w.vacateMonsterLocked(mon)
	mon.X = x
	mon.Y = y
	if w.nextObjectOrder > 0 {
		mon.ObjectOrder = w.nextObjectOrder
		w.nextObjectOrder++
	}
	w.occupyMonsterLocked(mon)
}

func (w *World) removeMonsterLocked(mon *Monster, adjustSpawn bool) {
	if mon == nil || !mon.Alive {
		return
	}
	w.vacateMonsterLocked(mon)
	mon.Alive = false
	mon.DeathAt = w.monsterActionTimeLocked()
	mon.GhostAt = time.Time{}
	mon.Ghost = false
	mon.TargetCharacterID = ""
	mon.TargetMonsterID = ""
	mon.PendingDeath = false
	mon.DeathHitterID = ""
	mon.LastHitterID = ""
	mon.LastHitterAt = time.Time{}
	mon.ExpHitterID = ""
	mon.ExpHitterAt = time.Time{}
	mon.TargetFocusAt = time.Time{}
	mon.NextSearchAt = time.Time{}
	mon.LastAttackAt = time.Time{}
	mon.LastWalkAt = time.Time{}
	mon.IncHealth = 0
	mon.IncSpell = 0
	mon.IncHealing = 0
	mon.RunAwayMode = false
	mon.RunAwayUntil = time.Time{}
	mon.RespawnAt = time.Time{}
	if mon.CastleDoor {
		w.castleDoorSetMapFlagsLocked(mon, 2)
		return
	}
	if mon.CastleWall {
		w.castleWallSetMapFlagLocked(mon)
		return
	}
	mon.MasterID = ""
	mon.MasterName = ""
	mon.MasterExpiresAt = time.Time{}
	mon.MasterTick = time.Time{}
	mon.MasterDeadSince = time.Time{}
	mon.MasterGhostSince = time.Time{}
	mon.SlaveMakeLevel = 0
	if adjustSpawn {
		state := w.spawnStateForLocked(mon.Spawn)
		if state.activeCount > 0 {
			state.activeCount--
		}
	}
}

func (w *World) killAnimalLocked(mon *Monster) {
	if mon == nil || !mon.Alive {
		return
	}
	w.vacateMonsterLocked(mon)
	mon.Alive = false
	mon.DeathAt = w.monsterActionTimeLocked()
	mon.GhostAt = time.Time{}
	mon.Ghost = false
	state := w.spawnStateForLocked(mon.Spawn)
	if state.activeCount > 0 {
		state.activeCount--
	}
	mon.PendingDeath = false
	mon.DeathHitterID = ""
	mon.MasterDeadSince = time.Time{}
	mon.MasterGhostSince = time.Time{}
	mon.TargetCharacterID = ""
	mon.TargetMonsterID = ""
	mon.TargetFocusAt = time.Time{}
	mon.IncHealth = 0
	mon.IncSpell = 0
	mon.IncHealing = 0
	mon.NextSearchAt = time.Time{}
	mon.RunAwayMode = false
	mon.RunAwayUntil = time.Time{}
}

func (w *World) scheduleMonsterRespawnLocked(mon *Monster, now time.Time) {
	if mon == nil || mon.CastleDoor || mon.CastleWall {
		return
	}
	if mon.Race == 96 {
		if mon.ZilkinKillCount > 0 {
			mon.ZilkinRebirth = true
			mon.RespawnAt = now.Add(time.Duration(4+w.rand.Intn(20)) * time.Second)
		}
		mon.ZilkinKillCount--
		return
	}
}

func (w *World) monsterActionTimeLocked() time.Time {
	if !w.actionNow.IsZero() {
		return w.actionNow
	}
	return time.Now()
}

func (w *World) setMonsterLastHitterLocked(mon *Monster, attackerID string) {
	w.setMonsterLastHitterAtLocked(mon, attackerID, time.Now())
}

func (w *World) setMonsterLastHitterAtLocked(mon *Monster, attackerID string, now time.Time) {
	if mon == nil || attackerID == "" {
		return
	}
	mon.LastHitterID = attackerID
	mon.LastHitterAt = now
	if mon.ExpHitterID == "" {
		mon.ExpHitterID = attackerID
		mon.ExpHitterAt = mon.LastHitterAt
	} else if mon.ExpHitterID == attackerID {
		mon.ExpHitterAt = mon.LastHitterAt
	}
}

func (w *World) setCharacterLastHitterAtLocked(ch *storage.Character, attackerID string, now time.Time) {
	if ch == nil || attackerID == "" {
		return
	}
	ch.LastHitterID = attackerID
	ch.LastHitterAt = now.UnixNano()
	if ch.ExpHitterID == "" {
		ch.ExpHitterID = attackerID
		ch.ExpHitterAt = ch.LastHitterAt
	} else if ch.ExpHitterID == attackerID {
		ch.ExpHitterAt = ch.LastHitterAt
	}
}

func (w *World) monsterAttackIntervalMSLocked(mon *Monster) int {
	if mon.AttackIntervalMS > 0 {
		return mon.AttackIntervalMS
	}
	return mon.AttackIntervalMS
}

func (w *World) monsterWalkSpeedMSLocked(mon *Monster) int {
	if mon.WalkSpeedMS > 0 {
		return mon.WalkSpeedMS
	}
	return mon.WalkSpeedMS
}

func (w *World) monsterStruckLocked(mon *Monster, now time.Time) {
	if mon == nil {
		return
	}
	hitDelay := 150 - minInt(130, mon.Level*4)
	if hitDelay < 0 {
		hitDelay = 0
	}
	if mon.LastAttackAt.IsZero() {
		mon.LastAttackAt = now.Add(time.Duration(hitDelay) * time.Millisecond)
	} else {
		mon.LastAttackAt = mon.LastAttackAt.Add(time.Duration(hitDelay) * time.Millisecond)
	}
}

func (w *World) monsterMagicDamageRecoveryResetLocked(mon *Monster) {
	if mon == nil {
		return
	}
	mon.HealthTick = 0
	mon.SpellTick = 0
	mon.PerHealth--
	mon.PerSpell--
}

func (w *World) decayMonsterMeatQualityLocked(mon *Monster, magicDamage int) {
	if mon == nil || !mon.Animal {
		return
	}
	decrement := magicDamage * 1000
	if magicDamage == 0 {
		if w.rand == nil {
			return
		}
		decrement = w.rand.Intn(300)
	}
	if mon.MeatQuality <= 0 {
		return
	}
	mon.MeatQuality -= decrement
	if mon.MeatQuality < 0 {
		mon.MeatQuality = 0
	}
}

func (w *World) monsterStruckByCharacterLocked(mon *Monster, caster storage.Character, players []storage.Character, now time.Time) storage.Character {
	return w.monsterStruckByCharacterModeLocked(mon, caster, players, now, true, true)
}

func (w *World) monsterStruckByCharacterModeLocked(mon *Monster, caster storage.Character, players []storage.Character, now time.Time, breakHolySeize, applyPKFlag bool) storage.Character {
	if mon == nil {
		return caster
	}
	if mon.CastleID != "" && caster.ID != "" {
		w.characterCastleTauntAt[caster.ID] = now
	}
	w.setMonsterLastHitterAtLocked(mon, caster.ID, now)
	if _, ok := w.characterByIDLocked(players, caster.ID); !ok {
		players = append(players, caster)
	}
	if mon.MasterID != "" {
		if _, ok := w.characterByIDLocked(players, mon.MasterID); !ok && w.store != nil {
			if master, found := w.store.Character(mon.MasterID); found {
				players = append(players, master)
			}
		}
	}
	targetAdjacent := false
	if mon.TargetCharacterID != "" {
		if target, ok := w.characterByIDLocked(players, mon.TargetCharacterID); ok {
			targetAdjacent = abs(target.X-mon.X) <= 1 && abs(target.Y-mon.Y) <= 1 && (target.X != mon.X || target.Y != mon.Y)
		}
	} else if mon.TargetMonsterID != "" {
		if target := w.monsters[mon.TargetMonsterID]; target != nil {
			targetAdjacent = abs(target.X-mon.X) <= 1 && abs(target.Y-mon.Y) <= 1 && (target.X != mon.X || target.Y != mon.Y)
		}
	}
	if (mon.TargetCharacterID == "" && mon.TargetMonsterID == "") || targetAdjacent || w.rand.Intn(6) == 0 {
		if w.monsterCanRetargetCharacterLocked(mon, caster, players) {
			mon.TargetCharacterID = caster.ID
			mon.TargetMonsterID = ""
			mon.TargetFocusAt = now
		}
	}
	w.decayMonsterMeatQualityLocked(mon, 0)
	w.monsterStruckLocked(mon, now)
	if breakHolySeize {
		mon.HolySeizeUntil = time.Time{}
	}
	if applyPKFlag && mon.MasterID != "" && mon.MasterID != caster.ID {
		master, masterFound := w.characterByIDLocked(players, mon.MasterID)
		mapData, mapFound := w.data.Maps[master.MapID]
		inFightZone := mapFound && (mapData.FightZone || mapData.Fight3Zone)
		if masterFound && characterPKLevel(master) < 2 && characterPKLevel(caster) < 2 && !inFightZone && w.monsters[caster.ID] == nil && w.canSetCharacterPKFlagLocked(master, caster) {
			caster.PKFlag = true
			caster.PKFlagUntil = now.Add(60 * time.Second).UnixNano()
		}
	}
	return caster
}

func (w *World) monsterCanRetargetCharacterLocked(mon *Monster, caster storage.Character, players []storage.Character) bool {
	if mon == nil || !w.monsterCanTargetCharacterLocked(mon, caster) {
		return false
	}
	if mon.MasterID == "" {
		return true
	}
	master, ok := w.characterByIDLocked(players, mon.MasterID)
	if !ok || caster.ID == master.ID || master.SlaveRelax || w.isSafeZoneLocked(caster) {
		return false
	}
	return master.LastHitterID == caster.ID || master.TargetID == caster.ID || caster.TargetID == master.ID
}

func (w *World) monsterStruckByMonsterLocked(target, attacker *Monster, now time.Time) {
	if target == nil || attacker == nil {
		return
	}
	if target.CastleID != "" {
		attacker.CastleTauntAt = now
	}
	w.setMonsterLastHitterAtLocked(target, attacker.ID, now)
	targetAdjacent := false
	if target.TargetCharacterID != "" && w.store != nil {
		if character, found := w.store.Character(target.TargetCharacterID); found {
			targetAdjacent = abs(character.X-target.X) <= 1 && abs(character.Y-target.Y) <= 1 && (character.X != target.X || character.Y != target.Y)
		}
	} else if target.TargetMonsterID != "" {
		if current := w.monsters[target.TargetMonsterID]; current != nil {
			targetAdjacent = abs(current.X-target.X) <= 1 && abs(current.Y-target.Y) <= 1 && (current.X != target.X || current.Y != target.Y)
		}
	}
	if (target.TargetCharacterID == "" && target.TargetMonsterID == "") || targetAdjacent || w.rand.Intn(6) == 0 {
		if w.monsterCanAttackMonsterTargetLocked(target, attacker) {
			target.TargetCharacterID = ""
			target.TargetMonsterID = attacker.ID
			target.TargetFocusAt = now
		}
	}
	w.decayMonsterMeatQualityLocked(target, 0)
	w.monsterStruckLocked(target, now)
	target.HolySeizeUntil = time.Time{}
}

func (w *World) monsterMagicStruckLocked(mon *Monster, now time.Time) {
	if mon == nil || mon.Race < 50 || mon.AdminMode || mon.Level >= 50 {
		return
	}
	walkDelay := 800 + w.rand.Intn(1000)
	if mon.LastWalkAt.IsZero() {
		mon.LastWalkAt = now
	}
	mon.LastWalkAt = mon.LastWalkAt.Add(time.Duration(walkDelay) * time.Millisecond)
}
