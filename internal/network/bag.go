package network

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"openmir2/internal/data"
	"openmir2/internal/protocol/mir176"
	"openmir2/internal/storage"
	"openmir2/internal/world"
)

func (s *Server) sendBagAddItem(conn net.Conn, ch storage.Character, itemID string, makeIndex int32) bool {
	item, ok := s.world.Item(itemID)
	if !ok {
		return false
	}
	for _, entry := range ch.BagItems {
		if entry.ItemID != itemID {
			continue
		}
		if makeIndex > 0 && entry.MakeIndex != makeIndex {
			continue
		}
		item = world.UpgradeClientItemForDisplay(item, entry, true)
		dura, duraMax := bagItemDurability(item, entry)
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMAddItem, Recog: world.CharacterActorID(ch), Series: 1}, EncodeBuffer(ClientItemBody(item, entry.Desc, entry.MakeIndex, dura, duraMax)))
		return true
	}
	return false
}

func (s *Server) sendEquippedItems(conn net.Conn, ch storage.Character) {
	body := EquippedItemsBody(s.world, ch)
	if len(body) == 0 {
		return
	}
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMSendUseItems}, body)
}

func (s *Server) sendBagItems(conn net.Conn, ch storage.Character) {
	body, count := BagItemsBodyAndCount(s.world, ch)
	if len(body) == 0 {
		return
	}
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMBagItems, Recog: world.CharacterActorID(ch), Param: 0, Tag: 0, Series: uint16(count)}, body)
}

func EquippedItemsBody(w *world.World, ch storage.Character) []byte {
	itemEntries := []byte{}
	for slot := 0; slot < 13; slot++ {
		equipped, ok := equippedItem(ch, slot)
		if !ok {
			continue
		}
		item, ok := w.Item(equipped.ItemID)
		if !ok {
			continue
		}
		item = world.UpgradeClientItemForDisplay(item, equipped, false)
		dura, duraMax := bagItemDurability(item, equipped)
		client := ClientItemBody(item, equipped.Desc, equipped.MakeIndex, dura, duraMax)
		encoded := EncodeBuffer(client)
		if len(encoded) == 0 {
			continue
		}
		itemEntries = append(itemEntries, []byte(strconv.Itoa(slot))...)
		itemEntries = append(itemEntries, '/')
		itemEntries = append(itemEntries, encoded...)
		itemEntries = append(itemEntries, '/')
	}
	return itemEntries
}

func EquippedItemsChanged(prev, updated storage.Character) bool {
	for slot := 0; slot < 13; slot++ {
		if EquippedItemAt(prev, slot) != EquippedItemAt(updated, slot) {
			return true
		}
		if equippedItemDesc(prev, slot) != equippedItemDesc(updated, slot) {
			return true
		}
		if EquippedItemMakeIndex(prev, slot) != EquippedItemMakeIndex(updated, slot) {
			return true
		}
		if equippedItemDura(prev, slot) != equippedItemDura(updated, slot) {
			return true
		}
	}
	return false
}

func EquippedItemAt(ch storage.Character, slot int) string {
	item, ok := equippedItem(ch, slot)
	if !ok {
		return ""
	}
	return item.ItemID
}

func EquippedItemMakeIndex(ch storage.Character, slot int) int32 {
	item, ok := equippedItem(ch, slot)
	if !ok {
		return 0
	}
	return item.MakeIndex
}

func BagSummary(ch storage.Character) string {
	parts := make([]string, 0, len(ch.BagItems))
	for slot, entry := range ch.BagItems {
		parts = append(parts, fmt.Sprintf("%d:%s#%d", slot, entry.ItemID, entry.MakeIndex))
	}
	return strings.Join(parts, ", ")
}

func EquippedItemSummaryFromEntries(entries []storage.UserItem) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, fmt.Sprintf("%s#%d", entry.ItemID, entry.MakeIndex))
	}
	return strings.Join(parts, ", ")
}

func EquippedItemEntries(ch storage.Character) []storage.UserItem {
	entries := make([]storage.UserItem, 0, 13)
	for slot := 0; slot < 13; slot++ {
		if entry, ok := equippedItem(ch, slot); ok {
			entries = append(entries, storage.UserItem{ItemID: entry.ItemID, MakeIndex: entry.MakeIndex, Desc: entry.Desc, Dura: entry.Dura, DuraMax: entry.DuraMax})
		}
	}
	return entries
}

func BagItemsBody(w *world.World, ch storage.Character) []byte {
	body, _ := BagItemsBodyAndCount(w, ch)
	return body
}

func BagItemsBodyAndCount(w *world.World, ch storage.Character) ([]byte, int) {
	itemEntries := []byte{}
	count := 0
	for _, entry := range ch.BagItems {
		item, ok := w.Item(entry.ItemID)
		if !ok {
			continue
		}
		item = world.UpgradeClientItemForDisplay(item, entry, false)
		makeIndex := entry.MakeIndex
		dura, duraMax := bagItemDurability(item, entry)
		client := ClientItemBody(item, entry.Desc, makeIndex, dura, duraMax)
		encoded := EncodeBuffer(client)
		if len(encoded) == 0 {
			continue
		}
		itemEntries = append(itemEntries, encoded...)
		itemEntries = append(itemEntries, '/')
		count++
	}
	return itemEntries, count
}

func equippedItem(ch storage.Character, slot int) (storage.UserItem, bool) {
	if slot < 0 || slot >= 13 {
		return storage.UserItem{}, false
	}
	if ch.EquippedItems == nil {
		return storage.UserItem{}, false
	}
	item, ok := ch.EquippedItems[slot]
	if !ok || item.ItemID == "" {
		return storage.UserItem{}, false
	}
	return item, true
}

func itemFromEquipped(ch storage.Character, slot int) storage.UserItem {
	item, _ := equippedItem(ch, slot)
	return item
}

func equippedItemDesc(ch storage.Character, slot int) [14]byte {
	item, ok := equippedItem(ch, slot)
	if !ok {
		return [14]byte{}
	}
	return item.Desc
}

func equippedItemDura(ch storage.Character, slot int) uint16 {
	item, ok := equippedItem(ch, slot)
	if !ok {
		return 0
	}
	return item.Dura
}

func bagItemDurability(item data.StdItem, entry storage.UserItem) (uint16, uint16) {
	duraMax := entry.DuraMax
	if duraMax == 0 {
		duraMax = world.ItemDuraForEquip(item)
	}
	dura := entry.Dura
	if dura == 0 {
		dura = duraMax
	}
	return dura, duraMax
}
