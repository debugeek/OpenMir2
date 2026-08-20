package world

import (
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func (w *World) moveMonsterTowardPointLocked(mon *Monster, x, y int, players map[string]storage.Character) bool {
	mp, ok := w.data.Maps[mon.MapID]
	if !ok {
		return false
	}
	if mon.X == x && mon.Y == y {
		return false
	}
	dir := direction(mon.X, mon.Y, x, y)
	step := 1
	if w.rand.Intn(2) == 0 {
		step = -1
	}
	tryDir := func(dir int) bool {
		off := dirOffsets[dir]
		nx, ny := mon.X+off[0], mon.Y+off[1]
		if !mp.Walkable(nx, ny) || w.monsterAtLocked(mon.MapID, nx, ny, mon.ID) || w.playerAtLocked(players, mon.MapID, nx, ny) {
			return false
		}
		w.moveMonsterLocked(mon, nx, ny)
		mon.Dir = dir
		return true
	}
	if tryDir(dir) {
		return true
	}
	next := dir
	for i := 0; i < len(dirOffsets)-1; i++ {
		next += step
		if next < 0 {
			next = len(dirOffsets) - 1
		}
		if next >= len(dirOffsets) {
			next = 0
		}
		if tryDir(next) {
			return true
		}
	}
	return false
}

func (w *World) tryMoveMonsterLocked(mon *Monster, target storage.Character, mp data.StdMap, dir int, players map[string]storage.Character) bool {
	off := dirOffsets[dir]
	x, y := mon.X+off[0], mon.Y+off[1]
	if !mp.Walkable(x, y) || (x == target.X && y == target.Y) || w.monsterAtLocked(mon.MapID, x, y, mon.ID) || w.playerAtLocked(players, mon.MapID, x, y) {
		return false
	}
	w.moveMonsterLocked(mon, x, y)
	mon.Dir = dir
	return true
}

func (w *World) moveMonsterTowardLocked(mon *Monster, target storage.Character, dir int, players map[string]storage.Character) bool {
	mp, ok := w.data.Maps[mon.MapID]
	if !ok {
		return false
	}
	if w.tryMoveMonsterLocked(mon, target, mp, dir, players) {
		return true
	}
	step := 1
	if w.rand.Intn(3) == 0 {
		step = -1
	}
	for i := 0; i < len(dirOffsets); i++ {
		dir += step
		if dir < 0 {
			dir = len(dirOffsets) - 1
		}
		if dir >= len(dirOffsets) {
			dir = 0
		}
		if w.tryMoveMonsterLocked(mon, target, mp, dir, players) {
			return true
		}
	}
	return false
}

func (w *World) monsterWalkReadyLocked(mon *Monster, now time.Time) bool {
	if mon.LastWalkAt.IsZero() || now.Before(mon.LastWalkAt) {
		mon.LastWalkAt = now.Add(-time.Duration(w.monsterWalkSpeedMSLocked(mon)+1) * time.Millisecond)
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

func (w *World) wanderMonsterLocked(mon *Monster, players map[string]storage.Character) (MonsterAction, bool) {
	if w.rand.Intn(20) != 0 {
		return MonsterAction{}, false
	}
	if w.rand.Intn(4) == 1 {
		mon.Dir = w.rand.Intn(len(dirOffsets))
		return w.monsterActionLocked(mon, MonsterActionTurn), true
	}
	mp, ok := w.data.Maps[mon.MapID]
	if !ok {
		return MonsterAction{}, false
	}
	off := dirOffsets[mon.Dir]
	x, y := mon.X+off[0], mon.Y+off[1]
	if !mp.Walkable(x, y) || w.monsterAtLocked(mon.MapID, x, y, mon.ID) || w.playerAtLocked(players, mon.MapID, x, y) {
		return MonsterAction{}, false
	}
	w.moveMonsterLocked(mon, x, y)
	return w.monsterActionLocked(mon, MonsterActionWalk), true
}
