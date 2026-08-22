package world

import (
	"fmt"
	"time"

	"openmir2/internal/storage"
)

type PickupResult struct {
	Character   storage.Character
	Drop        GroundDrop
	GoldChanged bool
	Gold        int
	AddedItems  []storage.UserItem
}

func (w *World) addDropToBagLocked(ch storage.Character, drop GroundDrop) (storage.Character, []storage.UserItem) {
	if drop.ItemID == "金币" {
		ch.Gold += drop.Count
		return ch, nil
	}
	added := make([]storage.UserItem, 0, max(1, drop.Count))
	for i := 0; i < max(1, drop.Count); i++ {
		makeIndex := drop.MakeIndex + int32(i)
		if makeIndex <= 0 {
			makeIndex = int32(w.nextID)
			w.nextID++
		}
		entry := storage.UserItem{
			ItemID:    drop.ItemID,
			MakeIndex: makeIndex,
			Desc:      drop.Desc,
			Dura:      drop.Dura,
			DuraMax:   drop.DuraMax,
		}
		if entry.DuraMax == 0 {
			if item, ok := w.data.Items[drop.ItemID]; ok {
				entry.DuraMax = itemDuraMax(item)
				if entry.Dura == 0 {
					entry.Dura = entry.DuraMax
				}
			}
		}
		if entry.Dura == 0 && entry.DuraMax > 0 {
			entry.Dura = entry.DuraMax
		}
		ch.BagItems = append(ch.BagItems, entry)
		added = append(added, entry)
	}
	return ch, added
}

func (w *World) canCarryDropLocked(ch storage.Character, drop GroundDrop) bool {
	item, ok := w.data.Items[drop.ItemID]
	if !ok {
		return true
	}
	return w.canCarryWeightLocked(ch, item.Weight*drop.Count)
}

func (w *World) canPickupDropLocked(ch storage.Character, drop GroundDrop, now time.Time) bool {
	if drop.OwnerID == "" {
		return true
	}
	if drop.OwnerID == ch.ID {
		return true
	}
	if ch.GroupOwnerID != "" && ch.GroupOwnerID == drop.OwnerID {
		return true
	}
	if drop.PickupAt.IsZero() {
		return true
	}
	return !now.Before(drop.PickupAt)
}

func (w *World) pickupableDropAtLocked(mapID string, x, y int) (GroundDrop, bool) {
	var (
		found GroundDrop
		ok    bool
	)
	for _, drop := range w.drops {
		if drop.MapID != mapID || drop.X != x || drop.Y != y {
			continue
		}
		if !ok || idSeq(drop.ID) < idSeq(found.ID) {
			found = drop
			ok = true
		}
	}
	return found, ok
}

func (w *World) Pickup(ch storage.Character, dropID string) (storage.Character, GroundDrop, error) {
	updated, result, err := w.PickupWithResult(ch, dropID)
	return updated, result.Drop, err
}

func (w *World) PickupWithResult(ch storage.Character, dropID string) (storage.Character, PickupResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	drop, ok := w.drops[dropID]
	if !ok {
		return ch, PickupResult{}, fmt.Errorf("drop not found")
	}
	if !w.canPickupDropLocked(ch, drop, time.Now()) {
		return ch, PickupResult{}, fmt.Errorf("item %s is not yet pickable", drop.ItemID)
	}
	if drop.MapID != ch.MapID || abs(drop.X-ch.X) > 1 || abs(drop.Y-ch.Y) > 1 {
		return ch, PickupResult{}, fmt.Errorf("drop is out of range")
	}
	if !w.canCarryDropLocked(ch, drop) {
		return ch, PickupResult{}, fmt.Errorf("item %s is too heavy", drop.ItemID)
	}
	if drop.ItemID != "金币" && !w.canCarryBagItemsLocked(ch, drop.Count) {
		return ch, PickupResult{}, fmt.Errorf("bag is full")
	}
	updated, added := w.addDropToBagLocked(ch, drop)
	delete(w.drops, dropID)
	result := PickupResult{Character: updated, Drop: drop}
	if drop.ItemID == "金币" {
		result.GoldChanged = true
		result.Gold = updated.Gold
	} else {
		result.AddedItems = added
	}
	return updated, result, w.store.SaveCharacter(updated)
}

func (w *World) PickupAt(ch storage.Character, x, y int) (storage.Character, GroundDrop, error) {
	updated, result, err := w.PickupAtWithResult(ch, x, y)
	return updated, result.Drop, err
}

func (w *World) PickupAtWithResult(ch storage.Character, x, y int) (storage.Character, PickupResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ch.MapID == "" || ch.X != x || ch.Y != y {
		return ch, PickupResult{}, fmt.Errorf("drop is out of range")
	}
	drop, ok := w.pickupableDropAtLocked(ch.MapID, x, y)
	if !ok {
		return ch, PickupResult{}, fmt.Errorf("drop not found")
	}
	if !w.canPickupDropLocked(ch, drop, time.Now()) {
		return ch, PickupResult{}, fmt.Errorf("item %s is not yet pickable", drop.ItemID)
	}
	if !w.canCarryDropLocked(ch, drop) {
		return ch, PickupResult{}, fmt.Errorf("item %s is too heavy", drop.ItemID)
	}
	if drop.ItemID != "金币" && !w.canCarryBagItemsLocked(ch, drop.Count) {
		return ch, PickupResult{}, fmt.Errorf("bag is full")
	}
	updated, added := w.addDropToBagLocked(ch, drop)
	delete(w.drops, drop.ID)
	result := PickupResult{Character: updated, Drop: drop}
	if drop.ItemID == "金币" {
		result.GoldChanged = true
		result.Gold = updated.Gold
	} else {
		result.AddedItems = added
	}
	return updated, result, w.store.SaveCharacter(updated)
}
