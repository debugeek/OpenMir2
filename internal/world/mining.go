package world

import (
	"fmt"
	"time"

	"openmir2/internal/storage"
)

func blockerMap(blockers []storage.Character) map[string]storage.Character {
	players := make(map[string]storage.Character, len(blockers))
	for index, blocker := range blockers {
		key := blocker.ID
		if key == "" {
			key = fmt.Sprintf("#%d", index)
		}
		players[key] = blocker
	}
	return players
}

func (w *World) mineWithHeavyHitLocked(ch storage.Character, dir int, blockers ...storage.Character) (AttackResult, bool, error) {
	mapData, ok := w.data.Maps[ch.MapID]
	if !ok || !mapData.Mine {
		return AttackResult{}, false, nil
	}
	weapon, ok := w.equippedItemLocked(ch, SlotWeapon)
	if !ok {
		return AttackResult{}, false, nil
	}
	item, ok := w.data.Items[weapon.ItemID]
	if !ok || item.Shape != 19 || weapon.Dura == 0 || dir < 0 || dir >= len(dirOffsets) {
		return AttackResult{}, false, nil
	}
	offset := dirOffsets[dir]
	frontX, frontY := ch.X+offset[0], ch.Y+offset[1]
	mapData = w.data.Maps[ch.MapID]
	if frontX < 0 || frontY < 0 || frontX >= mapData.Width || frontY >= mapData.Height {
		return AttackResult{}, false, nil
	}
	if mapData.Walkable(frontX, frontY) && !w.monsterAtLocked(ch.MapID, frontX, frontY, "") && !w.playerAtLocked(blockerMap(blockers), ch.MapID, frontX, frontY) && !w.npcAtLockedWithRun(ch.MapID, frontX, frontY, false) {
		return AttackResult{}, false, nil
	}
	if w.mineCounts == nil {
		w.mineCounts = map[monsterPosition]int{}
	}
	if w.mineAddCounts == nil {
		w.mineAddCounts = map[monsterPosition]int{}
	}
	if w.mineDepletedAt == nil {
		w.mineDepletedAt = map[monsterPosition]time.Time{}
	}
	position := monsterPosition{MapID: ch.MapID, X: frontX, Y: frontY}
	count, exists := w.mineCounts[position]
	if !exists {
		count = w.rand.Intn(200)
		w.mineCounts[position] = count
		w.mineAddCounts[position] = w.rand.Intn(80)
		if count == 0 {
			w.mineDepletedAt[position] = time.Now()
		}
	}
	result := AttackResult{Character: ch, MonsterX: ch.X, MonsterY: ch.Y, MonsterMapID: ch.MapID, ImpactDelay: time.Duration(w.gameplay.Combat.HitImpactDelayMS) * time.Millisecond, HeavyHitMining: true}
	if count <= 0 {
		depletedAt := w.mineDepletedAt[position]
		if !depletedAt.IsZero() && time.Since(depletedAt) > 10*time.Minute {
			count = w.mineAddCounts[position]
			w.mineCounts[position] = count
			w.mineDepletedAt[position] = time.Now()
		}
		return AttackResult{Character: ch, HeavyHitMining: true}, true, nil
	}
	w.mineCounts[position] = count - 1
	if count == 1 {
		w.mineDepletedAt[position] = time.Now()
	}
	hitRate := w.gameplay.Combat.MineHitRate
	if hitRate <= 0 {
		hitRate = 4
	}
	if w.rand.Intn(hitRate) != 0 {
		return AttackResult{Character: ch, HeavyHitMining: true}, true, nil
	}
	pile := SpellGroundEvent{}
	pileAt := false
	for _, event := range w.groundEvents {
		if event.MapID == ch.MapID && event.X == ch.X && event.Y == ch.Y {
			pile = event
			pileAt = true
			break
		}
	}
	if !pileAt {
		pile = SpellGroundEvent{ID: w.nextGroundEventIDLocked(), MapID: ch.MapID, X: ch.X, Y: ch.Y, Type: 3, Param: 1, Duration: 5 * time.Minute, StartAt: time.Now()}
		w.addGroundEventLocked(pile)
		result.GroundEvents = append(result.GroundEvents, pile)
	} else if pile.Type == 3 && pile.Param < 5 {
		pile.Param++
		w.addGroundEventLocked(pile)
	}
	result.HeavyHitFragment = true
	damageWeapon := func() {
		oldDisplayDura := referenceRound(float64(weapon.Dura) / 1.03)
		loss := w.rand.Intn(15) + 5
		if int(weapon.Dura) <= loss {
			weapon.Dura = 0
		} else {
			weapon.Dura -= uint16(loss)
		}
		if weapon.Dura == 0 {
			delete(ch.EquippedItems, SlotWeapon)
			result.DeletedItems = append(result.DeletedItems, weapon)
			result.Durability = append(result.Durability, SpellDurability{Slot: SlotWeapon, Dura: weapon.Dura, DuraMax: weapon.DuraMax})
		} else {
			ch.EquippedItems[SlotWeapon] = weapon
		}
		newDisplayDura := referenceRound(float64(weapon.Dura) / 1.03)
		if newDisplayDura != oldDisplayDura {
			result.Durability = append(result.Durability, SpellDurability{Slot: SlotWeapon, Dura: weapon.Dura, DuraMax: weapon.DuraMax})
		}
	}
	mineRate := w.gameplay.Combat.MineRate
	if mineRate <= 0 {
		mineRate = 12
	}
	if w.rand.Intn(mineRate) != 0 {
		damageWeapon()
		result.Character = ch
		return result, true, nil
	}
	canCarry := w.canCarryBagItemsLocked(ch, 1)
	if !canCarry {
		damageWeapon()
		result.Character = ch
		return result, true, nil
	}
	itemID := "铜矿"
	roll := w.rand.Intn(120) + 1
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
		damageWeapon()
		result.Character = ch
		return result, true, nil
	}
	entry := w.createUserItemFromStd(item, 0, [14]byte{})
	dura := w.rand.Intn(13000) + 3000
	if w.rand.Intn(20) == 0 {
		dura += w.rand.Intn(10000)
	}
	entry.Dura = uint16(dura)
	ch.BagItems = append(ch.BagItems, entry)
	result.AddedItems = append(result.AddedItems, entry)
	damageWeapon()
	result.Character = ch
	return result, true, nil
}
