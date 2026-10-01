package world

import "openmir2/internal/storage"

type SaySyncer interface {
	UserCommandSyncer
	SendLocalHear(storage.Character, string)
	SendGlobalHear(storage.Character, string)
	SendPrivate(storage.Character, string, string)
	SendGroup(storage.Character, string)
	SendGuild(storage.Character, string)
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
	} else if result.Chat.Private {
		syncer.SendPrivate(activeChar, result.Chat.TargetName, result.Chat.Message)
	} else if result.Chat.Group {
		syncer.SendGroup(activeChar, result.Chat.Message)
	} else if result.Chat.Guild {
		syncer.SendGuild(activeChar, result.Chat.Message)
		return
	} else {
		syncer.SendLocalHear(activeChar, result.Chat.Message)
	}
}
