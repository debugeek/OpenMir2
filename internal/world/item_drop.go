package world

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"openmir2/internal/storage"
)

var (
	ErrGoldDropPlacement = errors.New("no available gold drop position")
	ErrItemDropPlacement = errors.New("no available item drop position")
	ErrDropForbidden     = errors.New("drop is forbidden")
	ErrDropInSafeZone    = errors.New("drop is forbidden in safe zone")
	ErrDropGoldTooSmall  = errors.New("gold drop is below the minimum")
	ErrDropCooldown      = errors.New("drop is temporarily unavailable")
)

func (w *World) DropGold(ch storage.Character, amount int, blockers ...storage.Character) (storage.Character, GroundDrop, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.gameplay.Combat.InSafeDisableDrop && w.isSafeZoneLocked(ch) {
		return ch, GroundDrop{}, ErrDropInSafeZone
	}
	if w.gameplay.Combat.ControlDropItem && amount < w.gameplay.Combat.CanDropGold {
		return ch, GroundDrop{}, ErrDropGoldTooSmall
	}
	if mapData, ok := w.data.Maps[ch.MapID]; ok && mapData.NoThrowItem {
		return ch, GroundDrop{}, ErrDropForbidden
	}
	if amount <= 0 || amount >= ch.Gold {
		return ch, GroundDrop{}, fmt.Errorf("invalid gold amount")
	}
	ch = cloneCharacterForMutation(ch)
	ch.Gold -= amount
	drop := GroundDrop{
		ID:                fmt.Sprintf("drop-%d", w.nextID),
		MapID:             ch.MapID,
		ItemID:            "金币",
		Count:             amount,
		SourceCharacterID: ch.ID,
		SourceMapID:       ch.MapID,
		SourceX:           ch.X,
		SourceY:           ch.Y,
	}
	w.nextID++
	placed := w.placeReferenceDropsLocked(ch.MapID, ch.X, ch.Y, 3, []GroundDrop{drop}, blockers...)
	if len(placed) == 0 {
		ch.Gold += amount
		return ch, GroundDrop{}, nil
	}
	return ch, placed[0], nil
}

// DropItemCountByBagIndex removes an item entry from the bag using the
// reference-style MakeIndex identity before placing it on the ground.
func (w *World) DropItemCountByBagIndex(ch storage.Character, bagIndex int, itemID string, blockers ...storage.Character) (storage.Character, GroundDrop, error) {
	return w.dropItemCountByBagIndex(ch, bagIndex, itemID, false, blockers...)
}

func (w *World) DropItemCountByBagIndexWithCooldown(ch storage.Character, bagIndex int, itemID string, cooldown bool, blockers ...storage.Character) (storage.Character, GroundDrop, error) {
	return w.dropItemCountByBagIndex(ch, bagIndex, itemID, cooldown, blockers...)
}

func (w *World) dropItemCountByBagIndex(ch storage.Character, bagIndex int, itemID string, cooldown bool, blockers ...storage.Character) (storage.Character, GroundDrop, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	ch = cloneCharacterForMutation(ch)
	if w.gameplay.Combat.InSafeDisableDrop && w.isSafeZoneLocked(ch) {
		return ch, GroundDrop{}, ErrDropInSafeZone
	}
	if mapData, ok := w.data.Maps[ch.MapID]; ok && mapData.NoThrowItem {
		return ch, GroundDrop{}, ErrDropForbidden
	}
	if cooldown {
		return ch, GroundDrop{}, ErrDropCooldown
	}
	idx := w.findBagItemByMakeIndexLocked(ch, int32(bagIndex))
	if idx < 0 {
		return ch, GroundDrop{}, fmt.Errorf("item %s not in bag", itemID)
	}
	entry := ch.BagItems[idx]
	item, itemOK := w.data.Items[entry.ItemID]
	if !itemOK || !strings.EqualFold(item.Name, itemID) {
		return ch, GroundDrop{}, fmt.Errorf("item %s not in bag", itemID)
	}
	if w.gameplay.Combat.ControlDropItem && item.Price < w.gameplay.Combat.CanDropPrice {
		w.clearBagItemLocked(&ch, idx)
		return ch, GroundDrop{}, nil
	}
	if item.StdMode == 40 {
		if entry.Dura > 2000 {
			entry.Dura -= 2000
		} else {
			entry.Dura = 0
		}
	}
	drop := GroundDrop{
		ID:                fmt.Sprintf("drop-%d", w.nextID),
		MapID:             ch.MapID,
		ItemID:            entry.ItemID,
		Count:             1,
		MakeIndex:         entry.MakeIndex,
		Dura:              entry.Dura,
		DuraMax:           entry.DuraMax,
		Desc:              entry.Desc,
		SourceCharacterID: ch.ID,
		SourceMapID:       ch.MapID,
		SourceX:           ch.X,
		SourceY:           ch.Y,
	}
	ch.BagItems[idx] = entry
	w.nextID++
	placed := w.placeReferenceItemDropLocked(ch.MapID, ch.X, ch.Y, []GroundDrop{drop}, blockers...)
	if len(placed) == 0 {
		return ch, GroundDrop{}, ErrItemDropPlacement
	}
	w.clearBagItemLocked(&ch, idx)
	return ch, placed[0], nil
}

func (w *World) placeDropsLocked(mapID string, x, y, searchRadius int, drops []GroundDrop, blockers ...storage.Character) []GroundDrop {
	if len(drops) == 0 {
		return nil
	}
	maxPerTile := w.gameplay.Item.FloorDropMaxStackPerTile
	if maxPerTile <= 0 {
		maxPerTile = 5
	}
	return w.placeDropsWithPolicyLocked(mapID, x, y, searchRadius, drops, maxPerTile, blockers...)
}

func (w *World) placeReferenceItemDropLocked(mapID string, x, y int, drops []GroundDrop, blockers ...storage.Character) []GroundDrop {
	return w.placeReferenceDropsLocked(mapID, x, y, 1, drops, blockers...)
}

func (w *World) placeReferenceDropsLocked(mapID string, x, y, searchRadius int, drops []GroundDrop, blockers ...storage.Character) []GroundDrop {
	return w.placeDropsWithPolicyLocked(mapID, x, y, searchRadius, drops, 8, blockers...)
}

func (w *World) placeDropsWithPolicyLocked(mapID string, x, y, searchRadius int, drops []GroundDrop, maxPerTile int, blockers ...storage.Character) []GroundDrop {
	if len(drops) == 0 {
		return nil
	}
	blocked := map[monsterPosition]struct{}{}
	for _, blocker := range blockers {
		if blocker.MapID != mapID || blocker.ID == "" || blocker.Ghost || (blocker.MaxHP > 0 && blocker.HP <= 0) {
			continue
		}
		blocked[monsterPosition{MapID: blocker.MapID, X: blocker.X, Y: blocker.Y}] = struct{}{}
	}
	for _, mon := range w.monsters {
		if mon == nil || mon.MapID != mapID || !mon.Alive || mon.Ghost {
			continue
		}
		blocked[monsterPosition{MapID: mon.MapID, X: mon.X, Y: mon.Y}] = struct{}{}
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
		if drop.ItemID == "金币" {
			merged := false
			for id, existing := range w.drops {
				if existing.MapID != drop.MapID || existing.X != cell.X || existing.Y != cell.Y || existing.ItemID != "金币" {
					continue
				}
				if existing.Count+drop.Count > 2000 {
					continue
				}
				existing.Count += drop.Count
				existing.CreatedAt = time.Now()
				w.drops[id] = existing
				placed = append(placed, existing)
				merged = true
				break
			}
			if merged {
				continue
			}
		}
		if drop.CreatedAt.IsZero() {
			drop.CreatedAt = time.Now()
		}
		if drop.ObjectOrder == 0 {
			drop.ObjectOrder = w.nextObjectOrder
			w.nextObjectOrder++
		}
		drop.X = cell.X
		drop.Y = cell.Y
		w.drops[drop.ID] = drop
		counts[cell]++
		placed = append(placed, drop)
	}
	return placed
}

func (w *World) pickDropPositionLocked(mapID string, x, y, searchRadius int, blocked map[monsterPosition]struct{}, counts map[monsterPosition]int, maxPerTile int) (monsterPosition, bool) {
	_, ok := w.data.Maps[mapID]
	if !ok {
		return monsterPosition{}, false
	}
	if searchRadius < 1 {
		searchRadius = 1
	}
	best := monsterPosition{}
	bestCount := maxPerTile
	foundBest := false
	seen := map[monsterPosition]struct{}{}
	for radius := 1; radius <= searchRadius; radius++ {
		for yy := y - radius; yy <= y+radius; yy++ {
			for xx := x - radius; xx <= x+radius; xx++ {
				cell := monsterPosition{MapID: mapID, X: xx, Y: yy}
				if _, visited := seen[cell]; visited {
					continue
				}
				seen[cell] = struct{}{}
				if w.dropPositionBlockedLocked(mapID, xx, yy, blocked) {
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
	if !w.dropPositionBlockedLocked(mapID, x, y, blocked) {
		return monsterPosition{MapID: mapID, X: x, Y: y}, true
	}
	return monsterPosition{}, false
}

func (w *World) dropCandidatesLocked(mapID string, x, y, searchRadius int, blocked map[monsterPosition]struct{}) []monsterPosition {
	_, ok := w.data.Maps[mapID]
	if !ok {
		return nil
	}
	if searchRadius < 1 {
		searchRadius = 1
	}
	candidates := []monsterPosition{}
	seen := map[monsterPosition]struct{}{}
	for radius := 1; radius <= searchRadius; radius++ {
		for yy := y - radius; yy <= y+radius; yy++ {
			for xx := x - radius; xx <= x+radius; xx++ {
				cell := monsterPosition{MapID: mapID, X: xx, Y: yy}
				if _, ok := seen[cell]; ok {
					continue
				}
				seen[cell] = struct{}{}
				if w.dropPositionBlockedLocked(mapID, xx, yy, blocked) {
					continue
				}
				candidates = append(candidates, cell)
			}
		}
	}
	return candidates
}

func (w *World) dropPositionBlockedLocked(mapID string, x, y int, blocked map[monsterPosition]struct{}) bool {
	cell := monsterPosition{MapID: mapID, X: x, Y: y}
	if _, used := blocked[cell]; used {
		return true
	}
	mp, ok := w.data.Maps[mapID]
	if !ok || !mp.Walkable(x, y) {
		return true
	}
	for _, connection := range mp.Connections {
		if connection.FromX == x && connection.FromY == y {
			return true
		}
	}
	return false
}
