package world

import "openmir2/internal/storage"

func fleePointForMonster(mon *Monster, target storage.Character) (int, int) {
	dir := direction(mon.X, mon.Y, target.X, target.Y)
	off := dirOffsets[dir]
	return target.X + off[0]*5, target.Y + off[1]*5
}

func (w *World) retreatFromTargetLocked(mon *Monster, target storage.Character) (MonsterAction, bool) {
	mp, ok := w.data.Maps[mon.MapID]
	if !ok {
		return MonsterAction{}, false
	}
	dir := direction(target.X, target.Y, mon.X, mon.Y)
	off := dirOffsets[dir]
	x, y := mon.X+off[0], mon.Y+off[1]
	if !mp.Walkable(x, y) || w.monsterAtLocked(mon.MapID, x, y, mon.ID) {
		return MonsterAction{}, false
	}
	w.moveMonsterLocked(mon, x, y)
	mon.Dir = dir
	return w.monsterActionLocked(mon, MonsterActionWalk), true
}
