package world

import (
	"openmir2/internal/data"
	"openmir2/internal/storage"
)

// createUserItemFromStd must only be used when a brand-new item instance is
// first created from a StdItem template.
func (w *World) createUserItemFromStd(item data.StdItem, makeIndex int32, desc [14]byte) storage.UserItem {
	if makeIndex <= 0 {
		makeIndex = int32(w.nextID)
		w.nextID++
	} else if next := int(makeIndex) + 1; next > w.nextID {
		w.nextID = next
	}
	dura := itemDuraMax(item)
	return storage.UserItem{
		ItemID:    item.ID,
		MakeIndex: makeIndex,
		Desc:      desc,
		Dura:      dura,
		DuraMax:   dura,
	}
}
