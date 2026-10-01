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

func (w *World) cleanupCastleDefenseSlotsLocked() {
	if w.store == nil {
		return
	}
	castle, ok := w.store.Castle(w.data.Castle.ID)
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
		_ = w.store.SaveCastle(castle)
	}
}

func (w *World) CastleRequestWar(ch storage.Character, castleID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	castle, ok := w.store.Castle(castleID)
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
	return w.store.SaveCastle(castle)
}

func (w *World) ValidateCastleRequestWar(ch storage.Character, castleID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	castle, ok := w.store.Castle(castleID)
	if !ok || ch.GuildID == "" || ch.GuildRank != 1 || castle.OwnerGuildID == ch.GuildID {
		return errCastlePermission
	}
	if _, exists := castle.Attackers[ch.GuildID]; exists {
		return errors.New("guild has already requested castle war")
	}
	return nil
}

func (w *World) castleForCharacterLocked(ch storage.Character, castleID string) (storage.Castle, error) {
	castle, ok := w.store.Castle(castleID)
	if !ok || castle.OwnerGuildID == "" || ch.GuildID != castle.OwnerGuildID || ch.GuildRank != 1 {
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
	if err := w.store.SaveCastle(castle); err != nil {
		return ch, err
	}
	return ch, nil
}

func (w *World) CastleReceipt(ch storage.Character, castleID string, amount int) (storage.Character, error) {
	if amount <= 0 || ch.Gold < amount {
		return ch, errors.New("invalid castle amount")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	castle, err := w.castleForCharacterLocked(ch, castleID)
	if err != nil {
		return ch, errCastlePermission
	}
	if w.gameplay.Item.MaxGold > 0 && castle.Gold+amount > w.gameplay.Item.MaxGold {
		return ch, errors.New("castle gold capacity exceeded")
	}
	castle.Gold += amount
	ch.Gold -= amount
	if err := w.store.SaveCastle(castle); err != nil {
		return ch, err
	}
	return ch, nil
}

func (w *World) CastleSetMainDoor(ch storage.Character, castleID string, open bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	castle, err := w.castleForCharacterLocked(ch, castleID)
	if err != nil {
		return errCastlePermission
	}
	castle.MainDoorOpen = open
	return w.store.SaveCastle(castle)
}

func (w *World) CastleRepairDoor(ch storage.Character, castleID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	castle, err := w.castleForCharacterLocked(ch, castleID)
	maxHP := w.data.Castle.MainDoor.HP
	if maxHP <= 0 {
		maxHP = 2000
	}
	if err != nil {
		return errCastlePermission
	}
	price := w.gameplay.Castle.RepairDoorPrice
	if castle.Gold < price {
		return errors.New("insufficient castle gold")
	}
	if castle.UnderWar || castle.MainDoorHP >= maxHP || castle.MainDoorHitAt > 0 && time.Now().UnixMilli()-castle.MainDoorHitAt < 60*1000 {
		return errCastlePermission
	}
	castle.Gold -= price
	if castle.MainDoorHP <= 0 {
		castle.MainDoorOpen = false
	}
	castle.MainDoorHP = maxHP
	return w.store.SaveCastle(castle)
}

func (w *World) CastleRepairWall(ch storage.Character, castleID string, index int) error {
	if index < 0 || index >= 3 {
		return errors.New("invalid castle wall")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	castle, err := w.castleForCharacterLocked(ch, castleID)
	maxHP := 2000
	if index < len(w.data.Castle.Walls) && w.data.Castle.Walls[index].HP > 0 {
		maxHP = w.data.Castle.Walls[index].HP
	}
	if err != nil {
		return errCastlePermission
	}
	price := w.gameplay.Castle.RepairWallPrice
	if castle.Gold < price {
		return errors.New("insufficient castle gold")
	}
	if castle.UnderWar || castle.WallHP[index] >= maxHP || castle.WallHitAt[index] > 0 && time.Now().UnixMilli()-castle.WallHitAt[index] < 60*1000 {
		return errCastlePermission
	}
	castle.Gold -= price
	castle.WallHP[index] = maxHP
	return w.store.SaveCastle(castle)
}

func (w *World) HireCastleDefense(ch storage.Character, castleID string, archer bool, index int) (SpawnResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	castle, err := w.castleForCharacterLocked(ch, castleID)
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
	if (archer && castle.ArcherSlots[index] != "") || (!archer && castle.GuardSlots[index] != "") {
		return SpawnResult{}, errors.New("castle defense slot occupied")
	}
	if castle.UnderWar {
		return SpawnResult{}, errCastlePermission
	}
	slot := slots[index]
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
	w.occupyMonsterLocked(mon)
	castle.Gold -= price
	if archer {
		castle.ArcherSlots[index] = mon.ID
	} else {
		castle.GuardSlots[index] = mon.ID
	}
	if err := w.store.SaveCastle(castle); err != nil {
		delete(w.monsters, mon.ID)
		return SpawnResult{}, err
	}
	return SpawnResult{Monsters: []Monster{*mon}}, nil
}
