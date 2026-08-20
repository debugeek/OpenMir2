package world

import (
	"fmt"
	"time"

	"openmir2/internal/storage"
)

func (w *World) DropItem(ch storage.Character, itemID string) (storage.Character, GroundDrop, error) {
	return w.DropItemCount(ch, itemID, 0)
}

func (w *World) DropItemCount(ch storage.Character, itemID string, count int, blockers ...storage.Character) (storage.Character, GroundDrop, error) {
	return w.DropItemCountByBagIndex(ch, 0, itemID, count, blockers...)
}

// DropItemCountByBagIndex removes an item entry from the bag using the
// reference-style MakeIndex identity before placing it on the ground.
func (w *World) DropItemCountByBagIndex(ch storage.Character, bagIndex int, itemID string, count int, blockers ...storage.Character) (storage.Character, GroundDrop, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.normalizeBagItemMakeIndexesLocked(&ch) {
		if err := w.store.SaveCharacter(ch); err != nil {
			return ch, GroundDrop{}, err
		}
	}
	idx := w.findBagItemSlotLocked(ch, itemID, int32(bagIndex))
	if idx < 0 && bagIndex > 0 {
		idx = w.findBagItemSlotLocked(ch, itemID, 0)
	}
	if idx < 0 {
		return ch, GroundDrop{}, fmt.Errorf("item %s not in bag", itemID)
	}
	entry := ch.BagItems[idx]
	w.clearBagItemLocked(&ch, idx)
	w.pruneStaleEquippedItemsLocked(&ch)
	drop := GroundDrop{
		ID:        fmt.Sprintf("drop-%d", w.nextID),
		MapID:     ch.MapID,
		ItemID:    entry.ItemID,
		Count:     1,
		MakeIndex: entry.MakeIndex,
		Dura:      entry.Dura,
		DuraMax:   entry.DuraMax,
		OwnerID:   ch.ID,
		PickupAt:  time.Now().Add(time.Duration(w.gameplay.Item.FloorItemCanPickUpMS) * time.Millisecond),
		Desc:      entry.Desc,
	}
	w.nextID++
	placed := w.placeDropsLocked(ch.MapID, ch.X, ch.Y, 3, []GroundDrop{drop}, blockers...)
	if len(placed) == 0 {
		return ch, GroundDrop{}, fmt.Errorf("no available drop position")
	}
	return ch, placed[0], w.store.SaveCharacter(ch)
}

func (w *World) placeDropsLocked(mapID string, x, y, searchRadius int, drops []GroundDrop, blockers ...storage.Character) []GroundDrop {
	if len(drops) == 0 {
		return nil
	}
	maxPerTile := w.gameplay.Item.FloorDropMaxStackPerTile
	if maxPerTile <= 0 {
		maxPerTile = 5
	}
	blocked := map[monsterPosition]struct{}{}
	for _, blocker := range blockers {
		if blocker.MapID != mapID {
			continue
		}
		blocked[monsterPosition{MapID: blocker.MapID, X: blocker.X, Y: blocker.Y}] = struct{}{}
	}
	candidates := w.dropCandidatesLocked(mapID, x, y, searchRadius, blocked)
	if len(candidates) == 0 {
		return nil
	}
	counts := map[monsterPosition]int{}
	for _, drop := range w.drops {
		if drop.MapID != mapID {
			continue
		}
		counts[monsterPosition{MapID: drop.MapID, X: drop.X, Y: drop.Y}]++
	}
	placed := []GroundDrop{}
	pending := append([]GroundDrop(nil), drops...)
	for len(pending) > 0 {
		cell, ok := w.pickDropPositionLocked(mapID, x, y, searchRadius, blocked, counts, maxPerTile)
		if !ok {
			break
		}
		drop := pending[0]
		pending = pending[1:]
		drop.X = cell.X
		drop.Y = cell.Y
		w.drops[drop.ID] = drop
		counts[cell]++
		placed = append(placed, drop)
	}
	return placed
}

func (w *World) pickDropPositionLocked(mapID string, x, y, searchRadius int, blocked map[monsterPosition]struct{}, counts map[monsterPosition]int, maxPerTile int) (monsterPosition, bool) {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return monsterPosition{}, false
	}
	if searchRadius < 1 {
		searchRadius = 1
	}
	best := monsterPosition{}
	bestCount := maxPerTile
	foundBest := false
	for radius := 1; radius <= searchRadius; radius++ {
		for yy := y - radius; yy <= y+radius; yy++ {
			for xx := x - radius; xx <= x+radius; xx++ {
				if xx == x && yy == y {
					continue
				}
				cell := monsterPosition{MapID: mapID, X: xx, Y: yy}
				if _, used := blocked[cell]; used {
					continue
				}
				if !mp.Walkable(xx, yy) || w.monsterAtLocked(mapID, xx, yy, "") {
					continue
				}
				count := counts[cell]
				if count == 0 {
					return cell, true
				}
				if count < bestCount {
					best = cell
					bestCount = count
					foundBest = true
				}
			}
		}
	}
	if foundBest && bestCount < maxPerTile {
		return best, true
	}
	return monsterPosition{}, false
}

func (w *World) dropCandidatesLocked(mapID string, x, y, searchRadius int, blocked map[monsterPosition]struct{}) []monsterPosition {
	mp, ok := w.data.Maps[mapID]
	if !ok {
		return nil
	}
	if searchRadius < 1 {
		searchRadius = 1
	}
	candidates := []monsterPosition{}
	seen := map[monsterPosition]struct{}{}
	for radius := 1; radius <= searchRadius; radius++ {
		for _, offset := range dropRingOffsets(radius) {
			xx := x + offset[0]
			yy := y + offset[1]
			cell := monsterPosition{MapID: mapID, X: xx, Y: yy}
			if _, ok := seen[cell]; ok {
				continue
			}
			seen[cell] = struct{}{}
			if _, used := blocked[cell]; used {
				continue
			}
			if !mp.Walkable(xx, yy) || w.monsterAtLocked(mapID, xx, yy, "") {
				continue
			}
			candidates = append(candidates, cell)
		}
	}
	return candidates
}

func dropRingOffsets(radius int) [][2]int {
	offsets := make([][2]int, 0, radius*8)
	for dx := -radius; dx <= radius; dx++ {
		offsets = append(offsets, [2]int{dx, -radius})
	}
	for dy := -radius + 1; dy <= radius; dy++ {
		offsets = append(offsets, [2]int{radius, dy})
	}
	for dx := radius - 1; dx >= -radius; dx-- {
		offsets = append(offsets, [2]int{dx, radius})
	}
	for dy := radius - 1; dy >= -radius+1; dy-- {
		offsets = append(offsets, [2]int{-radius, dy})
	}
	return offsets
}
