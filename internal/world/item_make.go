package world

import (
	"fmt"
	"strings"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

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

func (w *World) findItemByNameLocked(name string) (data.StdItem, bool) {
	for _, item := range w.data.Items {
		if strings.EqualFold(item.Name, name) || strings.EqualFold(item.ID, name) {
			return item, true
		}
	}
	return data.StdItem{}, false
}
