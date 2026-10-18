package world

import "openmir2/internal/storage"

func fleePointForMonster(mon *Monster, target storage.Character) (int, int) {
	dir := direction(mon.X, mon.Y, target.X, target.Y)
	off := dirOffsets[dir]
	return target.X + off[0]*5, target.Y + off[1]*5
}

func (w *World) referenceAnimalFleePointLocked(mon *Monster, target storage.Character) (int, int) {
	x, y := target.X, target.Y
	dir := direction(mon.X, mon.Y, target.X, target.Y)
	mp, ok := w.data.Maps[mon.MapID]
	if !ok {
		return x, y
	}
	const distance = 5
	canMove := false
	switch dir {
	case 0:
		canMove = y > distance-1
	case 1:
		canMove = x > distance-1 && y < mp.Height-distance
	case 2:
		canMove = x < mp.Width-distance
	case 3:
		canMove = x < mp.Width-distance && y < mp.Height-distance
	case 4:
		canMove = y < mp.Width-distance
	case 5:
		canMove = x < mp.Width-distance && y > distance-1
	case 6:
		canMove = x > distance-1
	case 7:
		canMove = x > distance-1 && y > distance-1
	}
	if canMove {
		off := dirOffsets[dir]
		return x + off[0]*distance, y + off[1]*distance
	}
	return x, y
}

func (w *World) retreatFromTargetLocked(mon *Monster, target storage.Character) (MonsterAction, bool) {
	mp, ok := w.data.Maps[mon.MapID]
	if !ok {
		return MonsterAction{}, false
	}
	dir := direction(target.X, target.Y, mon.X, mon.Y)
	mon.Dir = dir
	off := dirOffsets[dir]
	x, y := mon.X+off[0], mon.Y+off[1]
	if !mp.Walkable(x, y) || w.movingObjectAtLocked(nil, mon.MapID, x, y, mon.ID) {
		return MonsterAction{}, false
	}
	w.moveMonsterLocked(mon, x, y)
	return w.monsterActionLocked(mon, MonsterActionWalk), true
}
