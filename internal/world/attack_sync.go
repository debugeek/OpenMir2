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
	SendAttackItemChanges(storage.Character, []SpellDurability, []storage.UserItem)
	SendWeaponBroken()
	SendSkillExp(uint16, byte, int, time.Duration)
}

type MiningFeedbackSyncer interface {
	SendMiningFeedback()
}

type AttackItemSyncer interface {
	SendBagAddItem(storage.Character, storage.UserItem)
	SendWeightChanged(storage.Character)
}

type AttackItemUpdateSyncer interface {
	SendUpdatedItem(storage.Character, storage.UserItem)
}

type AttackAbilitySyncer interface {
	SendAbilityOnly(storage.Character)
	SendSubAbility(storage.Character)
}

type AttackFeatureSyncer interface {
	SendFeatureChanged(storage.Character)
}

func ApplyAttackSync(syncer AttackSyncer, result AttackResult, attackIdent uint16) {
	syncer.UpdateClient(result.Character)
	if result.HeavyHitMining {
		if itemSyncer, ok := syncer.(AttackItemSyncer); ok && len(result.AddedItems) > 0 {
			itemSyncer.SendWeightChanged(result.Character)
			for _, item := range result.AddedItems {
				itemSyncer.SendBagAddItem(result.Character, item)
			}
		}
	}
	if itemSyncer, ok := syncer.(AttackItemUpdateSyncer); ok {
		for _, item := range result.UpdatedItems {
			itemSyncer.SendUpdatedItem(result.Character, item)
		}
	}
	if result.WeaponBroken && (len(result.Durability) > 0 || len(result.DeletedItems) > 0) {
		syncer.SendAttackItemChanges(result.Character, result.Durability, result.DeletedItems)
	}
	if result.AbilityChanged {
		if abilitySyncer, ok := syncer.(AttackAbilitySyncer); ok {
			abilitySyncer.SendAbilityOnly(result.Character)
			if result.SubAbilityChanged {
				abilitySyncer.SendSubAbility(result.Character)
			}
		}
	}
	if result.WeaponBroken {
		syncer.SendWeaponBroken()
	}
	if result.FeatureChanged {
		if result.WeaponBroken {
			if featureSyncer, ok := syncer.(AttackFeatureSyncer); ok {
				featureSyncer.SendFeatureChanged(result.Character)
			}
		}
	}
	for _, hit := range result.CharacterHits {
		syncer.SendCharacterHitChanges(hit)
	}
	for _, hit := range result.CharacterHits {
		struckDamage := hit.Damage
		if hit.StruckDamage > 0 {
			struckDamage = hit.StruckDamage
		}
		if struckDamage > 0 {
			syncer.BroadcastCharacterStruck(hit)
		}
	}
	if !result.WeaponBroken && (len(result.Durability) > 0 || len(result.DeletedItems) > 0) {
		syncer.SendAttackItemChanges(result.Character, result.Durability, result.DeletedItems)
	}
	if result.FeatureChanged && !result.WeaponBroken {
		if featureSyncer, ok := syncer.(AttackFeatureSyncer); ok {
			featureSyncer.SendFeatureChanged(result.Character)
		}
	}
	if result.Character.ID != "" {
		if result.HeavyHitMining || result.HeavyHitFragment {
			if mining, ok := syncer.(MiningFeedbackSyncer); ok {
				mining.SendMiningFeedback()
			} else {
				syncer.BroadcastCharacterHit(result.Character, attackIdent)
			}
		} else {
			syncer.BroadcastCharacterHit(result.Character, attackIdent)
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
	if itemSyncer, ok := syncer.(AttackItemSyncer); ok && !result.HeavyHitMining {
		if len(result.AddedItems) > 0 {
			itemSyncer.SendWeightChanged(result.Character)
			for _, item := range result.AddedItems {
				itemSyncer.SendBagAddItem(result.Character, item)
			}
		}
	}
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
