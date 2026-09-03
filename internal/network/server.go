package network

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"openmir2/internal/config"
	"openmir2/internal/data"
	"openmir2/internal/npc"
	"openmir2/internal/protocol/mir176"
	"openmir2/internal/storage"
	"openmir2/internal/world"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const itemNameLen = 14
const magicHitInterval = 800 * time.Millisecond
const actionInterval = 350 * time.Millisecond
const runMagicInterval = 900 * time.Millisecond

const maxPendingSpellMessages = 1

const (
	SlotDress  = world.SlotDress
	SlotWeapon = world.SlotWeapon
	SlotRingL  = world.SlotRingL
	SlotCharm  = world.SlotCharm
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

type Server struct {
	serverName string
	listeners  []config.Listener
	store      *storage.Store
	world      *world.World
	log        *slog.Logger

	sessionMu  sync.Mutex
	sessions   map[int32]string
	clientMu   sync.Mutex
	clients    map[net.Conn]*Client
	spellRefMu sync.Mutex
	spellRefs  map[string]spellRefSnapshot

	hitImpactDelay      time.Duration
	monsterTickInterval time.Duration
}

type spellRefSnapshot struct {
	at      time.Time
	mapID   string
	x       int
	y       int
	clients map[string]struct{}
}

type spellStartEvent struct {
	caster   storage.Character
	magicID  uint16
	effect   int
	targetX  int
	targetY  int
	targetID int32
}

type spellMagicFireEvent struct {
	caster   storage.Character
	skill    data.StdSkill
	targetX  int
	targetY  int
	targetID int32
}

type spellSkillExpEvent struct {
	magicID    uint16
	skillLevel byte
	skillTrain int
}

type spellObjectMessage struct {
	kind             spellObjectMessageKind
	start            spellStartEvent
	magicFire        spellMagicFireEvent
	magicFireFail    storage.Character
	state            storage.Character
	monsterState     world.Monster
	monsterStatus    world.Monster
	monsterNameColor world.Monster
	monsterUsername  world.Monster
	teleportFrom     storage.Character
	teleportTo       storage.Character
	monsterAction    world.MonsterAction
	summoned         world.Monster
	status           storage.Character
	health           storage.Character
	healthMonster    world.Monster
	ability          storage.Character
	gauge            storage.Character
	gaugeMonster     world.Monster
	rush             world.SpellRush
	spellHit         world.CharacterHit
	caster           storage.Character
	monsterHit       world.AttackResult
	drops            []world.GroundDrop
	durability       world.SpellDurability
	experience       int
	currentExp       int
	levelUp          storage.Character
	skillExp         spellSkillExpEvent
	characterPush    world.CharacterPush
	monsterPush      world.MonsterAction
	ground           world.SpellGroundEvent
	groundHideID     int32
	spaceMove        storage.Character
}

const clientOutputQueueSize = 256

type spellObjectMessageKind uint8

const (
	spellObjectMessageStart spellObjectMessageKind = iota
	spellObjectMessageMagicFire
	spellObjectMessageMagicFireFail
	spellObjectMessageSpaceMoveFire
	spellObjectMessageSpaceMoveMapChange
	spellObjectMessageSpaceMoveShow
	spellObjectMessageState
	spellObjectMessageMonsterState
	spellObjectMessageMonsterStatus
	spellObjectMessageMonsterNameColor
	spellObjectMessageMonsterUsername
	spellObjectMessageTeleport
	spellObjectMessageMonsterAction
	spellObjectMessageSummon
	spellObjectMessageHealth
	spellObjectMessageAbility
	spellObjectMessageStatus
	spellObjectMessageOpenHealth
	spellObjectMessageCloseHealth
	spellObjectMessageHealGauge
	spellObjectMessageRush
	spellObjectMessageSpellHit
	spellObjectMessageMonsterHealth
	spellObjectMessageMonsterHit
	spellObjectMessageMonsterDeath
	spellObjectMessageDurability
	spellObjectMessageExperience
	spellObjectMessageLevelUp
	spellObjectMessageSkillExp
	spellObjectMessageCharacterPush
	spellObjectMessageMonsterPush
	spellObjectMessageGroundShow
	spellObjectMessageGroundHide
)

func New(serverName string, listeners []config.Listener, store *storage.Store, world *world.World, log *slog.Logger) *Server {
	return &Server{serverName: serverName, listeners: listeners, store: store, world: world, log: log, sessions: map[int32]string{}, clients: map[net.Conn]*Client{}, spellRefs: map[string]spellRefSnapshot{}, hitImpactDelay: 200 * time.Millisecond, monsterTickInterval: 100 * time.Millisecond}
}

func (s *Server) SetHitImpactDelay(delay time.Duration) {
	s.hitImpactDelay = delay
}

func (s *Server) SetMonsterTickInterval(interval time.Duration) {
	s.monsterTickInterval = interval
}

type Client struct {
	conn                  net.Conn
	mu                    sync.Mutex
	ch                    storage.Character
	softVersionDate       int
	active                *storage.Character
	visibleMonsters       map[string]world.Monster
	visibleDrops          map[string]world.GroundDrop
	visibleNPCs           map[string]npc.Entity
	visibleEvents         map[int32]world.SpellGroundEvent
	activeNPCID           string
	merchantCurrentLabel  string
	merchantGoBackLabel   string
	pendingMerchantAction string
	powerHitCount         int
	powerHitPointCount    int
	powerHitArmed         bool
	fireHitArmed          bool
	fireHitLatestAt       time.Time
	spellActionAt         time.Time
	spellActionInterval   time.Duration
	spellActionCount      int
	pendingSpellMessages  int
	actionAt              time.Time
	actionIdent           uint16
	actionDir             int
	struckAt              time.Time
	chargeAt              time.Time
	probeLatestAt         time.Time
	spellMessages         chan spellObjectMessage
	spellQueue            []spellObjectMessage
	spellQueueCond        *sync.Cond
	spellMessagesDone     chan struct{}
	output                chan []byte
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

func (a itemUseSyncAdapter) SendBagItems(ch storage.Character) {
	a.s.sendBagItems(a.conn, ch)
}

func (a itemUseSyncAdapter) SendDelItems(removed []storage.UserItem) {
	a.s.sendDelItemList(a.conn, removed)
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

func (a attackSyncAdapter) SendDurability(ch storage.Character, durability world.SpellDurability) {
	if client, ok := a.s.ClientByCharacterID(ch.ID); ok {
		client.enqueueSpellMessage(a.s, spellObjectMessage{kind: spellObjectMessageDurability, durability: durability})
	}
}

func (a attackSyncAdapter) SendSkillExp(magicID uint16, level byte, train int, delay time.Duration) {
	if delay <= 0 {
		delay = 3 * time.Second
	}
	time.AfterFunc(delay, func() {
		a.s.sendMagicLevelExp(a.conn, magicID, level, train)
	})
}

func (a attackSyncAdapter) BroadcastCharacterHit(ch storage.Character, attackIdent uint16) {
	if clients := a.s.ClientsAroundExcept(ch.MapID, ch.X, ch.Y, playerViewRange, a.conn); len(clients) > 0 {
		a.s.broadcastCharacterHit(clients, ch, attackIdent)
	}
}

func (a attackSyncAdapter) BroadcastCharacterStruck(hit world.CharacterHit) {
	if hit.FeatureChanged {
		a.s.updateClientByCharacterID(hit.Character)
		a.s.broadcastCharacterStateRefresh(hit.Character)
	}
	if clients := a.s.ClientsAroundExcept(hit.Character.MapID, hit.Character.X, hit.Character.Y, playerViewRange, a.conn); len(clients) > 0 {
		a.s.broadcastCharacterStruck(clients, hit)
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
				continue
			}
			s.applyWorldTick(result, now)
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
							applyLoginNoticeClientTick(&ch, cmd)
							initializeSpellStateOnLogin(&ch)
							ch = s.world.RegisterCharacter(ch)
							activeClient = s.registerClient(conn, ch)
							activeChar = &ch
							activeClient.active = activeChar
							s.sendEnterWorldState(conn, ch)
							s.sendInitialLoginState(conn, ch)
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
						case mir176.CMSpell:
							s.handleSpell(conn, activeChar, cmd)
						case mir176.CMSay, mir176.CMUserCommand:
							s.handleSay(conn, activeChar, text)
						case mir176.CMClickNPC:
							s.handleClickNPC(conn, activeChar, activeClient, cmd)
						case mir176.CMMerchantQuerySellPrice:
							s.handleMerchantQuerySellPrice(conn, activeChar, activeClient, cmd, text)
						case mir176.CMMerchantDlgSelect:
							s.handleMerchantDlgSelect(conn, activeChar, activeClient, cmd, text)
						case mir176.CMUserSellItem:
							s.handleUserSellItem(conn, activeChar, activeClient, cmd, text)
						case mir176.CMUserBuyItem:
							s.handleUserBuyItem(conn, activeChar, activeClient, cmd, text)
						case mir176.CMUserGetDetailItem:
							s.handleUserGetDetailItem(conn, activeChar, activeClient, cmd, text)
						case mir176.CMUserRepairItem:
							s.handleUserRepairItem(conn, activeChar, activeClient, cmd, text)
						case mir176.CMMerchantQueryRepairCost:
							s.handleMerchantQueryRepairCost(conn, activeChar, activeClient, cmd, text)
						case mir176.CMUserStorageItem:
							s.handleUserStorageItem(conn, activeChar, activeClient, cmd, text)
						case mir176.CMUserTakeBackStorageItem:
							s.handleUserTakeBackStorageItem(conn, activeChar, activeClient, cmd, text)
						case mir176.CMUserMakeDrugItem:
							s.handleUserMakeDrugItem(conn, activeChar, activeClient, cmd, text)
						case mir176.CMMagicKeyChange:
							s.handleMagicKeyChange(conn, activeChar, cmd)
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
		s.sendMoveFail(conn, activeChar)
		return
	}
	*activeChar = updated
	s.updateClient(conn, updated)
	s.recordClientAction(conn, cmd.Ident, dir)
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
		s.sendMoveFail(conn, activeChar)
		return
	}
	*activeChar = updated
	s.updateClient(conn, updated)
	s.recordClientAction(conn, cmd.Ident, dir)
	s.sendActionOK(conn)
}

func (s *Server) handleSitDown(conn net.Conn, activeChar *storage.Character, cmd mir176.Command) {
	x := int(uint32(cmd.Recog) & 0xFFFF)
	y := int(uint32(cmd.Recog) >> 16)
	dir := int(cmd.Tag)
	updated, err := s.world.SitDown(*activeChar, x, y, dir)
	if err != nil {
		s.sendMoveFail(conn, activeChar)
		return
	}
	*activeChar = updated
	s.updateClient(conn, updated)
	s.recordClientAction(conn, cmd.Ident, dir)
	s.sendActionOK(conn)
}

func (s *Server) handleHit(conn net.Conn, activeChar *storage.Character, cmd mir176.Command) {
	client := s.clientForConn(conn)
	attackIdent := normalizeAttackIdent(*activeChar, cmd.Ident)
	if attackIdent == mir176.CMWideHit {
		state, _, ok := activeChar.Skills.Get("半月弯刀")
		if ok {
			skill, skillOK := s.world.Skill("半月弯刀")
			if !skillOK || activeChar.MP <= 0 {
				attackIdent = mir176.CMHit
			} else {
				cost := s.world.SpellCost(skill, state)
				if cost >= activeChar.MP {
					activeChar.MP = 0
				} else {
					activeChar.MP -= cost
				}
				s.updateClient(conn, *activeChar)
				s.sendHealthSpellChanged(conn, world.CharacterActorID(*activeChar), s.world.AbilityStats(*activeChar))
			}
		}
	}
	if client != nil {
		client.mu.Lock()
		switch cmd.Ident {
		case mir176.CMPowerHit:
			if !powerHitUsable(*activeChar) || !client.powerHitArmed {
				attackIdent = mir176.CMHit
				client.powerHitArmed = false
			} else {
				client.powerHitArmed = false
			}
		case mir176.CMFireHit:
			if !client.fireHitArmed {
				attackIdent = mir176.CMHit
			} else {
				client.fireHitArmed = false
				client.fireHitLatestAt = time.Now()
			}
		}
		client.mu.Unlock()
	}
	x := int(uint32(cmd.Recog) & 0xFFFF)
	y := int(uint32(cmd.Recog) >> 16)
	dir := int(cmd.Tag)
	if client != nil {
		s.advancePowerHitStateLocked(client, *activeChar, false)
	}
	result, err := s.world.HitWithIdent(*activeChar, x, y, dir, attackIdent, s.PlayerCharacters()...)
	if err != nil {
		s.sendMoveFail(conn, activeChar)
		return
	}
	*activeChar = result.Character
	s.recordClientAction(conn, cmd.Ident, dir)
	world.ApplyAttackSync(attackSyncAdapter{s: s, conn: conn}, result, attackIdent)
}

func normalizeAttackIdent(ch storage.Character, ident uint16) uint16 {
	switch ident {
	case mir176.CMLongHit:
		if ch.ThrustingDisabled {
			return mir176.CMHit
		}
		if _, _, ok := ch.Skills.Get("刺杀剑术"); !ok {
			return mir176.CMHit
		}
	case mir176.CMWideHit:
		if ch.HalfMoonDisabled {
			return mir176.CMHit
		}
		if _, _, ok := ch.Skills.Get("半月弯刀"); !ok {
			return mir176.CMHit
		}
	}
	return ident
}

func (s *Server) handleSpell(conn net.Conn, activeChar *storage.Character, cmd mir176.Command) {
	request := spellRequest{
		x:        int(uint32(cmd.Recog) & 0xFFFF),
		y:        int(uint32(cmd.Recog) >> 16),
		magicID:  cmd.Tag,
		targetID: int32(uint32(cmd.Series)<<16 | uint32(cmd.Param)),
	}
	s.processSpell(conn, activeChar, request)
}

func applyLoginNoticeClientTick(ch *storage.Character, cmd mir176.Command) {
	if ch == nil {
		return
	}
	ch.ClientTick = int(cmd.Series)
}

type spellRequest struct {
	x        int
	y        int
	magicID  uint16
	targetID int32
}

func (s *Server) processSpell(conn net.Conn, activeChar *storage.Character, request spellRequest) {
	s.processSpellDelivery(conn, activeChar, request, false)
}

func (s *Server) processSpellDelivery(conn net.Conn, activeChar *storage.Character, request spellRequest, lateDelivery bool) {
	if activeChar == nil || activeChar.HP <= 0 || activeChar.SpellBlocked || (activeChar.ParalyzedUntil > 0 && !s.world.CanSpellWhileParalyzed()) {
		s.sendActionFail(conn)
		return
	}
	x := request.x
	y := request.y
	magicID := request.magicID
	skillID, ok := s.world.SkillIDByMagicID(magicID)
	if !ok {
		s.sendActionFail(conn)
		return
	}
	if skillID == "基本剑术" || skillID == "精神力战法" || skillID == "攻杀剑术" {
		if _, _, ok := activeChar.Skills.Get(skillID); !ok {
			s.sendActionFail(conn)
			return
		}
		s.consumeSpellTick(activeChar)
		s.recordSpellCastTimestamp(conn)
		s.recordClientAction(conn, mir176.CMSpell, 0)
		s.sendActionOK(conn)
		return
	}
	if skillID == "刺杀剑术" || skillID == "半月弯刀" {
		if _, _, ok := activeChar.Skills.Get(skillID); !ok {
			s.sendActionFail(conn)
			return
		}
		s.consumeSpellTick(activeChar)
		s.recordSpellCastTimestamp(conn)
		if skillID == "刺杀剑术" {
			if activeChar.ThrustingDisabled {
				activeChar.ThrustingDisabled = false
				s.sendRawFrame(conn, "+LNG")
			} else {
				activeChar.ThrustingDisabled = true
				s.sendRawFrame(conn, "+ULNG")
			}
		} else {
			if activeChar.HalfMoonDisabled {
				activeChar.HalfMoonDisabled = false
				s.sendRawFrame(conn, "+WID")
			} else {
				activeChar.HalfMoonDisabled = true
				s.sendRawFrame(conn, "+UWID")
			}
		}
		s.recordClientAction(conn, mir176.CMSpell, 0)
		s.sendActionOK(conn)
		return
	}
	if skillID == "烈火剑法" {
		state, _, ok := activeChar.Skills.Get(skillID)
		if !ok {
			s.sendActionFail(conn)
			return
		}
		s.consumeSpellTick(activeChar)
		s.recordSpellCastTimestamp(conn)
		if !s.armFireHit(conn, *activeChar) {
			s.recordClientAction(conn, mir176.CMSpell, 0)
			s.sendActionOK(conn)
			return
		}
		skill, ok := s.world.Skill(skillID)
		if !ok {
			s.sendActionFail(conn)
			return
		}
		cost := s.world.SpellCost(skill, state)
		if activeChar.MP < cost {
			s.recordClientAction(conn, mir176.CMSpell, 0)
			s.sendActionOK(conn)
			return
		}
		if cost > 0 {
			activeChar.MP -= cost
			s.updateClient(conn, *activeChar)
			s.sendHealthSpellChanged(conn, world.CharacterActorID(*activeChar), s.world.AbilityStats(*activeChar))
		}
		s.sendRawFrame(conn, "+FIR")
		s.recordClientAction(conn, mir176.CMSpell, 0)
		s.sendActionOK(conn)
		return
	}
	if skillID == "野蛮冲撞" {
		if _, _, ok := activeChar.Skills.Get(skillID); !ok {
			s.sendActionFail(conn)
			return
		}
		s.consumeSpellTick(activeChar)
		s.recordSpellCastTimestamp(conn)
		dir := int(request.x)
		client := s.clientForConn(conn)
		if client != nil {
			client.mu.Lock()
			now := time.Now()
			if !client.chargeAt.IsZero() && now.Sub(client.chargeAt) < 3*time.Second {
				client.mu.Unlock()
				s.recordClientAction(conn, mir176.CMSpell, dir)
				s.sendActionOK(conn)
				return
			}
			client.chargeAt = now
			client.mu.Unlock()
		}
		result, err := s.world.DoCharge(*activeChar, dir, s.PlayerCharacters())
		if err != nil {
			s.recordClientAction(conn, mir176.CMSpell, dir)
			s.sendActionOK(conn)
			return
		}
		if !result.SpellStarted {
			s.recordClientAction(conn, mir176.CMSpell, dir)
			s.sendActionOK(conn)
			return
		}
		for _, event := range result.Events {
			s.handleSpellEvent(conn, activeChar, skillID, data.StdSkill{}, event)
		}
		*activeChar = result.Character
		s.updateClient(conn, *activeChar)
		s.recordClientAction(conn, mir176.CMSpell, dir)
		s.sendActionOK(conn)
		return
	}
	skill, ok := s.world.Skill(skillID)
	if !ok {
		s.sendActionFail(conn)
		return
	}
	if !activeChar.Skills.Has(skillID) {
		s.sendActionFail(conn)
		return
	}
	direction := world.Direction(activeChar.X, activeChar.Y, x, y)
	if client := s.clientForConn(conn); client != nil {
		client.mu.Lock()
		now := time.Now()
		delay := time.Duration(0)
		struckDelayActive := false
		if !lateDelivery && !client.struckAt.IsZero() {
			struckDelay := 100*time.Millisecond - now.Sub(client.struckAt)
			if struckDelay > delay {
				delay = struckDelay
				struckDelayActive = true
			}
		}
		if !lateDelivery && client.actionIdent != mir176.CMSpell && !client.actionAt.IsZero() {
			actionIntervalDelay := actionInterval - now.Sub(client.actionAt)
			if client.actionIdent == mir176.CMRun && client.actionDir != direction {
				actionIntervalDelay = runMagicInterval - now.Sub(client.actionAt)
			}
			if actionIntervalDelay > delay {
				delay = actionIntervalDelay
			}
		}
		if !lateDelivery && !client.spellActionAt.IsZero() && now.Sub(client.spellActionAt) < client.spellActionInterval {
			client.spellActionCount++
			spellDelay := client.spellActionInterval - now.Sub(client.spellActionAt)
			if spellDelay > delay {
				delay = spellDelay
			}
			if spellDelay > magicHitInterval/3 {
				if client.spellActionCount >= 4 {
					client.spellActionAt = now
					client.spellActionCount = 0
					if delay < magicHitInterval/3 {
						delay = magicHitInterval / 3
					}
				} else {
					client.spellActionCount = 0
				}
			}
		}
		if !lateDelivery && delay > 0 {
			s.consumeSpellTick(activeChar)
			if client.pendingSpellMessages >= maxPendingSpellMessages {
				client.spellActionCount = 0
				client.mu.Unlock()
				s.sendActionFail(conn)
				return
			}
			client.spellActionCount = 0
			if !struckDelayActive {
				client.actionIdent = mir176.CMSpell
				client.actionDir = direction
			}
			client.pendingSpellMessages++
			client.mu.Unlock()
			s.queueDelayedSpell(conn, request, delay)
			return
		}
		s.consumeSpellTick(activeChar)
		client.spellActionAt = now
		client.spellActionInterval = magicHitInterval + time.Duration(skill.Delay)*time.Millisecond
		client.spellActionCount = 0
		client.actionIdent = mir176.CMSpell
		client.actionDir = direction
		client.mu.Unlock()
	}
	activeChar.Dir = direction
	targetID := request.targetID
	result, err := s.world.DoSpell(*activeChar, skillID, x, y, targetID, s.PlayerCharacters())
	if err != nil {
		if !result.SpellStarted && len(result.Events) == 0 {
			if result.Character.ID != "" {
				*activeChar = result.Character
				s.updateClient(conn, result.Character)
			}
			if result.ManaConsumed && result.Character.ID != "" {
				s.sendHealthSpellChanged(conn, world.CharacterActorID(result.Character), s.world.AbilityStats(result.Character))
			}
			s.handleSpellEvent(conn, activeChar, skillID, skill, world.SpellEvent{Kind: world.SpellEventMagicFireFail, Character: *activeChar})
			if result.SkillID != "" || skillID != "" {
				s.recordClientAction(conn, mir176.CMSpell, direction)
				s.sendActionOK(conn)
				return
			}
			s.sendActionFail(conn)
			return
		}
		for _, event := range result.Events {
			s.handleSpellEvent(conn, activeChar, skillID, skill, event)
		}
		s.handleSpellEvent(conn, activeChar, skillID, skill, world.SpellEvent{Kind: world.SpellEventMagicFireFail, Character: *activeChar})
		s.recordClientAction(conn, mir176.CMSpell, direction)
		s.sendActionOK(conn)
		return
	}
	for _, event := range result.Events {
		s.handleSpellEvent(conn, activeChar, skillID, skill, event)
	}
	*activeChar = result.Character
	s.recordClientAction(conn, mir176.CMSpell, direction)
	s.sendActionOK(conn)
}

func (s *Server) recordClientAction(conn net.Conn, ident uint16, dir int) {
	client := s.clientForConn(conn)
	if client == nil {
		return
	}
	client.mu.Lock()
	client.actionAt = time.Now()
	client.actionIdent = ident
	client.actionDir = dir
	client.mu.Unlock()
}

func (s *Server) recordSpellAction(conn net.Conn) {
	s.recordSpellActionForSkill(conn, "")
}

func (s *Server) consumeSpellTick(ch *storage.Character) {
	if ch == nil {
		return
	}
	if ch.SpellTick > 450 {
		ch.SpellTick -= 450
	} else {
		ch.SpellTick = 0
	}
}

func (s *Server) recordSpellActionForSkill(conn net.Conn, skillID string) {
	client := s.clientForConn(conn)
	if client == nil {
		return
	}
	interval := magicHitInterval
	if skillID != "" {
		if skill, ok := s.world.Skill(skillID); ok {
			interval += time.Duration(skill.Delay) * time.Millisecond
		}
	}
	client.mu.Lock()
	client.spellActionAt = time.Now()
	client.spellActionInterval = interval
	client.spellActionCount = 0
	client.mu.Unlock()
}

func (s *Server) recordSpellCastTimestamp(conn net.Conn) {
	client := s.clientForConn(conn)
	if client == nil {
		return
	}
	client.mu.Lock()
	client.spellActionAt = time.Now()
	client.mu.Unlock()
}

func (s *Server) queueDelayedSpell(conn net.Conn, request spellRequest, delay time.Duration) {
	if delay <= 0 {
		delay = time.Millisecond
	}
	time.AfterFunc(delay, func() {
		client := s.clientForConn(conn)
		if client == nil {
			return
		}
		client.mu.Lock()
		if client.pendingSpellMessages > 0 {
			client.pendingSpellMessages--
		}
		client.mu.Unlock()
		active := client.character()
		s.processSpellDelivery(conn, &active, request, true)
	})
}

func (s *Server) handleSpellEvent(conn net.Conn, activeChar *storage.Character, skillID string, skill data.StdSkill, event world.SpellEvent) {
	switch event.Kind {
	case world.SpellEventCasterState:
		*activeChar = event.Character
		s.updateClient(conn, event.Character)
		if event.SendHealth {
			s.sendHealthSpellChanged(conn, world.CharacterActorID(event.Character), s.world.AbilityStats(event.Character))
			s.broadcastCharacterHealthSpellChanged(event.Character)
		}
	case world.SpellEventStart:
		s.dispatchSpellStart(spellStartEvent{caster: event.Caster, magicID: event.MagicID, effect: event.Effect, targetX: event.TargetX, targetY: event.TargetY, targetID: event.TargetID})
	case world.SpellEventItemDelete:
		s.sendDelItemList(conn, []storage.UserItem{event.DeletedItem})
	case world.SpellEventDurability:
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMDuraChange, Recog: int32(event.Durability.Dura), Param: uint16(event.Durability.Slot), Tag: uint16(event.Durability.DuraMax), Series: uint16(uint32(event.Durability.DuraMax) >> 16)}, nil)
	case world.SpellEventMagicFireFail:
		for _, client := range s.spellRefClients(event.Character) {
			if client.character().ID == event.Character.ID && client.conn == conn {
				s.sendCommand(conn, mir176.Command{Ident: mir176.SMMagicFireFail, Recog: world.CharacterActorID(event.Character)}, nil)
				continue
			}
			client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageMagicFireFail, magicFireFail: event.Character})
		}
	case world.SpellEventMagicFire:
		magicFire := spellMagicFireEvent{caster: event.Caster, skill: skill, targetX: event.TargetX, targetY: event.TargetY, targetID: event.TargetID}
		if client := s.clientForConn(conn); client != nil && client.character().ID == event.Caster.ID {
			s.handleSpellMagicFire(conn, magicFire)
			s.dispatchSpellMagicFireExcept(magicFire, conn)
		} else {
			s.dispatchSpellMagicFire(magicFire)
		}
	case world.SpellEventSpaceMoveFire:
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMSpacemoveHide2, Recog: world.CharacterActorID(event.Caster)}, nil)
		s.dispatchSpellSpaceMoveFireExcept(event.Caster, conn)
	case world.SpellEventSpaceMoveMapChange:
		s.sendSpellSpaceMoveMapChange(conn, event.Character)
	case world.SpellEventSpaceMoveShow:
		s.sendSpellSpaceMoveShow(conn, event.Character)
		s.dispatchSpellSpaceMoveShowExcept(event.Character, conn)
	case world.SpellEventRush:
		s.sendSpellRush(conn, event.Rush)
		s.broadcastSpellRushExcept(event.Rush, conn)
		if event.Rush.Kung {
			s.sendSystemMessageStyle(conn, event.Rush.Character, "冲撞力不够...", 0xFF, 0x38)
		}
	case world.SpellEventCharacter:
		*activeChar = event.Character
		s.updateClient(conn, event.Character)
		if event.Previous.ID != "" && !event.Teleport && !event.SuppressStateBroadcast {
			featureChanged := s.world.HumanFeatureForCharacter(event.Previous) != s.world.HumanFeatureForCharacter(event.Character) || event.Previous.MapID != event.Character.MapID || event.Previous.X != event.Character.X || event.Previous.Y != event.Character.Y
			statusChanged := s.world.CharacterStatus(event.Previous) != s.world.CharacterStatus(event.Character)
			if featureChanged {
				if client := s.clientForConn(conn); client != nil && client.character().ID == event.Character.ID {
					client.sendCharacterStateRefresh(s, event.Character)
					s.broadcastCharacterStateRefreshExcept(event.Character, event.Character.ID)
				} else {
					s.broadcastCharacterStateRefresh(event.Character)
				}
			}
			if statusChanged && !event.SuppressStatusBroadcast {
				if client := s.clientForConn(conn); client != nil && client.character().ID == event.Character.ID {
					s.handleCharacterStatusChanged(conn, event.Character)
					s.broadcastCharacterStatusChangedExcept(event.Character, event.Character.ID)
				} else {
					s.broadcastCharacterStatusChanged(event.Character)
				}
			}
		}
		if event.Character.ID != "" {
			if client, ok := s.ClientByCharacterID(event.Character.ID); ok {
				if event.SendHealth {
					client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageHealth, health: event.Character})
				}
			}
			if event.SendHealth {
				s.broadcastCharacterHealthSpellChanged(event.Character)
			}
		}
	case world.SpellEventTeleport:
		s.dispatchSpellTeleport(conn, event.Previous, event.Character)
	case world.SpellEventSummon:
		s.dispatchSpellSummon(event.Monster)
	case world.SpellEventMonsterRefresh:
		s.broadcastMonsterFeatureRefresh(event.Monster)
	case world.SpellEventMonsterNameColor:
		s.broadcastMonsterNameColor(event.Monster)
	case world.SpellEventMonsterUsername:
		s.broadcastMonsterUsername(event.Monster)
	case world.SpellEventMonsterHit:
		hit := event.MonsterHit
		clients := s.spellRefClientsFor("monster:"+hit.MonsterID, hit.Character.MapID, hit.MonsterX, hit.MonsterY)
		if !hit.Magic && skillID != "野蛮冲撞" {
			clients = s.ClientsAround(hit.Character.MapID, hit.MonsterX, hit.MonsterY, playerViewRange)
		}
		if hit.Magic {
			for _, client := range clients {
				if hit.MonsterHealthChanged {
					client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageMonsterHealth, monsterHit: hit})
				}
				client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageMonsterHit, monsterHit: hit})
			}
		} else {
			if hit.ImpactDelay > 0 {
				s.broadcastHitImpact(clients, hit)
			} else {
				s.broadcastMonsterStruck(clients, hit)
				if hit.Dead {
					s.broadcastMonsterDeath(clients, hit)
					if len(hit.Drops) > 0 {
						s.broadcastDropAppear(clients, hit.Drops)
					}
				}
			}
		}
	case world.SpellEventMonsterAction:
		if client := s.clientForConn(conn); client != nil {
			client.sendMonsterAction(s, event.MonsterAction)
		}
		s.dispatchSpellMonsterActionExcept(event.MonsterAction, conn)
	case world.SpellEventCharacterHit:
		hit := event.CharacterHit
		if hit.FeatureChanged {
			s.updateClientByCharacterID(hit.Character)
			if client, ok := s.ClientByCharacterID(hit.Character.ID); ok {
				client.sendCharacterStateRefresh(s, hit.Character)
			}
			s.broadcastCharacterStateRefreshExcept(hit.Character, hit.Character.ID)
		}
		for _, durability := range hit.Durability {
			if client, ok := s.ClientByCharacterID(hit.Character.ID); ok {
				s.sendCommand(client.conn, mir176.Command{Ident: mir176.SMDuraChange, Recog: int32(durability.Dura), Param: uint16(durability.Slot), Tag: uint16(durability.DuraMax), Series: uint16(uint32(durability.DuraMax) >> 16)}, nil)
			}
		}
		clients := s.spellRefClients(hit.Character)
		if !hit.Magic && skillID != "野蛮冲撞" {
			clients = s.clientsForCharacterHit(hit.Character)
		}
		if hit.Magic {
			caster := event.Character
			for _, client := range clients {
				client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageSpellHit, caster: caster, spellHit: hit})
			}
		} else {
			if hit.ImpactDelay > 0 {
				s.broadcastCharacterStruck(clients, hit)
			} else {
				s.sendCharacterStruck(clients, hit)
			}
		}
	case world.SpellEventAffectedCharacter:
		s.handleAffectedSpellCharacter(conn, activeChar, event)
	case world.SpellEventCharacterPush:
		s.updateClientByCharacterID(event.CharacterPush.Character)
		s.sendCharacterPush(conn, event.CharacterPush)
		s.broadcastCharacterPushExcept(event.CharacterPush, conn)
	case world.SpellEventHealingGauge:
		if event.Character.ID != "" {
			if client, ok := s.ClientByCharacterID(event.Character.ID); ok {
				client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageHealGauge, gauge: event.Character})
			}
		}
	case world.SpellEventExperience:
		s.sendWinExp(conn, event.Experience, event.CurrentExp)
	case world.SpellEventLevelUp:
		s.sendLevelUp(conn, event.Character)
	case world.SpellEventSkillExp:
		delay := event.SkillExpDelay
		if delay <= 0 {
			delay = time.Second
		}
		time.AfterFunc(delay, func() {
			if client, ok := s.ClientByCharacterID(event.Character.ID); ok {
				client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageSkillExp, skillExp: spellSkillExpEvent{magicID: event.MagicID, skillLevel: event.SkillLevel, skillTrain: event.SkillTrain}})
			}
		})
	}
}

func (s *Server) broadcastSpellRush(rush world.SpellRush) {
	s.broadcastSpellRushExcept(rush, nil)
}

func (s *Server) broadcastSpellRushExcept(rush world.SpellRush, except net.Conn) {
	for _, client := range s.spellRefClients(rush.Character) {
		if except != nil && client.conn == except {
			continue
		}
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageRush, rush: rush})
	}
}

func (s *Server) sendSpellRush(conn net.Conn, rush world.SpellRush) {
	ident := uint16(mir176.SMRush)
	if rush.Kung {
		ident = uint16(mir176.SMRushKung)
	}
	body := EncodeBuffer(CharDesc(s.world.HumanFeatureForCharacter(rush.Character), s.world.CharacterStatus(rush.Character)))
	s.sendCommand(conn, mir176.Command{
		Ident:  ident,
		Recog:  world.CharacterActorID(rush.Character),
		Param:  uint16(rush.X),
		Tag:    uint16(rush.Y),
		Series: makeWord(byte(rush.Dir), byte(s.world.MapLight(rush.Character.MapID))),
	}, body)
}

func (s *Server) handleAffectedSpellCharacter(conn net.Conn, activeChar *storage.Character, event world.SpellEvent) {
	ch := event.Character
	if ch.ID == "" {
		return
	}
	prev, hadPrev := storage.Character{}, false
	if client, ok := s.ClientByCharacterID(ch.ID); ok {
		prev = client.character()
		hadPrev = true
	}
	s.updateClientByCharacterID(ch)
	if activeChar != nil && activeChar.ID == ch.ID {
		*activeChar = ch
	}
	if hadPrev {
		featureChanged := s.world.HumanFeatureForCharacter(prev) != s.world.HumanFeatureForCharacter(ch) || prev.Dir != ch.Dir || prev.MapID != ch.MapID || prev.X != ch.X || prev.Y != ch.Y
		statusChanged := s.world.CharacterStatus(prev) != s.world.CharacterStatus(ch)
		if featureChanged {
			s.broadcastCharacterStateRefresh(ch)
		}
		if statusChanged && !event.SuppressStatusBroadcast {
			s.broadcastCharacterStatusChanged(ch)
		}
	}
	if client, ok := s.ClientByCharacterID(ch.ID); ok {
		if event.SystemMessage != "" {
			s.sendSystemMessageStyle(client.conn, ch, event.SystemMessage, 0xDB, 0xFF)
		}
		if event.SendStatus {
			if activeChar != nil && activeChar.ID == ch.ID {
				s.handleCharacterStatusChanged(conn, ch)
			} else {
				client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageStatus, status: ch})
			}
		}
		if event.SendHealth {
			s.sendHealthSpellChanged(client.conn, world.CharacterActorID(ch), s.world.AbilityStats(ch))
		}
		if event.SendAbility {
			if activeChar != nil && activeChar.ID == ch.ID {
				s.sendAbilityOnly(conn, ch)
			} else {
				client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageAbility, ability: ch})
			}
		}
	}
	if event.SendHealth {
		s.broadcastCharacterHealthSpellChanged(ch)
	}
	if event.SendUserState {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMSendUserState}, EncodeBuffer(UserStateBody(s.world, ch)))
	}
}

func (s *Server) advancePowerHitState(client *Client, ch storage.Character) {
	s.advancePowerHitStateLocked(client, ch, true)
}

func (s *Server) advancePowerHitStateLocked(client *Client, ch storage.Character, notify bool) bool {
	if client == nil {
		return false
	}
	if !powerHitUsable(ch) {
		client.mu.Lock()
		client.powerHitArmed = false
		client.mu.Unlock()
		return false
	}
	state, _, learned := ch.Skills.Get("攻杀剑术")
	if !learned || state.Level > 3 {
		return false
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.powerHitCount <= 0 {
		client.powerHitCount = 7 - int(state.Level)
		if client.powerHitCount < 1 {
			client.powerHitCount = 1
		}
		client.powerHitPointCount = rand.Intn(client.powerHitCount)
	}
	client.powerHitCount--
	if client.powerHitCount == client.powerHitPointCount {
		client.powerHitArmed = true
		client.powerHitCount = 7 - int(state.Level)
		if client.powerHitCount < 1 {
			client.powerHitCount = 1
		}
		client.powerHitPointCount = rand.Intn(client.powerHitCount)
		if notify {
			s.sendRawFrame(client.conn, "+PWR")
		}
		return true
	}
	if client.powerHitCount <= 0 {
		client.powerHitCount = 7 - int(state.Level)
		if client.powerHitCount < 1 {
			client.powerHitCount = 1
		}
		client.powerHitPointCount = rand.Intn(client.powerHitCount)
	}
	return false
}

func powerHitUsable(ch storage.Character) bool {
	weapon, ok := ch.EquippedItems[SlotWeapon]
	if !ok || weapon.Dura == 0 {
		return false
	}
	state, _, learned := ch.Skills.Get("攻杀剑术")
	return learned && state.Level <= 3
}

func (s *Server) sendRawFrame(conn net.Conn, text string) {
	response := mir176.WrapFrame([]byte(text))
	if client := s.clientForConn(conn); client != nil {
		client.enqueueOutput(response)
		return
	}
	_, _ = conn.Write(response)
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
	if result.Command != nil {
		s.handleUserCommandResult(conn, activeChar, *result.Command)
		return
	}
	world.ApplySaySync(itemUseSyncAdapter{s: s, conn: conn}, *activeChar, result)
}

func (s *Server) handleClickNPC(conn net.Conn, activeChar *storage.Character, activeClient *Client, cmd mir176.Command) {
	entity, ok := s.world.NPCByActorID(cmd.Recog)
	if !ok {
		return
	}
	if entity.MapID != activeChar.MapID || absInt(entity.X-activeChar.X) > 15 || absInt(entity.Y-activeChar.Y) > 15 {
		return
	}
	if activeClient != nil {
		activeClient.mu.Lock()
		activeClient.activeNPCID = entity.ID
		activeClient.merchantCurrentLabel = "@main"
		activeClient.merchantGoBackLabel = ""
		activeClient.mu.Unlock()
	}
	conversation, ok := s.world.NPCConversation(*activeChar, entity.ID, "@main")
	if !ok {
		return
	}
	s.sendNPCConversation(conn, conversation)
}

func (s *Server) handleMerchantDlgSelect(conn net.Conn, activeChar *storage.Character, activeClient *Client, cmd mir176.Command, text []byte) {
	entity, ok := s.resolveMerchantEntity(activeClient, cmd)
	if !ok {
		return
	}
	rawText := strings.TrimSpace(DecodeString(text))
	if activeClient != nil {
		activeClient.mu.Lock()
		activeClient.activeNPCID = entity.ID
		activeClient.mu.Unlock()
	}
	label := s.world.NPCLabelSelection(rawText)
	if s.handleTeleportMerchantDlgSelect(conn, activeChar, entity, label) {
		return
	}
	if s.handleWarehouseMerchantDlgSelect(conn, activeChar, entity, label) {
		return
	}
	if s.handleSpecialMerchantDlgSelect(conn, activeChar, activeClient, entity, label, rawText) {
		return
	}
	if activeClient != nil {
		activeClient.mu.Lock()
		if strings.EqualFold(label, "@back") {
			if activeClient.merchantGoBackLabel == "" {
				activeClient.merchantGoBackLabel = "@main"
			}
			label = activeClient.merchantGoBackLabel
		}
		if strings.HasPrefix(rawText, "@") {
			if activeClient.merchantCurrentLabel != "" && !strings.EqualFold(activeClient.merchantCurrentLabel, label) {
				activeClient.merchantGoBackLabel = activeClient.merchantCurrentLabel
			}
			activeClient.merchantCurrentLabel = label
		}
		pendingAction := activeClient.pendingMerchantAction
		if pendingAction != "" && rawText != "" && !strings.HasPrefix(rawText, "@") {
			activeClient.pendingMerchantAction = ""
			activeClient.mu.Unlock()
			if s.handlePendingMerchantAction(conn, activeChar, entity, pendingAction, rawText) {
				return
			}
			activeClient.mu.Lock()
		}
		activeClient.mu.Unlock()
	}
	if s.sendMerchantMenu(conn, cmd.Recog, *activeChar, entity, label) {
		return
	}
	if strings.EqualFold(label, "@exit") {
		s.sendMerchantDlgClose(conn, cmd.Recog)
		return
	}
	conversation, ok := s.world.NPCConversation(*activeChar, entity.ID, label)
	if !ok {
		return
	}
	s.sendNPCConversation(conn, conversation)
}

func (s *Server) handlePendingMerchantAction(conn net.Conn, activeChar *storage.Character, entity npc.Entity, action, text string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "buildguild":
		if text == "" {
			s.sendMerchantSay(conn, entity.Name, "请填写行会名称。\n<返回/@main>")
			return true
		}
		price := s.world.Gameplay().Guild.BuildGuildPrice
		if activeChar.Gold < price {
			s.sendMerchantSay(conn, entity.Name, "你身上的钱不够！请准备好后再来。\n<返回/@main>")
			return true
		}
		if !bagHasItemID(*activeChar, "沃玛号角") {
			s.sendMerchantSay(conn, entity.Name, "你没有准备好需要的全部物品。\n<返回/@main>")
			return true
		}
		updated, removed, ok := removeBagItemsByID(*activeChar, "沃玛号角", 1)
		if !ok {
			s.sendMerchantSay(conn, entity.Name, "你没有准备好需要的全部物品。\n<返回/@main>")
			return true
		}
		updated.Gold -= price
		*activeChar = updated
		s.sendDelItemList(conn, removed)
		s.sendGoldChanged(conn, updated.Gold)
		s.sendMerchantSay(conn, entity.Name, "行会创建申请已提交: "+text+"\n<返回/@main>")
		if err := s.store.SaveCharacter(updated); err != nil {
		}
		return true
	case "guildwar":
		if text == "" {
			s.sendMerchantSay(conn, entity.Name, fmt.Sprintf("填写与你交战的敌对行会的名字，申请行会战争必须支付%d金币。\n<立即申请行会战争/@@guildwar>\n<返回/@main>", s.world.Gameplay().Guild.GuildWarPrice))
			return true
		}
		price := s.world.Gameplay().Guild.GuildWarPrice
		if activeChar.Gold < price {
			s.sendMerchantSay(conn, entity.Name, "你身上的钱不够！请准备好后再来。\n<返回/@main>")
			return true
		}
		updated := *activeChar
		updated.Gold -= price
		*activeChar = updated
		s.sendGoldChanged(conn, updated.Gold)
		s.sendMerchantSay(conn, entity.Name, "行会战争申请已提交: "+text+"\n<返回/@main>")
		if err := s.store.SaveCharacter(updated); err != nil {
		}
		return true
	}
	return false
}

func (s *Server) handleTeleportMerchantDlgSelect(conn net.Conn, activeChar *storage.Character, entity npc.Entity, label string) bool {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "@anquan":
		if activeChar.Level <= 6 {
			s.sendMerchantSay(conn, entity.Name, "照你现在这个级别,我没什么能帮的上你!\n请你练到7级再来找我吧，祝你好运!")
			return true
		}
		s.sendMerchantSay(conn, entity.Name, "这里是<城区传送>服务,你必须给我2000金币的报酬!\n┏━━━━┳━━━━┳━━━━┳━━━━┓\n┃<比齐大城/@JIANAN>┃<毒蛇山谷/@FENGDI>┃<银杏小村/@XIAGU>┃<比奇村庄/@HAIBIN>┃\n┣━━━━╋━━━━╋━━━━╋━━━━┫\n┃<盟重土城/@YASHU>┃<苍月之岛/@HUANGCHENG>┃<封魔神谷/@JIANYU>┃<白 日 门/@SHADINDAO>┃\n┗━━━━┻━━━━┻━━━━┻━━━━┛")
		return true
	case "@xiane":
		if activeChar.Level > 34 {
			s.sendMerchantSay(conn, entity.Name, "这里是<险恶地区>服务，按照你的级别你可以前往以下地区:\n当然你还得付给我3000金币的报酬!\n┏━━━━┳━━━━┳━━━━┳━━━━┳━━━━┓\n┃<沃玛三层/@JM7>┃<猪洞七层/@JM8>┃<祖玛七层/@JM5>┃<死亡棺材/@JM6>┃<抉择之地/@S6>┃\n┣━━━━╋━━━━╋━━━━╋━━━━╋━━━━┫\n┃<比齐矿区/@JN1>┃<蜈蚣洞穴/@JN2>┃<天然洞穴/@JM1>┃<牛魔四层/@JM2>┃<封魔矿区/@FENGMOKOU>┃\n┣━━━━╋━━━━╋━━━━╋━━━━╋━━━━┫\n┃<未知暗殿/@JXJDVE>┃<尸 魔 洞/@JM3>┃<骨 魔 洞/@JM4>┃<尸王大殿/@LM2>┃<沙城区域/@沙城区域>┃\n┗━━━━┻━━━━┻━━━━┻━━━━┻━━━━┛")
			return true
		}
		if activeChar.Level > 21 {
			s.sendMerchantSay(conn, entity.Name, "这里是<险恶地区>服务，按照你的级别35级前你可以前往以下地区:\n当然你还得付给我3000金币的报酬!\n┏━━━━┳━━━━┳━━━━┳━━━━┳━━━━┓\n┃<沃玛二层/@S1>┃<猪洞一层/@S2>┃<祖玛三层/@S3>┃<赤月峡谷/@S5>┃<封魔矿区/@FENGMOKOU>┃\n┣━━━━╋━━━━╋━━━━╋━━━━╋━━━━┫\n┃<比齐矿区/@JN1>┃<蜈蚣洞穴/@JN2>┃<天然洞穴/@JM1>┃<牛魔一层/@NN7>┃<尸 魔 洞/@JM3>┃\n┣━━━━╋━━━━╋━━━━┻━━━━┻━━━━┫\n┃<骨 魔 洞/@JM4>┃<尸王大殿/@LM2>┃\n┗━━━━┻━━━━┛")
			return true
		}
		if activeChar.Level > 6 {
			s.sendMerchantSay(conn, entity.Name, "这里是<险恶地区>服务，按照你的级别22级前你只能前往以下地区:\n当然你还得付给我3000金币的报酬!\n┏━━━━┳━━━━┳━━━━┓\n┃<比齐矿区/@JN1>┃<蜈蚣洞穴/@JN2>┃<天然洞穴/@JM1>┃\n┣━━━━╋━━━━┻━━━━┛\n┃<封魔矿区/@FENGMOKOU>┃\n┗━━━━┛")
			return true
		}
		s.sendMerchantSay(conn, entity.Name, "照你现在这个级别,我没什么能帮的上你!\n请你练到7级再来找我吧，祝你好运!")
		return true
	case "@huan":
		s.sendMerchantSay(conn, entity.Name, "移动到幻境需要2万金币，移动吗？\n<移动/@移动> \n<不/@exit> \n<返 回/@Main>")
		return true
	case "@time":
		s.sendMerchantSay(conn, entity.Name, teleporterTimeMessage(time.Now(), activeChar.Name))
		return true
	case "@jianan":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "0", 333, 268, false, 2000, "比齐大城", "")
	case "@fengdi":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "2", 500, 485, false, 2000, "毒蛇山谷", "")
	case "@xiagu":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "0", 635, 612, false, 2000, "银杏小村", "")
	case "@haibin":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "0", 290, 615, false, 2000, "比奇村庄", "")
	case "@yashu":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "3", 330, 330, false, 2000, "盟重土城", "")
	case "@huangcheng":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "5", 140, 330, false, 2000, "苍月之岛", "")
	case "@jianyu":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "4", 240, 200, false, 2000, "封魔神谷", "")
	case "@shadindao":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "11", 180, 325, false, 2000, "白日门", "")
	case "@jm7":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "D024", 0, 0, true, 3000, "沃玛三层", "回城卷")
	case "@jm8":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "D716", 0, 0, true, 3000, "猪洞七层", "回城卷")
	case "@jm5":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "D5071", 8, 10, false, 3000, "祖玛七层", "回城卷")
	case "@jm6":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "D606", 0, 0, true, 3000, "死亡棺材", "回城卷")
	case "@s6":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "D1004", 0, 0, true, 3000, "抉择之地", "回城卷")
	case "@jn1":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "D401", 0, 0, true, 3000, "比齐矿区", "回城卷")
	case "@jn2":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "D601", 0, 0, true, 3000, "蜈蚣洞穴", "回城卷")
	case "@jm1":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "E001", 0, 0, true, 3000, "天然洞穴", "回城卷")
	case "@jm2":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "D2075", 0, 0, true, 3000, "牛魔四层", "回城卷")
	case "@fengmokou":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "4", 138, 69, false, 3000, "封魔矿区", "回城卷")
	case "@jxjdve":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "M001", 0, 0, true, 3000, "未知暗殿", "回城卷")
	case "@jm3":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "D2051", 0, 0, true, 3000, "尸魔洞", "回城卷")
	case "@jm4":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "D2061", 0, 0, true, 3000, "骨魔洞", "回城卷")
	case "@lm2":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "Q004", 0, 0, true, 3000, "尸王大殿", "回城卷")
	case "@沙城区域":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "3", 716, 407, false, 3000, "沙城区域", "回城卷")
	case "@s1":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "D023", 0, 0, true, 3000, "沃玛二层", "回城卷")
	case "@s2":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "D711", 0, 0, true, 3000, "猪洞一层", "回城卷")
	case "@s3":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "D503", 0, 0, true, 3000, "祖玛三层", "回城卷")
	case "@s5":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "D10011", 0, 0, true, 3000, "赤月峡谷", "回城卷")
	case "@nn7":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "D2071", 0, 0, true, 3000, "牛魔一层", "回城卷")
	case "@移动":
		return s.teleportMerchantCharacter(conn, activeChar, entity, "H001", 73, 67, false, 20000, "幻境", "回城卷")
	}
	return false
}

func (s *Server) teleportMerchantCharacter(conn net.Conn, activeChar *storage.Character, entity npc.Entity, mapID string, x, y int, random bool, goldCost int, destination, giftItemID string) bool {
	if activeChar.Gold < goldCost {
		s.sendMerchantSay(conn, entity.Name, "你身上的钱不够！请准备好后再来。\n<离 开/@exit>")
		return true
	}
	if giftItemID != "" && !s.world.CanCarryBagItems(*activeChar, 1) {
		s.sendMerchantSay(conn, entity.Name, "你的包裹已经满了，暂时不能前往那里。\n<离 开/@exit>")
		return true
	}
	prev := *activeChar
	var (
		updated storage.Character
		err     error
	)
	if random {
		updated, err = s.world.TeleportRandomInMap(prev, mapID)
	} else {
		updated, err = s.world.Teleport(prev, mapID, x, y)
	}
	if err != nil {
		s.sendMerchantSay(conn, entity.Name, "目的地暂时无法前往。\n<离 开/@exit>")
		return true
	}
	updated.Gold -= goldCost
	if giftItemID != "" && s.world.CanCarryBagItems(updated, 1) {
		gift := storage.UserItem{ItemID: giftItemID, MakeIndex: int32(time.Now().UnixNano() & 0x7fffffff)}
		updated.BagItems = append(updated.BagItems, gift)
		s.sendBagAddItem(conn, updated, gift.ItemID, gift.MakeIndex)
	}
	*activeChar = updated
	world.ApplyTeleportSync(teleportSyncAdapter{s: s, conn: conn}, world.TeleportEvent{From: prev, To: updated})
	s.sendGoldChanged(conn, updated.Gold)
	s.sendMerchantDlgClose(conn, s.world.NPCActorID(entity.ID))
	if err := s.store.SaveCharacter(updated); err != nil {
	}
	return true
}

func teleporterTimeMessage(now time.Time, username string) string {
	day := map[time.Weekday]string{
		time.Sunday:    "星期天",
		time.Monday:    "星期一",
		time.Tuesday:   "星期二",
		time.Wednesday: "星期三",
		time.Thursday:  "星期四",
		time.Friday:    "星期五",
		time.Saturday:  "星期六",
	}[now.Weekday()]
	if day == "" {
		day = "星期天"
	}
	hour := now.Hour()
	minute := now.Minute()
	greeting := "晚上好！"
	switch {
	case hour <= 5:
		greeting = "凌晨好！"
	case hour <= 10:
		greeting = "早上好！"
	case hour <= 12:
		greeting = "中午好！"
	case hour <= 17:
		greeting = "下午好！"
	}
	return fmt.Sprintf("%s %s今天是 <%s> 游戏时间 %02d:%02d。\n<返 回/@main>", username, greeting, day, hour, minute)
}

func (s *Server) handleWarehouseMerchantDlgSelect(conn net.Conn, activeChar *storage.Character, entity npc.Entity, label string) bool {
	if !entity.Merchant.Capabilities.Storage || !entity.Merchant.Capabilities.GetBack {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "@mbind":
		s.sendMerchantSay(conn, entity.Name, "你知道我是什么人吗？\n我做的是这样的事情...\n你要试一下吗？有什么要拜托的就说吧。\n用金币<交换/@changeGold>金条 \n用金条<交换/@changeMoney>金币 \n<捆/@bind>\n<离 开/@exit>")
		return true
	case "@changegold":
		if activeChar.Gold < 1002000 {
			s.sendMerchantSay(conn, entity.Name, "你连这点钱都没有，还换什么？\n等你有足够的钱，再来找我吧\n<返 回/@Main>")
			return true
		}
		s.sendMerchantSay(conn, entity.Name, "你说你要用金币换成金条?\n好的，我帮你换\n但是要支付手续费\n费用是2000金币，你还换吗？\n<交换/@changeGold_1>\n<离 开/@exit>")
		return true
	case "@changegold_1":
		if activeChar.Gold < 1002000 {
			s.sendMerchantSay(conn, entity.Name, "你的包里东西已经满了，或者你没有足够的钱支付手续费\n你再确认一下吧\n<离 开/@exit>")
			return true
		}
		if !s.world.CanCarryBagItems(*activeChar, 1) {
			s.sendMerchantSay(conn, entity.Name, "你的包里东西已经满了，或者你没有足够的钱支付手续费\n你再确认一下吧\n<离 开/@exit>")
			return true
		}
		updated := *activeChar
		updated.Gold -= 1002000
		bought := storage.UserItem{ItemID: "金条", MakeIndex: int32(time.Now().UnixNano() & 0x7fffffff)}
		updated.BagItems = append(updated.BagItems, bought)
		*activeChar = updated
		s.sendBagAddItem(conn, updated, bought.ItemID, bought.MakeIndex)
		s.sendGoldChanged(conn, updated.Gold)
		s.sendWeightChanged(conn, s.world.AbilityStats(updated))
		s.sendMerchantSay(conn, entity.Name, "金币已经换好金条了.\n还换吗？\n<交换/@changeGold>\n<离 开/@exit>")
		if err := s.store.SaveCharacter(updated); err != nil {
		}
		return true
	case "@changemoney":
		if !bagHasItemID(*activeChar, "金条") {
			s.sendMerchantSay(conn, entity.Name, "你都没有金条还换什么?\n想骗我?快滚!\n<离 开/@exit>")
			return true
		}
		s.sendMerchantSay(conn, entity.Name, "你要把金条换成金币?\n好的，我给你换\n不过需要支付手续费\n费用是2000金币，你还换吗？\n<交换/@changeMoney_1>\n<离 开/@exit>")
		return true
	case "@changemoney_1":
		if !bagHasItemID(*activeChar, "金条") {
			s.sendMerchantSay(conn, entity.Name, "你都没有金条还换什么?\n想骗我?快滚!\n<离 开/@exit>")
			return true
		}
		if activeChar.Gold >= 14000001 {
			s.sendMerchantSay(conn, entity.Name, "我也很想给你换，\n但是你钱太多了，我没办法给你换.\n<离 开/@exit>")
			return true
		}
		updated, removed, ok := removeBagItemsByID(*activeChar, "金条", 1)
		if !ok {
			s.sendMerchantSay(conn, entity.Name, "你都没有金条还换什么?\n想骗我?快滚!\n<离 开/@exit>")
			return true
		}
		updated.Gold += 998000
		*activeChar = updated
		s.sendDelItemList(conn, removed)
		s.sendGoldChanged(conn, updated.Gold)
		s.sendWeightChanged(conn, s.world.AbilityStats(updated))
		s.sendMerchantSay(conn, entity.Name, "金条已经换好金币.\n还继续换吗?\n<交换/@changeMoney>\n<返 回/@main>\n<关闭/@exit>")
		if err := s.store.SaveCharacter(updated); err != nil {
		}
		return true
	case "@bind":
		s.sendMerchantSay(conn, entity.Name, "目前我能捆的只有卷书和药水\n你要捆吗？\n要捆东西需要100金币.\n<捆/@P_bind>药水\n<捆/@Z_bind>卷书")
		return true
	case "@p_bind":
		s.sendMerchantSay(conn, entity.Name, "<捆/@ch_bind1>强效金创药\n<捆/@ma_bind1>强效魔法药 \n<捆/@ch_bind2>金创药(中量)\n<捆/@ma_bind2>魔法药(中量)\n<捆/@ch_bind3>金创药\n<捆/@ma_bind3>魔法药\n<返 回/@bind>")
		return true
	case "@z_bind":
		s.sendMerchantSay(conn, entity.Name, "<捆/@zum_bind1>地牢逃脱卷\n<捆/@zum_bind2>随机传送卷\n<捆/@zum_bind3>回城卷\n<捆/@zum_bind4>行会回城卷\n<返 回/@bind>")
		return true
	case "@ch_bind1":
		return s.handleWarehousePackExchange(conn, activeChar, entity, "强效金创药", "超级金创药")
	case "@ma_bind1":
		return s.handleWarehousePackExchange(conn, activeChar, entity, "强效魔法药", "超级魔法药")
	case "@ch_bind2":
		return s.handleWarehousePackExchange(conn, activeChar, entity, "金创药(中量)", "金创药(中)包")
	case "@ma_bind2":
		return s.handleWarehousePackExchange(conn, activeChar, entity, "魔法药(中量)", "魔法药(中)包")
	case "@ch_bind3":
		return s.handleWarehousePackExchange(conn, activeChar, entity, "金创药", "金创药(小)包")
	case "@ma_bind3":
		return s.handleWarehousePackExchange(conn, activeChar, entity, "魔法药", "魔法药(小)包")
	case "@zum_bind1":
		return s.handleWarehousePackExchange(conn, activeChar, entity, "地牢逃脱卷", "地牢逃脱卷包")
	case "@zum_bind2":
		return s.handleWarehousePackExchange(conn, activeChar, entity, "随机传送卷", "随机传送卷包")
	case "@zum_bind3":
		return s.handleWarehousePackExchange(conn, activeChar, entity, "回城卷", "回城卷包")
	case "@zum_bind4":
		return s.handleWarehousePackExchange(conn, activeChar, entity, "行会回城卷", "行会回城卷包")
	}
	return false
}

func (s *Server) handleSpecialMerchantDlgSelect(conn net.Conn, activeChar *storage.Character, activeClient *Client, entity npc.Entity, label, text string) bool {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "@upgradenow":
		return s.handleWeaponUpgradeStart(conn, activeChar, entity)
	case "@getbackupgnow":
		return s.handleWeaponUpgradeGetBack(conn, activeChar, entity)
	case "@buildguildnow":
		if activeClient != nil {
			activeClient.mu.Lock()
			activeClient.pendingMerchantAction = "buildguild"
			activeClient.mu.Unlock()
		}
		if text == "" || strings.EqualFold(text, label) || strings.HasPrefix(text, "@") {
			s.sendMerchantSay(conn, entity.Name, "请填写行会名称。\n<返回/@main>")
			return true
		}
		return s.handlePendingMerchantAction(conn, activeChar, entity, "buildguild", text)
	case "@guildwar":
		if activeClient != nil {
			activeClient.mu.Lock()
			activeClient.pendingMerchantAction = "guildwar"
			activeClient.mu.Unlock()
		}
		if text == "" || strings.EqualFold(text, label) || strings.HasPrefix(text, "@") {
			s.sendMerchantSay(conn, entity.Name, "填写与你交战的敌对行会的名字，申请行会战争必须支付3万金币。\n<立即申请行会战争/@@guildwar>\n<返回/@main>")
			return true
		}
		return s.handlePendingMerchantAction(conn, activeChar, entity, "guildwar", text)
	case "@@guildwar":
		if text == "" || strings.EqualFold(text, label) || strings.HasPrefix(text, "@") {
			if activeClient != nil {
				activeClient.mu.Lock()
				activeClient.pendingMerchantAction = "guildwar"
				activeClient.mu.Unlock()
			}
			s.sendMerchantSay(conn, entity.Name, "填写与你交战的敌对行会的名字，申请行会战争必须支付3万金币。\n<立即申请行会战争/@@guildwar>\n<返回/@main>")
			return true
		}
		return s.handlePendingMerchantAction(conn, activeChar, entity, "guildwar", text)
	case "@@withdrawal", "@@receipts":
		s.sendMerchantSay(conn, entity.Name, "沙巴克城堡功能暂未接入。\n<返回/@main>")
		return true
	case "@requestcastlewarnow":
		if !bagHasItemID(*activeChar, "祖玛头像") {
			s.sendMerchantSay(conn, entity.Name, "你没有祖玛教主的头像。\n<返回/@main>")
			return true
		}
		updated, removed, ok := removeBagItemsByID(*activeChar, "祖玛头像", 1)
		if !ok {
			s.sendMerchantSay(conn, entity.Name, "你没有祖玛教主的头像。\n<返回/@main>")
			return true
		}
		*activeChar = updated
		s.sendDelItemList(conn, removed)
		s.sendMerchantSay(conn, entity.Name, "沙巴克攻城申请已经提交。\n战争会在第二天内开始。\n<返回/@main>")
		if err := s.store.SaveCharacter(updated); err != nil {
		}
		return true
	case "@openmaindoor":
		s.sendMerchantSay(conn, entity.Name, "城门已打开.\n<返回/@treatdoor>")
		return true
	case "@closemaindoor":
		s.sendMerchantSay(conn, entity.Name, "城门已关闭.\n<返回/@treatdoor>")
		return true
	case "@repairdoornow":
		s.sendMerchantSay(conn, entity.Name, "城堡功能暂未接入。\n<返回/@repairdoor>")
		return true
	case "@repairwallnow1", "@repairwallnow2", "@repairwallnow3":
		s.sendMerchantSay(conn, entity.Name, "城堡功能暂未接入。\n<返回/@repairwalls>")
		return true
	case "@hireguardnow1", "@hireguardnow2", "@hireguardnow3", "@hireguardnow4":
		s.sendMerchantSay(conn, entity.Name, "城堡功能暂未接入。\n<返回/@hireguards>")
		return true
	case "@hirearchernow1", "@hirearchernow2", "@hirearchernow3", "@hirearchernow4", "@hirearchernow5", "@hirearchernow6", "@hirearchernow7", "@hirearchernow8", "@hirearchernow9", "@hirearchernow10", "@hirearchernow11", "@hirearchernow12":
		s.sendMerchantSay(conn, entity.Name, "城堡功能暂未接入。\n<返回/@hirearchers>")
		return true
	case "@guardrule_normalnow":
		s.sendMerchantSay(conn, entity.Name, "防守方式已经更改，守卫们已经目前处于正常防御状态.\n<返回/@guardcmd>")
		return true
	case "@guardrule_pkattack":
		s.sendMerchantSay(conn, entity.Name, "防守方式已经更改，守卫们已经目前处于对来犯者进攻状态.\n<返回/@guardcmd>")
		return true
	}
	return false
}

func (s *Server) handleWarehousePackExchange(conn net.Conn, activeChar *storage.Character, entity npc.Entity, fromItemID, toItemID string) bool {
	if !bagHasItemCount(*activeChar, fromItemID, 6) {
		s.sendMerchantSay(conn, entity.Name, "你都没有要捆的药水，还捆什么?\n等准备好药水之后再来找我吧..\n<离 开/@exit>")
		return true
	}
	if activeChar.Gold < 100 {
		s.sendMerchantSay(conn, entity.Name, "你都没有钱捆东西，\n还捆什么?\n快走吧....\n<离 开/@exit>")
		return true
	}
	updated, removed, ok := removeBagItemsByID(*activeChar, fromItemID, 6)
	if !ok {
		s.sendMerchantSay(conn, entity.Name, "你都没有要捆的药水，还捆什么?\n等准备好药水之后再来找我吧..\n<离 开/@exit>")
		return true
	}
	updated.Gold -= 100
	bought := storage.UserItem{ItemID: toItemID, MakeIndex: int32(time.Now().UnixNano() & 0x7fffffff)}
	updated.BagItems = append(updated.BagItems, bought)
	*activeChar = updated
	s.sendDelItemList(conn, removed)
	s.sendBagAddItem(conn, updated, bought.ItemID, bought.MakeIndex)
	s.sendGoldChanged(conn, updated.Gold)
	s.sendWeightChanged(conn, s.world.AbilityStats(updated))
	s.sendMerchantSay(conn, entity.Name, "已经捆好了... 我的技术不错吧..\n以后还有要捆的，就来找我吧..\n<继续捆/@P_bind>\n<离 开/@exit>")
	if err := s.store.SaveCharacter(updated); err != nil {
	}
	return true
}

func (s *Server) handleMerchantQuerySellPrice(conn net.Conn, activeChar *storage.Character, activeClient *Client, cmd mir176.Command, text []byte) {
	entity, ok := s.resolveMerchantEntity(activeClient, cmd)
	if !ok || !entity.Merchant.Capabilities.Sell {
		return
	}
	makeIndex := merchantMakeIndex(cmd)
	itemName := strings.TrimSpace(DecodeString(text))
	entry, item, ok := merchantBagItemByMakeIndex(*activeChar, makeIndex, itemName, s.world)
	if !ok {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMSendBuyPrice, Recog: 0}, nil)
		return
	}
	price := merchantSellPrice(item, entry, entity.Merchant.PriceRate)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMSendBuyPrice, Recog: int32(price)}, nil)
}

func (s *Server) handleUserSellItem(conn net.Conn, activeChar *storage.Character, activeClient *Client, cmd mir176.Command, text []byte) {
	entity, ok := s.resolveMerchantEntity(activeClient, cmd)
	if !ok || !entity.Merchant.Capabilities.Sell {
		return
	}
	makeIndex := merchantMakeIndex(cmd)
	itemName := strings.TrimSpace(DecodeString(text))
	slot, entry, ok := merchantBagItemSlotByMakeIndex(*activeChar, makeIndex, itemName, s.world)
	if !ok {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMUserSellItemFail, Recog: cmd.Recog}, nil)
		return
	}
	item, _ := s.world.Item(entry.ItemID)
	price := merchantSellPrice(item, entry, entity.Merchant.PriceRate)
	if price <= 0 {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMUserSellItemFail, Recog: cmd.Recog}, nil)
		return
	}
	updated := *activeChar
	updated.BagItems = append(updated.BagItems[:slot], updated.BagItems[slot+1:]...)
	updated.Gold += price
	*activeChar = updated
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMUserSellItemOK, Recog: int32(updated.Gold)}, nil)
	s.sendGoldChanged(conn, updated.Gold)
	s.world.AddMerchantStock(entity.ID, entry)
	if err := s.store.SaveCharacter(updated); err != nil {
	}
}

func (s *Server) handleUserBuyItem(conn net.Conn, activeChar *storage.Character, activeClient *Client, cmd mir176.Command, text []byte) {
	entity, ok := s.resolveMerchantEntity(activeClient, cmd)
	if !ok || !entity.Merchant.Capabilities.Buy {
		return
	}
	makeIndex := merchantMakeIndex(cmd)
	itemName := strings.TrimSpace(DecodeString(text))
	stock, item, ok := merchantStockItemByMakeIndexOrName(s.world, entity, makeIndex, itemName)
	if !ok {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMBuyItemFail, Recog: cmd.Recog}, nil)
		return
	}
	price := merchantPrice(item, entity.Merchant.PriceRate)
	if price <= 0 || activeChar.Gold < price {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMBuyItemFail, Recog: cmd.Recog}, nil)
		return
	}
	if !s.world.CanCarryBagItems(*activeChar, 1) {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMBuyItemFail, Param: 2}, nil)
		return
	}
	if !s.world.ConsumeMerchantStock(entity.ID, stock.MakeIndex, item.ID) {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMBuyItemFail, Recog: cmd.Recog}, nil)
		return
	}
	updated := *activeChar
	updated.Gold -= price
	bought := storage.UserItem{ItemID: item.ID, MakeIndex: int32(time.Now().UnixNano() & 0x7fffffff)}
	updated.BagItems = append(updated.BagItems, bought)
	*activeChar = updated
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMBuyItemSuccess, Recog: int32(updated.Gold), Param: 1}, nil)
	s.sendBagAddItem(conn, updated, bought.ItemID, bought.MakeIndex)
	s.sendGoldChanged(conn, updated.Gold)
	if err := s.store.SaveCharacter(updated); err != nil {
	}
}

func (s *Server) handleUserGetDetailItem(conn net.Conn, activeChar *storage.Character, activeClient *Client, cmd mir176.Command, text []byte) {
	entity, ok := s.resolveMerchantEntity(activeClient, cmd)
	if !ok {
		return
	}
	itemName := strings.TrimSpace(DecodeString(text))
	stocks := s.world.MerchantStock(entity.ID)
	if !merchantStockHasItemName(s.world, stocks, itemName) {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMSendDetailGoodsList, Recog: cmd.Recog}, nil)
		return
	}
	body, count, page := merchantDetailGoodsListBody(s.world, stocks, itemName, int(cmd.Param), entity.Merchant.PriceRate)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMSendDetailGoodsList, Recog: cmd.Recog, Param: uint16(count), Tag: uint16(page)}, EncodeString(string(body)))
}

func (s *Server) handleMerchantQueryRepairCost(conn net.Conn, activeChar *storage.Character, activeClient *Client, cmd mir176.Command, text []byte) {
	entity, ok := s.resolveMerchantEntity(activeClient, cmd)
	if !ok || !entity.Merchant.Capabilities.Repair {
		return
	}
	makeIndex := merchantMakeIndex(cmd)
	itemName := strings.TrimSpace(DecodeString(text))
	entry, item, ok := merchantBagItemByMakeIndex(*activeChar, makeIndex, itemName, s.world)
	if !ok {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMSendRepairCost, Recog: cmd.Recog}, nil)
		return
	}
	special := merchantIsSpecialRepair(activeClient)
	price := merchantRepairPrice(item, entry, special, s.world.Gameplay().Castle.SuperRepairPriceRate)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMSendRepairCost, Recog: cmd.Recog, Param: uint16(price)}, nil)
}

func (s *Server) handleUserRepairItem(conn net.Conn, activeChar *storage.Character, activeClient *Client, cmd mir176.Command, text []byte) {
	entity, ok := s.resolveMerchantEntity(activeClient, cmd)
	if !ok || !entity.Merchant.Capabilities.Repair {
		return
	}
	makeIndex := merchantMakeIndex(cmd)
	itemName := strings.TrimSpace(DecodeString(text))
	slot, entry, ok := merchantBagItemSlotByMakeIndex(*activeChar, makeIndex, itemName, s.world)
	if !ok {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMUserRepairItemFail, Recog: cmd.Recog}, nil)
		return
	}
	item, _ := s.world.Item(entry.ItemID)
	special := merchantIsSpecialRepair(activeClient)
	price := merchantRepairPrice(item, entry, special, s.world.Gameplay().Castle.SuperRepairPriceRate)
	if price <= 0 || activeChar.Gold < price || entry.DuraMax == 0 || entry.Dura >= entry.DuraMax {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMUserRepairItemFail, Recog: cmd.Recog}, nil)
		return
	}
	updated := *activeChar
	updated.Gold -= price
	if special {
		updated.BagItems[slot].Dura = updated.BagItems[slot].DuraMax
	} else {
		missing := int(entry.DuraMax - entry.Dura)
		dec := missing / 30
		if dec > 0 {
			if dec >= int(entry.DuraMax) {
				dec = int(entry.DuraMax) - 1
			}
			updated.BagItems[slot].DuraMax -= uint16(dec)
		}
		updated.BagItems[slot].Dura = updated.BagItems[slot].DuraMax
	}
	*activeChar = updated
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMUserRepairItemOK, Recog: int32(updated.Gold), Param: updated.BagItems[slot].Dura, Tag: updated.BagItems[slot].DuraMax}, nil)
	s.sendGoldChanged(conn, updated.Gold)
	if err := s.store.SaveCharacter(updated); err != nil {
	}
}

func (s *Server) handleUserStorageItem(conn net.Conn, activeChar *storage.Character, activeClient *Client, cmd mir176.Command, text []byte) {
	entity, ok := s.resolveMerchantEntity(activeClient, cmd)
	if !ok {
		return
	}
	if !entity.Merchant.Capabilities.Storage {
		return
	}
	makeIndex := merchantMakeIndex(cmd)
	itemName := strings.TrimSpace(DecodeString(text))
	slot, entry, ok := merchantBagItemSlotByMakeIndex(*activeChar, makeIndex, itemName, s.world)
	if !ok {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMStorageFail}, nil)
		return
	}
	if len(activeChar.StorageItems) >= 39 {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMStorageFull}, nil)
		return
	}
	updated := *activeChar
	updated.StorageItems = append(updated.StorageItems, entry)
	updated.BagItems = append(updated.BagItems[:slot], updated.BagItems[slot+1:]...)
	*activeChar = updated
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMStorageOK}, nil)
	s.sendWeightChanged(conn, s.world.AbilityStats(updated))
	if err := s.store.SaveCharacter(updated); err != nil {
	}
}

func (s *Server) handleUserTakeBackStorageItem(conn net.Conn, activeChar *storage.Character, activeClient *Client, cmd mir176.Command, text []byte) {
	entity, ok := s.resolveMerchantEntity(activeClient, cmd)
	if !ok {
		return
	}
	if !entity.Merchant.Capabilities.GetBack {
		return
	}
	makeIndex := merchantMakeIndex(cmd)
	itemName := strings.TrimSpace(DecodeString(text))
	slot, entry, ok := merchantStorageItemSlotByMakeIndex(*activeChar, makeIndex, itemName, s.world)
	if !ok {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMTakeBackStorageItemFail}, nil)
		return
	}
	if !s.world.CanCarryBagItems(*activeChar, 1) {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMTakeBackStorageItemFullBag}, nil)
		return
	}
	updated := *activeChar
	updated.BagItems = append(updated.BagItems, entry)
	updated.StorageItems = append(updated.StorageItems[:slot], updated.StorageItems[slot+1:]...)
	*activeChar = updated
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMTakeBackStorageItemOK, Recog: int32(entry.MakeIndex)}, nil)
	s.sendBagAddItem(conn, updated, entry.ItemID, entry.MakeIndex)
	s.sendWeightChanged(conn, s.world.AbilityStats(updated))
	if err := s.store.SaveCharacter(updated); err != nil {
	}
}

func (s *Server) handleUserMakeDrugItem(conn net.Conn, activeChar *storage.Character, activeClient *Client, cmd mir176.Command, text []byte) {
	entity, ok := s.resolveMerchantEntity(activeClient, cmd)
	if !ok {
		return
	}
	itemName := strings.TrimSpace(DecodeString(text))
	if itemName == "" {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMMakeDrugFail, Recog: 1}, nil)
		return
	}
	if !merchantStockHasItemName(s.world, s.world.MerchantStock(entity.ID), itemName) {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMMakeDrugFail, Recog: 1}, nil)
		return
	}
	if activeChar.Gold < makeDrugPrice {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMMakeDrugFail, Recog: 3}, nil)
		return
	}
	updated, removed, err := s.world.ConsumeMakeIngredients(*activeChar, itemName)
	if err != nil {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMMakeDrugFail, Recog: 4}, nil)
		return
	}
	item, ok := s.world.Item(itemName)
	if !ok {
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMMakeDrugFail, Recog: 1}, nil)
		return
	}
	if !s.world.CanCarryBagItems(updated, 1) {
		*activeChar = updated
		s.sendDelItemList(conn, removed)
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMMakeDrugFail, Recog: 2}, nil)
		if err := s.store.SaveCharacter(updated); err != nil {
		}
		return
	}
	updated.Gold -= makeDrugPrice
	bought := storage.UserItem{ItemID: item.ID, MakeIndex: int32(time.Now().UnixNano() & 0x7fffffff)}
	updated.BagItems = append(updated.BagItems, bought)
	*activeChar = updated
	s.sendDelItemList(conn, removed)
	s.sendBagAddItem(conn, updated, bought.ItemID, bought.MakeIndex)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMMakeDrugSuccess, Recog: int32(updated.Gold)}, nil)
	if err := s.store.SaveCharacter(updated); err != nil {
	}
}

func (s *Server) resolveMerchantEntity(activeClient *Client, cmd mir176.Command) (npc.Entity, bool) {
	entity, ok := s.world.NPCByActorID(cmd.Recog)
	if ok {
		return entity, true
	}
	if activeClient == nil {
		return npc.Entity{}, false
	}
	activeClient.mu.Lock()
	defer activeClient.mu.Unlock()
	if activeClient.activeNPCID == "" {
		return npc.Entity{}, false
	}
	return s.world.NPCByID(activeClient.activeNPCID)
}

func bagHasItemID(ch storage.Character, itemID string) bool {
	return bagHasItemCount(ch, itemID, 1)
}

func bagHasItemCount(ch storage.Character, itemID string, count int) bool {
	if count <= 0 {
		return true
	}
	have := 0
	for _, entry := range ch.BagItems {
		if strings.EqualFold(entry.ItemID, itemID) {
			have++
			if have >= count {
				return true
			}
		}
	}
	return false
}

func removeBagItemsByID(ch storage.Character, itemID string, count int) (storage.Character, []storage.UserItem, bool) {
	if count <= 0 {
		return ch, nil, true
	}
	if !bagHasItemCount(ch, itemID, count) {
		return ch, nil, false
	}
	updated := ch
	removed := make([]storage.UserItem, 0, count)
	for i := len(updated.BagItems) - 1; i >= 0 && len(removed) < count; i-- {
		if !strings.EqualFold(updated.BagItems[i].ItemID, itemID) {
			continue
		}
		removed = append(removed, updated.BagItems[i])
		updated.BagItems = append(updated.BagItems[:i], updated.BagItems[i+1:]...)
	}
	return updated, removed, len(removed) == count
}

func merchantMakeIndex(cmd mir176.Command) int32 {
	return int32(uint32(cmd.Param) | uint32(cmd.Tag)<<16)
}

func merchantBagItemSlotByMakeIndex(ch storage.Character, makeIndex int32, itemName string, w *world.World) (int, storage.UserItem, bool) {
	for i := len(ch.BagItems) - 1; i >= 0; i-- {
		entry := ch.BagItems[i]
		if entry.MakeIndex != makeIndex {
			continue
		}
		if itemName != "" {
			item, ok := w.Item(entry.ItemID)
			if !ok || !strings.EqualFold(item.Name, itemName) {
				continue
			}
		}
		return i, entry, true
	}
	return -1, storage.UserItem{}, false
}

func merchantBagItemByMakeIndex(ch storage.Character, makeIndex int32, itemName string, w *world.World) (storage.UserItem, data.StdItem, bool) {
	_, entry, ok := merchantBagItemSlotByMakeIndex(ch, makeIndex, itemName, w)
	if !ok {
		return storage.UserItem{}, data.StdItem{}, false
	}
	item, ok := w.Item(entry.ItemID)
	if !ok {
		return storage.UserItem{}, data.StdItem{}, false
	}
	return entry, item, true
}

func merchantStorageItemSlotByMakeIndex(ch storage.Character, makeIndex int32, itemName string, w *world.World) (int, storage.UserItem, bool) {
	for i := len(ch.StorageItems) - 1; i >= 0; i-- {
		entry := ch.StorageItems[i]
		if entry.MakeIndex != makeIndex {
			continue
		}
		if itemName != "" {
			item, ok := w.Item(entry.ItemID)
			if !ok || !strings.EqualFold(item.Name, itemName) {
				continue
			}
		}
		return i, entry, true
	}
	return -1, storage.UserItem{}, false
}

func merchantStockItemByMakeIndexOrName(w *world.World, entity npc.Entity, makeIndex int32, itemName string) (storage.UserItem, data.StdItem, bool) {
	stocks := w.MerchantStock(entity.ID)
	if makeIndex > 0 {
		for _, stock := range stocks {
			if stock.MakeIndex != makeIndex {
				continue
			}
			if itemName != "" {
				item, ok := w.Item(stock.ItemID)
				if !ok || !strings.EqualFold(item.Name, itemName) {
					continue
				}
				return stock, item, true
			}
			item, ok := w.Item(stock.ItemID)
			if !ok {
				return storage.UserItem{}, data.StdItem{}, false
			}
			return stock, item, true
		}
	}
	for _, stock := range stocks {
		item, ok := w.Item(stock.ItemID)
		if !ok {
			continue
		}
		if strings.EqualFold(item.Name, itemName) {
			return stock, item, true
		}
	}
	return storage.UserItem{}, data.StdItem{}, false
}

func merchantStockHasItemName(w *world.World, stocks []storage.UserItem, itemName string) bool {
	for _, stock := range stocks {
		item, ok := w.Item(stock.ItemID)
		if !ok {
			continue
		}
		if strings.EqualFold(item.Name, itemName) {
			return true
		}
	}
	return false
}

func (s *Server) sendDelItemList(conn net.Conn, removed []storage.UserItem) {
	if len(removed) == 0 {
		return
	}
	var body strings.Builder
	for _, item := range removed {
		fmt.Fprintf(&body, "%s/%d/", item.ItemID, item.MakeIndex)
	}
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMDelItems, Series: uint16(len(removed))}, EncodeString(body.String()))
}

func merchantSellPrice(item data.StdItem, entry storage.UserItem, rate int) int {
	base := merchantUserItemPrice(item, entry)
	if base <= 0 {
		return 0
	}
	return int(math.Round(float64(merchantPriceValue(base, rate)) / 2.0))
}

func merchantRepairPrice(item data.StdItem, entry storage.UserItem, special bool, superRate int) int {
	if item.Price <= 0 || entry.DuraMax == 0 || entry.Dura >= entry.DuraMax {
		return 0
	}
	lost := int(entry.DuraMax) - int(entry.Dura)
	base := int(item.Price) / 3 * lost / int(entry.DuraMax)
	if base < 1 {
		base = 1
	}
	if special {
		if superRate <= 0 {
			superRate = 1
		}
		return base * superRate
	}
	return base
}

func merchantPriceValue(base, rate int) int {
	if base <= 0 {
		return 0
	}
	if rate <= 0 {
		rate = 100
	}
	return int(math.Round(float64(base) * float64(rate) / 100.0))
}

func merchantUserItemPrice(item data.StdItem, entry storage.UserItem) int {
	price := float64(item.Price)
	if price <= 0 {
		return 0
	}
	if item.StdMode > 4 && item.DuraMax > 0 && entry.DuraMax > 0 {
		switch item.StdMode {
		case 40:
			if entry.Dura <= entry.DuraMax {
				price = math.Max(2, math.Round(price-price/2.0/float64(entry.DuraMax)*float64(entry.DuraMax-entry.Dura)))
			} else {
				price = price + math.Round(price/float64(entry.DuraMax)*2.0*float64(entry.DuraMax-entry.Dura))
			}
		case 43:
			userDuraMax := float64(entry.DuraMax)
			if userDuraMax < 10000 {
				userDuraMax = 10000
			}
			if float64(entry.Dura) <= userDuraMax {
				missing := userDuraMax - float64(entry.Dura)
				price = math.Max(2, math.Round(price-price/2.0/userDuraMax*missing))
			} else {
				excess := float64(entry.Dura) - userDuraMax
				price = price + math.Round(price/userDuraMax*1.3*excess)
			}
		}
		if item.StdMode > 4 {
			n14 := 0
			for i := 0; i < 8; i++ {
				if item.StdMode == 5 || item.StdMode == 6 {
					if i == 6 {
						if entry.Desc[i] > 10 {
							n14 += int(entry.Desc[i]-10) * 2
						}
					} else {
						n14 += int(entry.Desc[i])
					}
				} else {
					n14 += int(entry.Desc[i])
				}
			}
			if n14 > 0 {
				price = price / 5.0 * float64(n14)
			}
			price = math.Round(price / float64(item.DuraMax) * float64(entry.DuraMax))
			price = math.Max(2, math.Round(price-price/2.0/float64(entry.DuraMax)*float64(entry.DuraMax-entry.Dura)))
		}
	}
	return int(math.Round(price))
}

func merchantIsSpecialRepair(activeClient *Client) bool {
	if activeClient == nil {
		return false
	}
	activeClient.mu.Lock()
	defer activeClient.mu.Unlock()
	return strings.EqualFold(activeClient.merchantCurrentLabel, "@s_repair")
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
		s.sendCommand(conn, mir176.Command{Ident: mir176.SMTakeOffFail}, nil)
		return
	}
	*activeChar = updated
	world.ApplyUnequipSync(itemUseSyncAdapter{s: s, conn: conn}, result, mir176.SMTakeOffOK)
}

// handleDropItem implements CM_DROPITEM.
func (s *Server) handleDropItem(conn net.Conn, activeChar *storage.Character, cmd mir176.Command, text []byte) {
	itemID := DecodeString(text)
	updated, drop, err := s.world.DropItemCountByBagIndex(*activeChar, int(cmd.Recog), itemID, s.PlayerCharacters()...)
	if err != nil {
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

// handleMagicKeyChange implements CM_MAGICKEYCHANGE.
func (s *Server) handleMagicKeyChange(conn net.Conn, activeChar *storage.Character, cmd mir176.Command) {
	skillID, ok := s.world.SkillIDByMagicID(uint16(cmd.Recog))
	if !ok {
		return
	}
	updated, changed, err := s.world.SetSkillHotkey(*activeChar, skillID, byte(cmd.Param))
	if err != nil {
		return
	}
	if !changed {
		return
	}
	*activeChar = updated
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

func (s *Server) sendMagicLevelExp(conn net.Conn, magicID uint16, level byte, train int) {
	s.sendCommand(conn, mir176.Command{
		Ident:  mir176.SMMagicLvExp,
		Recog:  int32(magicID),
		Param:  uint16(level),
		Tag:    uint16(train),
		Series: uint16(uint32(train) >> 16),
	}, nil)
}

func (s *Server) sendInitialLoginState(conn net.Conn, ch storage.Character) {
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMAbility, Recog: int32(ch.Gold), Param: makeWord(byte(world.Plain6ClassID(ch.Class)), 99), Tag: uint16(ch.PremiumGold), Series: uint16(uint32(ch.PremiumGold) >> 16)}, EncodeBuffer(Ability(s.world.AbilityStats(ch))))
	s.sendCommand(conn, SubAbilityCommand(ch.Class), nil)
	s.sendCommand(conn, DayChangingCommand(0, 0), nil)
	s.sendEquippedItems(conn, ch)
	s.sendBagItems(conn, ch)
	s.sendUseMagic(conn, ch)
	s.sendAttackSkillFlags(conn, ch)
	if client := s.clientForConn(conn); client != nil {
		for _, event := range s.world.GroundEventsAround(ch.MapID, ch.X, ch.Y, playerViewRange, time.Now()) {
			s.broadcastGroundShowToClient(client, event)
		}
	}
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
	s.sendRawFrame(conn, ack)
}

func (s *Server) sendActionFail(conn net.Conn) {
	ack := fmt.Sprintf("+FAIL/%d", uint32(time.Now().UnixMilli()))
	s.sendRawFrame(conn, ack)
}

func (s *Server) armFireHit(conn net.Conn, ch storage.Character) bool {
	client := s.clientForConn(conn)
	if client == nil {
		return false
	}
	client.mu.Lock()
	now := time.Now()
	age := time.Duration(0)
	if !client.fireHitLatestAt.IsZero() {
		age = now.Sub(client.fireHitLatestAt)
	}
	if !client.fireHitLatestAt.IsZero() && age <= fireHitRearmCooldown {
		client.mu.Unlock()
		s.sendSystemMessageStyle(conn, ch, "召唤烈火精灵失败...", 0xFF, 0x38)
		return false
	}
	client.fireHitArmed = true
	client.fireHitLatestAt = now
	client.mu.Unlock()
	s.sendSystemMessageStyle(conn, ch, "召唤烈火精灵成功...", 0xDB, 0xFF)
	return true
}

func (s *Server) dispatchSpellMagicFire(event spellMagicFireEvent) {
	s.dispatchSpellMagicFireExcept(event, nil)
}

func (s *Server) dispatchSpellMagicFireExcept(event spellMagicFireEvent, except net.Conn) {
	for _, client := range s.spellRefClients(event.caster) {
		if except != nil && client.conn == except {
			continue
		}
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageMagicFire, magicFire: event})
	}
}

func (s *Server) dispatchSpellSpaceMoveFire(caster storage.Character) {
	s.dispatchSpellSpaceMoveFireExcept(caster, nil)
}

func (s *Server) dispatchSpellSpaceMoveFireExcept(caster storage.Character, except net.Conn) {
	for _, client := range s.spellRefClients(caster) {
		if except != nil && client.conn == except {
			continue
		}
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageSpaceMoveFire, caster: caster})
	}
}

func (s *Server) dispatchSpellSpaceMoveShow(ch storage.Character) {
	s.dispatchSpellSpaceMoveShowExcept(ch, nil)
}

func (s *Server) dispatchSpellSpaceMoveShowExcept(ch storage.Character, except net.Conn) {
	for _, client := range s.spellRefClients(ch) {
		if except != nil && client.conn == except {
			continue
		}
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageSpaceMoveShow, spaceMove: ch})
	}
}

func (s *Server) dispatchSpellSpaceMoveMapChange(ch storage.Character) {
	s.invalidateSpellRef(ch.ID)
	if client, ok := s.ClientByCharacterID(ch.ID); ok {
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageSpaceMoveMapChange, spaceMove: ch})
	}
}

func (s *Server) sendSpellSpaceMoveMapChange(conn net.Conn, ch storage.Character) {
	s.updateClient(conn, ch)
	s.invalidateSpellRef(ch.ID)
	if client, ok := s.ClientByCharacterID(ch.ID); ok {
		client.mu.Lock()
		client.visibleMonsters = map[string]world.Monster{}
		client.visibleDrops = map[string]world.GroundDrop{}
		client.visibleNPCs = map[string]npc.Entity{}
		client.mu.Unlock()
	}
	actorID := world.CharacterActorID(ch)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMClearObjects, Recog: actorID}, nil)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMChangeMap, Recog: actorID, Param: uint16(ch.X), Tag: uint16(ch.Y), Series: uint16(s.world.MapLight(ch.MapID))}, EncodeString(ch.MapID))
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMAreaState, Recog: s.world.CharacterAreaState(ch)}, nil)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMMapDescription, Recog: -1}, EncodeString(s.world.MapName(ch.MapID)))
	if ch.SoftVersionDate != 0 {
		s.sendCommand(conn, ServerConfigCommand(), EncodeBuffer(ServerConfigBody()))
	}
}

func (s *Server) sendSpellSpaceMoveShow(conn net.Conn, ch storage.Character) {
	body := EncodeBuffer(CharDesc(s.world.HumanFeatureForCharacter(ch), s.world.CharacterStatus(ch)))
	s.sendCommand(conn, mir176.Command{
		Ident:  mir176.SMSpacemoveShow2,
		Recog:  world.CharacterActorID(ch),
		Param:  uint16(ch.X),
		Tag:    uint16(ch.Y),
		Series: uint16(makeWord(byte(ch.Dir), byte(s.world.MapLight(ch.MapID)))),
	}, body)
}

func (s *Server) handleSpellMagicFire(conn net.Conn, event spellMagicFireEvent) {
	body := make([]byte, 4)
	binary.LittleEndian.PutUint32(body, uint32(event.targetID))
	body = EncodeBuffer(body)
	cmd := mir176.Command{
		Ident:  mir176.SMMagicFire,
		Recog:  world.CharacterActorID(event.caster),
		Param:  uint16(event.targetX),
		Tag:    uint16(event.targetY),
		Series: uint16(event.skill.EffectType) | uint16(event.skill.Effect)<<8,
	}
	s.sendCommand(conn, cmd, body)
}

// sendMoveFail resyncs the client to the server's authoritative position
// and facing after a rejected action, mirroring the reference server's
// unconditional (self included) SM_MOVEFAIL reply
// (PlayObject.Message.cs RM_MOVEFAIL case).
func (s *Server) sendMoveFail(conn net.Conn, activeChar *storage.Character) {
	feature := s.world.HumanFeatureForCharacter(*activeChar)
	status := s.world.CharacterStatus(*activeChar)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMMoveFail, Recog: world.CharacterActorID(*activeChar), Param: uint16(activeChar.X), Tag: uint16(activeChar.Y), Series: uint16(activeChar.Dir)}, EncodeBuffer(CharDesc(feature, status)))
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
	if updated, changed, err := s.world.SyncCharacterHomeFromStartPoint(ch); err == nil {
		if changed {
			ch = updated
		}
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
		client.visibleNPCs = map[string]npc.Entity{}
	}
	s.clientMu.Unlock()
	actorID := world.CharacterActorID(ch)
	feature := s.world.HumanFeatureForCharacter(ch)
	light := s.world.MapLight(ch.MapID)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMNewMap, Recog: actorID, Param: uint16(ch.X), Tag: uint16(ch.Y)}, EncodeString(ch.MapID))
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMChangeLight, Recog: actorID, Param: uint16(light), Tag: 500}, nil)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMLogon, Recog: actorID, Param: uint16(ch.X), Tag: uint16(ch.Y), Series: makeWord(byte(ch.Dir), byte(light))}, EncodeBuffer(LogonBody(feature, s.world.CharacterStatus(ch), ch.AllowGroup, s.world.CharacterFeatureEx(ch))))
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMFeatureChanged, Recog: actorID, Param: uint16(feature), Tag: uint16(uint32(feature) >> 16), Series: uint16(s.world.CharacterFeatureEx(ch))}, nil)
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
		s.sendCommand(conn, MonsterTurnCommand(mon, s.world.MapLight(mon.MapID)), MonsterTurnBody(mon))
		s.sendCommand(conn, MonsterFeatureCommand(mon), nil)
	}
	_, drops := s.world.SnapshotAround(ch.MapID, ch.X, ch.Y, playerViewRange)
	for _, drop := range drops {
		s.sendDropShow(conn, drop)
	}
	s.sendNPCsAround(conn, ch)
}

func (s *Server) sendSpaceMoveState(conn net.Conn, ch storage.Character) {
	versionDate := ch.SoftVersionDate
	s.clientMu.Lock()
	if client := s.clients[conn]; client != nil {
		client.visibleMonsters = map[string]world.Monster{}
		client.visibleDrops = map[string]world.GroundDrop{}
		client.visibleNPCs = map[string]npc.Entity{}
	}
	s.clientMu.Unlock()
	actorID := world.CharacterActorID(ch)
	showIdent := uint16(mir176.SMSpacemoveShow)
	showBody := EncodeBuffer(CharDesc(s.world.HumanFeatureForCharacter(ch), s.world.CharacterStatus(ch)))
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMSpacemoveHide, Recog: actorID}, nil)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMClearObjects, Recog: actorID}, nil)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMChangeMap, Recog: actorID, Param: uint16(ch.X), Tag: uint16(ch.Y), Series: uint16(s.world.MapLight(ch.MapID))}, EncodeString(ch.MapID))
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMAreaState, Recog: s.world.CharacterAreaState(ch)}, nil)
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMMapDescription, Recog: -1}, EncodeString(s.world.MapName(ch.MapID)))
	if versionDate != 0 {
		s.sendCommand(conn, ServerConfigCommand(), EncodeBuffer(ServerConfigBody()))
	}
	s.sendCommand(conn, mir176.Command{Ident: showIdent, Recog: actorID, Param: uint16(ch.X), Tag: uint16(ch.Y), Series: makeWord(byte(ch.Dir), byte(s.world.MapLight(ch.MapID)))}, showBody)
	s.sendNPCsAround(conn, ch)
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

func (s *Server) sendOpenHealth(conn net.Conn, ch storage.Character) {
	s.sendCommand(conn, mir176.Command{
		Ident: mir176.SMOpenHealth,
		Recog: world.CharacterActorID(ch),
		Param: uint16(ch.HP),
		Tag:   uint16(ch.MaxHP),
	}, nil)
}

func (s *Server) sendCloseHealth(conn net.Conn, ch storage.Character) {
	s.sendCommand(conn, mir176.Command{
		Ident: mir176.SMCloseHealth,
		Recog: world.CharacterActorID(ch),
	}, nil)
}

func (s *Server) sendInstanceHealGauge(conn net.Conn, ch storage.Character) {
	s.sendCommand(conn, mir176.Command{
		Ident: mir176.SMInstanceHealGauge,
		Recog: world.CharacterActorID(ch),
		Param: uint16(ch.HP),
		Tag:   uint16(ch.MaxHP),
	}, nil)
}

func (s *Server) sendInstanceHealGaugeMonster(conn net.Conn, mon world.Monster) {
	s.sendCommand(conn, mir176.Command{
		Ident: mir176.SMInstanceHealGauge,
		Recog: world.MonsterActorID(mon),
		Param: uint16(mon.HP),
		Tag:   uint16(mon.MaxHP),
	}, nil)
}

func (s *Server) broadcastCharacterOpenHealth(ch storage.Character) {
	clients := s.spellRefClients(ch)
	if len(clients) == 0 {
		return
	}
	for _, client := range clients {
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageOpenHealth, health: ch})
	}
}

func (s *Server) broadcastCharacterCloseHealth(ch storage.Character) {
	clients := s.spellRefClients(ch)
	if len(clients) == 0 {
		return
	}
	for _, client := range clients {
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageCloseHealth, health: ch})
	}
}

func (c *Client) sendOpenHealthMonster(s *Server, mon world.Monster) {
	c.writeCommand(s, mir176.Command{
		Ident: mir176.SMOpenHealth,
		Recog: world.MonsterActorID(mon),
		Param: uint16(mon.HP),
		Tag:   uint16(mon.MaxHP),
	}, nil)
}

func (c *Client) sendCloseHealthMonster(s *Server, mon world.Monster) {
	c.writeCommand(s, mir176.Command{
		Ident: mir176.SMCloseHealth,
		Recog: world.MonsterActorID(mon),
	}, nil)
}

func (s *Server) broadcastMonsterOpenHealth(mon world.Monster) {
	for _, client := range s.spellRefClientsFor("monster:"+mon.ID, mon.MapID, mon.X, mon.Y) {
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageOpenHealth, healthMonster: mon})
	}
}

func (s *Server) broadcastMonsterCloseHealth(mon world.Monster) {
	for _, client := range s.spellRefClientsFor("monster:"+mon.ID, mon.MapID, mon.X, mon.Y) {
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageCloseHealth, healthMonster: mon})
	}
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

func (s *Server) dispatchSpellSummon(mon world.Monster) {
	if mon.Hidden {
		return
	}
	for _, client := range s.spellRefClientsFor("monster:"+mon.ID, mon.MapID, mon.X, mon.Y) {
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageSummon, summoned: mon})
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

func (s *Server) dispatchSpellTeleport(conn net.Conn, from, to storage.Character) {
	seen := map[*Client]struct{}{}
	if from.MapID != "" {
		for _, client := range s.ClientsAroundExcept(from.MapID, from.X, from.Y, playerViewRange, conn) {
			seen[client] = struct{}{}
			client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageTeleport, teleportFrom: from, teleportTo: to})
		}
	}
	for _, client := range s.ClientsAroundExcept(to.MapID, to.X, to.Y, playerViewRange, conn) {
		if _, ok := seen[client]; ok {
			continue
		}
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageTeleport, teleportFrom: from, teleportTo: to})
	}
}

func (c *Client) sendTeleportMove(s *Server, from, to storage.Character) {
	if from.MapID != "" {
		c.sendCharacterDisappear(s, from)
	}
	c.sendCharacterAppear(s, to)
}

func (s *Server) broadcastCharacterDisappear(clients []*Client, ch storage.Character) {
	for _, client := range clients {
		client.sendCharacterDisappear(s, ch)
	}
}

func (c *Client) sendCharacterDisappear(s *Server, ch storage.Character) {
	c.writeCommand(s, mir176.Command{Ident: mir176.SMDisappear, Recog: world.CharacterActorID(ch)}, nil)
}

func (s *Server) broadcastCharacterAppear(clients []*Client, ch storage.Character) {
	for _, client := range clients {
		client.sendCharacterAppear(s, ch)
	}
}

func (c *Client) sendCharacterAppear(s *Server, ch storage.Character) {
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
	c.writeCommand(s, logonCmd, logonBody)
	c.writeCommand(s, featureCmd, nil)
	c.writeCommand(s, nameCmd, nameBody)
}

func (s *Server) sendNPCsAround(conn net.Conn, ch storage.Character) {
	npcs := s.world.NPCsInMap(ch.MapID)
	client := s.clientForConn(conn)
	if client == nil {
		return
	}
	current := map[string]struct{}{}
	for _, entity := range npcs {
		if absInt(entity.X-ch.X) > playerViewRange || absInt(entity.Y-ch.Y) > playerViewRange {
			continue
		}
		current[entity.ID] = struct{}{}
		client.ensureNPCVisible(s, entity)
	}
	client.forgetMissingNPCs(s, current)
}

func (s *Server) applyWorldTick(result world.TickResult, now time.Time) {
	s.expireFireHitStates(now)
	for _, id := range result.GroundEventHides {
		for _, client := range s.allClients() {
			client.mu.Lock()
			_, visible := client.visibleEvents[id]
			delete(client.visibleEvents, id)
			client.mu.Unlock()
			if visible {
				client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageGroundHide, groundHideID: id})
			}
		}
	}
	s.syncGroundEvents(result.GroundEvents)
	hitIDs := map[string]struct{}{}
	magicHitIDs := map[string]struct{}{}
	for _, hit := range result.CharacterHits {
		if hit.Character.ID != "" {
			hitIDs[hit.Character.ID] = struct{}{}
			if hit.Magic {
				magicHitIDs[hit.Character.ID] = struct{}{}
			}
		}
	}
	stateRefreshIDs := map[string]struct{}{}
	for _, ch := range result.StateRefreshCharacters {
		if ch.ID != "" {
			stateRefreshIDs[ch.ID] = struct{}{}
		}
	}
	statusRefreshIDs := map[string]struct{}{}
	for _, ch := range result.StatusRefreshCharacters {
		if ch.ID != "" {
			statusRefreshIDs[ch.ID] = struct{}{}
		}
	}
	orderedCharacterStatusIDs := map[string]struct{}{}
	orderedMonsterStatusIDs := map[string]struct{}{}
	orderedCharacterHitIDs := map[string]struct{}{}
	orderedMonsterHitIDs := map[string]struct{}{}
	orderedMonsterDeathIDs := map[string]struct{}{}
	orderedCharacterOpenIDs := map[string]struct{}{}
	orderedMonsterOpenIDs := map[string]struct{}{}
	orderedPoisonNotification := false
	orderedStatusEventIDs := map[string]struct{}{}
	for _, event := range result.OrderedStatusRefreshes {
		if event.Character != nil {
			orderedCharacterStatusIDs[event.Character.ID] = struct{}{}
		}
		if event.Monster != nil {
			orderedMonsterStatusIDs[event.Monster.ID] = struct{}{}
		}
	}
	for _, event := range result.OrderedSpellEvents {
		switch event.Kind {
		case world.OrderedSpellEventCharacterStatus:
			if event.Character.ID != "" {
				orderedStatusEventIDs["character:"+event.Character.ID] = struct{}{}
			}
		case world.OrderedSpellEventMonsterStatus:
			if event.Monster.ID != "" {
				orderedStatusEventIDs["monster:"+event.Monster.ID] = struct{}{}
			}
		case world.OrderedSpellEventCharacterHit:
			if event.CharacterHit.Character.ID != "" {
				orderedCharacterHitIDs[event.CharacterHit.Character.ID] = struct{}{}
			}
		case world.OrderedSpellEventMonsterHit:
			if event.MonsterHit.MonsterID != "" {
				orderedMonsterHitIDs[event.MonsterHit.MonsterID] = struct{}{}
				if event.MonsterHit.Dead {
					orderedMonsterDeathIDs[event.MonsterHit.MonsterID] = struct{}{}
				}
			}
		case world.OrderedSpellEventCharacterOpenHealth:
			if event.Character.ID != "" {
				orderedCharacterOpenIDs[event.Character.ID] = struct{}{}
			}
		case world.OrderedSpellEventMonsterOpenHealth:
			if event.Monster.ID != "" {
				orderedMonsterOpenIDs[event.Monster.ID] = struct{}{}
			}
		case world.OrderedSpellEventPoisonNotification:
			orderedPoisonNotification = true
		}
	}
	s.applyOrderedSpellEvents(result.OrderedSpellEvents)
	abilityRefreshIDs := map[string]struct{}{}
	for _, ch := range result.AbilityRefreshCharacters {
		if ch.ID != "" {
			abilityRefreshIDs[ch.ID] = struct{}{}
		}
	}
	healingIDs := map[string]struct{}{}
	for _, id := range result.HealingCharacters {
		healingIDs[id] = struct{}{}
	}
	showHPOpenIDs := map[string]struct{}{}
	for _, ch := range result.ShowHPOpenedCharacters {
		if ch.ID != "" {
			showHPOpenIDs[ch.ID] = struct{}{}
		}
	}
	showHPExpiredIDs := map[string]struct{}{}
	for _, ch := range result.ShowHPExpiredCharacters {
		if ch.ID != "" {
			showHPExpiredIDs[ch.ID] = struct{}{}
		}
	}
	for _, ch := range result.Characters {
		previous, hadPrevious := s.ClientByCharacterID(ch.ID)
		previousCharacter := storage.Character{}
		if hadPrevious {
			previousCharacter = previous.character()
		}
		s.updateClientByCharacterID(ch)
		healthChanged := hadPrevious && (previousCharacter.HP != ch.HP || previousCharacter.MP != ch.MP)
		if healthChanged {
			if _, magicHit := magicHitIDs[ch.ID]; magicHit {
				healthChanged = false
			} else {
				s.broadcastCharacterHealthSpellChanged(ch)
			}
		}
		stateRefresh := false
		statusRefresh := false
		abilityRefresh := false
		if _, ok := statusRefreshIDs[ch.ID]; ok {
			if _, ordered := orderedCharacterStatusIDs[ch.ID]; !ordered {
				s.broadcastCharacterStatusChanged(ch)
			}
			statusRefresh = true
		}
		if _, ok := stateRefreshIDs[ch.ID]; ok {
			s.broadcastCharacterStateRefresh(ch)
			stateRefresh = true
		}
		if _, refresh := abilityRefreshIDs[ch.ID]; refresh {
			abilityRefresh = true
			if client, ok := s.ClientByCharacterID(ch.ID); ok {
				client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageAbility, ability: ch})
			}
		}
		if _, ok := hitIDs[ch.ID]; ok {
			continue
		}
		if client, ok := s.ClientByCharacterID(ch.ID); ok {
			if _, healing := healingIDs[ch.ID]; healing && !healthChanged {
				continue
			}
			if _, opening := showHPOpenIDs[ch.ID]; opening {
				continue
			}
			if _, closing := showHPExpiredIDs[ch.ID]; closing {
				continue
			}
			if stateRefresh || statusRefresh || abilityRefresh {
				continue
			}
			client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageHealth, health: ch})
		}
	}
	for _, event := range result.OrderedStatusRefreshes {
		if event.Character != nil {
			if _, ordered := orderedStatusEventIDs["character:"+event.Character.ID]; ordered {
				continue
			}
		}
		if event.Monster != nil {
			if _, ordered := orderedStatusEventIDs["monster:"+event.Monster.ID]; ordered {
				continue
			}
		}
		if event.Character != nil {
			s.broadcastCharacterStatusChanged(*event.Character)
		} else if event.Monster != nil {
			s.broadcastMonsterStatusChanged(*event.Monster)
		}
	}
	for _, mon := range result.StatusRefreshMonsters {
		if _, ordered := orderedMonsterStatusIDs[mon.ID]; ordered {
			continue
		}
		s.broadcastMonsterStatusChanged(mon)
	}
	for _, ch := range result.ShowHPOpenedCharacters {
		if _, ordered := orderedCharacterOpenIDs[ch.ID]; ordered {
			continue
		}
		s.broadcastCharacterOpenHealth(ch)
	}
	for _, ch := range result.ShowHPExpiredCharacters {
		s.broadcastCharacterCloseHealth(ch)
	}
	for _, mon := range result.ShowHPOpenedMonsters {
		if _, ordered := orderedMonsterOpenIDs[mon.ID]; ordered {
			continue
		}
		s.broadcastMonsterOpenHealth(mon)
	}
	for _, mon := range result.ShowHPExpiredMonsters {
		s.broadcastMonsterCloseHealth(mon)
	}
	for _, notice := range result.PoisonNotifications {
		if orderedPoisonNotification {
			continue
		}
		if client, ok := s.ClientByCharacterID(notice.Character.ID); ok {
			s.sendSystemMessage(client.conn, notice.Character, fmt.Sprintf("你中毒了[时间:%d秒，点数:%d点].", notice.Seconds, notice.Points))
		}
	}
	for _, action := range result.MonsterActions {
		clients := s.spellRefClientsFor("monster:"+action.MonsterID, action.MapID, action.X, action.Y)
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
		case world.MonsterActionPush:
			s.broadcastMonsterPush(clients, action)
		}
	}
	for _, hit := range result.MonsterHits {
		if _, ordered := orderedMonsterHitIDs[hit.MonsterID]; ordered {
			continue
		}
		clients := s.spellRefClientsFor("monster:"+hit.MonsterID, hit.Character.MapID, hit.MonsterX, hit.MonsterY)
		if !hit.Magic {
			clients = s.ClientsAround(hit.Character.MapID, hit.MonsterX, hit.MonsterY, playerViewRange)
		}
		if len(clients) > 0 {
			s.broadcastHitImpact(clients, hit)
		}
	}
	for _, exp := range result.SpellExperience {
		client, ok := s.ClientByCharacterID(exp.CharacterID)
		if !ok {
			continue
		}
		s.sendWinExp(client.conn, exp.Experience, exp.CurrentExp)
		if exp.LevelUp {
			s.sendLevelUp(client.conn, exp.Character)
		}
	}
	for _, death := range result.MonsterDeaths {
		if _, ordered := orderedMonsterDeathIDs[death.MonsterID]; ordered {
			continue
		}
		clients := s.ClientsAround(death.MonsterMapID, death.MonsterX, death.MonsterY, playerViewRange)
		if len(clients) > 0 {
			s.broadcastMonsterDeath(clients, death)
			if len(death.Drops) > 0 {
				s.broadcastDropAppear(clients, death.Drops)
			}
		}
	}
	for _, hit := range result.CharacterHits {
		if _, ordered := orderedCharacterHitIDs[hit.Character.ID]; ordered {
			continue
		}
		clients := s.spellRefClients(hit.Character)
		if !hit.Magic {
			clients = s.clientsForCharacterHit(hit.Character)
		}
		if len(clients) > 0 {
			if hit.Magic {
				caster := storage.Character{ID: hit.AttackerID}
				if client, ok := s.ClientByCharacterID(hit.AttackerID); ok {
					caster = client.character()
				}
				for _, client := range clients {
					client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageSpellHit, caster: caster, spellHit: hit})
				}
			} else {
				s.broadcastCharacterStruck(clients, hit)
			}
		}
	}
	for _, mon := range result.AffectedMonsters {
		s.broadcastMonsterFeatureRefresh(mon)
	}
	for _, mon := range result.NameColorMonsters {
		s.broadcastMonsterNameColor(mon)
	}
	for _, mon := range result.NameMonsters {
		s.broadcastMonsterUsername(mon)
	}
	s.syncVisibleMonsters()
	s.syncVisibleDrops()
	s.syncVisibleNPCs()
}

func (s *Server) applyOrderedSpellEvents(events []world.OrderedSpellEvent) {
	for _, event := range events {
		switch event.Kind {
		case world.OrderedSpellEventCharacterStatus:
			s.broadcastCharacterStatusChanged(event.Character)
		case world.OrderedSpellEventMonsterStatus:
			s.broadcastMonsterStatusChanged(event.Monster)
		case world.OrderedSpellEventCharacterOpenHealth:
			s.broadcastCharacterOpenHealth(event.Character)
		case world.OrderedSpellEventMonsterOpenHealth:
			s.broadcastMonsterOpenHealth(event.Monster)
		case world.OrderedSpellEventPoisonNotification:
			notice := event.PoisonNotification
			if client, ok := s.ClientByCharacterID(notice.Character.ID); ok {
				s.sendSystemMessage(client.conn, notice.Character, fmt.Sprintf("你中毒了[时间:%d秒，点数:%d点].", notice.Seconds, notice.Points))
			}
		case world.OrderedSpellEventCharacterHit:
			hit := event.CharacterHit
			if hit.FeatureChanged {
				s.updateClientByCharacterID(hit.Character)
				if client, ok := s.ClientByCharacterID(hit.Character.ID); ok {
					client.sendCharacterStateRefresh(s, hit.Character)
				}
				s.broadcastCharacterStateRefreshExcept(hit.Character, hit.Character.ID)
			}
			for _, durability := range hit.Durability {
				if client, ok := s.ClientByCharacterID(hit.Character.ID); ok {
					s.sendCommand(client.conn, mir176.Command{Ident: mir176.SMDuraChange, Recog: int32(durability.Dura), Param: uint16(durability.Slot), Tag: uint16(durability.DuraMax), Series: uint16(uint32(durability.DuraMax) >> 16)}, nil)
				}
			}
			clients := s.spellRefClients(hit.Character)
			caster := storage.Character{ID: hit.AttackerID}
			if client, ok := s.ClientByCharacterID(hit.AttackerID); ok {
				caster = client.character()
			}
			for _, client := range clients {
				client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageSpellHit, caster: caster, spellHit: hit})
			}
		case world.OrderedSpellEventMonsterHit:
			hit := event.MonsterHit
			clients := s.spellRefClientsFor("monster:"+hit.MonsterID, hit.Character.MapID, hit.MonsterX, hit.MonsterY)
			if len(clients) > 0 {
				s.broadcastHitImpact(clients, hit)
			}
		}
	}
}

func (s *Server) expireFireHitStates(now time.Time) {
	s.clientMu.Lock()
	clients := make([]*Client, 0, len(s.clients))
	for _, client := range s.clients {
		clients = append(clients, client)
	}
	s.clientMu.Unlock()
	for _, client := range clients {
		client.mu.Lock()
		if !client.fireHitArmed || client.fireHitLatestAt.IsZero() || now.Sub(client.fireHitLatestAt) <= fireHitExpireDelay {
			client.mu.Unlock()
			continue
		}
		client.fireHitArmed = false
		client.mu.Unlock()
		s.sendRawFrame(client.conn, "+UFIR")
	}
}

const (
	fireHitRearmCooldown = 10 * time.Second
	fireHitExpireDelay   = 20 * time.Second
)

func (s *Server) syncVisibleNPCs() {
	for _, client := range s.allClients() {
		ch := client.character()
		npcs := s.world.NPCsInMap(ch.MapID)
		current := map[string]struct{}{}
		for _, entity := range npcs {
			if absInt(entity.X-ch.X) > playerViewRange || absInt(entity.Y-ch.Y) > playerViewRange {
				continue
			}
			current[entity.ID] = struct{}{}
			client.ensureNPCVisible(s, entity)
		}
		client.forgetMissingNPCs(s, current)
	}
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
	mon := world.Monster{ID: action.MonsterID, Name: action.Name, RaceImg: action.RaceImg, MonsterWeapon: action.MonsterWeapon, Appr: action.Appr, MapID: action.MapID, X: action.X, Y: action.Y, Dir: action.Dir}
	for _, client := range clients {
		client.ensureMonsterVisibleWithStatus(s, mon, action.Status)
		client.writeCommand(s, MonsterWalkCommand(action, s.world.MapLight(action.MapID)), MonsterWalkBodyWithStatus(mon, action.Status))
	}
}

func (s *Server) broadcastMonsterPush(clients []*Client, action world.MonsterAction) {
	for _, client := range clients {
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageMonsterPush, monsterPush: action})
	}
}

func (s *Server) broadcastCharacterPush(push world.CharacterPush) {
	s.broadcastCharacterPushExcept(push, nil)
}

func (s *Server) broadcastCharacterPushExcept(push world.CharacterPush, except net.Conn) {
	s.updateClientByCharacterID(push.Character)
	clients := s.spellRefClients(push.Character)
	for _, client := range clients {
		if except != nil && client.conn == except {
			continue
		}
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageCharacterPush, characterPush: push})
	}
}

func (s *Server) broadcastGroundShow(event world.SpellGroundEvent) {
	clients := s.ClientsAround(event.MapID, event.X, event.Y, playerViewRange)
	for _, client := range clients {
		client.mu.Lock()
		if client.visibleEvents == nil {
			client.visibleEvents = map[int32]world.SpellGroundEvent{}
		}
		client.visibleEvents[event.ID] = event
		client.mu.Unlock()
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageGroundShow, ground: event})
	}
}

func (s *Server) syncGroundEvents(events []world.SpellGroundEvent) {
	active := make(map[int32]world.SpellGroundEvent, len(events))
	for _, event := range events {
		active[event.ID] = event
	}
	for _, client := range s.allClients() {
		ch := client.character()
		hides := make([]int32, 0)
		client.mu.Lock()
		for id, event := range client.visibleEvents {
			if _, ok := active[id]; !ok || event.MapID != ch.MapID || absInt(event.X-ch.X) > playerViewRange || absInt(event.Y-ch.Y) > playerViewRange {
				delete(client.visibleEvents, id)
				hides = append(hides, id)
			}
		}
		client.mu.Unlock()
		for _, id := range hides {
			client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageGroundHide, groundHideID: id})
		}
	}
	for _, event := range events {
		for _, client := range s.ClientsAround(event.MapID, event.X, event.Y, playerViewRange) {
			client.mu.Lock()
			_, visible := client.visibleEvents[event.ID]
			client.mu.Unlock()
			if !visible {
				s.broadcastGroundShowToClient(client, event)
			}
		}
	}
}

func (s *Server) broadcastGroundShowToClient(client *Client, event world.SpellGroundEvent) {
	client.mu.Lock()
	if client.visibleEvents == nil {
		client.visibleEvents = map[int32]world.SpellGroundEvent{}
	}
	client.visibleEvents[event.ID] = event
	client.mu.Unlock()
	client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageGroundShow, ground: event})
}

func (s *Server) sendCharacterPush(conn net.Conn, push world.CharacterPush) {
	ch := push.Character
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMBackStep, Recog: world.CharacterActorID(ch), Param: uint16(ch.X), Tag: uint16(ch.Y), Series: uint16(makeWord(byte(push.Dir), byte(s.world.MapLight(ch.MapID))))}, EncodeBuffer(CharDesc(s.world.HumanFeatureForCharacter(ch), s.world.CharacterStatus(ch))))
}

func (s *Server) sendMonsterPush(conn net.Conn, action world.MonsterAction) {
	mon := world.Monster{ID: action.MonsterID, Name: action.Name, RaceImg: action.RaceImg, MonsterWeapon: action.MonsterWeapon, Appr: action.Appr, MapID: action.MapID, X: action.X, Y: action.Y, Dir: action.Dir}
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMBackStep, Recog: world.MonsterActorID(mon), Param: uint16(action.X), Tag: uint16(action.Y), Series: uint16(makeWord(byte(action.Dir), byte(s.world.MapLight(action.MapID))))}, EncodeBuffer(CharDesc(world.MonsterFeature(mon), action.Status)))
}

func (s *Server) sendGroundShow(conn net.Conn, event world.SpellGroundEvent) {
	body := make([]byte, 4)
	binary.LittleEndian.PutUint16(body, uint16(event.Param))
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMShowEvent, Recog: event.ID, Param: uint16(event.Type), Tag: uint16(event.X), Series: uint16(event.Y)}, EncodeBuffer(body))
}

func (s *Server) broadcastMonsterHit(clients []*Client, action world.MonsterAction) {
	for _, client := range clients {
		mon := world.Monster{ID: action.MonsterID, Name: action.Name, RaceImg: action.RaceImg, MonsterWeapon: action.MonsterWeapon, Appr: action.Appr, MapID: action.MapID, X: action.X, Y: action.Y}
		client.ensureMonsterVisibleWithStatus(s, mon, action.Status)
		client.writeCommand(s, MonsterHitCommand(action), nil)
	}
}

func (s *Server) broadcastMonsterTurn(clients []*Client, action world.MonsterAction) {
	mon := world.Monster{ID: action.MonsterID, Name: action.Name, RaceImg: action.RaceImg, MonsterWeapon: action.MonsterWeapon, Appr: action.Appr, MapID: action.MapID, X: action.X, Y: action.Y, Dir: action.Dir}
	for _, client := range clients {
		client.mu.Lock()
		if client.visibleMonsters == nil {
			client.visibleMonsters = map[string]world.Monster{}
		}
		client.visibleMonsters[mon.ID] = mon
		client.mu.Unlock()
		client.writeCommand(s, MonsterTurnCommand(mon, s.world.MapLight(mon.MapID)), MonsterTurnBodyWithStatus(mon, action.Status))
		client.writeCommand(s, MonsterFeatureCommand(mon), nil)
	}
}

func (s *Server) broadcastMonsterReveal(clients []*Client, action world.MonsterAction) {
	mon := world.Monster{ID: action.MonsterID, Name: action.Name, RaceImg: action.RaceImg, MonsterWeapon: action.MonsterWeapon, Appr: action.Appr, MapID: action.MapID, X: action.X, Y: action.Y, Dir: action.Dir}
	for _, client := range clients {
		client.writeCommand(s, MonsterDigUpCommand(mon, s.world.MapLight(mon.MapID)), MonsterDigUpBodyWithStatus(mon, action.Status))
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

func (s *Server) dispatchSpellMonsterAction(action world.MonsterAction) {
	s.dispatchSpellMonsterActionExcept(action, nil)
}

func (s *Server) dispatchSpellMonsterActionExcept(action world.MonsterAction, except net.Conn) {
	for _, client := range s.spellRefClientsFor("monster:"+action.MonsterID, action.MapID, action.X, action.Y) {
		if except != nil && client.conn == except {
			continue
		}
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageMonsterAction, monsterAction: action})
	}
}

func (c *Client) sendMonsterAction(s *Server, action world.MonsterAction) {
	mon := world.Monster{ID: action.MonsterID, Name: action.Name, RaceImg: action.RaceImg, MonsterWeapon: action.MonsterWeapon, Appr: action.Appr, MapID: action.MapID, X: action.X, Y: action.Y, Dir: action.Dir}
	switch action.Kind {
	case world.MonsterActionWalk:
		c.ensureMonsterVisibleWithStatus(s, mon, action.Status)
		c.writeCommand(s, MonsterWalkCommand(action, s.world.MapLight(action.MapID)), MonsterWalkBodyWithStatus(mon, action.Status))
	case world.MonsterActionHit:
		c.ensureMonsterVisibleWithStatus(s, mon, action.Status)
		c.writeCommand(s, MonsterHitCommand(action), nil)
	case world.MonsterActionTurn:
		c.mu.Lock()
		if c.visibleMonsters == nil {
			c.visibleMonsters = map[string]world.Monster{}
		}
		c.visibleMonsters[mon.ID] = mon
		c.mu.Unlock()
		c.writeCommand(s, MonsterTurnCommand(mon, s.world.MapLight(mon.MapID)), MonsterTurnBodyWithStatus(mon, action.Status))
		c.writeCommand(s, MonsterFeatureCommand(mon), nil)
	case world.MonsterActionReveal:
		c.writeCommand(s, MonsterDigUpCommand(mon, s.world.MapLight(mon.MapID)), MonsterDigUpBodyWithStatus(mon, action.Status))
		c.mu.Lock()
		if c.visibleMonsters == nil {
			c.visibleMonsters = map[string]world.Monster{}
		}
		c.visibleMonsters[mon.ID] = mon
		c.mu.Unlock()
	case world.MonsterActionHide:
		c.writeCommand(s, MonsterDigDownCommand(mon), nil)
		c.forgetMonster(mon.ID)
	case world.MonsterActionPush:
		c.ensureMonsterVisible(s, mon)
		s.sendMonsterPush(c.conn, action)
	}
}

func (s *Server) broadcastCharacterHit(clients []*Client, ch storage.Character, clientIdent uint16) {
	cmd := CharacterHitCommand(ch, clientIdent)
	for _, client := range clients {
		client.writeCommand(s, cmd, nil)
	}
}

func (s *Server) dispatchSpellStart(event spellStartEvent) {
	clients := s.spellRefClients(event.caster)
	if len(clients) == 0 {
		return
	}
	for _, client := range clients {
		if client.character().ID == event.caster.ID {
			continue
		}
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageStart, start: event})
	}
}

func (c *Client) enqueueSpellMessage(s *Server, message spellObjectMessage) {
	if c.spellQueueCond != nil {
		c.mu.Lock()
		c.spellQueue = append(c.spellQueue, message)
		c.spellQueueCond.Signal()
		c.mu.Unlock()
		return
	}
	if c.spellMessages == nil || c.spellMessagesDone == nil {
		c.handleQueuedSpellMessage(s, message)
		return
	}
	select {
	case c.spellMessages <- message:
	case <-c.spellMessagesDone:
	}
}

func (c *Client) runSpellMessages(s *Server) {
	if c.spellQueueCond != nil {
		for {
			c.mu.Lock()
			for len(c.spellQueue) == 0 {
				select {
				case <-c.spellMessagesDone:
					c.mu.Unlock()
					return
				default:
				}
				c.spellQueueCond.Wait()
			}
			message := c.spellQueue[0]
			c.spellQueue[0] = spellObjectMessage{}
			c.spellQueue = c.spellQueue[1:]
			c.mu.Unlock()
			c.handleQueuedSpellMessage(s, message)
		}
	}
	for {
		select {
		case message := <-c.spellMessages:
			c.handleQueuedSpellMessage(s, message)
		case <-c.spellMessagesDone:
			return
		}
	}
}

func (c *Client) runOutput() {
	for {
		select {
		case response := <-c.output:
			_, _ = c.conn.Write(response)
		case <-c.spellMessagesDone:
			return
		}
	}
}

func (c *Client) enqueueOutput(response []byte) {
	if c.output == nil || c.spellMessagesDone == nil {
		_, _ = c.conn.Write(response)
		return
	}
	select {
	case c.output <- response:
	case <-c.spellMessagesDone:
	}
}

func (c *Client) handleQueuedSpellMessage(s *Server, message spellObjectMessage) {
	switch message.kind {
	case spellObjectMessageStart:
		c.handleSpellStart(s, message.start)
	case spellObjectMessageMagicFire:
		s.handleSpellMagicFire(c.conn, message.magicFire)
	case spellObjectMessageMagicFireFail:
		s.sendCommand(c.conn, mir176.Command{Ident: mir176.SMMagicFireFail, Recog: world.CharacterActorID(message.magicFireFail)}, nil)
	case spellObjectMessageSpaceMoveFire:
		c.writeCommand(s, mir176.Command{Ident: mir176.SMSpacemoveHide2, Recog: world.CharacterActorID(message.caster)}, nil)
	case spellObjectMessageSpaceMoveMapChange:
		ch := message.spaceMove
		c.mu.Lock()
		c.visibleMonsters = map[string]world.Monster{}
		c.visibleDrops = map[string]world.GroundDrop{}
		c.visibleNPCs = map[string]npc.Entity{}
		c.mu.Unlock()
		actorID := world.CharacterActorID(ch)
		c.writeCommand(s, mir176.Command{Ident: mir176.SMClearObjects, Recog: actorID}, nil)
		c.writeCommand(s, mir176.Command{Ident: mir176.SMChangeMap, Recog: actorID, Param: uint16(ch.X), Tag: uint16(ch.Y), Series: uint16(s.world.MapLight(ch.MapID))}, EncodeString(ch.MapID))
	case spellObjectMessageSpaceMoveShow:
		ch := message.spaceMove
		body := EncodeBuffer(CharDesc(s.world.HumanFeatureForCharacter(ch), s.world.CharacterStatus(ch)))
		c.writeCommand(s, mir176.Command{
			Ident:  mir176.SMSpacemoveShow2,
			Recog:  world.CharacterActorID(ch),
			Param:  uint16(ch.X),
			Tag:    uint16(ch.Y),
			Series: uint16(makeWord(byte(ch.Dir), byte(s.world.MapLight(ch.MapID)))),
		}, body)
	case spellObjectMessageState:
		c.sendCharacterStateRefresh(s, message.state)
	case spellObjectMessageMonsterState:
		c.sendMonsterFeatureRefresh(s, message.monsterState)
	case spellObjectMessageMonsterStatus:
		c.sendMonsterStatusChanged(s, message.monsterStatus)
	case spellObjectMessageMonsterNameColor:
		c.sendMonsterNameColor(s, message.monsterNameColor)
	case spellObjectMessageMonsterUsername:
		c.sendMonsterUsername(s, message.monsterUsername)
	case spellObjectMessageTeleport:
		c.sendTeleportMove(s, message.teleportFrom, message.teleportTo)
	case spellObjectMessageMonsterAction:
		c.sendMonsterAction(s, message.monsterAction)
	case spellObjectMessageSummon:
		if !message.summoned.Hidden {
			c.ensureMonsterVisible(s, message.summoned)
		}
	case spellObjectMessageHealth:
		s.sendHealthSpellChanged(c.conn, world.CharacterActorID(message.health), s.world.AbilityStats(message.health))
	case spellObjectMessageAbility:
		s.sendAbilityOnly(c.conn, message.ability)
	case spellObjectMessageDurability:
		c.writeCommand(s, mir176.Command{Ident: mir176.SMDuraChange, Recog: int32(message.durability.Dura), Param: uint16(message.durability.Slot), Tag: uint16(message.durability.DuraMax), Series: uint16(uint32(message.durability.DuraMax) >> 16)}, nil)
	case spellObjectMessageExperience:
		s.sendWinExp(c.conn, message.experience, message.currentExp)
	case spellObjectMessageLevelUp:
		s.sendLevelUp(c.conn, message.levelUp)
	case spellObjectMessageSkillExp:
		s.sendMagicLevelExp(c.conn, message.skillExp.magicID, message.skillExp.skillLevel, message.skillExp.skillTrain)
	case spellObjectMessageStatus:
		s.handleCharacterStatusChanged(c.conn, message.status)
	case spellObjectMessageOpenHealth:
		if message.healthMonster.ID != "" {
			c.sendOpenHealthMonster(s, message.healthMonster)
		} else {
			s.sendOpenHealth(c.conn, message.health)
		}
	case spellObjectMessageCloseHealth:
		if message.healthMonster.ID != "" {
			c.sendCloseHealthMonster(s, message.healthMonster)
		} else {
			s.sendCloseHealth(c.conn, message.health)
		}
	case spellObjectMessageHealGauge:
		if message.gaugeMonster.ID != "" {
			s.sendInstanceHealGaugeMonster(c.conn, message.gaugeMonster)
		} else {
			s.sendInstanceHealGauge(c.conn, message.gauge)
		}
	case spellObjectMessageRush:
		s.sendSpellRush(c.conn, message.rush)
	case spellObjectMessageSpellHit:
		s.sendCharacterSpellStruck([]*Client{c}, message.caster, message.spellHit)
	case spellObjectMessageMonsterHealth:
		c.sendMonsterHealthSpellChanged(s, message.monsterHit)
	case spellObjectMessageMonsterHit:
		s.broadcastMonsterStruck([]*Client{c}, message.monsterHit)
	case spellObjectMessageMonsterDeath:
		s.broadcastMonsterDeath([]*Client{c}, message.monsterHit)
		for _, drop := range message.drops {
			c.ensureDropVisible(s, drop)
		}
	case spellObjectMessageCharacterPush:
		s.sendCharacterPush(c.conn, message.characterPush)
	case spellObjectMessageMonsterPush:
		c.ensureMonsterVisible(s, world.Monster{ID: message.monsterPush.MonsterID, Name: message.monsterPush.Name, RaceImg: message.monsterPush.RaceImg, MonsterWeapon: message.monsterPush.MonsterWeapon, Appr: message.monsterPush.Appr, MapID: message.monsterPush.MapID, X: message.monsterPush.X, Y: message.monsterPush.Y, Dir: message.monsterPush.Dir})
		s.sendMonsterPush(c.conn, message.monsterPush)
	case spellObjectMessageGroundShow:
		s.sendGroundShow(c.conn, message.ground)
	case spellObjectMessageGroundHide:
		s.sendCommand(c.conn, mir176.Command{Ident: mir176.SMHideEvent, Recog: message.groundHideID}, nil)
	}
}

func (c *Client) handleSpellStart(s *Server, event spellStartEvent) {
	if c.character().ID == event.caster.ID {
		return
	}
	body := []byte(strconv.Itoa(int(event.magicID)))
	cmd := mir176.Command{
		Ident:  mir176.SMSpell,
		Recog:  world.CharacterActorID(event.caster),
		Param:  uint16(event.targetX),
		Tag:    uint16(event.targetY),
		Series: uint16(event.effect),
	}
	c.writeCommand(s, cmd, body)
}

func (s *Server) broadcastCharacterStateRefresh(ch storage.Character) {
	clients := s.spellRefClients(ch)
	if len(clients) == 0 {
		return
	}
	for _, client := range clients {
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageState, state: ch})
	}
}

func (s *Server) broadcastCharacterStateRefreshExcept(ch storage.Character, exceptID string) {
	for _, client := range s.spellRefClients(ch) {
		if client.character().ID == exceptID {
			continue
		}
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageState, state: ch})
	}
}

func (c *Client) sendCharacterStateRefresh(s *Server, ch storage.Character) {
	actorID := world.CharacterActorID(ch)
	feature := s.world.HumanFeatureForCharacter(ch)
	cmd := mir176.Command{
		Ident:  mir176.SMFeatureChanged,
		Recog:  actorID,
		Param:  uint16(feature),
		Tag:    uint16(uint32(feature) >> 16),
		Series: uint16(s.world.CharacterFeatureEx(ch)),
	}
	c.writeCommand(s, cmd, nil)
}

func (s *Server) broadcastCharacterStatusChanged(ch storage.Character) {
	s.broadcastCharacterStatusChangedExcept(ch, "")
}

func (s *Server) broadcastCharacterStatusChangedExcept(ch storage.Character, exceptID string) {
	clients := s.spellRefClients(ch)
	for _, client := range clients {
		if exceptID != "" && client.character().ID == exceptID {
			continue
		}
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageStatus, status: ch})
	}
}

func (s *Server) broadcastCharacterHealthSpellChanged(ch storage.Character) {
	if ch.ShowHPUntil <= 0 {
		return
	}
	for _, client := range s.spellRefClients(ch) {
		if client.character().ID == ch.ID {
			continue
		}
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageHealth, health: ch})
	}
}

func (s *Server) handleCharacterStatusChanged(conn net.Conn, ch storage.Character) {
	status := s.world.CharacterStatus(ch)
	hitSpeed := s.world.CharacterHitSpeed(ch)
	s.sendCommand(conn, mir176.Command{
		Ident:  mir176.SMCharStatusChanged,
		Recog:  world.CharacterActorID(ch),
		Param:  uint16(status),
		Tag:    uint16(uint32(status) >> 16),
		Series: uint16(hitSpeed),
	}, nil)
}

func (s *Server) broadcastMonsterFeatureRefresh(mon world.Monster) {
	clients := s.spellRefClientsFor("monster:"+mon.ID, mon.MapID, mon.X, mon.Y)
	if len(clients) == 0 {
		return
	}
	for _, client := range clients {
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageMonsterState, monsterState: mon})
	}
}

func (s *Server) broadcastMonsterStatusChanged(mon world.Monster) {
	for _, client := range s.spellRefClientsFor("monster:"+mon.ID, mon.MapID, mon.X, mon.Y) {
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageMonsterStatus, monsterStatus: mon})
	}
}

func (s *Server) broadcastMonsterNameColor(mon world.Monster) {
	for _, client := range s.spellRefClientsFor("monster:"+mon.ID, mon.MapID, mon.X, mon.Y) {
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageMonsterNameColor, monsterNameColor: mon})
	}
}

func (s *Server) broadcastMonsterUsername(mon world.Monster) {
	for _, client := range s.spellRefClientsFor("monster:"+mon.ID, mon.MapID, mon.X, mon.Y) {
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageMonsterUsername, monsterUsername: mon})
	}
}

func (c *Client) sendMonsterFeatureRefresh(s *Server, mon world.Monster) {
	cmd := MonsterFeatureCommand(mon)
	c.writeCommand(s, cmd, nil)
	c.mu.Lock()
	if c.visibleMonsters == nil {
		c.visibleMonsters = map[string]world.Monster{}
	}
	c.visibleMonsters[mon.ID] = mon
	c.mu.Unlock()
}

func (c *Client) sendMonsterStatusChanged(s *Server, mon world.Monster) {
	status := world.MonsterStatus(mon, time.Now())
	c.writeCommand(s, mir176.Command{
		Ident:  mir176.SMCharStatusChanged,
		Recog:  world.MonsterActorID(mon),
		Param:  uint16(status),
		Tag:    uint16(uint32(status) >> 16),
		Series: 0,
	}, nil)
	c.mu.Lock()
	if c.visibleMonsters == nil {
		c.visibleMonsters = map[string]world.Monster{}
	}
	c.visibleMonsters[mon.ID] = mon
	c.mu.Unlock()
}

func (c *Client) sendMonsterNameColor(s *Server, mon world.Monster) {
	c.writeCommand(s, mir176.Command{
		Ident: mir176.SMChangeNameColor,
		Recog: world.MonsterActorID(mon),
		Param: world.MonsterNameColor(mon),
	}, nil)
}

func (c *Client) sendMonsterUsername(s *Server, mon world.Monster) {
	c.writeCommand(s, mir176.Command{
		Ident: mir176.SMUserName,
		Recog: world.MonsterActorID(mon),
		Param: world.MonsterNameColor(mon),
	}, EncodeString(world.MonsterDisplayName(mon)))
}

func (s *Server) broadcastHitImpact(clients []*Client, result world.AttackResult) {
	if result.Magic {
		for _, client := range clients {
			if result.MonsterHealthChanged {
				client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageMonsterHealth, monsterHit: result})
			}
			client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageMonsterHit, monsterHit: result})
			if result.Dead {
				client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageMonsterDeath, monsterHit: result, drops: result.Drops})
			}
		}
		return
	}
	if result.Dead {
		s.broadcastMonsterStruck(clients, result)
		s.broadcastMonsterDeath(clients, result)
		if len(result.Drops) > 0 {
			s.broadcastDropAppear(clients, result.Drops)
		}
		return
	}
	delay := s.hitImpactDelay
	if result.ImpactDelay > 0 {
		delay = result.ImpactDelay
	}
	if delay <= 0 {
		s.broadcastMonsterStruck(clients, result)
		return
	}
	time.AfterFunc(delay, func() {
		s.broadcastMonsterStruck(clients, result)
	})
}

func (c *Client) sendMonsterHealthSpellChanged(s *Server, result world.AttackResult) {
	c.writeCommand(s, mir176.Command{
		Ident:  mir176.SMHealthSpellChanged,
		Recog:  world.MonsterActorID(world.Monster{ID: result.MonsterID}),
		Param:  uint16(result.MonsterHP),
		Tag:    uint16(result.MonsterMP),
		Series: uint16(result.MonsterMaxHP),
	}, nil)
}

func (s *Server) broadcastCharacterStruck(clients []*Client, hit world.CharacterHit) {
	delay := s.hitImpactDelay
	if hit.ImpactDelay > 0 {
		delay = hit.ImpactDelay
	}
	if delay <= 0 {
		s.sendCharacterStruck(clients, hit)
		return
	}
	time.AfterFunc(delay, func() {
		s.sendCharacterStruck(clients, hit)
	})
}

func (s *Server) sendCharacterStruck(clients []*Client, hit world.CharacterHit) {
	attackerID := world.MonsterActorID(world.Monster{ID: hit.AttackerID})
	if attacker, ok := s.ClientByCharacterID(hit.AttackerID); ok {
		attackerID = world.CharacterActorID(attacker.character())
	}
	for _, client := range clients {
		if client.character().ID == hit.Character.ID {
			client.mu.Lock()
			client.struckAt = time.Now()
			client.mu.Unlock()
		}
		client.writeCommand(s, CharacterStruckCommand(hit), EncodeBuffer(MessageBodyWL(s.world.HumanFeatureForCharacter(hit.Character), s.world.CharacterStatus(hit.Character), attackerID, 0)))
		if hit.Dead {
			client.writeCommand(s, CharacterDeathCommand(hit.Character), EncodeBuffer(CharDesc(s.world.HumanFeatureForCharacter(hit.Character), s.world.CharacterStatus(hit.Character))))
		}
	}
}

func (s *Server) sendCharacterSpellStruck(clients []*Client, caster storage.Character, hit world.CharacterHit) {
	for _, client := range clients {
		isTarget := client.character().ID == hit.Character.ID
		if isTarget {
			client.mu.Lock()
			client.struckAt = time.Now()
			client.mu.Unlock()
		}
		if isTarget || hit.Character.ShowHPUntil > 0 {
			stats := s.world.AbilityStats(hit.Character)
			client.writeCommand(s, mir176.Command{
				Ident:  mir176.SMHealthSpellChanged,
				Recog:  world.CharacterActorID(hit.Character),
				Param:  uint16(hit.Character.HP),
				Tag:    uint16(hit.Character.MP),
				Series: uint16(stats.MaxHP),
			}, nil)
		}
		client.writeCommand(s, CharacterStruckCommand(hit), EncodeBuffer(MessageBodyWL(s.world.HumanFeatureForCharacter(hit.Character), s.world.CharacterStatus(hit.Character), world.CharacterActorID(caster), 1)))
		if hit.Dead {
			client.writeCommand(s, CharacterDeathCommand(hit.Character), EncodeBuffer(CharDesc(s.world.HumanFeatureForCharacter(hit.Character), s.world.CharacterStatus(hit.Character))))
		}
	}
}

func (s *Server) broadcastMonsterStruck(clients []*Client, result world.AttackResult) {
	attackerID := int32(ActorID)
	if result.Character.ID != "" {
		attackerID = world.CharacterActorID(result.Character)
	}
	for _, client := range clients {
		feature := world.MonsterFeature(world.Monster{RaceImg: result.MonsterRaceImg, MonsterWeapon: result.MonsterWeapon, Appr: result.MonsterAppr})
		magic := int32(0)
		if result.Magic {
			magic = 1
		}
		client.writeCommand(s, MonsterStruckCommand(result), EncodeBuffer(MessageBodyWL(feature, result.MonsterStatus, attackerID, magic)))
	}
}

func (s *Server) broadcastMonsterDeath(clients []*Client, result world.AttackResult) {
	for _, client := range clients {
		feature := world.MonsterFeature(world.Monster{RaceImg: result.MonsterRaceImg, MonsterWeapon: result.MonsterWeapon, Appr: result.MonsterAppr})
		client.writeCommand(s, MonsterDeathCommand(result), EncodeBuffer(CharDesc(feature, result.MonsterStatus)))
		client.forgetMonster(result.MonsterID)
	}
}

func (s *Server) registerClient(conn net.Conn, ch storage.Character) *Client {
	if ch.ObjectOrder == 0 {
		ch = s.world.RegisterCharacter(ch)
	}
	versionDate := ch.SoftVersionDate
	client := &Client{
		conn:              conn,
		ch:                ch,
		softVersionDate:   versionDate,
		visibleMonsters:   map[string]world.Monster{},
		visibleDrops:      map[string]world.GroundDrop{},
		visibleNPCs:       map[string]npc.Entity{},
		visibleEvents:     map[int32]world.SpellGroundEvent{},
		spellMessagesDone: make(chan struct{}),
		output:            make(chan []byte, clientOutputQueueSize),
	}
	client.spellQueueCond = sync.NewCond(&client.mu)
	if state, _, ok := ch.Skills.Get("攻杀剑术"); ok && state.Level <= 3 {
		client.powerHitCount = 7 - int(state.Level)
		if client.powerHitCount < 1 {
			client.powerHitCount = 1
		}
		client.powerHitPointCount = rand.Intn(client.powerHitCount)
	}
	monsters, _ := s.world.SnapshotAround(ch.MapID, ch.X, ch.Y, playerViewRange)
	for _, mon := range monsters {
		client.visibleMonsters[mon.ID] = mon
	}
	s.clientMu.Lock()
	s.clients[conn] = client
	s.clientMu.Unlock()
	go client.runSpellMessages(s)
	go client.runOutput()
	return client
}

func (s *Server) clientForConn(conn net.Conn) *Client {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	return s.clients[conn]
}

func (s *Server) unregisterClient(conn net.Conn) {
	s.clientMu.Lock()
	client := s.clients[conn]
	delete(s.clients, conn)
	s.clientMu.Unlock()
	if client != nil {
		if client.spellQueueCond != nil {
			client.mu.Lock()
			close(client.spellMessagesDone)
			client.spellQueueCond.Broadcast()
			client.mu.Unlock()
		} else {
			close(client.spellMessagesDone)
		}
		s.handleClientDisconnect(client.character())
	}
}

func (s *Server) updateClient(conn net.Conn, ch storage.Character) {
	s.clientMu.Lock()
	client := s.clients[conn]
	if client != nil {
		client.mu.Lock()
		client.ch = ch
		if client.active != nil {
			*client.active = ch
		}
		client.mu.Unlock()
	}
	s.clientMu.Unlock()
}

func (s *Server) updateClientByCharacterID(ch storage.Character) {
	s.clientMu.Lock()
	for _, client := range s.clients {
		client.mu.Lock()
		if client.ch.ID != ch.ID {
			client.mu.Unlock()
			continue
		}
		client.ch = ch
		if client.active != nil {
			*client.active = ch
		}
		client.mu.Unlock()
	}
	s.clientMu.Unlock()
}

func (s *Server) ClientByCharacterID(id string) (*Client, bool) {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	for _, client := range s.clients {
		client.mu.Lock()
		if client.ch.ID == id {
			client.mu.Unlock()
			return client, true
		}
		client.mu.Unlock()
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
	sort.SliceStable(chars, func(i, j int) bool {
		if chars[i].ObjectOrder == 0 || chars[j].ObjectOrder == 0 {
			return false
		}
		return chars[i].ObjectOrder < chars[j].ObjectOrder
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

func (s *Server) spellRefClients(caster storage.Character) []*Client {
	return s.spellRefClientsFor(caster.ID, caster.MapID, caster.X, caster.Y)
}

func (s *Server) invalidateSpellRef(ownerID string) {
	s.spellRefMu.Lock()
	delete(s.spellRefs, ownerID)
	s.spellRefMu.Unlock()
}

func (s *Server) spellRefClientsFor(ownerID, mapID string, x, y int) []*Client {
	now := time.Now()
	s.spellRefMu.Lock()
	snapshot, ok := s.spellRefs[ownerID]
	if !ok || now.Sub(snapshot.at) >= 500*time.Millisecond || snapshot.mapID != mapID {
		snapshot = spellRefSnapshot{at: now, mapID: mapID, x: x, y: y, clients: map[string]struct{}{}}
		for _, client := range s.ClientsAround(mapID, x, y, playerViewRange) {
			snapshot.clients[client.ch.ID] = struct{}{}
		}
		s.spellRefs[ownerID] = snapshot
		s.spellRefMu.Unlock()
		return s.clientsByIDs(snapshot.clients, mapID, x, y, playerViewRange, false)
	}
	s.spellRefMu.Unlock()
	return s.clientsByIDs(snapshot.clients, mapID, x, y, 10, true)
}

func (s *Server) clientsByIDs(ids map[string]struct{}, mapID string, x, y, viewRange int, strict bool) []*Client {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	clients := make([]*Client, 0, len(ids))
	for _, client := range s.clients {
		if _, ok := ids[client.ch.ID]; !ok || client.ch.MapID != mapID {
			continue
		}
		if strict {
			if absInt(client.ch.X-x) >= viewRange+1 || absInt(client.ch.Y-y) >= viewRange+1 {
				continue
			}
		} else if !world.CanObserveAt(client.ch, x, y, viewRange) {
			continue
		}
		clients = append(clients, client)
	}
	sortClientsByID(clients)
	return clients
}

func (s *Server) clientsForCharacterHit(ch storage.Character) []*Client {
	clients := s.ClientsAround(ch.MapID, ch.X, ch.Y, playerViewRange)
	target, ok := s.ClientByCharacterID(ch.ID)
	if !ok {
		return clients
	}
	for _, client := range clients {
		if client == target {
			return clients
		}
	}
	return append(clients, target)
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

func (s *Server) sendSystemMessage(conn net.Conn, ch storage.Character, msg string) {
	s.sendSystemMessageStyle(conn, ch, msg, 0xFF, 0x38)
}

func (s *Server) sendSystemMessageStyle(conn net.Conn, ch storage.Character, msg string, foreground, background byte) {
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMSystemMessage, Recog: world.CharacterActorID(ch), Param: makeWord(foreground, background), Series: 1}, EncodeString(msg))
}

func (s *Server) sendMerchantSay(conn net.Conn, npcName, msg string) {
	if msg == "" {
		return
	}
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMMerchantSay, Series: 1}, EncodeString(npcName+"/"+msg))
}

func (s *Server) sendMerchantDlgClose(conn net.Conn, merchantID int32) {
	s.sendCommand(conn, mir176.Command{Ident: mir176.SMMerchantDlgClose, Recog: merchantID}, nil)
}

func (s *Server) sendNPCConversation(conn net.Conn, conversation npc.Conversation) {
	s.sendMerchantSay(conn, conversation.NPC.Name, conversation.Text)
}

func (c *Client) writeCommand(s *Server, cmd mir176.Command, text []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeCommandLocked(s, cmd, text)
}

func (c *Client) ensureMonsterVisible(s *Server, mon world.Monster) {
	c.ensureMonsterVisibleWithStatus(s, mon, world.MonsterStatus(mon, time.Now()))
}

func (c *Client) ensureMonsterVisibleWithStatus(s *Server, mon world.Monster, status int32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.visibleMonsters == nil {
		c.visibleMonsters = map[string]world.Monster{}
	}
	if _, ok := c.visibleMonsters[mon.ID]; ok {
		c.visibleMonsters[mon.ID] = mon
		return
	}
	c.writeCommandLocked(s, MonsterTurnCommand(mon, s.world.MapLight(mon.MapID)), MonsterTurnBodyWithStatus(mon, status))
	c.writeCommandLocked(s, MonsterFeatureCommand(mon), nil)
	c.visibleMonsters[mon.ID] = mon
}

func (c *Client) ensureNPCVisible(s *Server, entity npc.Entity) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.visibleNPCs == nil {
		c.visibleNPCs = map[string]npc.Entity{}
	}
	if _, ok := c.visibleNPCs[entity.ID]; ok {
		c.visibleNPCs[entity.ID] = entity
		return
	}
	actorID := s.world.NPCActorID(entity.ID)
	feature := s.world.NPCFeature(entity)
	body := EncodeBuffer(CharDesc(feature, 0))
	body = append(body, EncodeString(fmt.Sprintf("%s/%d", entity.Name, 255))...)
	c.writeCommandLocked(s, mir176.Command{Ident: mir176.SMTurn, Recog: actorID, Param: uint16(entity.X), Tag: uint16(entity.Y), Series: uint16(entity.Dir)}, body)
	c.writeCommandLocked(s, mir176.Command{Ident: mir176.SMFeatureChanged, Recog: actorID, Param: uint16(feature), Tag: uint16(uint32(feature) >> 16)}, nil)
	c.writeCommandLocked(s, mir176.Command{Ident: mir176.SMUserName, Recog: actorID, Param: 255}, EncodeString(entity.Name))
	c.visibleNPCs[entity.ID] = entity
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

func (c *Client) forgetMissingNPCs(s *Server, current map[string]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.visibleNPCs == nil {
		return
	}
	for npcID, entity := range c.visibleNPCs {
		if _, ok := current[npcID]; ok {
			continue
		}
		c.writeCommandLocked(s, mir176.Command{Ident: mir176.SMDisappear, Recog: s.world.NPCActorID(entity.ID)}, nil)
		delete(c.visibleNPCs, npcID)
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
	c.enqueueOutput(response)
}

func (s *Server) sendCommand(conn net.Conn, cmd mir176.Command, text []byte) {
	response := encodeMessage(cmd, text)
	if client := s.clientForConn(conn); client != nil {
		client.enqueueOutput(response)
		return
	}
	_, _ = conn.Write(response)
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

func skillSummaryForLog(w *world.World, ch storage.Character) string {
	parts := make([]string, 0, len(ch.Skills))
	for _, skillState := range ch.Skills {
		skill, ok := w.Skill(skillState.ID)
		if !ok {
			continue
		}
		magicID, ok := w.MagicIDByName(skillState.ID)
		if !ok {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d:%s#%d(h=%q,l=%d,t=%d)", magicID, skillState.ID, skillState.Hotkey, skill.Name, skillState.Level, skillState.Train))
	}
	return strings.Join(parts, ",")
}

func (s *Server) sendAttackSkillFlags(conn net.Conn, ch storage.Character) {
	if _, _, ok := ch.Skills.Get("刺杀剑术"); ok && !ch.ThrustingDisabled {
		s.sendRawFrame(conn, "+LNG")
	}
}

func initializeSpellStateOnLogin(ch *storage.Character) {
	if ch == nil {
		return
	}
	ch.ThrustingDisabled = false
	ch.HalfMoonDisabled = true
}

func UseMagicBody(w *world.World, ch storage.Character) ([]byte, int) {
	body := bytes.NewBuffer(make([]byte, 0, 1024))
	count := 0
	for _, skillState := range ch.Skills {
		skill, ok := w.Skill(skillState.ID)
		if !ok {
			continue
		}
		magicID, ok := w.MagicIDByName(skillState.ID)
		if !ok {
			continue
		}
		record := bytes.NewBuffer(make([]byte, 0, 128))
		writeClientMagic(record, magicID, skillState, skill)
		body.Write(EncodeBuffer(record.Bytes()))
		writeByte(body, '/')
		count++
	}
	if count == 0 {
		return nil, 0
	}
	return body.Bytes(), count
}

func writeClientMagic(buf *bytes.Buffer, magicID uint16, state storage.SkillState, skill data.StdSkill) {
	payload := make([]byte, 84)
	payload[0] = state.Hotkey
	payload[1] = state.Level
	binary.LittleEndian.PutUint32(payload[4:8], uint32(state.Train))
	binary.LittleEndian.PutUint16(payload[8:10], magicID)
	copy(payload[10:23], encodeShortString(skill.Name, 12))
	payload[23] = byte(skill.EffectType)
	payload[24] = byte(skill.Effect)
	binary.LittleEndian.PutUint16(payload[26:28], uint16(skill.Spell))
	binary.LittleEndian.PutUint16(payload[28:30], uint16(skill.Power))
	payload[30] = byte(skill.NeedLevel1)
	maxTrains := []int{skill.TrainLevel1, skill.TrainLevel2, skill.TrainLevel3, skill.TrainLevel3}
	for i, off := range []int{36, 40, 44, 48} {
		train := maxTrains[i]
		if train <= 0 {
			train = skill.TrainLevel1
		}
		binary.LittleEndian.PutUint32(payload[off:off+4], uint32(train))
	}
	payload[52] = 3
	payload[53] = byte(skill.Job)
	binary.LittleEndian.PutUint32(payload[56:60], uint32(skill.Delay))
	binary.LittleEndian.PutUint16(payload[62:64], uint16(skill.MaxPower))
	copy(payload[65:81], encodeShortString("", 15))
	buf.Write(payload)
}

func encodeShortString(value string, maxLen int) []byte {
	encoded, err := simplifiedchinese.GB18030.NewEncoder().String(value)
	if err != nil {
		encoded = value
	}
	data := []byte(encoded)
	if len(data) > maxLen {
		data = data[:maxLen]
	}
	out := make([]byte, maxLen+1)
	out[0] = byte(len(data))
	copy(out[1:], data)
	return out
}

func CharDesc(feature, status int32) []byte {
	body := make([]byte, 8)
	binary.LittleEndian.PutUint32(body[0:4], uint32(feature))
	binary.LittleEndian.PutUint32(body[4:8], uint32(status))
	return body
}

func MonsterTurnCommand(mon world.Monster, light int) mir176.Command {
	return mir176.Command{
		Ident:  mir176.SMTurn,
		Recog:  world.MonsterActorID(mon),
		Param:  uint16(mon.X),
		Tag:    uint16(mon.Y),
		Series: uint16(makeWord(byte(mon.Dir), byte(light))),
	}
}

func MonsterDigUpCommand(mon world.Monster, light int) mir176.Command {
	return mir176.Command{
		Ident:  mir176.SMDigUp,
		Recog:  world.MonsterActorID(mon),
		Param:  uint16(mon.X),
		Tag:    uint16(mon.Y),
		Series: uint16(makeWord(byte(mon.Dir), byte(light))),
	}
}

func MonsterDigUpBody(mon world.Monster) []byte {
	return MonsterDigUpBodyWithStatus(mon, world.MonsterStatus(mon, time.Now()))
}

func MonsterDigUpBodyWithStatus(mon world.Monster, status int32) []byte {
	return EncodeBuffer(MessageBodyWL(world.MonsterFeature(mon), status, 0, 0))
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

func MonsterWalkCommand(action world.MonsterAction, light int) mir176.Command {
	return mir176.Command{
		Ident:  mir176.SMWalk,
		Recog:  world.MonsterActorID(world.Monster{ID: action.MonsterID}),
		Param:  uint16(action.X),
		Tag:    uint16(action.Y),
		Series: uint16(makeWord(byte(action.Dir), byte(light))),
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
		Recog:  world.CharacterActorID(hit.Character),
		Param:  uint16(hit.Character.HP),
		Tag:    uint16(hit.Character.MaxHP),
		Series: uint16(hit.Damage),
	}
}

func CharacterDeathCommand(ch storage.Character) mir176.Command {
	return mir176.Command{
		Ident:  mir176.SMNowDeath,
		Recog:  world.CharacterActorID(ch),
		Param:  uint16(ch.X),
		Tag:    uint16(ch.Y),
		Series: uint16(ch.Dir),
	}
}

func MonsterTurnBody(mon world.Monster) []byte {
	return MonsterTurnBodyWithStatus(mon, world.MonsterStatus(mon, time.Now()))
}

func MonsterTurnBodyWithStatus(mon world.Monster, status int32) []byte {
	body := EncodeBuffer(CharDesc(world.MonsterFeature(mon), status))
	body = append(body, EncodeString(world.MonsterDisplayName(mon)+fmt.Sprintf("/%d", world.MonsterNameColor(mon)))...)
	return body
}

func MonsterWalkBody(mon world.Monster) []byte {
	return MonsterWalkBodyWithStatus(mon, world.MonsterStatus(mon, time.Now()))
}

func MonsterWalkBodyWithStatus(mon world.Monster, status int32) []byte {
	return EncodeBuffer(CharDesc(world.MonsterFeature(mon), status))
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

func DecodeString(text []byte) string {
	decoded, err := simplifiedchinese.GB18030.NewDecoder().String(string(text))
	if err != nil {
		return string(text)
	}
	return decoded
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
