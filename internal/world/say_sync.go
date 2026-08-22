package world

import "openmir2/internal/storage"

type SaySyncer interface {
	UserCommandSyncer
	SendLocalHear(storage.Character, string)
	SendGlobalHear(storage.Character, string)
}

func ApplySaySync(syncer SaySyncer, activeChar storage.Character, result SayResult) {
	if result.Command != nil {
		ApplyUserCommandSync(syncer, *result.Command)
		return
	}
	if result.Chat == nil {
		return
	}
	if result.Chat.Global {
		syncer.SendGlobalHear(activeChar, result.Chat.Message)
		return
	}
	syncer.SendLocalHear(activeChar, result.Chat.Message)
}
