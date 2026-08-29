package network

import (
	"math"
	"math/rand"
	"net"
	"strings"
	"time"

	"openmir2/internal/data"
	"openmir2/internal/npc"
	"openmir2/internal/storage"
	"openmir2/internal/world"
)

func (s *Server) sendNPCConversationLabel(conn net.Conn, ch storage.Character, entity npc.Entity, label string) {
	conversation, ok := s.world.NPCConversation(ch, entity.ID, label)
	if !ok {
		return
	}
	s.sendNPCConversation(conn, conversation)
}

func (s *Server) handleWeaponUpgradeStart(conn net.Conn, activeChar *storage.Character, entity npc.Entity) bool {
	if activeChar.WeaponUpgrade != nil {
		s.sendNPCConversationLabel(conn, *activeChar, entity, "~@upgradenow_ing")
		return true
	}
	if activeChar.EquippedItems == nil {
		s.sendNPCConversationLabel(conn, *activeChar, entity, "~@upgradenow_fail")
		return true
	}
	weapon, ok := activeChar.EquippedItems[SlotWeapon]
	if !ok || weapon.ItemID == "" {
		s.sendNPCConversationLabel(conn, *activeChar, entity, "~@upgradenow_fail")
		return true
	}
	price := s.world.Gameplay().Item.UpgradeWeaponPrice
	if activeChar.Gold < price {
		s.sendNPCConversationLabel(conn, *activeChar, entity, "~@upgradenow_fail")
		return true
	}
	updated, removed, state, ok := s.startWeaponUpgrade(*activeChar)
	if !ok {
		s.sendNPCConversationLabel(conn, *activeChar, entity, "~@upgradenow_fail")
		return true
	}
	*activeChar = updated
	s.sendDelItemList(conn, removed)
	s.sendEquippedItems(conn, updated)
	s.sendAbilityOnly(conn, updated)
	s.sendWeightChanged(conn, s.world.AbilityStats(updated))
	_ = state
	s.sendNPCConversationLabel(conn, updated, entity, "~@upgradenow_ok")
	return true
}

func (s *Server) handleWeaponUpgradeGetBack(conn net.Conn, activeChar *storage.Character, entity npc.Entity) bool {
	if activeChar.WeaponUpgrade == nil {
		s.sendNPCConversationLabel(conn, *activeChar, entity, "~@getbackupgnow_fail")
		return true
	}
	if !s.world.CanCarryBagItems(*activeChar, 1) {
		s.sendNPCConversationLabel(conn, *activeChar, entity, "~@getbackupgnow_bagfull")
		return true
	}
	if !s.weaponUpgradeReady(*activeChar.WeaponUpgrade) {
		s.sendNPCConversationLabel(conn, *activeChar, entity, "~@getbackupgnow_ing")
		return true
	}
	updated, item, ok := s.finishWeaponUpgrade(*activeChar)
	if !ok {
		s.sendNPCConversationLabel(conn, *activeChar, entity, "~@getbackupgnow_fail")
		return true
	}
	*activeChar = updated
	s.sendBagAddItem(conn, updated, item.ItemID, item.MakeIndex)
	s.sendAbilityOnly(conn, updated)
	s.sendWeightChanged(conn, s.world.AbilityStats(updated))
	s.sendNPCConversationLabel(conn, updated, entity, "~@getbackupgnow_ok")
	return true
}

func (s *Server) startWeaponUpgrade(ch storage.Character) (storage.Character, []storage.UserItem, *storage.WeaponUpgradeState, bool) {
	price := s.world.Gameplay().Item.UpgradeWeaponPrice
	weapon, ok := ch.EquippedItems[SlotWeapon]
	if !ok || weapon.ItemID == "" || ch.Gold < price {
		return ch, nil, nil, false
	}
	bonusDC, bonusMC, bonusSC, bonusDura, removedMaterials, ok := s.weaponUpgradeMaterialStats(ch)
	if !ok {
		return ch, nil, nil, false
	}
	updated := ch
	updated.Gold -= price
	delete(updated.EquippedItems, SlotWeapon)
	updated.WeaponUpgrade = &storage.WeaponUpgradeState{
		Item:      weapon,
		StartedAt: time.Now().UnixMilli(),
		BonusDC:   bonusDC,
		BonusMC:   bonusMC,
		BonusSC:   bonusSC,
		BonusDura: bonusDura,
	}
	updated.BagItems = removeBagItemsByMatch(updated.BagItems, func(entry storage.UserItem) bool {
		return s.weaponUpgradeIsMaterial(entry)
	})
	removed := append([]storage.UserItem{weapon}, removedMaterials...)
	return updated, removed, updated.WeaponUpgrade, true
}

func (s *Server) finishWeaponUpgrade(ch storage.Character) (storage.Character, storage.UserItem, bool) {
	state := ch.WeaponUpgrade
	if state == nil {
		return ch, storage.UserItem{}, false
	}
	item := state.Item
	if item.ItemID == "" {
		return ch, storage.UserItem{}, false
	}
	item = s.applyWeaponUpgradeResult(item, *state)
	updated := ch
	updated.WeaponUpgrade = nil
	updated.BagItems = append(updated.BagItems, item)
	return updated, item, true
}

func (s *Server) weaponUpgradeReady(state storage.WeaponUpgradeState) bool {
	delay := time.Duration(s.world.Gameplay().Item.UpgradeWeaponGetBackMS) * time.Millisecond
	if delay <= 0 {
		delay = time.Hour
	}
	return time.Since(time.UnixMilli(state.StartedAt)) >= delay
}

func (s *Server) weaponUpgradeIsMaterial(entry storage.UserItem) bool {
	if entry.ItemID == "" {
		return false
	}
	if strings.EqualFold(entry.ItemID, "黑铁矿石") {
		return true
	}
	item, ok := s.world.Item(entry.ItemID)
	if !ok {
		return false
	}
	return world.IsAccessoryStdMode(item.StdMode)
}

func (s *Server) weaponUpgradeMaterialStats(ch storage.Character) (byte, byte, byte, byte, []storage.UserItem, bool) {
	var (
		topDC, secondDC int
		topMC, secondMC int
		topSC, secondSC int
		duraList        []int
		removed         []storage.UserItem
		hasStone        bool
		hasAccessory    bool
	)
	for _, entry := range ch.BagItems {
		if entry.ItemID == "" {
			continue
		}
		if strings.EqualFold(entry.ItemID, "黑铁矿石") {
			hasStone = true
			duraList = append(duraList, int(entry.Dura/1000))
			removed = append(removed, entry)
			continue
		}
		item, ok := s.world.Item(entry.ItemID)
		if !ok || !world.IsAccessoryStdMode(item.StdMode) {
			continue
		}
		hasAccessory = true
		removed = append(removed, entry)
		display := world.UpgradeClientItemForDisplay(item, entry, false)
		dc, mc, sc := weaponUpgradeContribution(display)
		topDC, secondDC = topTwo(topDC, secondDC, int(dc))
		topMC, secondMC = topTwo(topMC, secondMC, int(mc))
		topSC, secondSC = topTwo(topSC, secondSC, int(sc))
	}
	if !hasStone || !hasAccessory {
		return 0, 0, 0, 0, nil, false
	}
	sortIntsDesc(duraList)
	count := minInt(5, len(duraList))
	total := 0
	for i := 0; i < count; i++ {
		total += duraList[i]
	}
	var dura byte
	if count > 0 {
		avg := float64(total) / float64(count)
		dura = byte(math.Round(float64(count) + float64(count)*(avg/5.0)))
	}
	return byte(topDC/5 + secondDC/3), byte(topMC/5 + secondMC/3), byte(topSC/5 + secondSC/3), dura, removed, true
}

func (s *Server) applyWeaponUpgradeResult(item storage.UserItem, state storage.WeaponUpgradeState) storage.UserItem {
	best := 0
	switch {
	case state.BonusDC == state.BonusMC && state.BonusMC == state.BonusSC:
		best = rand.Intn(3)
	case state.BonusDC >= state.BonusMC && state.BonusDC >= state.BonusSC:
		best = 0
	case state.BonusMC >= state.BonusDC && state.BonusMC >= state.BonusSC:
		best = 1
	default:
		best = 2
	}
	inc := weaponUpgradeIncrement([]byte{state.BonusDC, state.BonusMC, state.BonusSC}[best])
	if inc == 0 {
		inc = 1
	}
	switch best {
	case 0:
		item.Desc[0] = clampAddByte(item.Desc[0], inc)
	case 1:
		item.Desc[1] = clampAddByte(item.Desc[1], inc)
	case 2:
		item.Desc[2] = clampAddByte(item.Desc[2], inc)
	}
	item.Desc[10] = 0
	return item
}

func weaponUpgradeContribution(item data.StdItem) (byte, byte, byte) {
	switch item.StdMode {
	case 19, 20, 21, 22, 23, 24, 26:
		dc := byte(byte(item.Stats.DcMin) + byte(item.Stats.DcMin>>8))
		mc := byte(byte(item.Stats.McMin) + byte(item.Stats.McMin>>8))
		sc := byte(byte(item.Stats.ScMin) + byte(item.Stats.ScMin>>8))
		if item.StdMode == 24 || item.StdMode == 26 {
			dc++
			mc++
			sc++
		}
		return dc, mc, sc
	default:
		return 0, 0, 0
	}
}

func weaponUpgradeIncrement(v byte) byte {
	switch {
	case v >= 15:
		return 3
	case v >= 10:
		return 2
	case v > 0:
		return 1
	default:
		return 0
	}
}

func topTwo(a, b, v int) (int, int) {
	if v >= a {
		return v, a
	}
	if v > b {
		return a, v
	}
	return a, b
}

func clampAddByte(v byte, add byte) byte {
	sum := int(v) + int(add)
	if sum > 255 {
		sum = 255
	}
	return byte(sum)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sortIntsDesc(nums []int) {
	for i := 0; i < len(nums); i++ {
		for j := i + 1; j < len(nums); j++ {
			if nums[j] > nums[i] {
				nums[i], nums[j] = nums[j], nums[i]
			}
		}
	}
}

func removeBagItemsByMatch(items []storage.UserItem, match func(storage.UserItem) bool) []storage.UserItem {
	if len(items) == 0 {
		return items
	}
	out := make([]storage.UserItem, 0, len(items))
	for _, entry := range items {
		if match(entry) {
			continue
		}
		out = append(out, entry)
	}
	return out
}
