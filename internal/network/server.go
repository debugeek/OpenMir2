package network

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"openmir2/internal/config"
	"openmir2/internal/data"
	"openmir2/internal/protocol/mir176"
	"openmir2/internal/storage"
	"openmir2/internal/world"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const itemNameLen = 14

const (
	SlotDress  = world.SlotDress
	SlotWeapon = world.SlotWeapon
	SlotRingL  = world.SlotRingL
	SlotCharm  = world.SlotCharm
)

type Server struct {
	serverName string
	listeners  []config.Listener
	store      *storage.Store
	world      *world.World
	log        *slog.Logger

	sessionMu sync.Mutex
	sessions  map[int32]string
	clientMu  sync.Mutex
	clients   map[net.Conn]*Client

	hitImpactDelay      time.Duration
	monsterTickInterval time.Duration
}

func New(serverName string, listeners []config.Listener, store *storage.Store, world *world.World, log *slog.Logger) *Server {
	return &Server{serverName: serverName, listeners: listeners, store: store, world: world, log: log, sessions: map[int32]string{}, clients: map[net.Conn]*Client{}, hitImpactDelay: 200 * time.Millisecond, monsterTickInterval: 100 * time.Millisecond}
}

func (s *Server) SetHitImpactDelay(delay time.Duration) {
	s.hitImpactDelay = delay
}

func (s *Server) SetMonsterTickInterval(interval time.Duration) {
	s.monsterTickInterval = interval
}

type Client struct {
	conn            net.Conn
	mu              sync.Mutex
	ch              storage.Character
	softVersionDate int
	active          *storage.Character
	visibleMonsters map[string]world.Monster
	visibleDrops    map[string]world.GroundDrop
}

type teleportSyncAdapter struct {
	s    *Server
	conn net.Conn
}

func (a teleportSyncAdapter) UpdateClient(ch storage.Character) {
	a.s.updateClient(a.conn, ch)
}

func (a teleportSyncAdapter) SendSpaceMoveState(ch storage.Character) {
	a.s.sendSpaceMoveState(a.conn, ch)
}

func (a teleportSyncAdapter) BroadcastTeleportMove(from, to storage.Character) {
	a.s.broadcastTeleportMove(a.conn, from, to)
}

type pickupSyncAdapter struct {
	s    *Server
	conn net.Conn
}

func (a pickupSyncAdapter) UpdateClient(ch storage.Character) {
	a.s.updateClient(a.conn, ch)
}

func (a pickupSyncAdapter) BroadcastDropHide(ch storage.Character, dropID string) {
	if clients := a.s.ClientsInMap(ch.MapID); len(clients) > 0 {
		a.s.broadcastDropHide(clients, dropID)
	}
}

func (a pickupSyncAdapter) SendGoldChanged(ch storage.Character, gold int) {
	_ = ch
	a.s.sendGoldChanged(a.conn, gold)
}

func (a pickupSyncAdapter) SendBagAddItem(ch storage.Character, item storage.UserItem) {
	a.s.sendBagAddItem(a.conn, ch, item.ItemID, item.MakeIndex)
}

func (a pickupSyncAdapter) SendWeightChanged(ch storage.Character) {
	a.s.sendWeightChanged(a.conn, a.s.world.AbilityStats(ch))
}

type itemUseSyncAdapter struct {
	s    *Server
	conn net.Conn
}

func (a itemUseSyncAdapter) UpdateClient(ch storage.Character) {
	a.s.updateClient(a.conn, ch)
}

func (a itemUseSyncAdapter) BroadcastTeleportMove(from, to storage.Character) {
	a.s.broadcastTeleportMove(a.conn, from, to)
}

func (a itemUseSyncAdapter) SendSpaceMoveState(ch storage.Character) {
	a.s.sendSpaceMoveState(a.conn, ch)
}

func (a itemUseSyncAdapter) SendBagAddItem(ch storage.Character, item storage.UserItem) {
	a.s.sendBagAddItem(a.conn, ch, item.ItemID, item.MakeIndex)
}

func (a itemUseSyncAdapter) SendAbilityOnly(ch storage.Character) {
	a.s.sendAbilityOnly(a.conn, ch)
}

func (a itemUseSyncAdapter) SendWinExp(exp int, currentExp int) {
	a.s.sendWinExp(a.conn, exp, currentExp)
}

func (a itemUseSyncAdapter) SendLevelUp(ch storage.Character) {
	a.s.sendLevelUp(a.conn, ch)
}

func (a itemUseSyncAdapter) SendHealthSpellChanged(ch storage.Character) {
	a.s.sendHealthSpellChanged(a.conn, world.CharacterActorID(ch), a.s.world.AbilityStats(ch))
}

func (a itemUseSyncAdapter) SendEquippedItems(ch storage.Character) {
	a.s.sendEquippedItems(a.conn, ch)
}

func (a itemUseSyncAdapter) SendWeightChanged(ch storage.Character) {
	a.s.sendWeightChanged(a.conn, a.s.world.AbilityStats(ch))
}

func (a itemUseSyncAdapter) SendAbilityRefresh(ch storage.Character, okIdent uint16) {
	a.s.sendAbilityRefresh(a.conn, ch, okIdent)
}

func (a itemUseSyncAdapter) SendLocalHear(ch storage.Character, msg string) {
	if clients := a.s.ClientsInMap(ch.MapID); len(clients) > 0 {
		a.s.broadcastHear(clients, msg, 0x00, 0xFF)
		return
	}
	a.s.sendHear(a.conn, msg, 0x00, 0xFF)
}

func (a itemUseSyncAdapter) SendGlobalHear(ch storage.Character, msg string) {
	_ = ch
	if clients := a.s.allClients(); len(clients) > 0 {
		a.s.broadcastHear(clients, msg, 0x00, 0x97)
		return
	}
	a.s.sendHear(a.conn, msg, 0x00, 0x97)
}

type attackSyncAdapter struct {
	s    *Server
	conn net.Conn
}

func (a attackSyncAdapter) UpdateClient(ch storage.Character) {
	a.s.updateClient(a.conn, ch)
}

func (a attackSyncAdapter) SendActionOK() {
	a.s.sendActionOK(a.conn)
}

func (a attackSyncAdapter) SendWinExp(exp int, currentExp int) {
	a.s.sendWinExp(a.conn, exp, currentExp)
}

func (a attackSyncAdapter) SendLevelUp(ch storage.Character) {
	a.s.sendLevelUp(a.conn, ch)
}

func (a attackSyncAdapter) SendHealthSpellChanged(ch storage.Character) {
	a.s.sendHealthSpellChanged(a.conn, world.CharacterActorID(ch), a.s.world.AbilityStats(ch))
}

func (a attackSyncAdapter) BroadcastCharacterHit(ch storage.Character, attackIdent uint16) {
	if clients := a.s.ClientsAroundExcept(ch.MapID, ch.X, ch.Y, playerViewRange, a.conn); len(clients) > 0 {
		a.s.broadcastCharacterHit(clients, ch, attackIdent)
	}
}

func (a attackSyncAdapter) BroadcastHitImpact(result world.AttackResult) {
	if clients := a.s.ClientsAround(result.Character.MapID, result.MonsterX, result.MonsterY, playerViewRange); len(clients) > 0 {
		a.s.broadcastHitImpact(clients, result)
	}
}

type groupSyncAdapter struct {
	s *Server
}

func (a groupSyncAdapter) UpdateClient(ch storage.Character) {
	a.s.updateClientByCharacterID(ch)
}

func (a groupSyncAdapter) SendGroupCancel(ch storage.Character) {
	if client, ok := a.s.ClientByCharacterID(ch.ID); ok {
		client.writeCommand(a.s, mir176.Command{Ident: mir176.SMGroupCancel}, nil)
	}
}

func (a groupSyncAdapter) SendGroupMembers(ownerID string) {
	a.s.sendGroupMembers(ownerID)
}

func (s *Server) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(s.listeners))
	closers := []io.Closer{}
	for _, cfg := range s.listeners {
		ln, err := net.Listen("tcp", cfg.Addr)
		if err != nil {
			for _, c := range closers {
				_ = c.Close()
			}
			return err
		}
		closers = append(closers, ln)
		s.log.Info("listener started", "name", cfg.Name, "addr", cfg.Addr)
		wg.Add(1)
		go func(listener config.Listener, ln net.Listener) {
			defer wg.Done()
			for {
				conn, err := ln.Accept()
				if err != nil {
					select {
					case <-ctx.Done():
						return
					default:
						errCh <- err
						return
					}
				}
				disableNagle(conn)
				go s.handleConn(ctx, listener.Name, conn)
			}
		}(cfg, ln)
	}
	go func() {
		<-ctx.Done()
		for _, c := range closers {
			_ = c.Close()
		}
	}()
	go s.runWorldTicks(ctx)
	wg.Wait()
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func (s *Server) runWorldTicks(ctx context.Context) {
	interval := s.monsterTickInterval
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			result, err := s.world.Tick(s.PlayerSnapshots(), now)
			if err != nil {
				s.log.Info("world tick failed", "error", err)
				continue
			}
			s.applyWorldTick(result)
		}
	}
}

// disableNagle turns off Nagle's algorithm on newly accepted TCP
// connections. The game protocol is a stream of many small request/ack
// packets (a handful of bytes each way per Turn/Walk/Run/SitDown/Hit) with
// no batching, which is exactly the traffic shape Nagle plus the client's
// own delayed-ACK timer tends to stall for tens of milliseconds at a time —
// observed as the character intermittently freezing and then catching up.
// The reference server runs on Windows sockets, which default TCP_NODELAY
// off same as Go, but its Gate proxy layer and client are tuned around
// that; this project talks the client protocol directly, so it has to set
// this explicitly instead.

type RunLogin struct {
	Account   string
	CharName  string
	SessionID int32
	Version   int
	Code      int
}

func (s *Server) handleProtocol(ctx context.Context, conn net.Conn) {
	buf := make([]byte, 4096)
	pending := []byte{}
	var pendingLogin *RunLogin
	var activeChar *storage.Character
	var activeClient *Client
	defer func() {
		if activeClient != nil {
			s.unregisterClient(conn)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := conn.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			pending = append(pending, chunk...)
			var frames [][]byte
			frames, pending = mir176.SplitFrames(pending)
			for _, frame := range frames {
				if login, ok := decodeRunLogin(frame); ok {
					s.log.Info("game run login", "account", login.Account, "char", login.CharName, "session", login.SessionID, "version", login.Version, "code", login.Code)
					if !s.validateRunLogin(login) {
						s.log.Info("game run login rejected", "account", login.Account, "char", login.CharName, "session", login.SessionID)
						return
					}
					pendingLogin = &login
					s.sendNotice(conn)
					continue
				}
				cmd, text, err := mir176.DecodePlain6ClientMessage(frame)
				if err == nil && isPlausibleProtocolIdent(cmd.Ident) {
					if cmd.Ident == mir176.CMLoginNoticeOK && pendingLogin != nil {
						if ch, ok := s.sendEnterWorld(conn, *pendingLogin); ok {
							s.sendEnterWorldState(conn, ch)
							s.sendInitialLoginState(conn, ch)
							activeChar = &ch
							activeClient = s.registerClient(conn, ch)
							activeClient.active = activeChar
						}
						pendingLogin = nil
						continue
					}
					if activeChar != nil {
						switch cmd.Ident {
						case mir176.CMTurn:
							s.handleTurn(conn, activeChar, cmd)
						case mir176.CMWalk:
							s.handleMove(conn, activeChar, cmd, false)
						case mir176.CMRun:
							s.handleMove(conn, activeChar, cmd, true)
						case mir176.CMSitDown:
							s.handleSitDown(conn, activeChar, cmd)
						case mir176.CMHit, mir176.CMHeavyHit, mir176.CMBigHit, mir176.CMPowerHit, mir176.CMLongHit, mir176.CMWideHit, mir176.CMFireHit:
							s.handleHit(conn, activeChar, cmd)
						case mir176.CMSay, mir176.CMUserCommand:
							s.handleSay(conn, activeChar, text)
						case mir176.CMQueryUserName:
							s.handleQueryUserName(conn, activeChar, cmd)
						case mir176.CMQueryUserState:
							s.handleQueryUserState(conn, activeChar, cmd)
						case mir176.CMDropItem:
							s.handleDropItem(conn, activeChar, cmd, text)
						case mir176.CMPickup:
							s.handlePickup(conn, activeChar, cmd)
						case mir176.CMQueryBagItems:
							s.handleQueryBagItems(conn, activeChar)
						case mir176.CMGroupMode:
							s.handleGroupMode(conn, activeChar, cmd)
						case mir176.CMCreateGroup:
							s.handleCreateGroup(conn, activeChar, text)
						case mir176.CMAddGroupMember:
							s.handleAddGroupMember(conn, activeChar, text)
						case mir176.CMDelGroupMember:
							s.handleDelGroupMember(conn, activeChar, text)
						case mir176.CMEat:
							s.handleEatItem(conn, activeChar, cmd, text)
						case mir176.CMTakeOnItem:
							s.handleTakeOnItem(conn, activeChar, cmd, text)
						case mir176.CMTakeOffItem:
							s.handleTakeOffItem(conn, activeChar, cmd, text)
						}
					}
					continue
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) handleTurn(conn net.Conn, activeChar *storage.Character, cmd mir176.Command) {
	x := int(uint32(cmd.Recog) & 0xFFFF)
	y := int(uint32(cmd.Recog) >> 16)
	dir := int(cmd.Tag)
	updated, err := s.world.Turn(*activeChar, x, y, dir)
	if err != nil {
		s.log.Info("game turn rejected", "account", activeChar.Account, "char", activeChar.Name, "error", err)
		s.sendMoveFail(conn, activeChar)
		return
	}
	*activeChar = updated
	s.updateClient(conn, updated)
	s.sendActionOK(conn)
}

func (s *Server) handleMove(conn net.Conn, activeChar *storage.Character, cmd mir176.Command, run bool) {
	x := int(uint32(cmd.Recog) & 0xFFFF)
	y := int(uint32(cmd.Recog) >> 16)
	dir := int(cmd.Tag)
	move := s.world.Walk
	if run {
		move = s.world.Run
	}
	updated, err := move(*activeChar, x, y, dir)
	if err != nil {
		s.log.Info("game move rejected", "account", activeChar.Account, "char", activeChar.Name, "ident", cmd.Ident, "error", err)
		s.sendMoveFail(conn, activeChar)
		return
	}
	*activeChar = updated
	s.updateClient(conn, updated)
	s.sendActionOK(conn)
}

func (s *Server) handleSitDown(conn net.Conn, activeChar *storage.Character, cmd mir176.Command) {
	x := int(uint32(cmd.Recog) & 0xFFFF)
	y := int(uint32(cmd.Recog) >> 16)
	dir := int(cmd.Tag)
	updated, err := s.world.SitDown(*activeChar, x, y, dir)
	if err != nil {
		s.log.Info("game sitdown rejected", "account", activeChar.Account, "char", activeChar.Name, "error", err)
		s.sendMoveFail(conn, activeChar)
		return
	}
	*activeChar = updated
	s.updateClient(conn, updated)
	s.sendActionOK(conn)
}

func (s *Server) handleHit(conn net.Conn, activeChar *storage.Character, cmd mir176.Command) {
	x := int(uint32(cmd.Recog) & 0xFFFF)
	y := int(uint32(cmd.Recog) >> 16)
	dir := int(cmd.Tag)
	result, err := s.world.Hit(*activeChar, x, y, dir, s.PlayerCharacters()...)
	if err != nil {
		s.log.Info("game hit rejected", "account", activeChar.Account, "char", activeChar.Name, "ident", cmd.Ident, "error", err)
		s.sendMoveFail(conn, activeChar)
		return
	}
	*activeChar = result.Character
	world.ApplyAttackSync(attackSyncAdapter{s: s, conn: conn}, result, cmd.Ident)
	if result.MonsterID != "" {
		s.log.Info("game hit connected", "monster", result.MonsterID, "damage", result.Damage, "dead", result.Dead)
	}
}

func (s *Server) handleSay(conn net.Conn, activeChar *storage.Character, text []byte) {
	line := strings.TrimSpace(DecodeString(text))
	if line == "" {
		return
	}
	result, ok := s.world.HandleSay(*activeChar, line)
	if !ok {
		return
	}
	world.ApplySaySync(itemUseSyncAdapter{s: s, conn: conn}, *activeChar, result)
}

func (s *Server) handleGroupMode(conn net.Conn, activeChar *storage.Character, cmd mir176.Command) {
	updated, result, err := s.world.SetGroupModeWithResult(*activeChar, cmd.Param != 0)
	if err != nil {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMGroupModeChanged}, nil)
		return
	}
	*activeChar = updated
	world.ApplyGroupSync(groupSyncAdapter{s: s}, result.Sync)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMGroupModeChanged, Param: result.ResponseParam}, nil)
}

func (s *Server) handleCreateGroup(conn net.Conn, activeChar *storage.Character, text []byte) {
	targetName := strings.TrimSpace(DecodeString(text))
	if targetName == "" {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMCreateGroupFail, Recog: -2}, nil)
		return
	}
	target, ok := s.ClientByName(targetName)
	if !ok {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMCreateGroupFail, Recog: -2}, nil)
		return
	}
	updatedOwner, updatedTarget, result, err := s.world.CreateGroupWithResult(*activeChar, target.ch, len(s.onlineGroupMembers(activeChar.ID)))
	if err != nil {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMCreateGroupFail}, nil)
		return
	}
	*activeChar = updatedOwner
	target.ch = updatedTarget
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMCreateGroupOK}, nil)
	world.ApplyGroupSync(groupSyncAdapter{s: s}, result)
}

func (s *Server) handleAddGroupMember(conn net.Conn, activeChar *storage.Character, text []byte) {
	targetName := strings.TrimSpace(DecodeString(text))
	target, ok := s.ClientByName(targetName)
	if !ok {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMGroupAddMemFail, Recog: -2}, nil)
		return
	}
	updatedOwner, updatedTarget, result, err := s.world.AddGroupMemberWithResult(*activeChar, target.ch, len(s.onlineGroupMembers(activeChar.ID)))
	if err != nil {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMGroupAddMemFail}, nil)
		return
	}
	*activeChar = updatedOwner
	target.ch = updatedTarget
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMGroupAddMemOK}, nil)
	world.ApplyGroupSync(groupSyncAdapter{s: s}, result)
}

func (s *Server) handleDelGroupMember(conn net.Conn, activeChar *storage.Character, text []byte) {
	targetName := strings.TrimSpace(DecodeString(text))
	if targetName == "" {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMGroupDelMemFail, Recog: -2}, nil)
		return
	}
	target, ok := s.ClientByName(targetName)
	if !ok {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMGroupDelMemFail, Recog: -2}, nil)
		return
	}
	updatedOwner, updatedTarget, result, err := s.world.DelGroupMemberWithResult(*activeChar, target.ch)
	if err != nil {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMGroupDelMemFail}, nil)
		return
	}
	*activeChar = updatedOwner
	target.ch = updatedTarget
	world.ApplyGroupSync(groupSyncAdapter{s: s}, result)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMGroupDelMemOK}, EncodeString(target.ch.Name))
}

// handleTakeOnItem implements CM_TAKEONITEM using the reference
// MakeIndex, display-name, and equip-slot fields.
func (s *Server) handleTakeOnItem(conn net.Conn, activeChar *storage.Character, cmd mir176.Command, text []byte) {
	itemID := DecodeString(text)
	updated, result, err := s.world.EquipItemByBagIndexWithResult(*activeChar, int(cmd.Param), int(cmd.Recog), itemID)
	if err != nil {
		s.log.Info("game take on item rejected", "account", activeChar.Account, "char", activeChar.Name, "item", itemID, "make_index", cmd.Recog, "slot", cmd.Param, "error", err)
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMTakeOnFail}, nil)
		return
	}
	*activeChar = updated
	world.ApplyEquipSync(itemUseSyncAdapter{s: s, conn: conn}, result, mir176.SMTakeOnOK)
}

// handleTakeOffItem implements CM_TAKEOFFITEM (ClientTakeOffItems).
func (s *Server) handleTakeOffItem(conn net.Conn, activeChar *storage.Character, cmd mir176.Command, text []byte) {
	itemID := DecodeString(text)
	updated, result, err := s.world.UnequipItemByMakeIndexWithResult(*activeChar, int(cmd.Param), int(cmd.Recog), itemID)
	if err != nil {
		s.log.Info("game take off item rejected", "account", activeChar.Account, "char", activeChar.Name, "slot", cmd.Param, "item", itemID, "error", err)
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMTakeOffFail}, nil)
		return
	}
	*activeChar = updated
	world.ApplyUnequipSync(itemUseSyncAdapter{s: s, conn: conn}, result, mir176.SMTakeOffOK)
}

// handleDropItem implements CM_DROPITEM.
func (s *Server) handleDropItem(conn net.Conn, activeChar *storage.Character, cmd mir176.Command, text []byte) {
	itemID := DecodeString(text)
	updated, drop, err := s.world.DropItemCountByBagIndex(*activeChar, int(cmd.Recog), itemID, int(cmd.Param), s.PlayerCharacters()...)
	if err != nil {
		s.log.Info("game drop item rejected", "account", activeChar.Account, "char", activeChar.Name, "item", itemID, "make_index", cmd.Recog, "count", cmd.Param, "error", err)
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMDropItemFail, Recog: cmd.Recog}, nil)
		return
	}
	*activeChar = updated
	clients := s.ClientsInMap(updated.MapID)
	if len(clients) > 0 {
		s.broadcastDropAppear(clients, []world.GroundDrop{drop})
	}
	s.sendEquippedItems(conn, updated)
	s.sendWeightChanged(conn, s.world.AbilityStats(updated))
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMDropItemSuccess, Recog: cmd.Recog}, EncodeString(itemID))
}

// handlePickup implements CM_PICKUP.
func (s *Server) handlePickup(conn net.Conn, activeChar *storage.Character, cmd mir176.Command) {
	x := int(cmd.Param)
	y := int(cmd.Tag)
	updated, result, err := s.world.PickupAtWithResult(*activeChar, x, y)
	if err != nil {
		s.log.Info("game pickup rejected", "account", activeChar.Account, "char", activeChar.Name, "error", err)
		return
	}
	*activeChar = updated
	world.ApplyPickupSync(pickupSyncAdapter{s: s, conn: conn}, result)
}

// handleEatItem implements CM_EAT.
func (s *Server) handleEatItem(conn net.Conn, activeChar *storage.Character, cmd mir176.Command, text []byte) {
	_ = text
	updated, useResult, err := s.world.UseItemByBagIndex(*activeChar, int(cmd.Recog))
	if err != nil {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMEatFail}, nil)
		return
	}
	*activeChar = updated
	world.ApplyItemUseSync(itemUseSyncAdapter{s: s, conn: conn}, useResult)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMEatOK}, nil)
}

// handleQueryBagItems implements CM_QUERYBAGITEMS.
func (s *Server) handleQueryBagItems(conn net.Conn, activeChar *storage.Character) {
	body, count := BagItemsBodyAndCount(s.world, *activeChar)
	if len(body) == 0 {
		return
	}
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMBagItems, Recog: world.CharacterActorID(*activeChar), Param: 0, Tag: 0, Series: uint16(count)}, body)
}

// sendAbilityRefresh mirrors the reference equip refresh chain:
// SM_ABILITY, SM_SUBABILITY, SM_TAKEON_OK/SM_TAKEOFF_OK, then
// SM_FEATURECHANGED.
func (s *Server) sendAbilityRefresh(conn net.Conn, ch storage.Character, okIdent uint16) {
	feature := s.world.HumanFeatureForCharacter(ch)
	job := world.Plain6ClassID(ch.Class)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMAbility, Recog: int32(ch.Gold), Param: makeWord(byte(job), 99), Tag: uint16(ch.PremiumGold), Series: uint16(uint32(ch.PremiumGold) >> 16)}, EncodeBuffer(Ability(s.world.AbilityStats(ch))))
	s.sendCommand(conn, SubAbilityCommand(ch.Class), nil)
	s.sendCommand(conn, mir176.Command{Ident: okIdent, Recog: feature}, nil)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMFeatureChanged, Recog: world.CharacterActorID(ch), Param: uint16(feature), Tag: uint16(uint32(feature) >> 16)}, nil)
}

func (s *Server) sendAbilityOnly(conn net.Conn, ch storage.Character) {
	job := world.Plain6ClassID(ch.Class)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMAbility, Recog: int32(ch.Gold), Param: makeWord(byte(job), 99), Tag: uint16(ch.PremiumGold), Series: uint16(uint32(ch.PremiumGold) >> 16)}, EncodeBuffer(Ability(s.world.AbilityStats(ch))))
}

func (s *Server) sendWinExp(conn net.Conn, exp int, currentExp int) {
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMWinExp, Recog: int32(currentExp), Param: uint16(exp), Tag: uint16(uint32(exp) >> 16)}, nil)
}

func (s *Server) sendLevelUp(conn net.Conn, ch storage.Character) {
	stats := s.world.AbilityStats(ch)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMLevelUp, Recog: int32(stats.Exp), Param: uint16(stats.Level)}, nil)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMAbility, Recog: int32(ch.Gold), Param: makeWord(byte(world.Plain6ClassID(ch.Class)), 99), Tag: uint16(ch.PremiumGold), Series: uint16(uint32(ch.PremiumGold) >> 16)}, EncodeBuffer(Ability(stats)))
	s.sendCommand(conn, SubAbilityCommand(ch.Class), nil)
}

func (s *Server) sendInitialLoginState(conn net.Conn, ch storage.Character) {
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMAbility, Recog: int32(ch.Gold), Param: makeWord(byte(world.Plain6ClassID(ch.Class)), 99), Tag: uint16(ch.PremiumGold), Series: uint16(uint32(ch.PremiumGold) >> 16)}, EncodeBuffer(Ability(s.world.AbilityStats(ch))))
	s.sendCommand(conn, SubAbilityCommand(ch.Class), nil)
	s.sendCommand(conn, DayChangingCommand(0, 0), nil)
	s.sendEquippedItems(conn, ch)
	s.sendBagItems(conn, ch)
	s.sendUseMagic(conn, ch)
}

// sendActionOK acknowledges a successful Turn/Walk/Run/SitDown/Hit the
// way the original server does for the acting player: a raw, unencoded
// "+GOOD/<tick>" status-good string (sSTATUS_GOOD in the original Delphi
// MirClient/M2Server Grobal2.pas, sent via ObjBase.pas's
// `SendSocket(nil, sSTATUS_GOOD + IntToStr(GetTickCount))`), relayed with no
// further encoding. The client itself confirms this literal: ClMain.pas
// splits the tag off an incoming "+"-prefixed status string and compares
// `if tagstr = 'GOOD' then ...`, so anything other than "+GOOD/" is not
// recognized as the good-tick ack. (The mirbeta-OpenMir2 C# rewrite shortens
// this to "+GD/" — MessageSettings.sSTATUS_GOOD — but that string never
// reaches an official client in this codebase's compatibility target, so the
// original Delphi source wins here.) The SM_TURN/SM_WALK/SM_HIT/… command
// replies are reserved for OTHER nearby players observing the action
// (PlayObject.Message.cs RM_* handlers all guard on
// `processMsg.ActorId != ActorId`); sending one to the actor itself is not
// part of the protocol and leaves the client waiting for the ack it
// actually expects.
func (s *Server) sendActionOK(conn net.Conn) {
	ack := fmt.Sprintf("+GOOD/%d", uint32(time.Now().UnixMilli()))
	if _, err := conn.Write(mir176.WrapFrame([]byte(ack))); err != nil {
		s.log.Info("game action ack failed", "error", err)
		return
	}
	s.log.Info("game action ack sent", "ack", ack)
}

// sendMoveFail resyncs the client to the server's authoritative position
// and facing after a rejected action, mirroring the reference server's
// unconditional (self included) SM_MOVEFAIL reply
// (PlayObject.Message.cs RM_MOVEFAIL case).
func (s *Server) sendMoveFail(conn net.Conn, activeChar *storage.Character) {
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMMoveFail, Recog: world.CharacterActorID(*activeChar), Param: uint16(activeChar.X), Tag: uint16(activeChar.Y), Series: uint16(activeChar.Dir)}, nil)
}

func (s *Server) sendNotice(conn net.Conn) {
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMSendNotice, Recog: 2000}, NoticeBody())
}

func (s *Server) sendGoldChanged(conn net.Conn, gold int) {
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMGoldChanged, Recog: int32(gold)}, nil)
}

func (s *Server) handleQueryUserName(conn net.Conn, activeChar *storage.Character, cmd mir176.Command) {
	targetID := cmd.Recog
	target, ok := s.ClientByActorID(targetID)
	if !ok && activeChar != nil && world.CharacterActorID(*activeChar) == targetID {
		target = &Client{ch: *activeChar}
		ok = true
	}
	if !ok {
		return
	}
	targetChar := target.character()
	x := int(cmd.Param)
	y := int(cmd.Tag)
	if !world.CanInspectCharacterAt(targetChar, x, y) {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMGhost, Recog: targetID, Param: uint16(x), Tag: uint16(y)}, nil)
		return
	}
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMUserName, Recog: targetID, Param: s.world.CharacterNameColor(targetChar)}, EncodeString(s.world.CharacterDisplayName(targetChar)))
}

func (s *Server) handleQueryUserState(conn net.Conn, activeChar *storage.Character, cmd mir176.Command) {
	targetID := cmd.Recog
	target, ok := s.ClientByActorID(targetID)
	if !ok && activeChar != nil && world.CharacterActorID(*activeChar) == targetID {
		target = &Client{ch: *activeChar}
		ok = true
	}
	if !ok {
		return
	}
	targetChar := target.character()
	x := int(cmd.Param)
	y := int(cmd.Tag)
	if !world.CanInspectCharacterAt(targetChar, x, y) {
		return
	}
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMSendUserState}, EncodeBuffer(UserStateBody(s.world, targetChar)))
}

func NoticeBody() []byte {
	return EncodeString("OpenMir2 \x1b")
}

func UserStateBody(w *world.World, ch storage.Character) []byte {
	body := bytes.NewBuffer(make([]byte, 0, 1024))
	writeI32(body, w.HumanFeatureForCharacter(ch))
	writeGBKAsciiString(body, w.CharacterDisplayName(ch), 14)
	writeByte(body, 0)
	writeU32(body, uint32(w.CharacterNameColor(ch)))
	writeGBKAsciiString(body, "", 20)
	writeGBKAsciiString(body, "", 14)
	for slot := 0; slot < 13; slot++ {
		equipped, ok := equippedItem(ch, slot)
		if !ok {
			body.Write(ClientItemBody(data.StdItem{}, [14]byte{}, 0, 0, 0))
			continue
		}
		item, ok := w.Item(equipped.ItemID)
		if !ok {
			body.Write(ClientItemBody(data.StdItem{}, [14]byte{}, 0, 0, 0))
			continue
		}
		item = world.UpgradeClientItemForDisplay(item, equipped, false)
		dura, duraMax := bagItemDurability(item, equipped)
		body.Write(ClientItemBody(item, equipped.Desc, equipped.MakeIndex, dura, duraMax))
	}
	writeByte(body, 0)
	writeGBKAsciiString(body, "", 14)
	return body.Bytes()
}

func decodeRunLogin(frame []byte) (RunLogin, bool) {
	encoded, err := mir176.UnwrapFrame(frame)
	if err != nil {
		return RunLogin{}, false
	}
	if len(encoded) > 0 && encoded[0] >= '1' && encoded[0] <= '9' {
		encoded = encoded[1:]
	}
	text, err := mir176.DecodePlain6Payload(encoded)
	if err != nil {
		return RunLogin{}, false
	}
	if !strings.HasPrefix(string(text), "**") {
		return RunLogin{}, false
	}
	parts := strings.Split(string(text[2:]), "/")
	if len(parts) < 5 {
		return RunLogin{}, false
	}
	sessionID, err := strconv.Atoi(parts[2])
	if err != nil {
		return RunLogin{}, false
	}
	version, err := strconv.Atoi(parts[3])
	if err != nil {
		return RunLogin{}, false
	}
	code, err := strconv.Atoi(parts[4])
	if err != nil {
		return RunLogin{}, false
	}
	return RunLogin{Account: parts[0], CharName: parts[1], SessionID: int32(sessionID), Version: version, Code: code}, true
}

func (s *Server) validateRunLogin(login RunLogin) bool {
	if account, ok := s.sessionAccount(login.SessionID); ok && account != login.Account {
		return false
	}
	for _, ch := range s.store.Characters(login.Account) {
		if ch.Name == login.CharName {
			return true
		}
	}
	return false
}

const (
	ActorID         = int32(1)
	playerViewRange = 12
)

func (s *Server) sendEnterWorld(conn net.Conn, login RunLogin) (storage.Character, bool) {
	ch, ok := s.characterByName(login.Account, login.CharName)
	if !ok {
		return storage.Character{}, false
	}
	if ch.HP <= 0 {
		revived, err := s.world.ReviveCharacterAtHome(ch)
		if err != nil {
			return storage.Character{}, false
		}
		ch = revived
	}
	if normalized, changed := s.world.NormalizeCharacterState(ch); changed {
		ch = normalized
	}
	return ch, true
}

func (s *Server) sendEnterWorldState(conn net.Conn, ch storage.Character) {
	versionDate := ch.SoftVersionDate
	s.clientMu.Lock()
	if client := s.clients[conn]; client != nil {
		client.softVersionDate = versionDate
		client.visibleMonsters = map[string]world.Monster{}
		client.visibleDrops = map[string]world.GroundDrop{}
	}
	s.clientMu.Unlock()
	actorID := world.CharacterActorID(ch)
	feature := s.world.HumanFeatureForCharacter(ch)
	light := s.world.MapLight(ch.MapID)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMNewMap, Recog: actorID, Param: uint16(ch.X), Tag: uint16(ch.Y)}, EncodeString(ch.MapID))
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMChangeLight, Recog: actorID, Param: uint16(light), Tag: 500}, nil)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMLogon, Recog: actorID, Param: uint16(ch.X), Tag: uint16(ch.Y), Series: makeWord(byte(ch.Dir), byte(light))}, EncodeBuffer(LogonBody(feature, s.world.CharacterStatus(ch), ch.AllowGroup, s.world.CharacterFeatureEx(ch))))
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMFeatureChanged, Recog: actorID, Param: uint16(feature), Tag: uint16(uint32(feature) >> 16), Series: uint16(s.world.CharacterFeatureEx(ch))}, nil)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMAttackMode, Recog: int32(s.world.CharacterAttackMode(ch))}, nil)
	if versionDate != 0 {
		s.sendCommand(conn, ServerConfigCommand(), EncodeBuffer(ServerConfigBody()))
	}
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMUserName, Recog: actorID, Param: s.world.CharacterNameColor(ch)}, EncodeString(s.world.CharacterDisplayName(ch)))
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMAreaState, Recog: s.world.CharacterAreaState(ch)}, nil)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMMapDescription, Recog: -1}, EncodeString(s.world.MapName(ch.MapID)))
	if versionDate != 0 {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMGameGoldName, Recog: int32(ch.PremiumGold), Param: uint16(ch.PremiumPoint), Tag: uint16(uint32(ch.PremiumPoint) >> 16)}, GoldNameBody())
	}
	monsters, _ := s.world.SnapshotAround(ch.MapID, ch.X, ch.Y, playerViewRange)
	for _, mon := range monsters {
		s.sendCommand(conn, MonsterTurnCommand(mon), MonsterTurnBody(mon))
		s.sendCommand(conn, MonsterFeatureCommand(mon), nil)
	}
	_, drops := s.world.SnapshotAround(ch.MapID, ch.X, ch.Y, playerViewRange)
	for _, drop := range drops {
		s.sendDropShow(conn, drop)
	}
}

func (s *Server) sendSpaceMoveState(conn net.Conn, ch storage.Character) {
	versionDate := ch.SoftVersionDate
	s.clientMu.Lock()
	if client := s.clients[conn]; client != nil {
		client.visibleMonsters = map[string]world.Monster{}
		client.visibleDrops = map[string]world.GroundDrop{}
	}
	s.clientMu.Unlock()
	actorID := world.CharacterActorID(ch)
	showIdent := uint16(mir176.SMSpacemoveShow)
	showBody := EncodeBuffer(CharDesc(s.world.HumanFeatureForCharacter(ch), s.world.CharacterStatus(ch)))
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMSpacemoveHide, Recog: actorID}, nil)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMClearObjects, Recog: actorID}, nil)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMChangeMap, Recog: actorID, Param: uint16(ch.X), Tag: uint16(ch.Y), Series: uint16(s.world.CharacterAreaState(ch))}, EncodeString(ch.MapID))
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMAreaState, Recog: s.world.CharacterAreaState(ch)}, nil)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMMapDescription, Recog: -1}, EncodeString(s.world.MapName(ch.MapID)))
	if versionDate != 0 {
		s.sendCommand(conn, ServerConfigCommand(), EncodeBuffer(ServerConfigBody()))
	}
	s.sendCommand(conn, mir176.Command{Ident: showIdent, Recog: actorID, Param: uint16(ch.X), Tag: uint16(ch.Y), Series: makeWord(byte(ch.Dir), byte(s.world.MapLight(ch.MapID)))}, showBody)
}

// sendHealthSpellChanged sends SM_HEALTHSPELLCHANGED (RM_HEALTHSPELLCHANGED
// in the reference server's BaseObject.HealthSpellChanged), which drives the
// client's always-on HP/MP orb HUD. That HUD reads current HP/MP separately
// from the SM_ABILITY panel: the reference sends it whenever HP/MP changes,
// typically from regen, combat, or potion-heal paths, so the orb can stay in
// sync with the data panel.
func (s *Server) sendHealthSpellChanged(conn net.Conn, actorID int32, stats world.AbilityStats) {
	s.sendCommand(conn, mir176.Command{
		Ident:  mir176.SMHealthSpellChanged,
		Recog:  actorID,
		Param:  uint16(stats.HP),
		Tag:    uint16(stats.MP),
		Series: uint16(stats.MaxHP),
	}, nil)
}

func (s *Server) sendWeightChanged(conn net.Conn, stats world.AbilityStats) {
	s.sendCommand(conn, mir176.Command{
		Ident:  mir176.SMWeightChanged,
		Recog:  int32(stats.Weight),
		Param:  uint16(stats.WearWeight),
		Tag:    uint16(stats.HandWeight),
		Series: uint16(((stats.Weight + stats.WearWeight + stats.HandWeight) ^ 0x3A5F ^ 0x1F35 ^ 0xAA21)),
	}, nil)
}

func (s *Server) broadcastMonsterAppear(clients []*Client, monsters []world.Monster) {
	for _, mon := range monsters {
		if mon.Hidden {
			continue
		}
		nearby := s.ClientsAround(mon.MapID, mon.X, mon.Y, playerViewRange)
		for _, client := range nearby {
			client.ensureMonsterVisible(s, mon)
		}
	}
}

func (s *Server) broadcastDropAppear(clients []*Client, drops []world.GroundDrop) {
	for _, drop := range drops {
		for _, client := range clients {
			client.ensureDropVisible(s, drop)
		}
	}
}

func (s *Server) broadcastDropHide(clients []*Client, dropID string) {
	for _, client := range clients {
		client.forgetDrop(s, dropID)
	}
}

func (s *Server) broadcastTeleportMove(conn net.Conn, from, to storage.Character) {
	if from.MapID != "" {
		clients := s.ClientsAroundExcept(from.MapID, from.X, from.Y, playerViewRange, conn)
		if len(clients) > 0 {
			s.broadcastCharacterDisappear(clients, from)
		}
	}
	clients := s.ClientsAroundExcept(to.MapID, to.X, to.Y, playerViewRange, conn)
	if len(clients) > 0 {
		s.broadcastCharacterAppear(clients, to)
	}
}

func (s *Server) broadcastCharacterDisappear(clients []*Client, ch storage.Character) {
	cmd := mir176.Command{Ident: mir176.SMDisappear, Recog: world.CharacterActorID(ch)}
	for _, client := range clients {
		client.writeCommand(s, cmd, nil)
	}
}

func (s *Server) broadcastCharacterAppear(clients []*Client, ch storage.Character) {
	actorID := world.CharacterActorID(ch)
	feature := s.world.HumanFeatureForCharacter(ch)
	logonCmd := mir176.Command{
		Ident:  mir176.SMLogon,
		Recog:  actorID,
		Param:  uint16(ch.X),
		Tag:    uint16(ch.Y),
		Series: makeWord(byte(ch.Dir), byte(s.world.MapLight(ch.MapID))),
	}
	featureCmd := mir176.Command{
		Ident:  mir176.SMFeatureChanged,
		Recog:  actorID,
		Param:  uint16(feature),
		Tag:    uint16(uint32(feature) >> 16),
		Series: uint16(s.world.CharacterFeatureEx(ch)),
	}
	nameCmd := mir176.Command{
		Ident: mir176.SMUserName,
		Recog: actorID,
		Param: s.world.CharacterNameColor(ch),
	}
	logonBody := EncodeBuffer(LogonBody(feature, s.world.CharacterStatus(ch), ch.AllowGroup, s.world.CharacterFeatureEx(ch)))
	nameBody := EncodeString(s.world.CharacterDisplayName(ch))
	for _, client := range clients {
		client.writeCommand(s, logonCmd, logonBody)
		client.writeCommand(s, featureCmd, nil)
		client.writeCommand(s, nameCmd, nameBody)
	}
}

func (s *Server) applyWorldTick(result world.TickResult) {
	hitIDs := map[string]struct{}{}
	for _, hit := range result.CharacterHits {
		if hit.Character.ID != "" {
			hitIDs[hit.Character.ID] = struct{}{}
		}
	}
	for _, ch := range result.Characters {
		s.updateClientByCharacterID(ch)
		if _, ok := hitIDs[ch.ID]; ok {
			continue
		}
		if client, ok := s.ClientByCharacterID(ch.ID); ok {
			client.writeCommand(s, mir176.Command{
				Ident:  mir176.SMHealthSpellChanged,
				Recog:  world.CharacterActorID(ch),
				Param:  uint16(ch.HP),
				Tag:    uint16(ch.MP),
				Series: uint16(s.world.AbilityStats(ch).MaxHP),
			}, nil)
		}
	}
	for _, action := range result.MonsterActions {
		clients := s.ClientsAround(action.MapID, action.X, action.Y, playerViewRange)
		if len(clients) == 0 {
			continue
		}
		switch action.Kind {
		case world.MonsterActionWalk:
			s.broadcastMonsterWalk(clients, action)
		case world.MonsterActionHit:
			s.broadcastMonsterHit(clients, action)
		case world.MonsterActionTurn:
			s.broadcastMonsterTurn(clients, action)
		case world.MonsterActionReveal:
			s.broadcastMonsterReveal(clients, action)
		case world.MonsterActionHide:
			s.broadcastMonsterHide(clients, action)
		}
	}
	for _, hit := range result.CharacterHits {
		clients := s.ClientsAround(hit.Character.MapID, hit.Character.X, hit.Character.Y, playerViewRange)
		if len(clients) > 0 {
			s.broadcastCharacterStruck(clients, hit)
		}
	}
	s.syncVisibleMonsters()
	s.syncVisibleDrops()
}

func (s *Server) syncVisibleMonsters() {
	for _, client := range s.allClients() {
		ch := client.character()
		monsters, _ := s.world.SnapshotAround(ch.MapID, ch.X, ch.Y, playerViewRange)
		current := map[string]struct{}{}
		for _, mon := range monsters {
			current[mon.ID] = struct{}{}
			client.ensureMonsterVisible(s, mon)
		}
		client.forgetMissingMonsters(s, current)
	}
}

func (s *Server) syncVisibleDrops() {
	for _, client := range s.allClients() {
		ch := client.character()
		_, drops := s.world.SnapshotAround(ch.MapID, ch.X, ch.Y, playerViewRange)
		current := map[string]struct{}{}
		for _, drop := range drops {
			current[drop.ID] = struct{}{}
			client.ensureDropVisible(s, drop)
		}
		client.forgetMissingDrops(s, current)
	}
}

func (s *Server) broadcastMonsterWalk(clients []*Client, action world.MonsterAction) {
	for _, client := range clients {
		client.ensureMonsterVisible(s, world.Monster{ID: action.MonsterID, Name: action.Name, RaceImg: action.RaceImg, MonsterWeapon: action.MonsterWeapon, Appr: action.Appr, MapID: action.MapID, X: action.X, Y: action.Y})
		client.writeCommand(s, MonsterWalkCommand(action), nil)
	}
}

func (s *Server) broadcastMonsterHit(clients []*Client, action world.MonsterAction) {
	for _, client := range clients {
		client.ensureMonsterVisible(s, world.Monster{ID: action.MonsterID, Name: action.Name, RaceImg: action.RaceImg, MonsterWeapon: action.MonsterWeapon, Appr: action.Appr, MapID: action.MapID, X: action.X, Y: action.Y})
		client.writeCommand(s, MonsterHitCommand(action), nil)
	}
}

func (s *Server) broadcastMonsterTurn(clients []*Client, action world.MonsterAction) {
	mon := world.Monster{ID: action.MonsterID, Name: action.Name, RaceImg: action.RaceImg, MonsterWeapon: action.MonsterWeapon, Appr: action.Appr, MapID: action.MapID, X: action.X, Y: action.Y, Dir: action.Dir}
	for _, client := range clients {
		client.ensureMonsterVisible(s, mon)
		client.writeCommand(s, MonsterTurnCommand(mon), MonsterTurnBody(mon))
	}
}

func (s *Server) broadcastMonsterReveal(clients []*Client, action world.MonsterAction) {
	mon := world.Monster{ID: action.MonsterID, Name: action.Name, RaceImg: action.RaceImg, MonsterWeapon: action.MonsterWeapon, Appr: action.Appr, MapID: action.MapID, X: action.X, Y: action.Y, Dir: action.Dir}
	for _, client := range clients {
		client.writeCommand(s, MonsterDigUpCommand(mon), MonsterDigUpBody(mon))
		client.mu.Lock()
		if client.visibleMonsters == nil {
			client.visibleMonsters = map[string]world.Monster{}
		}
		client.visibleMonsters[mon.ID] = mon
		client.mu.Unlock()
	}
}

func (s *Server) broadcastMonsterHide(clients []*Client, action world.MonsterAction) {
	mon := world.Monster{ID: action.MonsterID, Name: action.Name, RaceImg: action.RaceImg, MonsterWeapon: action.MonsterWeapon, Appr: action.Appr, MapID: action.MapID, X: action.X, Y: action.Y, Dir: action.Dir}
	for _, client := range clients {
		client.writeCommand(s, MonsterDigDownCommand(mon), nil)
		client.forgetMonster(mon.ID)
	}
}

func (s *Server) broadcastCharacterHit(clients []*Client, ch storage.Character, clientIdent uint16) {
	cmd := CharacterHitCommand(ch, clientIdent)
	for _, client := range clients {
		client.writeCommand(s, cmd, nil)
	}
}

func (s *Server) broadcastHitImpact(clients []*Client, result world.AttackResult) {
	if result.Dead {
		s.broadcastMonsterStruck(clients, result)
		s.broadcastMonsterDeath(clients, result)
		if len(result.Drops) > 0 {
			s.broadcastDropAppear(clients, result.Drops)
		}
		return
	}
	if s.hitImpactDelay <= 0 {
		s.broadcastMonsterStruck(clients, result)
		return
	}
	time.AfterFunc(s.hitImpactDelay, func() {
		s.broadcastMonsterStruck(clients, result)
	})
}

func (s *Server) broadcastCharacterStruck(clients []*Client, hit world.CharacterHit) {
	if s.hitImpactDelay <= 0 {
		s.sendCharacterStruck(clients, hit)
		return
	}
	time.AfterFunc(s.hitImpactDelay, func() {
		s.sendCharacterStruck(clients, hit)
	})
}

func (s *Server) sendCharacterStruck(clients []*Client, hit world.CharacterHit) {
	for _, client := range clients {
		client.writeCommand(s, CharacterStruckCommand(hit), EncodeBuffer(MessageBodyWL(s.world.HumanFeatureForCharacter(hit.Character), 0, world.MonsterActorID(world.Monster{ID: hit.AttackerID}), 0)))
		stats := s.world.AbilityStats(hit.Character)
		client.writeCommand(s, mir176.Command{
			Ident:  mir176.SMHealthSpellChanged,
			Recog:  world.CharacterActorID(hit.Character),
			Param:  uint16(hit.Character.HP),
			Tag:    uint16(hit.Character.MP),
			Series: uint16(stats.MaxHP),
		}, nil)
		if hit.Dead {
			client.writeCommand(s, CharacterDeathCommand(hit.Character), EncodeBuffer(CharDesc(s.world.HumanFeatureForCharacter(hit.Character), 0)))
		}
	}
}

func (s *Server) broadcastMonsterStruck(clients []*Client, result world.AttackResult) {
	for _, client := range clients {
		feature := world.MonsterFeature(world.Monster{RaceImg: result.MonsterRaceImg, MonsterWeapon: result.MonsterWeapon, Appr: result.MonsterAppr})
		client.writeCommand(s, MonsterStruckCommand(result), EncodeBuffer(MessageBodyWL(feature, 0, ActorID, 0)))
	}
}

func (s *Server) broadcastMonsterDeath(clients []*Client, result world.AttackResult) {
	for _, client := range clients {
		feature := world.MonsterFeature(world.Monster{RaceImg: result.MonsterRaceImg, MonsterWeapon: result.MonsterWeapon, Appr: result.MonsterAppr})
		client.writeCommand(s, MonsterDeathCommand(result), EncodeBuffer(CharDesc(feature, 0)))
		client.forgetMonster(result.MonsterID)
	}
}

func (s *Server) registerClient(conn net.Conn, ch storage.Character) *Client {
	versionDate := ch.SoftVersionDate
	client := &Client{conn: conn, ch: ch, softVersionDate: versionDate, visibleMonsters: map[string]world.Monster{}}
	monsters, _ := s.world.SnapshotAround(ch.MapID, ch.X, ch.Y, playerViewRange)
	for _, mon := range monsters {
		client.visibleMonsters[mon.ID] = mon
	}
	s.clientMu.Lock()
	s.clients[conn] = client
	s.clientMu.Unlock()
	return client
}

func (s *Server) unregisterClient(conn net.Conn) {
	s.clientMu.Lock()
	client := s.clients[conn]
	delete(s.clients, conn)
	s.clientMu.Unlock()
	if client != nil {
		s.handleClientDisconnect(client.ch)
	}
}

func (s *Server) updateClient(conn net.Conn, ch storage.Character) {
	s.clientMu.Lock()
	client := s.clients[conn]
	if client != nil {
		client.ch = ch
		if client.active != nil {
			*client.active = ch
		}
	}
	s.clientMu.Unlock()
}

func (s *Server) updateClientByCharacterID(ch storage.Character) {
	s.clientMu.Lock()
	for _, client := range s.clients {
		if client.ch.ID != ch.ID {
			continue
		}
		client.ch = ch
		if client.active != nil {
			*client.active = ch
		}
	}
	s.clientMu.Unlock()
}

func (s *Server) ClientByCharacterID(id string) (*Client, bool) {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	for _, client := range s.clients {
		if client.ch.ID == id {
			return client, true
		}
	}
	return nil, false
}

func (s *Server) ClientByActorID(actorID int32) (*Client, bool) {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	for _, client := range s.clients {
		if world.CharacterActorID(client.ch) == actorID {
			return client, true
		}
	}
	return nil, false
}

func (s *Server) ClientByName(name string) (*Client, bool) {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	for _, client := range s.clients {
		if client.ch.Name == name {
			return client, true
		}
	}
	return nil, false
}

func (s *Server) onlineGroupMembers(ownerID string) []*Client {
	ownerClient, ok := s.ClientByCharacterID(ownerID)
	if !ok {
		return nil
	}
	owner := ownerClient.character()
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	clients := make([]*Client, 0, len(owner.GroupMembers))
	for _, memberID := range owner.GroupMembers {
		for _, client := range s.clients {
			if client.ch.ID == memberID {
				clients = append(clients, client)
				break
			}
		}
	}
	sortClientsByID(clients)
	return clients
}

func (s *Server) sendGroupMembers(ownerID string) {
	clients := s.onlineGroupMembers(ownerID)
	if len(clients) == 0 {
		return
	}
	names := make([]string, 0, len(clients))
	for _, client := range clients {
		names = append(names, client.ch.Name)
	}
	body := EncodeString(strings.Join(names, "/") + "/")
	for _, client := range clients {
		client.writeCommand(s, mir176.Command{Ident: mir176.SMGroupMembers}, body)
	}
}

func (s *Server) handleClientDisconnect(ch storage.Character) {
	changed, result, err := s.world.HandleGroupDisconnectWithResult(ch)
	if err != nil {
		return
	}
	_ = changed
	world.ApplyGroupSync(groupSyncAdapter{s: s}, result)
}

func (s *Server) PlayerSnapshots() []world.PlayerSnapshot {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	players := make([]world.PlayerSnapshot, 0, len(s.clients))
	for _, client := range s.clients {
		players = append(players, world.PlayerSnapshot{Character: client.ch})
	}
	sort.Slice(players, func(i, j int) bool {
		if players[i].Character.ID == players[j].Character.ID {
			return players[i].Character.Name < players[j].Character.Name
		}
		return players[i].Character.ID < players[j].Character.ID
	})
	return players
}

func (s *Server) PlayerCharacters() []storage.Character {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	chars := make([]storage.Character, 0, len(s.clients))
	for _, client := range s.clients {
		chars = append(chars, client.ch)
	}
	sort.Slice(chars, func(i, j int) bool {
		if chars[i].ID == chars[j].ID {
			return chars[i].Name < chars[j].Name
		}
		return chars[i].ID < chars[j].ID
	})
	return chars
}

func (s *Server) allClients() []*Client {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	clients := make([]*Client, 0, len(s.clients))
	for _, client := range s.clients {
		clients = append(clients, client)
	}
	sortClientsByID(clients)
	return clients
}

func (s *Server) ClientsInMap(mapID string) []*Client {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	clients := make([]*Client, 0, len(s.clients))
	for _, client := range s.clients {
		if client.ch.MapID == mapID {
			clients = append(clients, client)
		}
	}
	sortClientsByID(clients)
	return clients
}

func (s *Server) ClientsAround(mapID string, x, y, viewRange int) []*Client {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	clients := make([]*Client, 0, len(s.clients))
	for _, client := range s.clients {
		if client.ch.MapID != mapID {
			continue
		}
		if !world.CanObserveAt(client.ch, x, y, viewRange) {
			continue
		}
		clients = append(clients, client)
	}
	sortClientsByID(clients)
	return clients
}

func (s *Server) ClientsAroundExcept(mapID string, x, y, viewRange int, except net.Conn) []*Client {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	clients := make([]*Client, 0, len(s.clients))
	for conn, client := range s.clients {
		if conn == except || client.ch.MapID != mapID {
			continue
		}
		if !world.CanObserveAt(client.ch, x, y, viewRange) {
			continue
		}
		clients = append(clients, client)
	}
	sortClientsByID(clients)
	return clients
}

func (s *Server) ClientsInMapExcept(mapID string, except net.Conn) []*Client {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	clients := make([]*Client, 0, len(s.clients))
	for conn, client := range s.clients {
		if conn != except && client.ch.MapID == mapID {
			clients = append(clients, client)
		}
	}
	sortClientsByID(clients)
	return clients
}

func sortClientsByID(clients []*Client) {
	sort.Slice(clients, func(i, j int) bool {
		if clients[i].ch.ID == clients[j].ch.ID {
			return clients[i].ch.Name < clients[j].ch.Name
		}
		return clients[i].ch.ID < clients[j].ch.ID
	})
}

func (s *Server) broadcastHear(clients []*Client, msg string, fg, bg byte) {
	for _, client := range clients {
		client.writeCommand(s, mir176.Command{Ident: mir176.SMHear, Param: makeWord(fg, bg), Series: 1}, EncodeString(msg))
	}
}

func (s *Server) sendHear(conn net.Conn, msg string, fg, bg byte) {
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMHear, Param: makeWord(fg, bg), Series: 1}, EncodeString(msg))
}

func (c *Client) writeCommand(s *Server, cmd mir176.Command, text []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeCommandLocked(s, cmd, text)
}

func (c *Client) ensureMonsterVisible(s *Server, mon world.Monster) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.visibleMonsters == nil {
		c.visibleMonsters = map[string]world.Monster{}
	}
	if _, ok := c.visibleMonsters[mon.ID]; ok {
		c.visibleMonsters[mon.ID] = mon
		return
	}
	c.writeCommandLocked(s, MonsterTurnCommand(mon), MonsterTurnBody(mon))
	c.writeCommandLocked(s, MonsterFeatureCommand(mon), nil)
	c.visibleMonsters[mon.ID] = mon
}

func (c *Client) forgetMonster(monsterID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.visibleMonsters, monsterID)
}

func (c *Client) forgetMissingMonsters(s *Server, current map[string]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.visibleMonsters == nil {
		return
	}
	for monsterID, mon := range c.visibleMonsters {
		if _, ok := current[monsterID]; ok {
			continue
		}
		s.sendCommand(c.conn, MonsterDisappearCommand(mon), nil)
		delete(c.visibleMonsters, monsterID)
	}
}

func (c *Client) ensureDropVisible(s *Server, drop world.GroundDrop) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.visibleDrops == nil {
		c.visibleDrops = map[string]world.GroundDrop{}
	}
	if _, ok := c.visibleDrops[drop.ID]; ok {
		return
	}
	s.sendDropShowLocked(c, drop)
	c.visibleDrops[drop.ID] = drop
}

func (c *Client) forgetDrop(s *Server, dropID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.visibleDrops == nil {
		return
	}
	drop, ok := c.visibleDrops[dropID]
	if !ok {
		return
	}
	s.sendDropHideLocked(c, drop)
	delete(c.visibleDrops, dropID)
}

func (c *Client) forgetMissingDrops(s *Server, current map[string]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.visibleDrops == nil {
		return
	}
	for dropID := range c.visibleDrops {
		if _, ok := current[dropID]; ok {
			continue
		}
		s.sendDropHideLocked(c, c.visibleDrops[dropID])
		delete(c.visibleDrops, dropID)
	}
}

func (c *Client) character() storage.Character {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ch
}

func (c *Client) writeCommandLocked(s *Server, cmd mir176.Command, text []byte) {
	response := encodeMessage(cmd, text)
	if _, err := c.conn.Write(response); err != nil {
		s.log.Info("game response failed", "ident", cmd.Ident, "error", err)
		return
	}
	s.log.Info("game response sent", "ident", cmd.Ident, "text_len", len(text))
}

func (s *Server) sendCommand(conn net.Conn, cmd mir176.Command, text []byte) {
	response := encodeMessage(cmd, text)
	if _, err := conn.Write(response); err != nil {
		s.log.Info("game response failed", "ident", cmd.Ident, "error", err)
		return
	}
	s.log.Info("game response sent", "ident", cmd.Ident, "text_len", len(text))
}

func encodeMessage(cmd mir176.Command, text []byte) []byte {
	payload := append(mir176.EncodePlain6Command(cmd), text...)
	return mir176.WrapFrame(payload)
}

func (s *Server) characterByName(account, name string) (storage.Character, bool) {
	for _, ch := range s.store.Characters(account) {
		if ch.Name == name {
			return ch, true
		}
	}
	return storage.Character{}, false
}

func MessageBodyWL(param1, param2, tag1, tag2 int32) []byte {
	body := make([]byte, 16)
	binary.LittleEndian.PutUint32(body[0:4], uint32(param1))
	binary.LittleEndian.PutUint32(body[4:8], uint32(param2))
	binary.LittleEndian.PutUint32(body[8:12], uint32(tag1))
	binary.LittleEndian.PutUint32(body[12:16], uint32(tag2))
	return body
}

func LogonBody(feature, status int32, allowGroup bool, featureEx int32) []byte {
	body := MessageBodyWL(feature, status, 0, 0)
	if allowGroup {
		binary.LittleEndian.PutUint32(body[8:12], uint32(1)|uint32(uint16(featureEx))<<16)
	}
	return body
}

func SubAbilityCommand(class string) mir176.Command {
	return mir176.Command{
		Ident:  mir176.SMSubAbility,
		Recog:  int32(makeWord(1, 0)),
		Param:  makeWord(5, world.SubAbilitySpeed(class)),
		Tag:    makeWord(0, 0),
		Series: makeWord(0, 0),
	}
}

func ServerConfigCommand() mir176.Command {
	return mir176.Command{
		Ident:  mir176.SMServerConfig,
		Recog:  0,
		Param:  0,
		Tag:    0,
		Series: 0,
	}
}

func DayChangingCommand(bright, dayBright byte) mir176.Command {
	return mir176.Command{
		Ident:  mir176.SMDayChanging,
		Recog:  0,
		Param:  uint16(bright),
		Tag:    uint16(dayBright),
		Series: 0,
	}
}

func ServerConfigBody() []byte {
	body := bytes.NewBuffer(make([]byte, 0, 18))
	writeByte(body, 17)
	writeByte(body, 1)
	writeByte(body, 1)
	writeByte(body, 1)
	writeByte(body, 0)
	writeByte(body, 1)
	writeByte(body, 1)
	writeByte(body, 1)
	writeByte(body, 0)
	writeByte(body, 0)
	writeByte(body, 0)
	writeByte(body, 0)
	writeByte(body, 0)
	writeByte(body, 0)
	writeByte(body, 0)
	writeByte(body, 0)
	writeByte(body, 1)
	writeByte(body, 0)
	return body.Bytes()
}

func GoldNameBody() []byte {
	return EncodeString("元宝\r游戏点")
}

func (s *Server) sendUseMagic(conn net.Conn, ch storage.Character) {
	body, count := UseMagicBody(s.world, ch)
	if len(body) == 0 {
		return
	}
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMSendMyMagic, Series: uint16(count)}, body)
}

func UseMagicBody(w *world.World, ch storage.Character) ([]byte, int) {
	body := bytes.NewBuffer(make([]byte, 0, 1024))
	count := 0
	for _, skillName := range ch.Skills {
		skill, ok := w.Skill(skillName)
		if !ok {
			continue
		}
		magicID, ok := w.MagicIDByName(skillName)
		if !ok {
			continue
		}
		record := bytes.NewBuffer(make([]byte, 0, 128))
		writeClientMagic(record, magicID, skill)
		body.Write(EncodeBuffer(record.Bytes()))
		writeByte(body, '/')
		count++
	}
	if count == 0 {
		return nil, 0
	}
	return body.Bytes(), count
}

func writeClientMagic(buf *bytes.Buffer, magicID uint16, skill data.StdSkill) {
	writeByte(buf, 0)
	writeByte(buf, 0)
	writeByte(buf, 0)
	writeByte(buf, 0)
	writeI32(buf, 0)
	writeU16(buf, magicID)
	writeGBKAsciiString(buf, skill.Name, 14)
	writeByte(buf, 0)
	writeByte(buf, 0)
	writeByte(buf, 0)
	writeU16(buf, uint16(skill.Spell))
	writeU16(buf, uint16(skill.Power))
	writeByte(buf, byte(skill.NeedLevel1))
	writeByte(buf, 0)
	writeByte(buf, 0)
	writeByte(buf, 0)
	writeI32(buf, int32(skill.TrainLevel1))
	writeI32(buf, 0)
	writeI32(buf, 0)
	writeI32(buf, 0)
	writeByte(buf, 3)
	writeByte(buf, byte(skill.Job))
	writeI32(buf, int32(skill.Delay))
	writeByte(buf, 0)
	writeByte(buf, 0)
	writeU16(buf, uint16(skill.MaxPower))
	writeByte(buf, 0)
	writeGBKAsciiString(buf, "", 15)
	writeByte(buf, 0)
	writeByte(buf, 0)
	writeByte(buf, 0)
}

func CharDesc(feature, status int32) []byte {
	body := make([]byte, 8)
	binary.LittleEndian.PutUint32(body[0:4], uint32(feature))
	binary.LittleEndian.PutUint32(body[4:8], uint32(status))
	return body
}

func MonsterTurnCommand(mon world.Monster) mir176.Command {
	return mir176.Command{
		Ident:  mir176.SMTurn,
		Recog:  world.MonsterActorID(mon),
		Param:  uint16(mon.X),
		Tag:    uint16(mon.Y),
		Series: uint16(mon.Dir),
	}
}

func MonsterDigUpCommand(mon world.Monster) mir176.Command {
	return mir176.Command{
		Ident:  mir176.SMDigUp,
		Recog:  world.MonsterActorID(mon),
		Param:  uint16(mon.X),
		Tag:    uint16(mon.Y),
		Series: uint16(mon.Dir),
	}
}

func MonsterDigUpBody(mon world.Monster) []byte {
	return EncodeBuffer(MessageBodyWL(world.MonsterFeature(mon), 0, 0, 0))
}

func MonsterDigDownCommand(mon world.Monster) mir176.Command {
	return mir176.Command{
		Ident: mir176.SMDigDown,
		Recog: world.MonsterActorID(mon),
		Param: uint16(mon.X),
		Tag:   uint16(mon.Y),
	}
}

func MonsterDisappearCommand(mon world.Monster) mir176.Command {
	return mir176.Command{
		Ident: mir176.SMDisappear,
		Recog: world.MonsterActorID(mon),
	}
}

func MonsterFeatureCommand(mon world.Monster) mir176.Command {
	feature := world.MonsterFeature(mon)
	return mir176.Command{
		Ident:  mir176.SMFeatureChanged,
		Recog:  world.MonsterActorID(mon),
		Param:  uint16(feature),
		Tag:    uint16(uint32(feature) >> 16),
		Series: 0,
	}
}

func MonsterWalkCommand(action world.MonsterAction) mir176.Command {
	return mir176.Command{
		Ident:  mir176.SMWalk,
		Recog:  world.MonsterActorID(world.Monster{ID: action.MonsterID}),
		Param:  uint16(action.X),
		Tag:    uint16(action.Y),
		Series: uint16(action.Dir),
	}
}

func MonsterHitCommand(action world.MonsterAction) mir176.Command {
	return mir176.Command{
		Ident:  mir176.SMHit,
		Recog:  world.MonsterActorID(world.Monster{ID: action.MonsterID}),
		Param:  uint16(action.X),
		Tag:    uint16(action.Y),
		Series: uint16(action.Dir),
	}
}

func CharacterHitCommand(ch storage.Character, clientIdent uint16) mir176.Command {
	return mir176.Command{
		Ident:  HitServerIdent(clientIdent),
		Recog:  ActorID,
		Param:  uint16(ch.X),
		Tag:    uint16(ch.Y),
		Series: uint16(ch.Dir),
	}
}

func HitServerIdent(clientIdent uint16) uint16 {
	switch clientIdent {
	case mir176.CMHeavyHit:
		return mir176.SMHeavyHit
	case mir176.CMBigHit:
		return mir176.SMBigHit
	case mir176.CMPowerHit:
		return mir176.SMPowerHit
	case mir176.CMLongHit:
		return mir176.SMLongHit
	case mir176.CMWideHit:
		return mir176.SMWideHit
	case mir176.CMFireHit:
		return mir176.SMFireHit
	default:
		return mir176.SMHit
	}
}

func CharacterStruckCommand(hit world.CharacterHit) mir176.Command {
	return mir176.Command{
		Ident:  mir176.SMStruck,
		Recog:  ActorID,
		Param:  uint16(hit.Character.HP),
		Tag:    uint16(hit.Character.MaxHP),
		Series: uint16(hit.Damage),
	}
}

func CharacterDeathCommand(ch storage.Character) mir176.Command {
	return mir176.Command{
		Ident:  mir176.SMNowDeath,
		Recog:  ActorID,
		Param:  uint16(ch.X),
		Tag:    uint16(ch.Y),
		Series: uint16(ch.Dir),
	}
}

func MonsterTurnBody(mon world.Monster) []byte {
	body := EncodeBuffer(CharDesc(world.MonsterFeature(mon), 0))
	body = append(body, EncodeString(mon.Name+"/0")...)
	return body
}

func DropShowCommand(drop world.GroundDrop, looks int32) mir176.Command {
	return mir176.Command{
		Ident:  mir176.SMItemShow,
		Recog:  world.DropActorID(drop),
		Param:  uint16(drop.X),
		Tag:    uint16(drop.Y),
		Series: uint16(looks),
	}
}

func DropHideCommand(dropID string, x, y int) mir176.Command {
	return mir176.Command{
		Ident: mir176.SMItemHide,
		Recog: world.DropActorID(world.GroundDrop{ID: dropID}),
		Param: uint16(x),
		Tag:   uint16(y),
	}
}

func MonsterStruckCommand(result world.AttackResult) mir176.Command {
	return mir176.Command{
		Ident:  mir176.SMStruck,
		Recog:  world.MonsterActorID(world.Monster{ID: result.MonsterID}),
		Param:  uint16(result.MonsterHP),
		Tag:    uint16(result.MonsterMaxHP),
		Series: uint16(result.Damage),
	}
}

func MonsterDeathCommand(result world.AttackResult) mir176.Command {
	return mir176.Command{
		Ident:  mir176.SMNowDeath,
		Recog:  world.MonsterActorID(world.Monster{ID: result.MonsterID}),
		Param:  uint16(result.MonsterX),
		Tag:    uint16(result.MonsterY),
		Series: uint16(result.MonsterDir),
	}
}

func (s *Server) sendDropShow(conn net.Conn, drop world.GroundDrop) {
	looks := s.world.DropLooks(drop.ItemID)
	name := s.world.DropDisplayName(drop)
	s.sendCommand(conn, DropShowCommand(drop, looks), EncodeString(name))
}

func (s *Server) sendDropShowLocked(client *Client, drop world.GroundDrop) {
	looks := s.world.DropLooks(drop.ItemID)
	name := s.world.DropDisplayName(drop)
	client.writeCommandLocked(s, DropShowCommand(drop, looks), EncodeString(name))
}

func (s *Server) sendDropHideLocked(client *Client, drop world.GroundDrop) {
	client.writeCommandLocked(s, DropHideCommand(drop.ID, drop.X, drop.Y), nil)
}

func EncodeString(text string) []byte {
	encoded, err := simplifiedchinese.GB18030.NewEncoder().String(text)
	if err != nil {
		encoded = text
	}
	return mir176.EncodePlain6Payload([]byte(encoded))
}

func EncodeBuffer(body []byte) []byte {
	return mir176.EncodePlain6Payload(body)
}

func BytesHex(body []byte) string {
	return fmt.Sprintf("% x", body)
}

func DescHex(desc [14]byte) string {
	return fmt.Sprintf("% x", desc[:])
}

func AddItemBody(item data.StdItem, drop world.GroundDrop, makeIndex int32) []byte {
	return ClientItemBody(item, drop.Desc, makeIndex, world.ItemDuraForDrop(item, drop), world.ItemDuraForDrop(item, drop))
}

func DecodeString(text []byte) string {
	decoded, err := simplifiedchinese.GB18030.NewDecoder().String(string(text))
	if err != nil {
		return string(text)
	}
	return decoded
}

func (s *Server) sendAddItem(conn net.Conn, drop world.GroundDrop) {
	item, ok := s.world.Item(drop.ItemID)
	if !ok {
		item = data.StdItem{ID: drop.ItemID, Name: drop.ItemID, Kind: "misc"}
	}
	item = world.UpgradeClientItemForDisplay(item, storage.UserItem{Desc: drop.Desc}, true)
	dura := world.ItemDuraForDrop(item, drop)
	makeIndex := world.DropMakeIndex(drop)
	s.sendItem(conn, item, drop.Desc, makeIndex, dura, dura, mir176.SMAddItem)
}

func (s *Server) sendPickupAddItem(conn net.Conn, ch storage.Character, drop world.GroundDrop) {
	item, ok := s.world.Item(drop.ItemID)
	if !ok {
		item = data.StdItem{ID: drop.ItemID, Name: drop.ItemID, Kind: "misc"}
	}
	item = world.UpgradeClientItemForDisplay(item, storage.UserItem{Desc: drop.Desc}, true)
	dura := world.ItemDuraForDrop(item, drop)
	for i := 0; i < max(1, drop.Count); i++ {
		makeIndex := world.DropMakeIndex(drop) + int32(i)
		body := EncodeBuffer(ClientItemBody(item, drop.Desc, makeIndex, dura, dura))
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMAddItem, Recog: world.CharacterActorID(ch), Series: 1}, body)
	}
}

func (s *Server) sendItem(conn net.Conn, item data.StdItem, desc [14]byte, makeIndex int32, dura, duraMax uint16, ident uint16) {
	s.sendCommand(conn, mir176.Command{Ident: ident}, EncodeBuffer(ClientItemBody(item, desc, makeIndex, dura, duraMax)))
}

func decodeItemDura(desc [14]byte, fallback uint16) uint16 {
	if desc[0] == 0 && desc[1] == 0 {
		return fallback
	}
	return uint16(desc[0]) | uint16(desc[1])<<8
}

func MakeWord(low, high int) uint16 {
	return uint16(byte(low)) | uint16(byte(high))<<8
}

func writeByte(buf *bytes.Buffer, b byte) {
	_ = buf.WriteByte(b)
}

func writeGBKAsciiString(buf *bytes.Buffer, value string, defaultSize int) {
	encoded, err := simplifiedchinese.GB18030.NewEncoder().String(value)
	if err != nil {
		encoded = value
	}
	data := []byte(encoded)
	if defaultSize <= 0 {
		writeByte(buf, 0)
		return
	}
	if len(data) > defaultSize {
		data = data[:defaultSize]
		writeByte(buf, byte(defaultSize))
	} else {
		writeByte(buf, byte(len(data)))
	}
	buf.Write(data)
	if pad := defaultSize - len(data); pad > 0 {
		buf.Write(make([]byte, pad))
	}
}

func writeU16(buf *bytes.Buffer, v uint16) {
	var tmp [2]byte
	binary.LittleEndian.PutUint16(tmp[:], v)
	buf.Write(tmp[:])
}

func writeU32(buf *bytes.Buffer, v uint32) {
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], v)
	buf.Write(tmp[:])
}

func writeI32(buf *bytes.Buffer, v int32) {
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], uint32(v))
	buf.Write(tmp[:])
}

// abilityStats derives the SM_ABILITY payload for ch: sane HP/level
// defaults for characters saved before those fields existed, the combat
// stats summed from whatever is equipped (world.CombatStats), and MaxExp
// from the same level-up threshold the world instance already uses server
// side (world.RequiredExperience), so the client's exp bar denominator
// matches when the server actually levels the character up.
// Ability encodes the reference project's 40-byte Ability struct
// (OpenMir2/Packets/ClientPackets/Ability.cs): the client reads these fields
// at fixed offsets regardless of what the server actually computed, so any
// mismatch here (as the previous 50-byte/wrong-offset version had) shows up
// as HP 0/0, a maxed-out exp bar, or zeroed AC/MAC no matter what values the
// caller passes in.
func Ability(s world.AbilityStats) []byte {
	body := make([]byte, 40)
	body[0] = byte(s.Level)
	binary.LittleEndian.PutUint16(body[2:4], uint16(s.AC))
	binary.LittleEndian.PutUint16(body[4:6], uint16(s.MAC))
	binary.LittleEndian.PutUint16(body[6:8], uint16(s.DC))
	binary.LittleEndian.PutUint16(body[8:10], uint16(s.MC))
	binary.LittleEndian.PutUint16(body[10:12], uint16(s.SC))
	binary.LittleEndian.PutUint16(body[12:14], uint16(s.HP))
	binary.LittleEndian.PutUint16(body[14:16], uint16(s.MP))
	binary.LittleEndian.PutUint16(body[16:18], uint16(s.MaxHP))
	binary.LittleEndian.PutUint16(body[18:20], uint16(s.MaxMP))
	// bytes 20-23 (Reserved2, ExpCount, ExpMaxCount) are unused in the
	// reference server too; leave them zero.
	binary.LittleEndian.PutUint32(body[24:28], uint32(s.Exp))
	binary.LittleEndian.PutUint32(body[28:32], uint32(s.MaxExp))
	binary.LittleEndian.PutUint16(body[32:34], uint16(s.Weight))
	binary.LittleEndian.PutUint16(body[34:36], uint16(s.MaxWeight))
	body[36] = byte(s.WearWeight)
	body[37] = byte(s.MaxWearWeight)
	body[38] = byte(s.HandWeight)
	body[39] = byte(s.MaxHandWeight)
	return body
}

func makeWord(lo, hi byte) uint16 {
	return uint16(lo) | uint16(hi)<<8
}

func (s *Server) handleGame(ctx context.Context, conn net.Conn) {
	_, _ = fmt.Fprintln(conn, "OpenMir2 debug game protocol")
	_, _ = fmt.Fprintln(conn, "commands: login test test | chars | create name warrior | enter char-id | look | move x y | attack mon-id | pickup drop-id | me | quit")
	scanner := bufio.NewScanner(conn)
	var account string
	var ch storage.Character
	entered := false
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		switch parts[0] {
		case "quit":
			_, _ = fmt.Fprintln(conn, "bye")
			return
		case "login":
			if len(parts) != 3 {
				_, _ = fmt.Fprintln(conn, "ERR usage: login username password")
				continue
			}
			if !s.store.Authenticate(parts[1], parts[2]) {
				_, _ = fmt.Fprintln(conn, "ERR login failed")
				continue
			}
			account = parts[1]
			_, _ = fmt.Fprintln(conn, "OK login")
		case "chars":
			if account == "" {
				_, _ = fmt.Fprintln(conn, "ERR login first")
				continue
			}
			writeJSON(conn, s.store.Characters(account))
		case "create":
			if account == "" {
				_, _ = fmt.Fprintln(conn, "ERR login first")
				continue
			}
			if len(parts) != 3 {
				_, _ = fmt.Fprintln(conn, "ERR usage: create name class")
				continue
			}
			mapID, x, y := s.world.DefaultSpawn()
			created, err := s.world.CreateCharacter(account, parts[1], parts[2], mapID, x, y)
			if err != nil {
				_, _ = fmt.Fprintln(conn, "ERR", err)
				continue
			}
			writeJSON(conn, created)
		case "enter":
			if account == "" {
				_, _ = fmt.Fprintln(conn, "ERR login first")
				continue
			}
			if len(parts) != 2 {
				_, _ = fmt.Fprintln(conn, "ERR usage: enter char-id")
				continue
			}
			loaded, ok := s.store.Character(parts[1])
			if !ok || loaded.Account != account {
				_, _ = fmt.Fprintln(conn, "ERR character not found")
				continue
			}
			ch = loaded
			entered = true
			writeJSON(conn, ch)
		case "look":
			if !entered {
				_, _ = fmt.Fprintln(conn, "ERR enter first")
				continue
			}
			monsters, drops := s.world.Snapshot(ch.MapID)
			writeJSON(conn, map[string]any{"character": ch, "monsters": monsters, "drops": drops})
		case "move":
			if !entered {
				_, _ = fmt.Fprintln(conn, "ERR enter first")
				continue
			}
			if len(parts) != 3 {
				_, _ = fmt.Fprintln(conn, "ERR usage: move x y")
				continue
			}
			var x, y int
			if _, err := fmt.Sscanf(parts[1]+" "+parts[2], "%d %d", &x, &y); err != nil {
				_, _ = fmt.Fprintln(conn, "ERR invalid coordinates")
				continue
			}
			moved, err := s.world.Move(ch, x, y)
			if err != nil {
				_, _ = fmt.Fprintln(conn, "ERR", err)
				continue
			}
			ch = moved
			writeJSON(conn, ch)
		case "attack":
			if !entered {
				_, _ = fmt.Fprintln(conn, "ERR enter first")
				continue
			}
			if len(parts) != 2 {
				_, _ = fmt.Fprintln(conn, "ERR usage: attack mon-id")
				continue
			}
			result, err := s.world.Attack(ch, parts[1])
			if err != nil {
				_, _ = fmt.Fprintln(conn, "ERR", err)
				continue
			}
			ch = result.Character
			writeJSON(conn, result)
		case "pickup":
			if !entered {
				_, _ = fmt.Fprintln(conn, "ERR enter first")
				continue
			}
			if len(parts) != 2 {
				_, _ = fmt.Fprintln(conn, "ERR usage: pickup drop-id")
				continue
			}
			updated, drop, err := s.world.Pickup(ch, parts[1])
			if err != nil {
				_, _ = fmt.Fprintln(conn, "ERR", err)
				continue
			}
			ch = updated
			writeJSON(conn, map[string]any{"picked": drop, "character": ch})
		case "me":
			if !entered {
				_, _ = fmt.Fprintln(conn, "ERR enter first")
				continue
			}
			writeJSON(conn, ch)
		default:
			_, _ = fmt.Fprintln(conn, "ERR unknown command")
		}
	}
}

func writeJSON(w io.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		_, _ = fmt.Fprintln(w, "ERR", err)
		return
	}
	_, _ = fmt.Fprintln(w, string(b))
}
