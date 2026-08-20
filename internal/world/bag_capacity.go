package world

import "openmir2/internal/storage"

func (w *World) canCarryLocked(ch storage.Character) bool {
	return w.canCarryWeightLocked(ch, 0)
}

func (w *World) canCarryWeightLocked(ch storage.Character, addWeight int) bool {
	level := ch.Level
	if level <= 0 {
		level = 1
	}
	base := Base(ch.Class, level)
	return w.bagItemsWeightLocked(ch)+addWeight <= base.MaxWeight
}

func (w *World) canCarryBagItemsLocked(ch storage.Character, addItems int) bool {
	if addItems <= 0 {
		return true
	}
	return w.bagItemCountLocked(ch)+addItems <= w.gameplay.Item.MaxBagItem
}

func (w *World) bagItemsWeightLocked(ch storage.Character) int {
	total := 0
	for _, entry := range ch.BagItems {
		if item, ok := w.data.Items[entry.ItemID]; ok {
			total += item.Weight
		}
	}
	return total
}
