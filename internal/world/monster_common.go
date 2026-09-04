package world

import (
	"time"

	"openmir2/internal/storage"
)

func (w *World) monsterActionLocked(mon *Monster, kind MonsterActionKind) MonsterAction {
	return MonsterAction{MonsterID: mon.ID, Name: mon.Name, RaceImg: mon.RaceImg, MonsterWeapon: mon.MonsterWeapon, Appr: mon.Appr, MapID: mon.MapID, X: mon.X, Y: mon.Y, Dir: mon.Dir, Status: MonsterStatus(*mon, time.Now()), Kind: kind}
}

func (w *World) playerAtLocked(players map[string]storage.Character, mapID string, x, y int) bool {
	for _, ch := range players {
		if ch.MapID == mapID && ch.HP > 0 && ch.X == x && ch.Y == y {
			return true
		}
	}
	return false
}

func (w *World) monsterAtLocked(mapID string, x, y int, exceptID string) bool {
	id, ok := w.occupied[monsterPosition{MapID: mapID, X: x, Y: y}]
	return ok && id != exceptID
}

func (w *World) monsterAtPointLocked(mapID string, x, y, radius int) *Monster {
	var found *Monster
	bestDist := -1
	for _, mon := range w.monsters {
		if mon == nil || !mon.Alive || mon.MapID != mapID {
			continue
		}
		dx := abs(mon.X - x)
		dy := abs(mon.Y - y)
		if dx > radius || dy > radius {
			continue
		}
		dist := dx + dy
		if found == nil || dist < bestDist || (dist == bestDist && mon.ID < found.ID) {
			found = mon
			bestDist = dist
		}
	}
	return found
}

func (w *World) occupyMonsterLocked(mon *Monster) {
	if mon.Alive {
		w.occupied[monsterPosition{MapID: mon.MapID, X: mon.X, Y: mon.Y}] = mon.ID
	}
}

func (w *World) vacateMonsterLocked(mon *Monster) {
	delete(w.occupied, monsterPosition{MapID: mon.MapID, X: mon.X, Y: mon.Y})
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
	mon.TargetCharacterID = ""
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
	mon.RunAwayMode = false
	mon.RunAwayUntil = time.Time{}
	mon.RespawnAt = time.Time{}
	mon.MasterID = ""
	mon.MasterName = ""
	mon.MasterExpiresAt = time.Time{}
	mon.MasterTick = time.Time{}
	mon.SlaveMakeLevel = 0
	if adjustSpawn {
		state := w.spawnStateForLocked(mon.Spawn)
		if state.activeCount > 0 {
			state.activeCount--
		}
	}
}

func (w *World) setMonsterLastHitterLocked(mon *Monster, attackerID string) {
	if mon == nil || attackerID == "" {
		return
	}
	mon.LastHitterID = attackerID
	mon.LastHitterAt = time.Now()
	if mon.ExpHitterID == "" {
		mon.ExpHitterID = attackerID
		mon.ExpHitterAt = mon.LastHitterAt
	} else if mon.ExpHitterID == attackerID {
		mon.ExpHitterAt = mon.LastHitterAt
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
	if mon.Animal {
		w.rand.Intn(300)
	}
	hitDelay := 150 - minInt(130, mon.Level*4)
	if hitDelay < 0 {
		hitDelay = 0
	}
	if mon.LastAttackAt.IsZero() || mon.LastAttackAt.Before(now) {
		mon.LastAttackAt = now.Add(time.Duration(hitDelay) * time.Millisecond)
	} else {
		mon.LastAttackAt = mon.LastAttackAt.Add(time.Duration(hitDelay) * time.Millisecond)
	}
}

func (w *World) monsterStruckByCharacterLocked(mon *Monster, caster storage.Character, players []storage.Character, now time.Time) {
	if mon == nil {
		return
	}
	if mon.TargetCharacterID == "" {
		if w.isProperMonsterTargetLocked(caster, players, mon) {
			mon.TargetCharacterID = caster.ID
			mon.TargetFocusAt = now
		}
	} else if target, ok := w.characterByIDLocked(players, mon.TargetCharacterID); ok && abs(target.X-mon.X) <= 1 && abs(target.Y-mon.Y) <= 1 && (target.X != mon.X || target.Y != mon.Y) {
		if w.isProperMonsterTargetLocked(caster, players, mon) {
			mon.TargetCharacterID = caster.ID
			mon.TargetFocusAt = now
		}
	} else if w.rand.Intn(6) == 0 && w.isProperMonsterTargetLocked(caster, players, mon) {
		mon.TargetCharacterID = caster.ID
		mon.TargetFocusAt = now
	}
	w.monsterStruckLocked(mon, now)
}

func (w *World) monsterMagicStruckLocked(mon *Monster, now time.Time) {
	if mon == nil || mon.Race < 50 || mon.AdminMode || mon.Level >= 50 {
		return
	}
	walkDelay := 800 + w.rand.Intn(1000)
	if mon.LastWalkAt.IsZero() || mon.LastWalkAt.Before(now) {
		mon.LastWalkAt = now.Add(time.Duration(walkDelay) * time.Millisecond)
	} else {
		mon.LastWalkAt = mon.LastWalkAt.Add(time.Duration(walkDelay) * time.Millisecond)
	}
}
