package network

import (
	"net"

	"openmir2/internal/protocol/mir176"
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
	for _, notice := range result.SystemNotices {
		s.sendSystemMessageStyle(conn, *activeChar, notice.Message, notice.Foreground, notice.Background)
	}
	world.ApplyUserCommandSync(itemUseSyncAdapter{s: s, conn: conn}, result)
	for _, teleport := range result.Teleports {
		if client, ok := s.ClientByCharacterID(teleport.To.ID); ok {
			if teleport.SpaceMoveHide {
				s.sendCommand(client.conn, mir176.Command{Ident: mir176.SMSpacemoveHide, Recog: world.CharacterActorID(teleport.To)}, nil)
			}
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
