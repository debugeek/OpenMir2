package world

import (
	"errors"
	"fmt"
	"time"

	"openmir2/internal/storage"
)

var ErrPickupNotAllowed = errors.New("item is not yet pickable")

type PickupTooHeavyError struct {
	Over int
}

func (e *PickupTooHeavyError) Error() string {
	return fmt.Sprintf("item is too heavy by %d", e.Over)
}

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
		entry := storage.UserItem{
			ItemID:    drop.ItemID,
			MakeIndex: makeIndex,
			Desc:      drop.Desc,
			Dura:      drop.Dura,
			DuraMax:   drop.DuraMax,
		}
		ch.BagItems = append(ch.BagItems, entry)
		added = append(added, entry)
	}
	return ch, added
}

func (w *World) canCarryDropLocked(ch storage.Character, drop GroundDrop) bool {
	item, ok := w.data.Items[drop.ItemID]
	if !ok {
		return false
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
	if drop.OwnerGroupID != "" && drop.OwnerGroupID == ch.GroupOwnerID {
		return true
	}
	if ch.GroupOwnerID != "" && ch.GroupOwnerID == drop.OwnerID {
		return true
	}
	if drop.PickupAt.IsZero() {
		return true
	}
	return now.After(drop.PickupAt)
}

func (w *World) releaseExpiredDropOwnerLocked(drop *GroundDrop, now time.Time) {
	if drop == nil || drop.PickupAt.IsZero() || !now.After(drop.PickupAt) || drop.OwnerID == "" && drop.OwnerGroupID == "" {
		return
	}
	drop.OwnerID = ""
	drop.OwnerGroupID = ""
	w.drops[drop.ID] = *drop
}

func (w *World) restoreDropToMapLocked(drop GroundDrop) {
	if w.nextObjectOrder > 0 {
		drop.ObjectOrder = w.nextObjectOrder
		w.nextObjectOrder++
	}
	w.drops[drop.ID] = drop
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
		if !ok || groundDropBefore(drop, found) {
			found = drop
			ok = true
		}
	}
	return found, ok
}

func groundDropBefore(candidate, current GroundDrop) bool {
	if candidate.ObjectOrder != 0 || current.ObjectOrder != 0 {
		if candidate.ObjectOrder == 0 {
			return false
		}
		if current.ObjectOrder == 0 {
			return true
		}
		if candidate.ObjectOrder != current.ObjectOrder {
			return candidate.ObjectOrder < current.ObjectOrder
		}
	}
	return idSeq(candidate.ID) < idSeq(current.ID)
}

func (w *World) PickupWithResult(ch storage.Character, dropID string) (storage.Character, PickupResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	ch = cloneCharacterForMutation(ch)
	drop, ok := w.drops[dropID]
	if !ok {
		return ch, PickupResult{}, fmt.Errorf("drop not found")
	}
	now := time.Now()
	w.releaseExpiredDropOwnerLocked(&drop, now)
	if !w.canPickupDropLocked(ch, drop, now) {
		return ch, PickupResult{}, fmt.Errorf("%w: %s", ErrPickupNotAllowed, drop.ItemID)
	}
	if drop.MapID != ch.MapID || abs(drop.X-ch.X) > 1 || abs(drop.Y-ch.Y) > 1 {
		return ch, PickupResult{}, fmt.Errorf("drop is out of range")
	}
	if drop.ItemID != "金币" && !w.canCarryBagItemsLocked(ch, drop.Count) {
		return ch, PickupResult{}, fmt.Errorf("bag is full")
	}
	delete(w.drops, dropID)
	if drop.ItemID == "金币" && w.gameplay.Item.MaxGold > 0 && ch.Gold+drop.Count > w.gameplay.Item.MaxGold {
		w.restoreDropToMapLocked(drop)
		return ch, PickupResult{}, fmt.Errorf("gold limit reached")
	}
	if drop.ItemID != "金币" && !w.canCarryDropLocked(ch, drop) {
		item := w.data.Items[drop.ItemID]
		w.restoreDropToMapLocked(drop)
		return ch, PickupResult{}, &PickupTooHeavyError{Over: w.bagItemsWeightLocked(ch) + item.Weight*drop.Count - w.maxCarryWeightLocked(ch)}
	}
	updated, added := w.addDropToBagLocked(ch, drop)
	result := PickupResult{Character: updated, Drop: drop}
	if drop.ItemID == "金币" {
		result.GoldChanged = true
		result.Gold = updated.Gold
	} else {
		result.AddedItems = added
	}
	return updated, result, nil
}

func (w *World) PickupAt(ch storage.Character, x, y int) (storage.Character, GroundDrop, error) {
	updated, result, err := w.PickupAtWithResult(ch, x, y)
	return updated, result.Drop, err
}

func (w *World) PickupAtWithResult(ch storage.Character, x, y int) (storage.Character, PickupResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	ch = cloneCharacterForMutation(ch)
	if ch.MapID == "" || ch.X != x || ch.Y != y {
		return ch, PickupResult{}, fmt.Errorf("drop is out of range")
	}
	mp, ok := w.data.Maps[ch.MapID]
	if !ok || !mp.Walkable(x, y) {
		return ch, PickupResult{}, fmt.Errorf("drop not found")
	}
	drop, ok := w.pickupableDropAtLocked(ch.MapID, x, y)
	if !ok {
		return ch, PickupResult{}, fmt.Errorf("drop not found")
	}
	now := time.Now()
	w.releaseExpiredDropOwnerLocked(&drop, now)
	if !w.canPickupDropLocked(ch, drop, now) {
		return ch, PickupResult{}, fmt.Errorf("%w: %s", ErrPickupNotAllowed, drop.ItemID)
	}
	if drop.ItemID != "金币" && !w.canCarryBagItemsLocked(ch, drop.Count) {
		return ch, PickupResult{}, fmt.Errorf("bag is full")
	}
	delete(w.drops, drop.ID)
	if drop.ItemID == "金币" && w.gameplay.Item.MaxGold > 0 && ch.Gold+drop.Count > w.gameplay.Item.MaxGold {
		w.restoreDropToMapLocked(drop)
		return ch, PickupResult{}, fmt.Errorf("gold limit reached")
	}
	if drop.ItemID != "金币" && !w.canCarryDropLocked(ch, drop) {
		item := w.data.Items[drop.ItemID]
		w.restoreDropToMapLocked(drop)
		return ch, PickupResult{}, &PickupTooHeavyError{Over: w.bagItemsWeightLocked(ch) + item.Weight*drop.Count - w.maxCarryWeightLocked(ch)}
	}
	updated, added := w.addDropToBagLocked(ch, drop)
	result := PickupResult{Character: updated, Drop: drop}
	if drop.ItemID == "金币" {
		result.GoldChanged = true
		result.Gold = updated.Gold
	} else {
		result.AddedItems = added
	}
	return updated, result, nil
}
