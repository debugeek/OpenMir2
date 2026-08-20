package network

import (
	"net"

	"openmir2/internal/storage"
)

func (s *Server) handleUserCommand(conn net.Conn, activeChar *storage.Character, line string) {
	prev := *activeChar
	result, ok := s.world.HandleUserCommand(*activeChar, line)
	if !ok {
		return
	}
	if result.Character.ID != "" {
		*activeChar = result.Character
		if result.Moved {
			s.updateClient(conn, *activeChar)
			s.sendSpaceMoveState(conn, *activeChar, false)
			s.broadcastTeleportMove(conn, prev, *activeChar)
		}
	}
	if result.Message != "" {
		s.sendHear(conn, result.Message, 0x00, 0xFF)
	}
	for _, added := range result.AddedItems {
		s.sendBagAddItem(conn, *activeChar, added.ItemID, added.MakeIndex)
	}
	if len(result.AddedItems) > 0 {
		s.sendWeightChanged(conn, s.world.AbilityStats(*activeChar))
	}
	if len(result.Monsters) == 0 {
		return
	}
	if clients := s.ClientsInMap(activeChar.MapID); len(clients) > 0 {
		s.broadcastMonsterAppear(clients, result.Monsters)
	}
}
