package world

import (
	"time"

	"openmir2/internal/storage"
)

type AttackSyncer interface {
	UpdateClient(storage.Character)
	SendActionOK()
	SendWinExp(int, int)
	SendLevelUp(storage.Character)
	SendHealthSpellChanged(storage.Character)
	SendCharacterHitChanges(CharacterHit)
	BroadcastCharacterHit(storage.Character, uint16)
	BroadcastCharacterStruck(CharacterHit)
	BroadcastCharacterNameColor(storage.Character)
	BroadcastHitImpact(AttackResult)
	SendSkillExp(uint16, byte, int, time.Duration)
}

func ApplyAttackSync(syncer AttackSyncer, result AttackResult, attackIdent uint16) {
	syncer.UpdateClient(result.Character)
	for _, hit := range result.CharacterHits {
		syncer.SendCharacterHitChanges(hit)
	}
	syncer.BroadcastCharacterHit(result.Character, attackIdent)
	for _, hit := range result.CharacterHits {
		if hit.Damage > 0 {
			syncer.BroadcastCharacterStruck(hit)
		}
	}
	if len(result.MonsterHits) > 0 {
		for _, hit := range result.MonsterHits {
			if hit.Damage > 0 {
				syncer.BroadcastHitImpact(hit)
			}
		}
	} else if result.MonsterID != "" && result.Damage > 0 {
		syncer.BroadcastHitImpact(result)
	}
	syncer.SendActionOK()
	for _, hit := range result.CharacterHits {
		if hit.Damage > 0 && hit.AttackerNameColorChanged {
			syncer.BroadcastCharacterNameColor(result.Character)
		}
	}
	if result.Experience > 0 {
		syncer.SendWinExp(result.Experience, result.CurrentExp)
	}
	if result.LevelUp {
		syncer.SendLevelUp(result.Character)
		syncer.SendHealthSpellChanged(result.Character)
	}
	for _, skillExp := range result.SkillExperiences {
		syncer.SendSkillExp(skillExp.MagicID, skillExp.Level, skillExp.Train, skillExp.Delay)
	}
	if result.SkillExp && len(result.SkillExperiences) == 0 {
		syncer.SendSkillExp(result.SkillMagicID, result.SkillLevel, result.SkillTrain, result.SkillExpDelay)
	}
}
