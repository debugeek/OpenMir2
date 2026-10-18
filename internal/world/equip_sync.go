package world

import "openmir2/internal/storage"

type EquipSyncer interface {
	UpdateClient(storage.Character)
	SendWeightChanged(storage.Character)
	SendBagAddItem(storage.Character, storage.UserItem)
	SendSkillAdded(storage.Character, storage.SkillState)
	SendSkillRemoved(storage.Character, string)
	SendEquipSuccess(storage.Character, uint16)
	SendAbilityOnly(storage.Character)
	SendSubAbility(storage.Character)
	SendFeatureChanged(storage.Character)
}

func ApplyEquipSync(syncer EquipSyncer, result EquipResult, okIdent uint16) {
	syncer.UpdateClient(result.Character)
	if result.HasSwappedOut {
		syncer.SendWeightChanged(result.Character)
		syncer.SendBagAddItem(result.Character, result.SwappedOut)
	}
	for _, skill := range result.AddedSkills {
		syncer.SendSkillAdded(result.Character, skill)
	}
	for _, skillID := range result.RemovedSkills {
		syncer.SendSkillRemoved(result.Character, skillID)
	}
	syncer.SendAbilityOnly(result.Character)
	syncer.SendSubAbility(result.Character)
	syncer.SendEquipSuccess(result.Character, okIdent)
	syncer.SendFeatureChanged(result.Character)
}

func ApplyUnequipSync(syncer EquipSyncer, result UnequipResult, okIdent uint16) {
	syncer.UpdateClient(result.Character)
	if result.HasRemovedItem {
		syncer.SendWeightChanged(result.Character)
		syncer.SendBagAddItem(result.Character, result.RemovedItem)
	}
	for _, skill := range result.AddedSkills {
		syncer.SendSkillAdded(result.Character, skill)
	}
	for _, skillID := range result.RemovedSkills {
		syncer.SendSkillRemoved(result.Character, skillID)
	}
	syncer.SendAbilityOnly(result.Character)
	syncer.SendSubAbility(result.Character)
	syncer.SendEquipSuccess(result.Character, okIdent)
	syncer.SendFeatureChanged(result.Character)
}
