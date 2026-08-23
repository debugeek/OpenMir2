package world

import (
	"fmt"
	"strings"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func (w *World) MakeRecipe(itemName string) ([]data.StdMakeIngredient, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	recipe, ok := w.data.MakeItems[itemName]
	if !ok {
		return nil, false
	}
	return append([]data.StdMakeIngredient(nil), recipe...), true
}

func (w *World) MakeItemsByName(ch storage.Character, itemName string, count int) (storage.Character, []storage.UserItem, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if itemName == "" {
		return ch, nil, fmt.Errorf("item name required")
	}
	if count <= 0 {
		return ch, nil, fmt.Errorf("invalid item count")
	}
	item, ok := w.findItemByNameLocked(itemName)
	if !ok {
		return ch, nil, fmt.Errorf("item %s not found", itemName)
	}
	if w.normalizeBagItemMakeIndexesLocked(&ch) {
		if err := w.store.SaveCharacter(ch); err != nil {
			return ch, nil, err
		}
	}
	added := make([]storage.UserItem, 0, count)
	for i := 0; i < count; i++ {
		if len(ch.BagItems) >= w.gameplay.Item.MaxBagItem {
			break
		}
		entry := w.createUserItemFromStd(item, 0, [14]byte{})
		ch.BagItems = append(ch.BagItems, entry)
		added = append(added, entry)
	}
	if len(added) == 0 {
		return ch, nil, nil
	}
	return ch, added, w.store.SaveCharacter(ch)
}

func (w *World) ConsumeMakeIngredients(ch storage.Character, itemName string) (storage.Character, []storage.UserItem, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	recipe, ok := w.data.MakeItems[itemName]
	if !ok {
		return ch, nil, fmt.Errorf("item %s not found", itemName)
	}
	if w.normalizeBagItemMakeIndexesLocked(&ch) {
		if err := w.store.SaveCharacter(ch); err != nil {
			return ch, nil, err
		}
	}
	for _, ingredient := range recipe {
		if ingredient.Count <= 0 || ingredient.ItemID == "" {
			return ch, nil, fmt.Errorf("invalid recipe for %s", itemName)
		}
		have := 0
		for _, entry := range ch.BagItems {
			if entry.ItemID == ingredient.ItemID {
				have++
			}
		}
		if have < ingredient.Count {
			return ch, nil, fmt.Errorf("missing ingredient %s", ingredient.ItemID)
		}
	}
	removed := make([]storage.UserItem, 0, len(recipe))
	for _, ingredient := range recipe {
		need := ingredient.Count
		for i := len(ch.BagItems) - 1; i >= 0 && need > 0; i-- {
			entry := ch.BagItems[i]
			if entry.ItemID != ingredient.ItemID {
				continue
			}
			removed = append(removed, entry)
			ch.BagItems = append(ch.BagItems[:i], ch.BagItems[i+1:]...)
			need--
		}
	}
	return ch, removed, nil
}

func (w *World) findItemByNameLocked(name string) (data.StdItem, bool) {
	for _, item := range w.data.Items {
		if strings.EqualFold(item.Name, name) || strings.EqualFold(item.ID, name) {
			return item, true
		}
	}
	return data.StdItem{}, false
}
