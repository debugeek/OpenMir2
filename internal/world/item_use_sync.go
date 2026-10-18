package world

import "openmir2/internal/storage"

type ItemUseSyncer interface {
	TeleportSyncer
	SendSpaceMoveFire(storage.Character)
	SendDelItems([]storage.UserItem)
	SendBagAddItem(storage.Character, storage.UserItem)
	SendSkillAdded(storage.Character, storage.SkillState)
	SendRawFrame(string)
	SendAbilityOnly(storage.Character)
	SendSubAbility(storage.Character)
	SendDurability(SpellDurability)
	SendSystemMessage(storage.Character, string)
	SendSystemMessageStyle(storage.Character, string, byte, byte)
	SendLevelUp(storage.Character)
	SendHealthSpellChanged(storage.Character)
	SendWeightChanged(storage.Character)
}

func ApplyItemUseSync(syncer ItemUseSyncer, result ItemUseResult) {
	if !result.Consumed {
		if result.SystemMessageEvents == nil && result.SystemMessage != "" {
			syncer.SendSystemMessage(result.Character, result.SystemMessage)
		}
		for _, message := range result.SystemMessageEvents {
			syncer.SendSystemMessageStyle(result.Character, message.Text, message.Foreground, message.Background)
		}
		return
	}
	if result.Character.ID != "" {
		syncer.UpdateClient(result.Character)
	}
	if result.Teleport != nil {
		if result.Teleport.SpaceMoveFire {
			syncer.SendSpaceMoveFire(result.Teleport.From)
		}
		ApplyTeleportSync(syncer, *result.Teleport)
	}
	for _, skill := range result.AddedSkills {
		syncer.SendSkillAdded(result.Character, skill)
	}
	for _, message := range result.SystemMessages {
		syncer.SendSystemMessageStyle(result.Character, message.Text, message.Foreground, message.Background)
	}
	if result.EnableThrusting {
		syncer.SendRawFrame("+LNG")
	}
	if result.EnableHalfMoon {
		syncer.SendRawFrame("+WID")
	}
	if result.AbilityChanged {
		if result.SystemMessageBeforeAbility && result.SystemMessage != "" {
			syncer.SendSystemMessage(result.Character, result.SystemMessage)
		}
		syncer.SendAbilityOnly(result.Character)
		if result.SubAbilityChanged {
			syncer.SendSubAbility(result.Character)
		}
		if !result.SystemMessageBeforeAbility && result.SystemMessage != "" {
			syncer.SendSystemMessage(result.Character, result.SystemMessage)
		}
	} else if result.Durability != nil {
		syncer.SendDurability(*result.Durability)
		if result.SystemMessage != "" {
			syncer.SendSystemMessage(result.Character, result.SystemMessage)
		}
	}
	if result.HealthChanged {
		syncer.SendHealthSpellChanged(result.Character)
	}
	if len(result.RemovedItems) > 0 && !result.SuppressRemovedSync {
		syncer.SendDelItems(result.RemovedItems)
	}
	if len(result.AddedItems) > 0 {
		for _, added := range result.AddedItems {
			syncer.SendBagAddItem(result.Character, added)
		}
	}
}

func ApplyItemUsePostSync(syncer ItemUseSyncer, result ItemUseResult) {
	if !result.Consumed {
		return
	}
	if result.LevelUp {
		syncer.SendLevelUp(result.Character)
		syncer.SendHealthSpellChanged(result.Character)
	}
	syncer.SendWeightChanged(result.Character)
}
