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
	castleID := w.data.Castle.ID
	castle, okCastle := w.store.Castle(castleID)
	if entity.CastleOfficial && !okCastle {
		castle = storage.Castle{ID: castleID, MainDoorHP: w.data.Castle.MainDoor.HP}
		for i := 0; i < len(castle.WallHP) && i < len(w.data.Castle.Walls); i++ {
			castle.WallHP[i] = w.data.Castle.Walls[i].HP
		}
		_ = w.store.SaveCastle(castle)
	}
	castleGold := castle.Gold
	castleDoorState := "关闭"
	if castle.MainDoorOpen {
		castleDoorState = "打开"
	}
	ownerGuild := castle.OwnerGuildID
	if ownerGuild == "" {
		ownerGuild = "无"
	}
	ctx := npc.Context{
		OwnerGuild:       ownerGuild,
		Lord:             "无",
		CastleGold:       castleGold,
		TodayIncome:      castle.TodayIncome,
		CastleDoorState:  castleDoorState,
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
	if strings.HasPrefix(label, "@@") {
		return label
	}
	if strings.HasPrefix(label, "@") {
		return label
	}
	return "@" + label
}
