package network

import (
	"net"

	"openmir2/internal/storage"
	"openmir2/internal/world"
)

func (s *Server) handleUserCommandResult(conn net.Conn, activeChar *storage.Character, result world.UserCommandResult) {
	if result.Message != "" {
		s.sendHear(conn, world.CharacterActorID(*activeChar), result.Message, 0x00, 0xFF)
	}
	if result.Character.ID != "" {
		*activeChar = result.Character
	}
	for _, updated := range result.CharacterUpdates {
		s.updateClientByCharacterID(updated)
	}
	for _, notice := range result.Notices {
		s.sendSystemMessage(conn, *activeChar, notice)
	}
	world.ApplyUserCommandSync(itemUseSyncAdapter{s: s, conn: conn}, result)
	for _, teleport := range result.Teleports {
		if client, ok := s.ClientByCharacterID(teleport.To.ID); ok {
			world.ApplyTeleportSync(teleportSyncAdapter{s: s, conn: client.conn}, teleport)
		}
	}
	if len(result.Monsters) == 0 {
		return
	}
	if clients := s.ClientsInMap(activeChar.MapID); len(clients) > 0 {
		s.broadcastMonsterAppear(clients, result.Monsters)
	}
}
