package world

import (
	"openmir2/internal/data"
	"openmir2/internal/storage"
)

const itemMakeIndexStart int32 = 1
const itemMakeIndexMax int32 = 1<<30 - 2

func (w *World) nextItemMakeIndexLocked() int32 {
	if w.nextItemID < itemMakeIndexStart || w.nextItemID > itemMakeIndexMax {
		w.nextItemID = itemMakeIndexStart
	}
	makeIndex := w.nextItemID
	w.nextItemID++
	if w.nextItemID > itemMakeIndexMax {
		w.nextItemID = itemMakeIndexStart
	}
	return makeIndex
}

func (w *World) AllocateItemMakeIndex() int32 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.nextItemMakeIndexLocked()
}

// createUserItemFromStd must only be used when a brand-new item instance is
// first created from a StdItem template.
func (w *World) createUserItemFromStd(item data.StdItem, makeIndex int32, desc [14]byte) storage.UserItem {
	if makeIndex <= 0 {
		makeIndex = w.nextItemMakeIndexLocked()
	} else if makeIndex <= itemMakeIndexMax && makeIndex >= w.nextItemID {
		next := makeIndex + 1
		if next > itemMakeIndexMax {
			next = itemMakeIndexStart
		}
		w.nextItemID = next
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
