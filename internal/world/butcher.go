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
	if abs(x-ch.X) > 2 || abs(y-ch.Y) > 2 {
		return ButcherResult{Character: ch}, fmt.Errorf("animal corpse not found")
	}
	var mon *Monster
	if abs(x-ch.X) <= 2 && abs(y-ch.Y) <= 2 {
		ch.Dir = dir
		mon = w.butcherTargetLocked(ch.MapID, actorID, x, y)
	}
	if mon == nil {
		return ButcherResult{Character: ch}, fmt.Errorf("animal corpse not found")
	}
	if mon.Alive || mon.Ghost || mon.Skeleton || !mon.Animal {
		return ButcherResult{Character: ch, Monster: *mon}, fmt.Errorf("animal corpse not found")
	}
	mon.BodyLeathery -= 5 + w.rand.Intn(16)
	mon.MeatQuality -= 100 + w.rand.Intn(201)
	if mon.MeatQuality < 0 {
		mon.MeatQuality = 0
	}
	result := ButcherResult{Character: ch, Monster: *mon}
	if mon.BodyLeathery > 0 {
		mon.DeathAt = w.monsterActionTimeLocked()
		result.Monster = *mon
		return result, nil
	}
	mon.Skeleton = true
	result.Skeleton = mon.Race >= 50 && mon.Race < 80
	if result.Skeleton {
		for i := range mon.CorpseItems {
			std, ok := w.data.Items[mon.CorpseItems[i].ItemID]
			if ok && std.StdMode == 40 {
				mon.CorpseItems[i].Dura = uint16(minInt(mon.MeatQuality, int(^uint16(0))))
			}
		}
	} else {
		mon.Skeleton = false
	}
	for len(mon.CorpseItems) > 0 {
		item := mon.CorpseItems[0]
		if !w.canCarryBagItemsLocked(ch, 1) {
			break
		}
		ch.BagItems = append(ch.BagItems, item)
		mon.CorpseItems = mon.CorpseItems[1:]
		result.AddedItems = append(result.AddedItems, item)
		result.AddedItemCharacters = append(result.AddedItemCharacters, cloneCharacterForMutation(ch))
		if !result.FoundItem {
			result.AddedItem = item
		}
		result.FoundItem = true
		result.Character = ch
	}
	mon.BodyLeathery = 50
	if !result.FoundItem {
		result.NothingFound = true
	}
	result.Character = ch
	result.Monster = *mon
	mon.DeathAt = w.monsterActionTimeLocked()
	result.Monster = *mon
	return result, nil
}

func (w *World) butcherTargetLocked(mapID string, actorID int32, x, y int) *Monster {
	var found *Monster
	for _, candidate := range w.monsters {
		if candidate == nil || candidate.MapID != mapID {
			continue
		}
		if MonsterActorID(*candidate) != actorID || candidate.X != x || candidate.Y != y {
			continue
		}
		if found == nil || monsterSnapshotBefore(*candidate, *found) {
			found = candidate
		}
	}
	return found
}

func (w *World) resetAnimalCorpseLocked(mon *Monster) {
	mon.Skeleton = false
	mon.BodyLeathery = 0
	mon.MeatQuality = 0
	mon.LastAttackAt = time.Time{}
	mon.CorpseItems = nil
	mon.CorpseGold = 0
}
