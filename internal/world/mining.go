package world

import (
	"fmt"
	"time"

	"openmir2/internal/storage"
)

func (w *World) mineWithHeavyHitLocked(ch storage.Character, dir int) (AttackResult, bool, error) {
	mapData, ok := w.data.Maps[ch.MapID]
	if !ok || !mapData.Mine {
		return AttackResult{}, false, nil
	}
	weapon, ok := w.equippedItemLocked(ch, SlotWeapon)
	if !ok {
		return AttackResult{Character: ch}, true, nil
	}
	item, ok := w.data.Items[weapon.ItemID]
	if !ok || item.Shape != 19 || dir < 0 || dir >= len(dirOffsets) {
		return AttackResult{Character: ch}, true, nil
	}
	offset := dirOffsets[dir]
	frontX, frontY := ch.X+offset[0], ch.Y+offset[1]
	mapData = w.data.Maps[ch.MapID]
	if mapData.Walkable(frontX, frontY) {
		return AttackResult{Character: ch}, true, nil
	}
	if w.mineCounts == nil {
		w.mineCounts = map[monsterPosition]int{}
	}
	position := monsterPosition{MapID: ch.MapID, X: frontX, Y: frontY}
	count, exists := w.mineCounts[position]
	if !exists {
		count = w.rand.Intn(200)
		w.mineCounts[position] = count
	}
	result := AttackResult{Character: ch, MonsterX: ch.X, MonsterY: ch.Y, MonsterMapID: ch.MapID, ImpactDelay: time.Duration(w.gameplay.Combat.HitImpactDelayMS) * time.Millisecond}
	if count <= 0 {
		return result, true, nil
	}
	w.mineCounts[position] = count - 1
	hitRate := w.gameplay.Combat.MineHitRate
	if hitRate <= 0 {
		hitRate = 4
	}
	if w.rand.Intn(hitRate) != 0 {
		return result, true, nil
	}
	mineRate := w.gameplay.Combat.MineRate
	if mineRate <= 0 {
		mineRate = 12
	}
	if w.rand.Intn(mineRate) != 0 {
		weapon.Dura -= uint16(w.rand.Intn(15) + 5)
		if weapon.Dura > weapon.DuraMax {
			weapon.Dura = 0
		}
		if weapon.Dura == 0 {
			delete(ch.EquippedItems, SlotWeapon)
			result.DeletedItems = append(result.DeletedItems, weapon)
		} else {
			ch.EquippedItems[SlotWeapon] = weapon
		}
		result.Durability = append(result.Durability, SpellDurability{Slot: SlotWeapon, Dura: weapon.Dura, DuraMax: weapon.DuraMax})
		result.Character = ch
		return result, true, w.store.SaveCharacter(ch)
	}
	canCarry := w.canCarryBagItemsLocked(ch, 1)
	weapon.Dura -= uint16(w.rand.Intn(15) + 5)
	if weapon.Dura > weapon.DuraMax {
		weapon.Dura = 0
	}
	if weapon.Dura == 0 {
		delete(ch.EquippedItems, SlotWeapon)
		result.DeletedItems = append(result.DeletedItems, weapon)
	} else {
		ch.EquippedItems[SlotWeapon] = weapon
	}
	result.Durability = append(result.Durability, SpellDurability{Slot: SlotWeapon, Dura: weapon.Dura, DuraMax: weapon.DuraMax})
	if !canCarry {
		result.Character = ch
		return result, true, w.store.SaveCharacter(ch)
	}
	roll := w.rand.Intn(120) + 1
	itemID := "铜矿"
	switch {
	case roll <= 2:
		itemID = "金矿"
	case roll <= 20:
		itemID = "银矿"
	case roll <= 45:
		itemID = "铁矿"
	case roll <= 56:
		itemID = "黑铁矿石"
	}
	item, ok = w.data.Items[itemID]
	if !ok {
		return AttackResult{}, true, fmt.Errorf("mine item %s not found", itemID)
	}
	entry := w.createUserItemFromStd(item, 0, [14]byte{})
	ch.BagItems = append(ch.BagItems, entry)
	result.AddedItems = append(result.AddedItems, entry)
	result.Character = ch
	return result, true, w.store.SaveCharacter(ch)
}
