package world

import "openmir2/internal/storage"

type ItemUseSyncer interface {
	TeleportSyncer
	SendDelItems([]storage.UserItem)
	SendBagAddItem(storage.Character, storage.UserItem)
	SendSkillAdded(storage.Character, storage.SkillState)
	SendAbilityOnly(storage.Character)
	SendWinExp(int, int)
	SendLevelUp(storage.Character)
	SendHealthSpellChanged(storage.Character)
	SendWeightChanged(storage.Character)
}

func ApplyItemUseSync(syncer ItemUseSyncer, result ItemUseResult) {
	if result.Character.ID != "" {
		syncer.UpdateClient(result.Character)
	}
	if result.Teleport != nil {
		ApplyTeleportSync(syncer, *result.Teleport)
	}
	if len(result.RemovedItems) > 0 && !result.SuppressRemovedSync {
		syncer.SendDelItems(result.RemovedItems)
	}
	if len(result.AddedItems) > 0 {
		for _, added := range result.AddedItems {
			syncer.SendBagAddItem(result.Character, added)
		}
		syncer.SendWeightChanged(result.Character)
	}
	for _, skill := range result.AddedSkills {
		syncer.SendSkillAdded(result.Character, skill)
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
	if len(result.AddedItems) == 0 {
		syncer.SendWeightChanged(result.Character)
	}
}
