package world

import "openmir2/internal/storage"

type EquipSyncer interface {
	UpdateClient(storage.Character)
	SendBagAddItem(storage.Character, storage.UserItem)
	SendAbilityRefresh(storage.Character, uint16)
}

func ApplyEquipSync(syncer EquipSyncer, result EquipResult, okIdent uint16) {
	syncer.UpdateClient(result.Character)
	if result.HasSwappedOut {
		syncer.SendBagAddItem(result.Character, result.SwappedOut)
	}
	syncer.SendAbilityRefresh(result.Character, okIdent)
}

func ApplyUnequipSync(syncer EquipSyncer, result UnequipResult, okIdent uint16) {
	syncer.UpdateClient(result.Character)
	if result.HasRemovedItem {
		syncer.SendBagAddItem(result.Character, result.RemovedItem)
	}
	syncer.SendAbilityRefresh(result.Character, okIdent)
}
