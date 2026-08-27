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

func merchantDetailGoodsListBody(w *world.World, stocks []storage.UserItem, itemName string, page, rate int) ([]byte, int, int) {
	var body bytes.Buffer
	if len(stocks) == 0 {
		return nil, 0, 0
	}
	matches := make([]storage.UserItem, 0, len(stocks))
	for _, stock := range stocks {
		item, ok := w.Item(stock.ItemID)
		if !ok || !strings.EqualFold(item.Name, itemName) {
			continue
		}
		matches = append(matches, stock)
	}
	if len(matches) == 0 {
		return nil, 0, 0
	}
	if page < 0 {
		page = 0
	}
	if page >= len(matches) {
		page = len(matches) - 10
		if page < 0 {
			page = 0
		}
	}
	end := len(matches) - page
	if end > len(matches) {
		end = len(matches)
	}
	start := end - 10
	if start < 0 {
		start = 0
	}
	count := 0
	for i := end - 1; i >= start; i-- {
		entry := matches[i]
		item, ok := w.Item(entry.ItemID)
		if !ok {
			continue
		}
		base := merchantUserItemPrice(item, entry)
		price := merchantPriceValue(base, rate)
		if price <= 0 {
			continue
		}
		display := world.UpgradeClientItemForDisplay(item, entry, false)
		dura, _ := bagItemDurability(display, entry)
		display.Price = price
		body.Write(EncodeBuffer(ClientItemBody(display, entry.Desc, entry.MakeIndex, dura, uint16(price))))
		body.WriteByte('/')
		count++
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

func merchantMakeDrugListBody(w *world.World, stocks []storage.UserItem) (string, int) {
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

func merchantGoodsListBody(w *world.World, stocks []storage.UserItem, entity npc.Entity) (string, int) {
	type summary struct {
		item  data.StdItem
		count int
	}
	var b strings.Builder
	summaries := map[string]summary{}
	order := make([]string, 0, len(stocks))
	for _, stock := range stocks {
		item, ok := w.Item(stock.ItemID)
		if !ok {
			continue
		}
		sum, ok := summaries[stock.ItemID]
		if !ok {
			order = append(order, stock.ItemID)
			sum.item = item
		}
		sum.count++
		summaries[stock.ItemID] = sum
	}
	count := 0
	for _, itemID := range order {
		sum := summaries[itemID]
		price := merchantPrice(sum.item, entity.Merchant.PriceRate)
		if price <= 0 {
			continue
		}
		subMenu := 1
		if sum.item.StdMode <= 4 || sum.item.StdMode == 31 || sum.item.StdMode == 42 {
			subMenu = 0
		}
		fmt.Fprintf(&b, "%s/%d/%d/%d/", sum.item.Name, subMenu, price, sum.count)
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
