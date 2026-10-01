package world

import (
	"fmt"
	"time"

	"openmir2/internal/storage"
)

func (w *World) ButcherAnimal(ch storage.Character, actorID int32, x, y, dir int) (ButcherResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ch.ID == "" {
		return ButcherResult{Character: ch}, fmt.Errorf("character cannot butcher")
	}
	if abs(x-ch.X) <= 2 && abs(y-ch.Y) <= 2 {
		ch.Dir = dir
	}
	var mon *Monster
	for _, candidate := range w.monsters {
		if candidate == nil || candidate.MapID != ch.MapID || candidate.Alive || candidate.Skeleton || !candidate.Animal {
			continue
		}
		if MonsterActorID(*candidate) == actorID && abs(candidate.X-x) <= 2 && abs(candidate.Y-y) <= 2 {
			mon = candidate
			break
		}
	}
	if mon == nil {
		return ButcherResult{Character: ch}, fmt.Errorf("animal corpse not found")
	}
	mon.BodyLeathery -= 5 + w.rand.Intn(16)
	mon.MeatQuality -= 100 + w.rand.Intn(201)
	if mon.MeatQuality < 0 {
		mon.MeatQuality = 0
	}
	result := ButcherResult{Character: ch, Monster: *mon}
	if mon.BodyLeathery > 0 {
		result.Monster = *mon
		return result, nil
	}
	mon.Skeleton = true
	mon.BodyLeathery = 50
	result.Skeleton = true
	itemID := "肉"
	if mon.Race == 51 {
		itemID = "鸡肉"
	}
	item, ok := w.data.Items[itemID]
	if !ok || !w.canCarryBagItemsLocked(ch, 1) || !w.canCarryWeightLocked(ch, item.Weight) {
		result.Monster = *mon
		return result, fmt.Errorf("bag is full")
	}
	entry := w.createUserItemFromStd(item, 0, [14]byte{})
	entry.Dura = uint16(minInt(mon.MeatQuality, int(^uint16(0))))
	ch.BagItems = append(ch.BagItems, entry)
	result.Character = ch
	result.Monster = *mon
	result.AddedItem = entry
	result.FoundItem = true
	result.Skeleton = true
	return result, w.store.SaveCharacter(ch)
}

func (w *World) resetAnimalCorpseLocked(mon *Monster) {
	mon.Skeleton = false
	mon.BodyLeathery = 0
	mon.MeatQuality = 0
	mon.LastAttackAt = time.Time{}
}
