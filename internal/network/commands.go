package network

import (
	"net"

	"openmir2/internal/storage"
	"openmir2/internal/world"
)

func (s *Server) handleUserCommandResult(conn net.Conn, activeChar *storage.Character, result world.UserCommandResult) {
	if result.Message != "" {
		s.sendHear(conn, result.Message, 0x00, 0xFF)
	}
	if result.Character.ID != "" {
		*activeChar = result.Character
	}
	world.ApplyUserCommandSync(itemUseSyncAdapter{s: s, conn: conn}, result)
	if len(result.Monsters) == 0 {
		return
	}
	if clients := s.ClientsInMap(activeChar.MapID); len(clients) > 0 {
		s.broadcastMonsterAppear(clients, result.Monsters)
	}
}
