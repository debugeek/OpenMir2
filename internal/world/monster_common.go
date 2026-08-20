package world

import "openmir2/internal/storage"

func (w *World) monsterActionLocked(mon *Monster, kind MonsterActionKind) MonsterAction {
	return MonsterAction{MonsterID: mon.ID, Name: mon.Name, RaceImg: mon.RaceImg, MonsterWeapon: mon.MonsterWeapon, Appr: mon.Appr, MapID: mon.MapID, X: mon.X, Y: mon.Y, Dir: mon.Dir, Kind: kind}
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
	w.occupyMonsterLocked(mon)
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
