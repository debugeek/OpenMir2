package world

import "openmir2/internal/storage"

type ItemUseSyncer interface {
	TeleportSyncer
	SendBagAddItem(storage.Character, storage.UserItem)
	SendAbilityOnly(storage.Character)
	SendWinExp(int, int)
	SendLevelUp(storage.Character)
	SendHealthSpellChanged(storage.Character)
	SendEquippedItems(storage.Character)
	SendWeightChanged(storage.Character)
}

func ApplyItemUseSync(syncer ItemUseSyncer, result ItemUseResult) {
	if result.Character.ID != "" {
		syncer.UpdateClient(result.Character)
	}
	if result.Teleport != nil {
		ApplyTeleportSync(syncer, *result.Teleport)
	}
	for _, added := range result.AddedItems {
		syncer.SendBagAddItem(result.Character, added)
	}
	if result.AbilityChanged {
		syncer.SendAbilityOnly(result.Character)
	}
	if result.Experience > 0 {
		syncer.SendWinExp(result.Experience, result.CurrentExp)
	}
	if result.LevelUp {
		syncer.SendLevelUp(result.Character)
		syncer.SendHealthSpellChanged(result.Character)
	} else if result.HealthChanged {
		syncer.SendHealthSpellChanged(result.Character)
	}
	syncer.SendEquippedItems(result.Character)
	syncer.SendWeightChanged(result.Character)
}
