package network

import (
	"bytes"
	"fmt"
	"math"
	"net"
	"strings"

	"openmir2/internal/data"
	"openmir2/internal/npc"
	"openmir2/internal/protocol/mir176"
	"openmir2/internal/storage"
	"openmir2/internal/world"
)

const makeDrugPrice = 100

func (s *Server) sendMerchantMenu(conn net.Conn, merchantID int32, ch storage.Character, entity npc.Entity, label string) bool {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "@buy":
		if entity.Merchant.Capabilities.Buy {
			s.sendMerchantBuyList(conn, merchantID, entity)
			return true
		}
	case "@sell":
		if entity.Merchant.Capabilities.Sell {
			s.sendMerchantSellWindow(conn, merchantID)
			return true
		}
	case "@repair", "@s_repair":
		if entity.Merchant.Capabilities.Repair {
			s.sendMerchantRepairWindow(conn, merchantID)
			return true
		}
	case "@storage":
		if entity.Merchant.Capabilities.Storage {
			s.sendMerchantStorageWindow(conn, merchantID, ch)
			return true
		}
	case "@getback":
		if entity.Merchant.Capabilities.GetBack {
			s.sendMerchantGetBackList(conn, merchantID, ch)
			return true
		}
	case "@makedrug":
		if s.sendMerchantMakeDrugList(conn, merchantID, entity) {
			return true
		}
	}
	return false
}

func (s *Server) sendMerchantBuyList(conn net.Conn, merchantID int32, entity npc.Entity) {
	body, count := merchantGoodsListBody(s.world, s.world.MerchantStock(entity.ID), entity)
	if count == 0 {
		return
	}
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMSendGoodsList, Recog: merchantID, Param: uint16(count)}, EncodeString(body))
}

func merchantDetailGoodsListBody(w *world.World, stocks []npc.MerchantStockItem, itemName string, page, rate int) ([]byte, int, int) {
	var body bytes.Buffer
	count := 0
	if len(stocks) == 0 {
		return nil, 0, 0
	}
	if page >= len(stocks) {
		page = len(stocks) - 10
		if page < 0 {
			page = 0
		}
	}
	for _, stock := range stocks {
		item, ok := w.Item(stock.ItemID)
		if !ok {
			continue
		}
		if !strings.EqualFold(item.Name, itemName) {
			continue
		}
		price := merchantPrice(item, rate)
		if price <= 0 {
			continue
		}
		repeat := stock.Count
		if repeat > 10 {
			repeat = 10
		}
		dura := uint16(item.DuraMax)
		if dura == 0 {
			dura = uint16(price)
		}
		for i := repeat - 1; i >= 0; i-- {
			display := item
			display.Price = price
			body.Write(EncodeBuffer(ClientItemBody(display, [14]byte{}, int32(i+1), dura, uint16(price))))
			body.WriteByte('/')
			count++
		}
		break
	}
	return body.Bytes(), count, page
}

func (s *Server) sendMerchantSellWindow(conn net.Conn, merchantID int32) {
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMSendUserSell, Recog: merchantID}, nil)
}

func (s *Server) sendMerchantRepairWindow(conn net.Conn, merchantID int32) {
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMSendUserRepair, Recog: merchantID}, nil)
}

func (s *Server) sendMerchantStorageWindow(conn net.Conn, merchantID int32, ch storage.Character) {
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMSendUserStorageItem, Param: uint16(merchantID)}, nil)
}

func (s *Server) sendMerchantGetBackList(conn net.Conn, merchantID int32, ch storage.Character) {
	body, count := storageItemListBody(s.world, ch)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMSaveItemList, Recog: merchantID, Series: uint16(count)}, EncodeBuffer(body))
}

func (s *Server) sendMerchantMakeDrugList(conn net.Conn, merchantID int32, entity npc.Entity) bool {
	body, count := merchantMakeDrugListBody(s.world, s.world.MerchantStock(entity.ID))
	if count == 0 {
		return false
	}
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMSendUserMakeDrugItemList, Param: uint16(merchantID), Series: uint16(count)}, EncodeString(body))
	return true
}

func merchantMakeDrugListBody(w *world.World, stocks []npc.MerchantStockItem) (string, int) {
	var b strings.Builder
	count := 0
	for _, stock := range stocks {
		item, ok := w.Item(stock.ItemID)
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "%s/%d/%d/%d/", item.Name, 0, makeDrugPrice, 1)
		count++
	}
	return b.String(), count
}

func merchantGoodsListBody(w *world.World, stocks []npc.MerchantStockItem, entity npc.Entity) (string, int) {
	var b strings.Builder
	count := 0
	for _, stock := range stocks {
		item, ok := w.Item(stock.ItemID)
		if !ok {
			continue
		}
		price := merchantPrice(item, entity.Merchant.PriceRate)
		if price <= 0 {
			continue
		}
		subMenu := 1
		if item.StdMode <= 4 || item.StdMode == 31 || item.StdMode == 42 {
			subMenu = 0
		}
		fmt.Fprintf(&b, "%s/%d/%d/%d/", item.Name, subMenu, price, stock.Count)
		count++
	}
	return b.String(), count
}

func storageItemListBody(w *world.World, ch storage.Character) ([]byte, int) {
	var body bytes.Buffer
	count := 0
	for _, entry := range ch.StorageItems {
		item, ok := w.Item(entry.ItemID)
		if !ok {
			continue
		}
		display := world.UpgradeClientItemForDisplay(item, entry, false)
		dura, duraMax := bagItemDurability(display, entry)
		body.Write(EncodeBuffer(ClientItemBody(display, entry.Desc, entry.MakeIndex, dura, duraMax)))
		body.WriteByte('/')
		count++
	}
	return body.Bytes(), count
}

func merchantPrice(item data.StdItem, rate int) int {
	if rate <= 0 {
		rate = 100
	}
	return int(math.Round(float64(item.Price) * float64(rate) / 100))
}
