package network

import (
	"net"
	"strconv"

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

func (s *Server) sendDelItem(conn net.Conn, ch storage.Character, removed storage.UserItem) bool {
	item, ok := s.world.Item(removed.ItemID)
	if !ok {
		return false
	}
	item = world.UpgradeClientItemForDisplay(item, removed, false)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMDelItem, Recog: world.CharacterActorID(ch), Series: 1}, EncodeBuffer(ClientItemBody(item, removed.Desc, removed.MakeIndex, removed.Dura, removed.DuraMax)))
	return true
}

func (s *Server) sendCharacterDeletedItems(conn net.Conn, ch storage.Character, removed []storage.UserItem) {
	for _, item := range removed {
		s.sendDelItem(conn, ch, item)
	}
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
