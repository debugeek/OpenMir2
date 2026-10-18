package world

import (
	"sort"
	"strings"

	"openmir2/internal/npc"
	"openmir2/internal/storage"
)

func (w *World) NPCByID(id string) (npc.Entity, bool) {
	entity, ok := w.data.NPCs.Entities[id]
	return entity, ok
}

func (w *World) NPCsInMap(mapID string) []npc.Entity {
	return w.npcEntitiesInMapOrdered(mapID, false)
}

func (w *World) npcInCastleArea(entity npc.Entity) bool {
	castle := w.data.Castle
	if entity.MapID == castle.PalaceMap || entity.MapID == castle.SecretMap {
		return true
	}
	for _, mapID := range castle.WarMaps {
		if entity.MapID == mapID {
			return true
		}
	}
	return entity.MapID == castle.CastleMap &&
		abs(entity.X-castle.HomeX) < castle.WarRangeX &&
		abs(entity.Y-castle.HomeY) < castle.WarRangeY
}

func (w *World) npcEntitiesInMapOrdered(mapID string, includeHidden bool) []npc.Entity {
	out := make([]npc.Entity, 0)
	for _, entity := range w.data.NPCs.Entities {
		if (mapID == "" || entity.MapID == mapID) && (includeHidden || (!entity.Hidden && !w.npcFixedHides[entity.ID])) {
			out = append(out, entity)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if mapID == "" {
			if out[i].ReferenceOrder > 0 && out[j].ReferenceOrder > 0 && out[i].ReferenceOrder != out[j].ReferenceOrder {
				return out[i].ReferenceOrder < out[j].ReferenceOrder
			}
			if out[i].ReferenceOrder > 0 != (out[j].ReferenceOrder > 0) {
				return out[i].ReferenceOrder > 0
			}
		}
		if out[i].X != out[j].X {
			return out[i].X < out[j].X
		}
		if out[i].Y != out[j].Y {
			return out[i].Y < out[j].Y
		}
		if out[i].ReferenceOrder > 0 && out[j].ReferenceOrder > 0 && out[i].ReferenceOrder != out[j].ReferenceOrder {
			return out[i].ReferenceOrder < out[j].ReferenceOrder
		}
		if out[i].ReferenceOrder > 0 != (out[j].ReferenceOrder > 0) {
			return out[i].ReferenceOrder > 0
		}
		return out[i].ID < out[j].ID
	})
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
	hasCastleContext := entity.CastleOfficial && w.npcInCastleArea(entity)
	castle, okCastle := w.CastleSnapshot(castleID)
	if hasCastleContext && !okCastle {
		castle = storage.Castle{ID: castleID, MainDoorOpen: true, MainDoorHP: w.data.Castle.MainDoor.HP}
		for i := 0; i < len(castle.WallHP) && i < len(w.data.Castle.Walls); i++ {
			castle.WallHP[i] = w.data.Castle.Walls[i].HP
		}
		_ = w.store.SaveCastle(castle)
		w.mu.Lock()
		w.castleRuntime[castleID] = castle
		w.mu.Unlock()
	}
	castleGold := 0
	todayIncome := 0
	castleDoorState := "????"
	ownerGuild := "????"
	lord := "????"
	if hasCastleContext {
		castleGold = castle.Gold
		todayIncome = castle.TodayIncome
		castleDoorState = "关闭"
		if castle.MainDoorHP <= 0 {
			castleDoorState = "损坏"
		} else if castle.MainDoorOpen {
			castleDoorState = "打开"
		}
		ownerGuild = castle.OwnerGuildID
		if ownerGuild == "" {
			ownerGuild = "游戏管理员"
		}
		lord = "管理员"
	}
	if hasCastleContext && castle.OwnerGuildID != "" && w.store != nil {
		if guild, ok := w.store.Guild(castle.OwnerGuildID); ok {
			if len(guild.Ranks) > 0 && len(guild.Ranks[0].Members) > 0 && guild.Ranks[0].Members[0] != "" {
				lord = guild.Ranks[0].Members[0]
			}
		}
	}
	ctx := npc.Context{
		Username:         activeChar.Name,
		OwnerGuild:       ownerGuild,
		Lord:             lord,
		CastleGold:       castleGold,
		TodayIncome:      todayIncome,
		CastleDoorState:  castleDoorState,
		RepairDoorGold:   w.gameplay.Castle.RepairDoorPrice,
		RepairWallGold:   w.gameplay.Castle.RepairWallPrice,
		GuardFee:         w.gameplay.Castle.HireGuardPrice,
		ArcherFee:        w.gameplay.Castle.HireArcherPrice,
		UpgradeWeaponFee: w.gameplay.Item.UpgradeWeaponPrice,
		UserWeapon: func() string {
			item, ok := w.equippedItemLocked(activeChar, SlotWeapon)
			if !ok {
				return "Weapon"
			}
			if stdItem, ok := w.Item(item.ItemID); ok {
				return stdItem.Name
			}
			return ""
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
