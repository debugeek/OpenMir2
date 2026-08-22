package world

import "openmir2/internal/storage"

type AttackSyncer interface {
	UpdateClient(storage.Character)
	SendActionOK()
	SendWinExp(int, int)
	SendLevelUp(storage.Character)
	SendHealthSpellChanged(storage.Character)
	BroadcastCharacterHit(storage.Character, uint16)
	BroadcastHitImpact(AttackResult)
}

func ApplyAttackSync(syncer AttackSyncer, result AttackResult, attackIdent uint16) {
	syncer.UpdateClient(result.Character)
	syncer.SendActionOK()
	if result.Experience > 0 {
		syncer.SendWinExp(result.Experience, result.CurrentExp)
	}
	if result.LevelUp {
		syncer.SendLevelUp(result.Character)
		syncer.SendHealthSpellChanged(result.Character)
	}
	syncer.BroadcastCharacterHit(result.Character, attackIdent)
	if result.MonsterID != "" {
		syncer.BroadcastHitImpact(result)
	}
}
