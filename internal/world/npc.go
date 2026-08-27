package world

import (
	"strings"

	"openmir2/internal/npc"
	"openmir2/internal/storage"
)

func (w *World) NPCByID(id string) (npc.Entity, bool) {
	entity, ok := w.data.NPCs.Entities[id]
	return entity, ok
}

func (w *World) NPCsInMap(mapID string) []npc.Entity {
	out := make([]npc.Entity, 0)
	for _, entity := range w.data.NPCs.Entities {
		if entity.MapID == mapID {
			out = append(out, entity)
		}
	}
	return out
}

func (w *World) NPCConversation(activeChar storage.Character, npcID, label string) (npc.Conversation, bool) {
	entity, ok := w.NPCByID(npcID)
	if !ok {
		return npc.Conversation{}, false
	}
	if label == "" {
		label = "@main"
	}
	ctx := npc.Context{
		OwnerGuild:       "无",
		Lord:             "无",
		CastleGold:       0,
		TodayIncome:      0,
		CastleDoorState:  "关闭",
		RepairDoorGold:   w.gameplay.Castle.RepairDoorPrice,
		RepairWallGold:   w.gameplay.Castle.RepairWallPrice,
		GuardFee:         w.gameplay.Castle.HireGuardPrice,
		ArcherFee:        w.gameplay.Castle.HireArcherPrice,
		UpgradeWeaponFee: w.gameplay.Item.UpgradeWeaponPrice,
		UserWeapon: func() string {
			item, ok := w.equippedItemLocked(activeChar, SlotWeapon)
			if !ok {
				return ""
			}
			if stdItem, ok := w.Item(item.ItemID); ok {
				return stdItem.Name
			}
			return item.ItemID
		}(),
	}
	return w.data.NPCs.Conversation(entity.ID, label, ctx)
}

func (w *World) NPCLabelSelection(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "@main"
	}
	if strings.HasPrefix(label, "@@InPutString") || strings.HasPrefix(label, "@@InPutInteger") {
		return label
	}
	if keepSpecialDoubleLabel(label) {
		return label
	}
	if strings.HasPrefix(label, "@@") {
		return "@" + strings.TrimPrefix(label, "@@")
	}
	if strings.HasPrefix(label, "@") {
		return label
	}
	return "@" + label
}

func keepSpecialDoubleLabel(label string) bool {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "@@guildwar", "@@withdrawal", "@@receipts", "@@castlename", "@@sendmsg", "@@getmaster", "@@getmarry", "@@useitemname", "@@offlinemsg", "@@dealgold", "@@lycreatehero", "@@buhero":
		return true
	default:
		return false
	}
}
