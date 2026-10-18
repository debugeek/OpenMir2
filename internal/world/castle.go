package world

import (
	"errors"
	"fmt"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

var errCastlePermission = errors.New("castle permission denied")

func (w *World) CastleID() string {
	return w.data.Castle.ID
}

func (w *World) castleDoorLocked(id string) *Monster {
	for _, mon := range w.monsters {
		if mon != nil && mon.CastleDoor && mon.CastleID == id {
			return mon
		}
	}
	return nil
}

func (w *World) castleWallLocked(id string, index int) *Monster {
	for _, mon := range w.monsters {
		if mon != nil && mon.CastleWall && mon.CastleID == id && mon.CastleWallIndex == index {
			return mon
		}
	}
	return nil
}

func castleDoorDirection(mon *Monster) int {
	if mon == nil || mon.MaxHP <= 0 {
		return 0
	}
	direction := 3 - referenceRound(float64(mon.HP)/float64(mon.MaxHP)*3)
	if direction >= 3 {
		return 0
	}
	return direction
}

func castleWallDirection(mon *Monster) int {
	if mon == nil || mon.MaxHP <= 0 {
		return 0
	}
	if mon.HP <= 0 {
		return 4
	}
	direction := 3 - referenceRound(float64(mon.HP)/float64(mon.MaxHP)*3)
	if direction >= 5 {
		return 0
	}
	return direction
}

func (w *World) castleDoorSetMapFlagsLocked(mon *Monster, flag int) {
	if mon == nil || !mon.CastleDoor {
		return
	}
	positions := []monsterPosition{
		{MapID: mon.MapID, X: mon.X, Y: mon.Y},
		{MapID: mon.MapID, X: mon.X, Y: mon.Y - 1},
		{MapID: mon.MapID, X: mon.X, Y: mon.Y - 2},
		{MapID: mon.MapID, X: mon.X + 1, Y: mon.Y - 1},
		{MapID: mon.MapID, X: mon.X + 1, Y: mon.Y - 2},
		{MapID: mon.MapID, X: mon.X - 1, Y: mon.Y},
		{MapID: mon.MapID, X: mon.X - 2, Y: mon.Y},
		{MapID: mon.MapID, X: mon.X - 1, Y: mon.Y - 1},
		{MapID: mon.MapID, X: mon.X - 1, Y: mon.Y + 1},
	}
	for _, position := range positions {
		delete(w.castleBlocked, position)
	}
	switch flag {
	case 1:
		for _, position := range positions {
			w.castleBlocked[position] = struct{}{}
		}
	case 0:
		for _, position := range []monsterPosition{positions[2], positions[3], positions[4]} {
			w.castleBlocked[position] = struct{}{}
		}
	}
}

func (w *World) castleMapWalkableLocked(mapID string, x, y int) bool {
	_, blocked := w.castleBlocked[monsterPosition{MapID: mapID, X: x, Y: y}]
	return !blocked
}

func (w *World) runWarBlocksMovementAtLocked(mapID string, x, y int) bool {
	return w.gameplay.Movement.RunWarAll && w.characterInCastleWarAreaLocked(storage.Character{MapID: mapID, X: x, Y: y})
}

func (w *World) castleWallSetMapFlagLocked(mon *Monster) {
	if mon == nil || !mon.CastleWall {
		return
	}
	position := monsterPosition{MapID: mon.MapID, X: mon.X, Y: mon.Y}
	if mon.Alive && !mon.Ghost {
		w.castleBlocked[position] = struct{}{}
	} else {
		delete(w.castleBlocked, position)
	}
}

func (w *World) recordCastleDoorDamageLocked(mon *Monster, now time.Time) {
	if mon == nil || !mon.CastleDoor {
		return
	}
	castle, ok := w.castleSnapshotLocked(mon.CastleID)
	if !ok {
		return
	}
	castle.MainDoorHP = mon.HP
	castle.MainDoorHitAt = now.UnixMilli()
	w.setCastleRuntimeLocked(castle)
}

func (w *World) recordCastleWallDamageLocked(mon *Monster, now time.Time) {
	if mon == nil || !mon.CastleWall || mon.CastleWallIndex < 0 || mon.CastleWallIndex >= 3 {
		return
	}
	castle, ok := w.castleSnapshotLocked(mon.CastleID)
	if !ok {
		return
	}
	castle.WallHP[mon.CastleWallIndex] = mon.HP
	castle.WallHitAt[mon.CastleWallIndex] = now.UnixMilli()
	w.setCastleRuntimeLocked(castle)
}

func (w *World) recordCastleDamageLocked(mon *Monster, now time.Time) {
	w.recordCastleDoorDamageLocked(mon, now)
	w.recordCastleWallDamageLocked(mon, now)
	w.recordCastleDefenseDamageLocked(mon)
}

func (w *World) recordCastleDefenseDamageLocked(mon *Monster) {
	if mon == nil || (!mon.CastleDefenseArcher && !mon.CastleDefenseGuard) {
		return
	}
	castle, ok := w.castleSnapshotLocked(mon.CastleID)
	if !ok {
		return
	}
	if mon.CastleDefenseArcher && mon.CastleDefenseIndex >= 0 && mon.CastleDefenseIndex < len(castle.ArcherHP) {
		castle.ArcherHP[mon.CastleDefenseIndex] = mon.HP
	} else if mon.CastleDefenseGuard && mon.CastleDefenseIndex >= 0 && mon.CastleDefenseIndex < len(castle.GuardHP) {
		castle.GuardHP[mon.CastleDefenseIndex] = mon.HP
	} else {
		return
	}
	w.setCastleRuntimeLocked(castle)
}

func (w *World) spawnCastleDoor() {
	if w.store == nil || w.data.Castle.MainDoor.MonsterID == "" {
		return
	}
	castle, ok := w.store.Castle(w.data.Castle.ID)
	if !ok {
		return
	}
	tpl, ok := w.data.Monsters[w.data.Castle.MainDoor.MonsterID]
	if !ok || tpl.Behavior != "castle_door" {
		return
	}
	spawn := data.StdSpawn{
		ID:        "castle-door:" + castle.ID,
		MapID:     w.data.Castle.CastleMap,
		MonsterID: tpl.ID,
		X:         w.data.Castle.MainDoor.X,
		Y:         w.data.Castle.MainDoor.Y,
		Count:     1,
	}
	mon := newMonster(w, spawn.ID, tpl, spawn.MapID, spawn.X, spawn.Y, spawn)
	mon.CastleDoor = true
	mon.CastleID = castle.ID
	mon.HP = castle.MainDoorHP
	mon.StoneMode = castle.MainDoorOpen && mon.HP > 0
	if mon.StoneMode {
		mon.Dir = 7
	} else {
		mon.Dir = castleDoorDirection(mon)
	}
	if mon.HP <= 0 {
		w.castleDoorSetMapFlagsLocked(mon, 2)
	} else if mon.StoneMode {
		w.castleDoorSetMapFlagsLocked(mon, 0)
	} else {
		w.castleDoorSetMapFlagsLocked(mon, 1)
	}
	if mon.HP <= 0 {
		mon.Alive = false
		mon.DeathAt = time.Now()
	}
	w.registerMonsterLocked(mon, nil)
	w.occupyMonsterLocked(mon)
}

func (w *World) spawnCastleWalls() {
	if w.store == nil || len(w.data.Castle.Walls) == 0 {
		return
	}
	castle, ok := w.store.Castle(w.data.Castle.ID)
	if !ok {
		return
	}
	for index, slot := range w.data.Castle.Walls {
		if slot.MonsterID == "" {
			continue
		}
		tpl, ok := w.data.Monsters[slot.MonsterID]
		if !ok || tpl.Behavior != "castle_wall" {
			continue
		}
		spawn := data.StdSpawn{
			ID:        fmt.Sprintf("castle-wall:%s:%d", castle.ID, index),
			MapID:     w.data.Castle.CastleMap,
			MonsterID: tpl.ID,
			X:         slot.X,
			Y:         slot.Y,
			Count:     1,
		}
		mon := newMonster(w, spawn.ID, tpl, spawn.MapID, spawn.X, spawn.Y, spawn)
		mon.CastleID = castle.ID
		mon.CastleWall = true
		mon.CastleWallIndex = index
		mon.Dir = 0
		if slot.HP > 0 {
			mon.MaxHP = slot.HP
			mon.BaseMaxHP = slot.HP
		}
		if index >= len(castle.WallHP) {
			continue
		}
		mon.HP = castle.WallHP[index]
		if mon.HP <= 0 {
			mon.Alive = false
			mon.DeathAt = time.Now()
		}
		w.registerMonsterLocked(mon, nil)
		w.castleWallSetMapFlagLocked(mon)
		if mon.Alive {
			w.occupyMonsterLocked(mon)
		}
	}
}

func (w *World) spawnCastleDefenders() {
	if w.store == nil {
		return
	}
	castle, ok := w.store.Castle(w.data.Castle.ID)
	if !ok {
		return
	}
	if !castle.DefenseHPSet {
		for index, slot := range w.data.Castle.Guards {
			if index < len(castle.GuardHP) {
				castle.GuardHP[index] = slot.HP
			}
		}
		for index, slot := range w.data.Castle.Archers {
			if index < len(castle.ArcherHP) {
				castle.ArcherHP[index] = slot.HP
			}
		}
		castle.DefenseHPSet = true
	}
	spawn := func(slots []data.CastleDefenseSlot, archer bool) {
		for index, slot := range slots {
			hp := 0
			if archer && index < len(castle.ArcherHP) {
				hp = castle.ArcherHP[index]
			} else if !archer && index < len(castle.GuardHP) {
				hp = castle.GuardHP[index]
			}
			if hp <= 0 || slot.MonsterID == "" {
				continue
			}
			tpl, ok := w.data.Monsters[slot.MonsterID]
			if !ok {
				continue
			}
			mon := w.createSpawnMonsterLocked(data.StdSpawn{
				ID:        fmt.Sprintf("castle-defender:%s:%t:%d", castle.ID, archer, index),
				MapID:     w.data.Castle.CastleMap,
				MonsterID: tpl.ID,
				X:         slot.X,
				Y:         slot.Y,
				Count:     1,
			}, tpl, slot.X, slot.Y)
			if mon == nil {
				continue
			}
			mon.HP = hp
			mon.CastleID = castle.ID
			if archer {
				mon.CastleDefenseArcher = true
			} else {
				mon.CastleDefenseGuard = true
			}
			mon.CastleDefenseIndex = index
			if archer {
				mon.Dir = 3
				mon.GuardDirection = 3
			}
			w.occupyMonsterLocked(mon)
		}
	}
	spawn(w.data.Castle.Archers, true)
	spawn(w.data.Castle.Guards, false)
	w.setCastleRuntimeLocked(castle)
}

func (w *World) castleSnapshotLocked(id string) (storage.Castle, bool) {
	if castle, ok := w.castleRuntime[id]; ok {
		return castle, true
	}
	castle, ok := w.store.Castle(id)
	if ok {
		w.castleRuntime[id] = castle
	}
	return castle, ok
}

func (w *World) setCastleRuntimeLocked(castle storage.Castle) {
	w.castleRuntime[castle.ID] = castle
	w.dirtyCastles[castle.ID] = struct{}{}
}

func (w *World) CastleSnapshot(id string) (storage.Castle, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.castleSnapshotLocked(id)
}

func (w *World) CastleDoorSnapshot(id string) (Monster, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	door := w.castleDoorLocked(id)
	if door == nil {
		return Monster{}, false
	}
	return *door, true
}

func (w *World) CastleWallSnapshot(id string, index int) (Monster, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	wall := w.castleWallLocked(id, index)
	if wall == nil {
		return Monster{}, false
	}
	return *wall, true
}

func (w *World) RestoreCastleRuntime(castle storage.Castle) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.castleRuntime[castle.ID] = castle
	w.dirtyCastles[castle.ID] = struct{}{}
}

func (w *World) FlushCastlePersistence() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for id := range w.dirtyCastles {
		castle, ok := w.castleRuntime[id]
		if !ok {
			delete(w.dirtyCastles, id)
			continue
		}
		if err := w.store.SaveCastle(castle); err == nil {
			delete(w.dirtyCastles, id)
		}
	}
}

func (w *World) cleanupCastleDefenseSlotsLocked() {
	if w.store == nil {
		return
	}
	castle, ok := w.castleSnapshotLocked(w.data.Castle.ID)
	if !ok {
		return
	}
	changed := false
	for i, monsterID := range castle.GuardSlots {
		if monsterID == "" {
			continue
		}
		if monster := w.monsters[monsterID]; monster == nil || monster.Ghost {
			castle.GuardSlots[i] = ""
			changed = true
		}
	}
	for i, monsterID := range castle.ArcherSlots {
		if monsterID == "" {
			continue
		}
		if monster := w.monsters[monsterID]; monster == nil || monster.Ghost {
			castle.ArcherSlots[i] = ""
			changed = true
		}
	}
	if changed {
		w.setCastleRuntimeLocked(castle)
	}
}

func (w *World) CastleRequestWar(ch storage.Character, castleID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	castle, ok := w.castleSnapshotLocked(castleID)
	if !ok || ch.GuildID == "" || ch.GuildRank != 1 || castle.OwnerGuildID == ch.GuildID {
		return errCastlePermission
	}
	if castle.Attackers == nil {
		castle.Attackers = map[string]int64{}
	}
	if _, exists := castle.Attackers[ch.GuildID]; exists {
		return errors.New("guild has already requested castle war")
	}
	days := w.gameplay.Castle.StartWarDays
	if days <= 0 {
		days = 4
	}
	castle.Attackers[ch.GuildID] = time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
	if err := w.store.SaveCastle(castle); err != nil {
		return err
	}
	w.castleRuntime[castleID] = castle
	delete(w.dirtyCastles, castleID)
	return nil
}

func (w *World) CastleRequestWarWithCharacter(ch, updated storage.Character, castleID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	castle, ok := w.castleSnapshotLocked(castleID)
	if !ok || ch.GuildID == "" || ch.GuildRank != 1 || castle.OwnerGuildID == ch.GuildID {
		return errCastlePermission
	}
	if _, exists := castle.Attackers[ch.GuildID]; exists {
		return errors.New("guild has already requested castle war")
	}
	days := w.gameplay.Castle.StartWarDays
	if days <= 0 {
		days = 4
	}
	if err := w.store.SaveCharacter(updated); err != nil {
		return err
	}
	if castle.Attackers == nil {
		castle.Attackers = map[string]int64{}
	}
	castle.Attackers[ch.GuildID] = time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
	if err := w.store.SaveCastle(castle); err != nil {
		_ = w.store.SaveCharacter(ch)
		return err
	}
	w.castleRuntime[castleID] = castle
	delete(w.dirtyCastles, castleID)
	return nil
}

func (w *World) ValidateCastleRequestWar(ch storage.Character, castleID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	castle, ok := w.castleSnapshotLocked(castleID)
	if !ok || ch.GuildID == "" || ch.GuildRank != 1 || castle.OwnerGuildID == ch.GuildID {
		return errCastlePermission
	}
	if _, exists := castle.Attackers[ch.GuildID]; exists {
		return errors.New("guild has already requested castle war")
	}
	return nil
}

func (w *World) castleForCharacterLocked(ch storage.Character, castleID string) (storage.Castle, error) {
	castle, ok := w.castleSnapshotLocked(castleID)
	if !ok || castle.OwnerGuildID == "" || ch.GuildID != castle.OwnerGuildID || ch.GuildRank != 1 {
		return storage.Castle{}, errCastlePermission
	}
	return castle, nil
}

func (w *World) castleOfficialForCharacterLocked(ch storage.Character, castleID string) (storage.Castle, error) {
	castle, ok := w.castleSnapshotLocked(castleID)
	if !ok || !ch.AdminMode && (castle.OwnerGuildID == "" || ch.GuildID != castle.OwnerGuildID || ch.GuildRank != 1) {
		return storage.Castle{}, errCastlePermission
	}
	return castle, nil
}

func (w *World) CastleWithdraw(ch storage.Character, castleID string, amount int) (storage.Character, error) {
	if amount <= 0 {
		return ch, errors.New("invalid castle amount")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	castle, err := w.castleForCharacterLocked(ch, castleID)
	if err != nil || amount > castle.Gold {
		return ch, errCastlePermission
	}
	if w.gameplay.Item.MaxGold > 0 && ch.Gold+amount > w.gameplay.Item.MaxGold {
		return ch, errors.New("gold capacity exceeded")
	}
	castle.Gold -= amount
	ch.Gold += amount
	w.setCastleRuntimeLocked(castle)
	return ch, nil
}

func (w *World) CastleReceipt(ch storage.Character, castleID string, amount int) (storage.Character, error) {
	if amount <= 0 {
		return ch, errors.New("invalid castle amount")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	castle, err := w.castleForCharacterLocked(ch, castleID)
	if err != nil {
		return ch, errCastlePermission
	}
	if ch.Gold < amount {
		return ch, errors.New("invalid castle amount")
	}
	if w.gameplay.Castle.GoldMax > 0 && castle.Gold+amount > w.gameplay.Castle.GoldMax {
		return ch, errors.New("castle gold capacity exceeded")
	}
	castle.Gold += amount
	ch.Gold -= amount
	w.setCastleRuntimeLocked(castle)
	return ch, nil
}

func (w *World) CastleSetMainDoor(ch storage.Character, castleID string, open bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	castle, err := w.castleOfficialForCharacterLocked(ch, castleID)
	if err != nil {
		return errCastlePermission
	}
	door := w.castleDoorLocked(castle.ID)
	if door == nil || !door.Alive || door.StoneMode == open {
		return nil
	}
	castle.MainDoorOpen = open
	door.StoneMode = open
	if open {
		door.Dir = 7
		w.castleDoorSetMapFlagsLocked(door, 0)
	} else {
		door.Dir = castleDoorDirection(door)
		w.castleDoorSetMapFlagsLocked(door, 1)
	}
	w.setCastleRuntimeLocked(castle)
	return nil
}

func (w *World) CastleRepairDoor(ch storage.Character, castleID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	castle, err := w.castleOfficialForCharacterLocked(ch, castleID)
	if err != nil {
		return errCastlePermission
	}
	price := w.gameplay.Castle.RepairDoorPrice
	if castle.Gold < price {
		return errors.New("insufficient castle gold")
	}
	door := w.castleDoorLocked(castle.ID)
	if door == nil {
		return errCastlePermission
	}
	maxHP := door.MaxHP
	if maxHP <= 0 {
		maxHP = w.data.Castle.MainDoor.HP
	}
	if maxHP <= 0 {
		maxHP = 2000
	}
	if castle.UnderWar || door.HP >= maxHP || castle.MainDoorHitAt > 0 && time.Now().UnixMilli()-castle.MainDoorHitAt < 60*1000 {
		return errCastlePermission
	}
	castle.Gold -= price
	if door.HP <= 0 {
		castle.MainDoorOpen = false
	}
	castle.MainDoorHP = maxHP
	wasDead := !door.Alive
	door.HP = maxHP
	door.Dir = castleDoorDirection(door)
	door.Alive = true
	door.Ghost = false
	door.DeathAt = time.Time{}
	door.GhostAt = time.Time{}
	if wasDead {
		door.StoneMode = false
		w.occupyMonsterLocked(door)
	}
	w.setCastleRuntimeLocked(castle)
	return nil
}

func (w *World) CastleRepairWall(ch storage.Character, castleID string, index int) error {
	if index < 0 || index >= 3 {
		return errors.New("invalid castle wall")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	castle, err := w.castleOfficialForCharacterLocked(ch, castleID)
	if err != nil {
		return errCastlePermission
	}
	price := w.gameplay.Castle.RepairWallPrice
	if castle.Gold < price {
		return errors.New("insufficient castle gold")
	}
	wall := w.castleWallLocked(castle.ID, index)
	if wall == nil {
		return errCastlePermission
	}
	maxHP := wall.MaxHP
	if maxHP <= 0 {
		maxHP = 2000
	}
	if castle.UnderWar || wall.HP >= maxHP || castle.WallHitAt[index] > 0 && time.Now().UnixMilli()-castle.WallHitAt[index] < 60*1000 {
		return errCastlePermission
	}
	castle.Gold -= price
	castle.WallHP[index] = maxHP
	castle.WallHitAt[index] = 0
	wall.HP = maxHP
	wall.MaxHP = maxHP
	wall.Dir = castleWallDirection(wall)
	wall.Alive = true
	wall.Ghost = false
	wall.PendingDeath = false
	wall.DeathAt = time.Time{}
	wall.GhostAt = time.Time{}
	w.occupyMonsterLocked(wall)
	w.setCastleRuntimeLocked(castle)
	return nil
}

func (w *World) HireCastleDefense(ch storage.Character, castleID string, archer bool, index int) (SpawnResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	castle, err := w.castleOfficialForCharacterLocked(ch, castleID)
	if err != nil {
		return SpawnResult{}, errCastlePermission
	}
	price := w.gameplay.Castle.HireGuardPrice
	slots := w.data.Castle.Guards
	if archer {
		price = w.gameplay.Castle.HireArcherPrice
		slots = w.data.Castle.Archers
	}
	if castle.Gold < price {
		return SpawnResult{}, errors.New("insufficient castle gold")
	}
	if index < 0 || index >= len(slots) {
		return SpawnResult{}, errors.New("invalid castle defense slot")
	}
	slot := slots[index]
	occupied := false
	for _, mon := range w.monsters {
		if mon == nil || mon.Ghost || mon.CastleID != castle.ID || mon.CastleDefenseIndex != index {
			continue
		}
		if archer && mon.CastleDefenseArcher || !archer && mon.CastleDefenseGuard {
			occupied = true
			break
		}
	}
	if occupied || (archer && castle.ArcherSlots[index] != "") || (!archer && castle.GuardSlots[index] != "") {
		return SpawnResult{}, errors.New("castle defense slot occupied")
	}
	if castle.UnderWar {
		return SpawnResult{}, errCastlePermission
	}
	if slot.MonsterID == "" {
		return SpawnResult{}, errors.New("castle defense monster is not configured")
	}
	mp, ok := w.data.Maps[w.data.Castle.CastleMap]
	if !ok || !mp.Walkable(slot.X, slot.Y) {
		return SpawnResult{}, errors.New("castle defense coordinate is blocked")
	}
	tpl, ok := w.monsterTemplateByIDLocked(slot.MonsterID)
	if !ok {
		return SpawnResult{}, fmt.Errorf("monster %s not found", slot.MonsterID)
	}
	spawn := data.StdSpawn{MapID: w.data.Castle.CastleMap, MonsterID: tpl.ID, X: slot.X, Y: slot.Y, Count: 1}
	mon := w.createSpawnMonsterLocked(spawn, tpl, slot.X, slot.Y)
	if mon == nil {
		return SpawnResult{}, errors.New("castle defense monster could not be created")
	}
	mon.Dir = 3
	mon.GuardDirection = 3
	mon.CastleID = castle.ID
	mon.CastleDefenseIndex = index
	if archer {
		mon.CastleDefenseArcher = true
		castle.ArcherHP[index] = mon.HP
	} else {
		mon.CastleDefenseGuard = true
		castle.GuardHP[index] = mon.HP
	}
	castle.DefenseHPSet = true
	w.occupyMonsterLocked(mon)
	castle.Gold -= price
	if archer {
		castle.ArcherSlots[index] = mon.ID
	} else {
		castle.GuardSlots[index] = mon.ID
	}
	w.setCastleRuntimeLocked(castle)
	return SpawnResult{Monsters: []Monster{*mon}}, nil
}
