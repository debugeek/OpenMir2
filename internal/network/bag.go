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
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMAddItem, Recog: world.CharacterActorID(ch), Series: 1}, EncodeBuffer(itemBodyForAdd(ch, item, entry.Desc, entry.MakeIndex, entry.Dura, entry.DuraMax)))
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
	body := s.itemBodyForStateSync(conn, item, removed.Desc, removed.MakeIndex, removed.Dura, removed.DuraMax)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMDelItem, Recog: world.CharacterActorID(ch), Series: 1}, EncodeBuffer(body))
	return true
}

func (s *Server) sendUpdatedItem(conn net.Conn, ch storage.Character, updated storage.UserItem) bool {
	item, ok := s.world.Item(updated.ItemID)
	if !ok {
		return false
	}
	item = world.UpgradeClientItemForDisplay(item, updated, false)
	body := s.itemBodyForStateSync(conn, item, updated.Desc, updated.MakeIndex, updated.Dura, updated.DuraMax)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMUpdateItem, Recog: world.CharacterActorID(ch), Series: 1}, EncodeBuffer(body))
	return true
}

func (s *Server) sendCharacterDeletedItems(conn net.Conn, ch storage.Character, removed []storage.UserItem) {
	for _, item := range removed {
		s.sendDelItem(conn, ch, item)
	}
}

func (s *Server) sendEquippedItems(conn net.Conn, ch storage.Character) {
	body := s.equippedItemsBodyForClient(conn, ch)
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

func (s *Server) itemBodyForClient(conn net.Conn, item data.StdItem, desc [14]byte, makeIndex int32, dura, duraMax uint16) []byte {
	client := s.clientForConn(conn)
	if client != nil {
		client.mu.Lock()
		legacy := client.softVersion == 0 || client.clientTick == 0
		client.mu.Unlock()
		if legacy {
			return LegacyClientItemBody(item, makeIndex, dura, duraMax)
		}
	}
	return ClientItemBody(item, desc, makeIndex, dura, duraMax)
}

func (s *Server) itemBodyForStateSync(conn net.Conn, item data.StdItem, desc [14]byte, makeIndex int32, dura, duraMax uint16) []byte {
	client := s.clientForConn(conn)
	if client != nil {
		client.mu.Lock()
		legacy := client.softVersion == 0 && client.clientTick == 0
		client.mu.Unlock()
		if legacy {
			return LegacyClientItemBody(item, makeIndex, dura, duraMax)
		}
	}
	return ClientItemBody(item, desc, makeIndex, dura, duraMax)
}

func (s *Server) equippedItemsBodyForClient(conn net.Conn, ch storage.Character) []byte {
	client := s.clientForConn(conn)
	legacy := false
	if client != nil {
		client.mu.Lock()
		legacy = client.softVersion == 0 && client.clientTick == 0
		client.mu.Unlock()
	}
	return equippedItemsBody(s.world, ch, func(item data.StdItem, desc [14]byte, makeIndex int32, dura, duraMax uint16) []byte {
		if legacy {
			return LegacyEquippedItemBody(item, makeIndex, dura, duraMax)
		}
		return ClientEquippedItemBody(item, desc, makeIndex, dura, duraMax)
	})
}

func EquippedItemsBody(w *world.World, ch storage.Character) []byte {
	return equippedItemsBody(w, ch, nil)
}

func equippedItemsBody(w *world.World, ch storage.Character, bodyFor func(data.StdItem, [14]byte, int32, uint16, uint16) []byte) []byte {
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
		client := itemBodyForEquipped(ch, item, equipped.Desc, equipped.MakeIndex, equipped.Dura, equipped.DuraMax)
		if bodyFor != nil {
			client = bodyFor(item, equipped.Desc, equipped.MakeIndex, equipped.Dura, equipped.DuraMax)
		}
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
	return bagItemsBodyAndCount(w, ch, nil)
}

func (s *Server) bagItemsBodyAndCountForClient(conn net.Conn, ch storage.Character) ([]byte, int) {
	legacy := false
	if client := s.clientForConn(conn); client != nil {
		client.mu.Lock()
		legacy = client.softVersion == 0 || client.clientTick == 0
		client.mu.Unlock()
		s.log.Info("bag sync format", "legacy", legacy, "items", len(ch.BagItems))
	}
	return bagItemsBodyAndCount(s.world, ch, func(item data.StdItem, desc [14]byte, makeIndex int32, dura, duraMax uint16) []byte {
		if legacy {
			return LegacyBagItemBody(item, makeIndex, dura, duraMax)
		}
		return ClientBagItemBody(item, desc, makeIndex, dura, duraMax)
	})
}

func (s *Server) itemBodyWithoutTimedDurabilityForClient(conn net.Conn, item data.StdItem, desc [14]byte, makeIndex int32, dura, duraMax uint16) []byte {
	legacy := false
	if client := s.clientForConn(conn); client != nil {
		client.mu.Lock()
		legacy = client.softVersion == 0 && client.clientTick == 0
		client.mu.Unlock()
	}
	if legacy {
		return LegacyNoTimedDurabilityItemBody(item, makeIndex, dura, duraMax)
	}
	return ClientNoTimedDurabilityItemBody(item, desc, makeIndex, dura, duraMax)
}

func bagItemsBodyAndCount(w *world.World, ch storage.Character, bodyFor func(data.StdItem, [14]byte, int32, uint16, uint16) []byte) ([]byte, int) {
	itemEntries := []byte{}
	for _, entry := range ch.BagItems {
		item, ok := w.Item(entry.ItemID)
		if !ok {
			continue
		}
		item = world.UpgradeClientItemForDisplay(item, entry, false)
		makeIndex := entry.MakeIndex
		client := itemBodyForBag(ch, item, entry.Desc, makeIndex, entry.Dura, entry.DuraMax)
		if bodyFor != nil {
			client = bodyFor(item, entry.Desc, makeIndex, entry.Dura, entry.DuraMax)
		}
		encoded := EncodeBuffer(client)
		if len(encoded) == 0 {
			continue
		}
		itemEntries = append(itemEntries, encoded...)
		itemEntries = append(itemEntries, '/')
	}
	return itemEntries, len(ch.BagItems)
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

func itemBodyForAdd(ch storage.Character, item data.StdItem, desc [14]byte, makeIndex int32, dura, duraMax uint16) []byte {
	_ = ch
	return ClientItemBody(item, desc, makeIndex, dura, duraMax)
}

func itemBodyForBag(ch storage.Character, item data.StdItem, desc [14]byte, makeIndex int32, dura, duraMax uint16) []byte {
	_ = ch
	return ClientNoTimedDurabilityItemBody(item, desc, makeIndex, dura, duraMax)
}

func itemBodyForEquipped(ch storage.Character, item data.StdItem, desc [14]byte, makeIndex int32, dura, duraMax uint16) []byte {
	return ClientEquippedItemBody(item, desc, makeIndex, dura, duraMax)
}
