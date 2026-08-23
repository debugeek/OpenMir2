package network

import (
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"openmir2/internal/config"
	"openmir2/internal/data"
	"openmir2/internal/npc"
	"openmir2/internal/protocol/mir176"
	"openmir2/internal/storage"
	"openmir2/internal/world"
)

const (
	testConfigsDir      = "../../configs"
	testMapID           = "0"
	testMonsterID       = "鸡"
	testWeaponID        = "木剑"
	testArmorID         = "布衣(男)"
	testHPItemID        = "金创药(小量)"
	testInstantHPItemID = "太阳水"
)

func WireString(t *testing.T, text string) []byte {
	t.Helper()
	payload, err := mir176.DecodePlain6Payload(EncodeString(text))
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	return payload
}

func countBagItems(items []storage.UserItem) int {
	return len(items)
}

func setEquippedItem(ch *storage.Character, slot int, item storage.UserItem) {
	if ch.EquippedItems == nil {
		ch.EquippedItems = map[int]storage.UserItem{}
	}
	if item.ItemID == "" {
		delete(ch.EquippedItems, slot)
		return
	}
	ch.EquippedItems[slot] = item
}

func TestDisableNagleIgnoresNonTCPConnWithoutPanic(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	disableNagle(server)
}

func TestDisableNagleSetsNoDelayOnRealTCPConn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("Listen() error = %v", err)
	}
	defer ln.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			disableNagle(conn)
		}
		accepted <- conn
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	server := <-accepted
	if server == nil {
		t.Fatalf("Accept() returned nil conn")
	}
	defer server.Close()
	if _, ok := server.(*net.TCPConn); !ok {
		t.Fatalf("accepted conn type = %T, want *net.TCPConn", server)
	}
}

func TestLoginCredentialsPlainText(t *testing.T) {
	account, password := loginCredentials([]byte("test/test"))
	if account != "test" || password != "test" {
		t.Fatalf("loginCredentials() = %q/%q, want test/test", account, password)
	}
}

func TestLoginCredentialsObservedOfficialClientTestPassword(t *testing.T) {
	account, password := loginCredentials([]byte("test/fwNoei{MlIG[pL"))
	if account != "test" || password != "test" {
		t.Fatalf("loginCredentials() = %q/%q, want test/test", account, password)
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	bundle, err := data.Load(testConfigsDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	addTestGuideNPC(&bundle)
	mp, ok := bundle.Maps[testMapID]
	if !ok {
		t.Fatalf("map %s missing from configs", testMapID)
	}
	bundle.Spawns = []data.StdSpawn{{
		MapID:          testMapID,
		MonsterID:      testMonsterID,
		X:              mp.StartPoints[0].X + 2,
		Y:              mp.StartPoints[0].Y,
		Count:          1,
		RespawnSeconds: 10,
	}}
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := world.New(bundle, store)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New("test", nil, store, w, log)
	s.hitImpactDelay = 0
	return s
}

func addTestGuideNPC(bundle *data.StdBundle) {
	if bundle.NPCs.Entities == nil {
		bundle.NPCs.Entities = map[string]npc.Entity{}
	}
	if bundle.NPCs.Scripts == nil {
		bundle.NPCs.Scripts = map[string]npc.Script{}
	}
	bundle.NPCs.Entities["guide"] = npc.Entity{
		ID:       "guide",
		Name:     "Guide",
		Kind:     npc.KindMerchant,
		MapID:    testMapID,
		X:        666,
		Y:        87,
		Dir:      2,
		ScriptID: "guide_script",
		Merchant: npc.MerchantProfile{
			PriceRate: 100,
			Capabilities: npc.MerchantCapabilities{
				Buy:     true,
				Sell:    true,
				Storage: true,
				GetBack: true,
				Repair:  true,
			},
			Stock: []npc.MerchantStockItem{{ItemID: testHPItemID, Count: 3}},
		},
	}
	bundle.NPCs.Scripts["guide_script"] = npc.Script{
		ID: "guide_script",
		Labels: map[string]npc.Label{
			"@main": {
				Name: "@main",
				Procedures: []npc.Procedure{
					{Say: "你好，这是 NPC 标准库测试。\\ \\<继续/@info>"},
				},
			},
			"@info": {
				Name: "@info",
				Procedures: []npc.Procedure{
					{Say: "你已经点到 NPC 了。\\ \\<返回/@main>"},
				},
			},
		},
	}
}

func testGuideNPC() npc.Entity {
	return npc.Entity{
		ID:       "guide",
		Name:     "Guide",
		Kind:     npc.KindMerchant,
		MapID:    testMapID,
		X:        666,
		Y:        87,
		Dir:      2,
		ScriptID: "guide_script",
		Merchant: npc.MerchantProfile{
			PriceRate: 100,
			Capabilities: npc.MerchantCapabilities{
				Buy:     true,
				Sell:    true,
				Storage: true,
				GetBack: true,
				Repair:  true,
			},
			Stock: []npc.MerchantStockItem{{ItemID: testHPItemID, Count: 3}},
		},
	}
}

func testMakeDrugNPC() npc.Entity {
	entity := testGuideNPC()
	entity.ID = "maker"
	entity.Name = "Maker"
	entity.ScriptID = "makedrug_script"
	entity.Merchant.Stock = []npc.MerchantStockItem{{ItemID: "灰色药粉(少量)", Count: 1}}
	return entity
}

func addTestMakeDrugNPC(bundle *data.StdBundle) {
	if bundle.NPCs.Entities == nil {
		bundle.NPCs.Entities = map[string]npc.Entity{}
	}
	if bundle.NPCs.Scripts == nil {
		bundle.NPCs.Scripts = map[string]npc.Script{}
	}
	bundle.NPCs.Entities["maker"] = testMakeDrugNPC()
	bundle.NPCs.Scripts["makedrug_script"] = npc.Script{
		ID: "makedrug_script",
		Labels: map[string]npc.Label{
			"@main": {
				Name: "@main",
				Procedures: []npc.Procedure{
					{Say: "你是来炼什么药？\\ \\<炼/@makedrug>药\\ \\<关 闭/@exit>"},
				},
			},
		},
	}
}

func newTestServerWithMakeDrugNPC(t *testing.T) *Server {
	t.Helper()
	bundle, err := data.Load(testConfigsDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	addTestGuideNPC(&bundle)
	addTestMakeDrugNPC(&bundle)
	mp, ok := bundle.Maps[testMapID]
	if !ok {
		t.Fatalf("map %s missing from configs", testMapID)
	}
	bundle.Spawns = []data.StdSpawn{{
		MapID:          testMapID,
		MonsterID:      testMonsterID,
		X:              mp.StartPoints[0].X + 2,
		Y:              mp.StartPoints[0].Y,
		Count:          1,
		RespawnSeconds: 10,
	}}
	return newTestServerWithBundle(t, bundle, config.DefaultGameplay())
}

func newTestServerWithBundle(t *testing.T, bundle data.StdBundle, gameplay config.Gameplay) *Server {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := world.New(bundle, store, gameplay)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New("test", nil, store, w, log)
	s.hitImpactDelay = 0
	return s
}

func newDataDirTestServer(t *testing.T, dir string) *Server {
	t.Helper()
	bundle, err := data.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	bundle.Spawns = nil
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := world.New(bundle, store)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New("test", nil, store, w, log)
	s.hitImpactDelay = 0
	return s
}

func newGuaranteedDropServer(t *testing.T) (*Server, storage.Character, net.Conn, net.Conn) {
	t.Helper()
	bundle, err := data.Load(testConfigsDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	mp, ok := bundle.Maps[testMapID]
	if !ok {
		t.Fatalf("map %s missing from configs", testMapID)
	}
	dropTableID := "test-dropper"
	bundle.Drops[dropTableID] = data.StdDropTable{
		ID: dropTableID,
		Entries: []data.StdDropEntry{{
			ItemID:   testWeaponID,
			Chance:   1,
			MinCount: 1,
			MaxCount: 1,
		}},
	}
	mon := bundle.Monsters[testMonsterID]
	mon.ID = dropTableID
	mon.Name = "test-dropper"
	mon.HP = 1
	bundle.Monsters[dropTableID] = mon
	bundle.Spawns = []data.StdSpawn{{
		MapID:          testMapID,
		MonsterID:      dropTableID,
		X:              mp.StartPoints[0].X + 2,
		Y:              mp.StartPoints[0].Y,
		Count:          1,
		RespawnSeconds: 10,
	}}
	store, err := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	w := world.New(bundle, store)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New("test", nil, store, w, log)
	s.hitImpactDelay = 0
	mapID, x, y := w.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	s.registerClient(server, ch)
	return s, ch, server, client
}

func TestHandleTurnUpdatesCharacterAndAcks(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BagItems = []storage.UserItem{{ItemID: testWeaponID, MakeIndex: 1}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	dir := 5
	recog := int32(uint32(x) | uint32(y)<<16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleTurn(server, &ch, mir176.Command{Ident: mir176.CMTurn, Recog: recog, Tag: uint16(dir)})
	}()

	frame := readFrame(t, client)
	assertActionAck(t, frame)
	<-done

	if ch.Dir != dir {
		t.Fatalf("character Dir = %d, want %d", ch.Dir, dir)
	}
}

func TestHandleTurnRejectsCoordinateMismatchAndResyncs(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	recog := int32(uint32(x+1) | uint32(y)<<16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleTurn(server, &ch, mir176.Command{Ident: mir176.CMTurn, Recog: recog, Tag: 2})
	}()

	frame := readFrame(t, client)
	cmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	<-done

	if cmd.Ident != mir176.SMMoveFail {
		t.Fatalf("reply ident = %d, want %d", cmd.Ident, mir176.SMMoveFail)
	}
	if int(cmd.Param) != x || int(cmd.Tag) != y {
		t.Fatalf("resync position = (%d,%d), want (%d,%d)", cmd.Param, cmd.Tag, x, y)
	}
	if ch.Dir != 0 {
		t.Fatalf("character Dir = %d, want unchanged 0", ch.Dir)
	}
}

func TestClientsAroundFiltersByDistance(t *testing.T) {
	s := newTestServer(t)
	nearServer, nearClient := net.Pipe()
	defer nearServer.Close()
	defer nearClient.Close()
	farServer, farClient := net.Pipe()
	defer farServer.Close()
	defer farClient.Close()

	s.clientMu.Lock()
	s.clients[nearServer] = &Client{conn: nearServer, ch: storage.Character{ID: "near", MapID: testMapID, X: 10, Y: 10}}
	s.clients[farServer] = &Client{conn: farServer, ch: storage.Character{ID: "far", MapID: testMapID, X: 40, Y: 40}}
	s.clientMu.Unlock()

	clients := s.ClientsAround(testMapID, 10, 10, 12)
	if len(clients) != 1 {
		t.Fatalf("ClientsAround() len = %d, want 1", len(clients))
	}
	if clients[0].ch.ID != "near" {
		t.Fatalf("ClientsAround() = %q, want near client", clients[0].ch.ID)
	}
}

func TestHandleMoveWalkUpdatesCharacterAndAcks(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	dir := 2
	recog := int32(uint32(x+1) | uint32(y)<<16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleMove(server, &ch, mir176.Command{Ident: mir176.CMWalk, Recog: recog, Tag: uint16(dir)}, false)
	}()

	frame := readFrame(t, client)
	assertActionAck(t, frame)
	<-done

	if ch.X != x+1 || ch.Y != y || ch.Dir != dir {
		t.Fatalf("walk result = (%d,%d,dir %d), want (%d,%d,dir %d)", ch.X, ch.Y, ch.Dir, x+1, y, dir)
	}
}

func TestHandleMoveRejectsTooFarAndResyncs(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	recog := int32(uint32(x+5) | uint32(y)<<16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleMove(server, &ch, mir176.Command{Ident: mir176.CMWalk, Recog: recog, Tag: 2}, false)
	}()

	frame := readFrame(t, client)
	cmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	<-done

	if cmd.Ident != mir176.SMMoveFail {
		t.Fatalf("reply ident = %d, want %d", cmd.Ident, mir176.SMMoveFail)
	}
	if int(cmd.Param) != x || int(cmd.Tag) != y {
		t.Fatalf("resync position = (%d,%d), want (%d,%d)", cmd.Param, cmd.Tag, x, y)
	}
}

func TestHandleHitConnectsAndAcks(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	monsters, _ := s.world.Snapshot(ch.MapID)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters")
	}
	mon := monsters[0]
	ch.X, ch.Y = mon.X-1, mon.Y
	dir := 2 // east, matching the (-1, 0) offset to the monster
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)

	recog := int32(uint32(ch.X) | uint32(ch.Y)<<16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleHit(server, &ch, mir176.Command{Ident: mir176.CMHit, Recog: recog, Tag: uint16(dir)})
	}()

	frame := readFrame(t, client)
	assertActionAck(t, frame)
	struckCmd, struckBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode monster struck frame error = %v", err)
	}
	if struckCmd.Ident != mir176.SMStruck || struckCmd.Recog != world.MonsterActorID(mon) || struckCmd.Param != uint16(mon.HP-4) || struckCmd.Tag != uint16(mon.MaxHP) || struckCmd.Series != 4 {
		t.Fatalf("monster struck = %+v, want recog=%d hp=%d/%d damage=4", struckCmd, world.MonsterActorID(mon), mon.HP-4, mon.MaxHP)
	}
	assertMessageBodyWL(t, struckBody, world.MonsterFeature(mon), 0, ActorID, 0)
	<-done

	if ch.Dir != dir {
		t.Fatalf("character Dir = %d, want %d", ch.Dir, dir)
	}
}

func TestHandleHitBroadcastsDeathWhenMonsterHPReachesZero(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	monsters, _ := s.world.Snapshot(ch.MapID)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters")
	}
	mon := monsters[0]
	if mon.HP != 5 {
		t.Fatalf("test monster HP = %d, want 5", mon.HP)
	}
	ch.X, ch.Y = mon.X-1, mon.Y
	dir := 2
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)

	for i := 1; i <= 2; i++ {
		recog := int32(uint32(ch.X) | uint32(ch.Y)<<16)
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.handleHit(server, &ch, mir176.Command{Ident: mir176.CMHit, Recog: recog, Tag: uint16(dir)})
		}()

		assertActionAck(t, readFrame(t, client))
		if i == 2 {
			winExpCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
			if err != nil {
				t.Fatalf("decode win exp frame error = %v", err)
			}
			if winExpCmd.Ident != mir176.SMWinExp {
				t.Fatalf("win exp command = %+v, want SM_WINEXP", winExpCmd)
			}
			if winExpCmd.Recog == 0 || winExpCmd.Param == 0 {
				t.Fatalf("win exp command = %+v, want current exp and gained exp", winExpCmd)
			}
		}
		struckCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
		if err != nil {
			t.Fatalf("decode struck frame %d error = %v", i, err)
		}
		wantHP := mon.HP - i*4
		if wantHP < 0 {
			wantHP = 0
		}
		if struckCmd.Ident != mir176.SMStruck || struckCmd.Recog != world.MonsterActorID(mon) || struckCmd.Param != uint16(wantHP) {
			t.Fatalf("struck frame %d = %+v, want hp %d", i, struckCmd, wantHP)
		}
		if i == 2 {
			deathCmd, deathBody, err := decodeMessageLikeClient(readFrame(t, client))
			if err != nil {
				t.Fatalf("decode death frame error = %v", err)
			}
			if deathCmd.Ident != mir176.SMNowDeath || deathCmd.Recog != world.MonsterActorID(mon) || int(deathCmd.Param) != mon.X || int(deathCmd.Tag) != mon.Y {
				t.Fatalf("death command = %+v, want monster death at (%d,%d)", deathCmd, mon.X, mon.Y)
			}
			assertCharDesc(t, deathBody, world.MonsterFeature(mon), 0)
			showCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
			if err != nil {
				t.Fatalf("decode drop show frame error = %v", err)
			}
			if showCmd.Ident != mir176.SMItemShow {
				t.Fatalf("drop show command = %+v, want SM_ITEMSHOW", showCmd)
			}
			dx := int(showCmd.Param) - mon.X
			dy := int(showCmd.Tag) - mon.Y
			if dx < -3 || dx > 3 || dy < -3 || dy > 3 {
				t.Fatalf("drop show command = %+v, want near monster death at (%d,%d)", showCmd, mon.X, mon.Y)
			}
		}
		<-done
	}
}

func TestHandleHitBroadcastsDropWhenMonsterDies(t *testing.T) {
	s, ch, server, client := newGuaranteedDropServer(t)
	defer server.Close()
	defer client.Close()
	defer s.unregisterClient(server)

	monsters, _ := s.world.Snapshot(ch.MapID)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters")
	}
	mon := monsters[0]
	ch.X, ch.Y = mon.X-1, mon.Y
	dir := 2

	recog := int32(uint32(ch.X) | uint32(ch.Y)<<16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleHit(server, &ch, mir176.Command{Ident: mir176.CMHit, Recog: recog, Tag: uint16(dir)})
	}()

	assertActionAck(t, readFrame(t, client))
	winExpCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode win exp frame error = %v", err)
	}
	if winExpCmd.Ident != mir176.SMWinExp {
		t.Fatalf("win exp command = %+v, want SM_WINEXP", winExpCmd)
	}
	if _, _, err := decodeMessageLikeClient(readFrame(t, client)); err != nil {
		t.Fatalf("decode struck frame error = %v", err)
	}
	if _, _, err := decodeMessageLikeClient(readFrame(t, client)); err != nil {
		t.Fatalf("decode death frame error = %v", err)
	}
	showCmd, showBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode drop show frame error = %v", err)
	}
	if showCmd.Ident != mir176.SMItemShow {
		t.Fatalf("drop show command = %+v, want SM_ITEMSHOW", showCmd)
	}
	dx := int(showCmd.Param) - mon.X
	dy := int(showCmd.Tag) - mon.Y
	if dx < -3 || dx > 3 || dy < -3 || dy > 3 {
		t.Fatalf("drop show command = %+v, want near monster death at (%d,%d)", showCmd, mon.X, mon.Y)
	}
	weapon, ok := s.world.Item(testWeaponID)
	if !ok {
		t.Fatalf("world.Item(%q) missing", testWeaponID)
	}
	if showCmd.Series != uint16(weapon.Looks) {
		t.Fatalf("drop show looks = %d, want %d from item looks", showCmd.Series, weapon.Looks)
	}
	text, err := mir176.DecodePlain6Payload(showBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if got := DecodeString(text); got != "木剑" {
		t.Fatalf("drop name = %q, want 木剑", got)
	}
	<-done
}

func TestHandleHitBroadcastsAttackerActionToObservers(t *testing.T) {
	s := newTestServer(t)
	s.hitImpactDelay = 25 * time.Millisecond
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	monsters, _ := s.world.Snapshot(ch.MapID)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters")
	}
	mon := monsters[0]
	ch.X, ch.Y = mon.X-1, mon.Y
	dir := 2
	observer := ch
	observer.ID = "observer"
	observer.Account = "observer"
	observer.Name = "observer"

	attackerServer, attackerClient := net.Pipe()
	defer attackerServer.Close()
	defer attackerClient.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(attackerServer, ch)
	defer s.unregisterClient(attackerServer)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	recog := int32(uint32(ch.X) | uint32(ch.Y)<<16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleHit(attackerServer, &ch, mir176.Command{Ident: mir176.CMFireHit, Recog: recog, Tag: uint16(dir)})
	}()

	assertActionAck(t, readFrame(t, attackerClient))
	actionCmd, _, err := decodeMessageLikeClient(readFrame(t, observerClient))
	if err != nil {
		t.Fatalf("decode hit action frame error = %v", err)
	}
	if actionCmd.Ident != mir176.SMFireHit || actionCmd.Recog != ActorID || int(actionCmd.Param) != ch.X || int(actionCmd.Tag) != ch.Y || int(actionCmd.Series) != dir {
		t.Fatalf("hit action = %+v, want SM_FIREHIT from attacker at (%d,%d) dir=%d", actionCmd, ch.X, ch.Y, dir)
	}
	<-done
	readFrame(t, attackerClient)
	readFrame(t, observerClient)
}

func TestHandleHitDelaysImpactBroadcast(t *testing.T) {
	s := newTestServer(t)
	s.hitImpactDelay = 40 * time.Millisecond
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	monsters, _ := s.world.Snapshot(ch.MapID)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters")
	}
	mon := monsters[0]
	ch.X, ch.Y = mon.X-1, mon.Y
	dir := 2
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)

	recog := int32(uint32(ch.X) | uint32(ch.Y)<<16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleHit(server, &ch, mir176.Command{Ident: mir176.CMHit, Recog: recog, Tag: uint16(dir)})
	}()

	assertActionAck(t, readFrame(t, client))
	if err := client.SetReadDeadline(time.Now().Add(10 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	buf := make([]byte, 4096)
	if n, err := client.Read(buf); err == nil {
		t.Fatalf("unexpected immediate impact frame len=%d", n)
	} else if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Fatalf("Read() error = %v, want timeout", err)
	}
	if err := client.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	frame := readFrame(t, client)
	if err := client.SetReadDeadline(time.Time{}); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	struckCmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode delayed struck frame error = %v", err)
	}
	if struckCmd.Ident != mir176.SMStruck || struckCmd.Recog != world.MonsterActorID(mon) {
		t.Fatalf("delayed impact = %+v, want SM_STRUCK for monster %d", struckCmd, world.MonsterActorID(mon))
	}
	<-done
}

func assertMessageBodyWL(t *testing.T, body []byte, wantParam1, wantParam2, wantTag1, wantTag2 int32) {
	t.Helper()
	decoded, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if len(decoded) != 16 {
		t.Fatalf("MessageBodyWL len = %d, want 16", len(decoded))
	}
	gotParam1 := int32(binary.LittleEndian.Uint32(decoded[0:4]))
	gotParam2 := int32(binary.LittleEndian.Uint32(decoded[4:8]))
	gotTag1 := int32(binary.LittleEndian.Uint32(decoded[8:12]))
	gotTag2 := int32(binary.LittleEndian.Uint32(decoded[12:16]))
	if gotParam1 != wantParam1 || gotParam2 != wantParam2 || gotTag1 != wantTag1 || gotTag2 != wantTag2 {
		t.Fatalf("MessageBodyWL = (%d,%d,%d,%d), want (%d,%d,%d,%d)", gotParam1, gotParam2, gotTag1, gotTag2, wantParam1, wantParam2, wantTag1, wantTag2)
	}
}

func assertCharDesc(t *testing.T, body []byte, wantFeature, wantStatus int32) {
	t.Helper()
	decoded, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if len(decoded) != 8 {
		t.Fatalf("CharDesc len = %d, want 8", len(decoded))
	}
	gotFeature := int32(binary.LittleEndian.Uint32(decoded[0:4]))
	gotStatus := int32(binary.LittleEndian.Uint32(decoded[4:8]))
	if gotFeature != wantFeature || gotStatus != wantStatus {
		t.Fatalf("CharDesc = (%d,%d), want (%d,%d)", gotFeature, gotStatus, wantFeature, wantStatus)
	}
}

func TestHandleSaySendsHearMessage(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSay(server, &ch, []byte("hello"))
	}()

	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	<-done

	if cmd.Ident != mir176.SMHear {
		t.Fatalf("ident = %d, want SM_HEAR (%d)", cmd.Ident, mir176.SMHear)
	}
	if cmd.Param != makeWord(0x00, 0xFF) || cmd.Tag != 0 || cmd.Series != 1 {
		t.Fatalf("message command = %+v, want white SM_HEAR", cmd)
	}
	text, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if string(text) != "tester:hello" {
		t.Fatalf("message = %q, want tester:hello", text)
	}
}

func TestHandleClickNPCSendsMerchantSay(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleClickNPC(server, &ch, activeClient, mir176.Command{Ident: mir176.CMClickNPC, Recog: s.world.NPCActorID("guide")})
	}()

	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	<-done
	if cmd.Ident != mir176.SMMerchantSay {
		t.Fatalf("ident = %d, want SMMerchantSay (%d)", cmd.Ident, mir176.SMMerchantSay)
	}
	text, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if got := DecodeString(text); got != "Guide/你好，这是 NPC 标准库测试。\\ \\<继续/@info>" {
		t.Fatalf("message = %q, want guide main dialogue", got)
	}
}

func TestHandleMerchantDlgSelectContinuesNPCScript(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	clickDone := make(chan struct{})
	go func() {
		defer close(clickDone)
		s.handleClickNPC(server, &ch, activeClient, mir176.Command{Ident: mir176.CMClickNPC, Recog: s.world.NPCActorID("guide")})
	}()
	if _, _, err := decodeMessageLikeClient(readFrame(t, client)); err != nil {
		t.Fatalf("decode initial NPC frame error = %v", err)
	}
	<-clickDone

	selectDone := make(chan struct{})
	go func() {
		defer close(selectDone)
		s.handleMerchantDlgSelect(server, &ch, activeClient, mir176.Command{Ident: mir176.CMMerchantDlgSelect, Recog: s.world.NPCActorID("guide")}, WireString(t, "info"))
	}()
	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	<-selectDone
	if cmd.Ident != mir176.SMMerchantSay {
		t.Fatalf("ident = %d, want SMMerchantSay (%d)", cmd.Ident, mir176.SMMerchantSay)
	}
	text, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if got := DecodeString(text); got != "Guide/你已经点到 NPC 了。\\ \\<返回/@main>" {
		t.Fatalf("message = %q, want info dialogue", got)
	}
}

func TestHandleMerchantDlgSelectOpensBuyList(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleMerchantDlgSelect(server, &ch, activeClient, mir176.Command{Ident: mir176.CMMerchantDlgSelect, Recog: s.world.NPCActorID("guide")}, WireString(t, "@buy"))
	}()
	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	<-done
	if cmd.Ident != mir176.SMSendGoodsList {
		t.Fatalf("ident = %d, want SMSendGoodsList (%d)", cmd.Ident, mir176.SMSendGoodsList)
	}
	if len(body) == 0 {
		t.Fatal("expected buy list body")
	}
}

func TestHandleMerchantDlgSelectBackReturnsToMain(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	clickDone := make(chan struct{})
	go func() {
		defer close(clickDone)
		s.handleClickNPC(server, &ch, activeClient, mir176.Command{Ident: mir176.CMClickNPC, Recog: s.world.NPCActorID("guide")})
	}()
	if _, _, err := decodeMessageLikeClient(readFrame(t, client)); err != nil {
		t.Fatalf("decode initial NPC frame error = %v", err)
	}
	<-clickDone

	buyDone := make(chan struct{})
	go func() {
		defer close(buyDone)
		s.handleMerchantDlgSelect(server, &ch, activeClient, mir176.Command{Ident: mir176.CMMerchantDlgSelect, Recog: s.world.NPCActorID("guide")}, WireString(t, "@buy"))
	}()
	if _, _, err := decodeMessageLikeClient(readFrame(t, client)); err != nil {
		t.Fatalf("decode buy list error = %v", err)
	}
	<-buyDone

	backDone := make(chan struct{})
	go func() {
		defer close(backDone)
		s.handleMerchantDlgSelect(server, &ch, activeClient, mir176.Command{Ident: mir176.CMMerchantDlgSelect, Recog: s.world.NPCActorID("guide")}, WireString(t, "@back"))
	}()
	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode back frame error = %v", err)
	}
	<-backDone
	if cmd.Ident != mir176.SMMerchantSay {
		t.Fatalf("ident = %d, want SMMerchantSay (%d)", cmd.Ident, mir176.SMMerchantSay)
	}
	text, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if got := DecodeString(text); got != "Guide/你好，这是 NPC 标准库测试。\\ \\<继续/@info>" {
		t.Fatalf("back message = %q, want main dialogue", got)
	}
}

func TestHandleMerchantDlgSelectBuildGuildFlow(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Gold = 1000000
	ch.BagItems = append(ch.BagItems, storage.UserItem{ItemID: "沃玛号角", MakeIndex: 1001})

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		s.handleMerchantDlgSelect(server, &ch, activeClient, mir176.Command{Ident: mir176.CMMerchantDlgSelect, Recog: s.world.NPCActorID("guide")}, WireString(t, "@buildguildnow"))
	}()
	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	if cmd.Ident != mir176.SMMerchantSay {
		t.Fatalf("ident = %d, want SMMerchantSay (%d)", cmd.Ident, mir176.SMMerchantSay)
	}
	text, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if got := DecodeString(text); got != "Guide/请填写行会名称。\n<返回/@main>" {
		t.Fatalf("prompt = %q, want build guild prompt", got)
	}

	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		s.handleMerchantDlgSelect(server, &ch, activeClient, mir176.Command{Ident: mir176.CMMerchantDlgSelect, Recog: s.world.NPCActorID("guide")}, WireString(t, "天龙会"))
	}()
	cmd, body, err = decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	if cmd.Ident != mir176.SMDelItems {
		t.Fatalf("first follow-up ident = %d, want SMDelItems (%d)", cmd.Ident, mir176.SMDelItems)
	}
	cmd, _, err = decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() gold change error = %v", err)
	}
	if cmd.Ident != mir176.SMGoldChanged {
		t.Fatalf("second follow-up ident = %d, want SMGoldChanged (%d)", cmd.Ident, mir176.SMGoldChanged)
	}
	cmd, body, err = decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() final say error = %v", err)
	}
	<-secondDone
	<-firstDone
	if cmd.Ident != mir176.SMMerchantSay {
		t.Fatalf("third follow-up ident = %d, want SMMerchantSay (%d)", cmd.Ident, mir176.SMMerchantSay)
	}
	text, err = mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if got := DecodeString(text); got != "Guide/行会创建申请已提交: 天龙会\n<返回/@main>" {
		t.Fatalf("result = %q, want build guild success message", got)
	}
	if got := ch.Gold; got != 0 {
		t.Fatalf("gold = %d, want 0 after build guild", got)
	}
	if bagHasItemID(ch, "沃玛号角") {
		t.Fatal("expected 沃玛号角 to be consumed")
	}
}

func TestHandleUserGetDetailItemReturnsEncodedItemRows(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleUserGetDetailItem(server, &ch, activeClient, mir176.Command{
			Ident: mir176.CMUserGetDetailItem,
			Recog: s.world.NPCActorID("guide"),
			Param: 5,
		}, WireString(t, testHPItemID))
	}()
	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	<-done
	if cmd.Ident != mir176.SMSendDetailGoodsList {
		t.Fatalf("ident = %d, want SMSendDetailGoodsList (%d)", cmd.Ident, mir176.SMSendDetailGoodsList)
	}
	if cmd.Recog != s.world.NPCActorID("guide") {
		t.Fatalf("merchant id = %d, want %d", cmd.Recog, s.world.NPCActorID("guide"))
	}
	if int(cmd.Param) == 0 {
		t.Fatal("expected non-zero detail item count")
	}
	if cmd.Tag != 0 {
		t.Fatalf("page = %d, want 0", cmd.Tag)
	}
	if cmd.Series != 0 {
		t.Fatalf("series = %d, want 0", cmd.Series)
	}
	parts := bytes.Split(body, []byte{'/'})
	if len(parts) == 0 || len(parts[0]) == 0 {
		t.Fatal("expected encoded item row body")
	}
	raw, err := mir176.DecodePlain6Payload(parts[0])
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	nameLen := int(raw[0])
	if got := DecodeString(raw[1 : 1+nameLen]); got != testHPItemID {
		t.Fatalf("detail row name = %q, want %q", got, testHPItemID)
	}
}

func TestHandleMerchantQuerySellPriceReturnsPrice(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BagItems = []storage.UserItem{{ItemID: testHPItemID, MakeIndex: 77}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleMerchantQuerySellPrice(server, &ch, activeClient, mir176.Command{
			Ident: mir176.CMMerchantQuerySellPrice,
			Recog: s.world.NPCActorID("guide"),
			Param: uint16(77),
		}, WireString(t, testHPItemID))
	}()
	cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	<-done
	if cmd.Ident != mir176.SMSendBuyPrice {
		t.Fatalf("ident = %d, want SMSendBuyPrice (%d)", cmd.Ident, mir176.SMSendBuyPrice)
	}
	item, _ := s.world.Item(testHPItemID)
	want := merchantSellPrice(item, ch.BagItems[0], entity.Merchant.PriceRate)
	if int(cmd.Recog) != want {
		t.Fatalf("price = %d, want %d", cmd.Recog, want)
	}
}

func TestHandleUserBuyItemAddsItemImmediately(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Gold = 100000
	before := len(ch.BagItems)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleUserBuyItem(server, &ch, activeClient, mir176.Command{
			Ident: mir176.CMUserBuyItem,
			Recog: s.world.NPCActorID("guide"),
		}, WireString(t, testHPItemID))
	}()

	cmd1, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() #1 error = %v", err)
	}
	cmd2, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() #2 error = %v", err)
	}
	cmd3, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() #3 error = %v", err)
	}
	<-done
	if cmd1.Ident != mir176.SMBuyItemSuccess {
		t.Fatalf("first ident = %d, want SMBuyItemSuccess (%d)", cmd1.Ident, mir176.SMBuyItemSuccess)
	}
	if cmd2.Ident != mir176.SMAddItem {
		t.Fatalf("second ident = %d, want SMAddItem (%d)", cmd2.Ident, mir176.SMAddItem)
	}
	if cmd3.Ident != mir176.SMGoldChanged {
		t.Fatalf("third ident = %d, want SMGoldChanged (%d)", cmd3.Ident, mir176.SMGoldChanged)
	}
	if len(ch.BagItems) != before+1 {
		t.Fatalf("bag len = %d, want %d", len(ch.BagItems), before+1)
	}
	found := false
	for _, entry := range ch.BagItems {
		if entry.ItemID == testHPItemID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("bag = %+v, want %s to appear immediately", ch.BagItems, testHPItemID)
	}
	stocks := s.world.MerchantStock("guide")
	if len(stocks) != 1 || stocks[0].Count != 2 {
		t.Fatalf("merchant stocks = %+v, want one item with count 2", stocks)
	}
}

func TestHandleUserBuyItemFailsWhenBagIsFull(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Gold = 100000
	fullBag := make([]storage.UserItem, 46)
	for i := range fullBag {
		fullBag[i] = storage.UserItem{ItemID: testWeaponID, MakeIndex: int32(i + 1)}
	}
	ch.BagItems = fullBag
	beforeGold := ch.Gold
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleUserBuyItem(server, &ch, activeClient, mir176.Command{
			Ident: mir176.CMUserBuyItem,
			Recog: s.world.NPCActorID("guide"),
		}, WireString(t, testHPItemID))
	}()

	cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	<-done
	if cmd.Ident != mir176.SMBuyItemFail {
		t.Fatalf("ident = %d, want SMBuyItemFail (%d)", cmd.Ident, mir176.SMBuyItemFail)
	}
	if len(ch.BagItems) != len(fullBag) {
		t.Fatalf("bag len = %d, want %d", len(ch.BagItems), len(fullBag))
	}
	if ch.Gold != beforeGold {
		t.Fatalf("gold = %d, want %d", ch.Gold, beforeGold)
	}
}

func TestHandleUserSellItemAddsMerchantStock(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Gold = 100000
	ch.BagItems = []storage.UserItem{{ItemID: testHPItemID, MakeIndex: 77}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleUserSellItem(server, &ch, activeClient, mir176.Command{
			Ident: mir176.CMUserSellItem,
			Recog: s.world.NPCActorID("guide"),
			Param: uint16(77),
		}, WireString(t, testHPItemID))
	}()

	cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	goldCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() gold error = %v", err)
	}
	<-done
	if cmd.Ident != mir176.SMUserSellItemOK {
		t.Fatalf("ident = %d, want SMUserSellItemOK (%d)", cmd.Ident, mir176.SMUserSellItemOK)
	}
	if goldCmd.Ident != mir176.SMGoldChanged {
		t.Fatalf("gold ident = %d, want SMGoldChanged (%d)", goldCmd.Ident, mir176.SMGoldChanged)
	}
	stocks := s.world.MerchantStock("guide")
	if len(stocks) != 1 || stocks[0].Count != 4 {
		t.Fatalf("merchant stocks = %+v, want one item with count 4", stocks)
	}
}

func TestHandleMerchantDlgSelectOpensStorageWindow(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleMerchantDlgSelect(server, &ch, activeClient, mir176.Command{Ident: mir176.CMMerchantDlgSelect, Recog: s.world.NPCActorID("guide")}, WireString(t, "@storage"))
	}()

	cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	<-done
	if cmd.Ident != mir176.SMSendUserStorageItem {
		t.Fatalf("ident = %d, want SMSendUserStorageItem (%d)", cmd.Ident, mir176.SMSendUserStorageItem)
	}
	if cmd.Recog != 0 {
		t.Fatalf("recog = %d, want 0", cmd.Recog)
	}
	if cmd.Param != uint16(s.world.NPCActorID("guide")) {
		t.Fatalf("merchant id = %d, want %d", cmd.Param, s.world.NPCActorID("guide"))
	}
}

func TestHandleMerchantDlgSelectOpensGetBackList(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.StorageItems = []storage.UserItem{{ItemID: testHPItemID, MakeIndex: 77}}
	if err := s.store.SaveCharacter(ch); err != nil {
		t.Fatalf("SaveCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleMerchantDlgSelect(server, &ch, activeClient, mir176.Command{Ident: mir176.CMMerchantDlgSelect, Recog: s.world.NPCActorID("guide")}, WireString(t, "@getback"))
	}()

	cmd1, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() #1 error = %v", err)
	}
	<-done
	if cmd1.Ident != mir176.SMSaveItemList {
		t.Fatalf("ident = %d, want SMSaveItemList (%d)", cmd1.Ident, mir176.SMSaveItemList)
	}
	if cmd1.Recog != s.world.NPCActorID("guide") {
		t.Fatalf("recog = %d, want %d", cmd1.Recog, s.world.NPCActorID("guide"))
	}
	if cmd1.Series != 1 {
		t.Fatalf("series = %d, want 1", cmd1.Series)
	}
	if len(body) == 0 {
		t.Fatal("expected storage item list body")
	}
}

func TestHandleMerchantDlgSelectOpensWarehouseBindMenu(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleMerchantDlgSelect(server, &ch, activeClient, mir176.Command{Ident: mir176.CMMerchantDlgSelect, Recog: s.world.NPCActorID("仓库-0")}, WireString(t, "@mbind"))
	}()

	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	<-done
	if cmd.Ident != mir176.SMMerchantSay {
		t.Fatalf("ident = %d, want SMMerchantSay (%d)", cmd.Ident, mir176.SMMerchantSay)
	}
	text, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	got := DecodeString(text)
	if !strings.Contains(got, "用金币<交换/@changeGold>金条") {
		t.Fatalf("message = %q, want warehouse bind menu", got)
	}
}

func TestHandleMerchantDlgSelectOpensMakeDrugList(t *testing.T) {
	s := newTestServerWithMakeDrugNPC(t)
	entity := testMakeDrugNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleMerchantDlgSelect(server, &ch, activeClient, mir176.Command{Ident: mir176.CMMerchantDlgSelect, Recog: s.world.NPCActorID("maker")}, WireString(t, "@makedrug"))
	}()

	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	<-done
	if cmd.Ident != mir176.SMSendUserMakeDrugItemList {
		t.Fatalf("ident = %d, want SMSendUserMakeDrugItemList (%d)", cmd.Ident, mir176.SMSendUserMakeDrugItemList)
	}
	if cmd.Param != uint16(s.world.NPCActorID("maker")) {
		t.Fatalf("merchant id = %d, want %d", cmd.Param, s.world.NPCActorID("maker"))
	}
	if len(body) == 0 {
		t.Fatal("expected make drug list body")
	}
}

func TestHandleMerchantDlgSelectExchangesGoldToGoldBar(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Gold = 1002000
	ch.BagItems = nil
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleMerchantDlgSelect(server, &ch, activeClient, mir176.Command{Ident: mir176.CMMerchantDlgSelect, Recog: s.world.NPCActorID("仓库-0")}, WireString(t, "@changeGold_1"))
	}()

	cmd1, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() #1 error = %v", err)
	}
	cmd2, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() #2 error = %v", err)
	}
	cmd3, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() #3 error = %v", err)
	}
	cmd4, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() #4 error = %v", err)
	}
	<-done
	if cmd1.Ident != mir176.SMAddItem {
		t.Fatalf("first ident = %d, want SMAddItem (%d)", cmd1.Ident, mir176.SMAddItem)
	}
	if cmd2.Ident != mir176.SMGoldChanged {
		t.Fatalf("second ident = %d, want SMGoldChanged (%d)", cmd2.Ident, mir176.SMGoldChanged)
	}
	if cmd3.Ident != mir176.SMWeightChanged {
		t.Fatalf("third ident = %d, want SMWeightChanged (%d)", cmd3.Ident, mir176.SMWeightChanged)
	}
	if cmd4.Ident != mir176.SMMerchantSay {
		t.Fatalf("fourth ident = %d, want SMMerchantSay (%d)", cmd4.Ident, mir176.SMMerchantSay)
	}
	text, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if got := DecodeString(text); !strings.Contains(got, "金币已经换好金条了") {
		t.Fatalf("message = %q, want gold exchange success", got)
	}
	if ch.Gold != 0 {
		t.Fatalf("gold = %d, want 0", ch.Gold)
	}
	if len(ch.BagItems) != 1 || ch.BagItems[0].ItemID != "金条" {
		t.Fatalf("bag = %+v, want one 金条", ch.BagItems)
	}
}

func TestHandleMerchantDlgSelectBundlesScrolls(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Gold = 100
	ch.BagItems = []storage.UserItem{
		{ItemID: "回城卷", MakeIndex: 1},
		{ItemID: "回城卷", MakeIndex: 2},
		{ItemID: "回城卷", MakeIndex: 3},
		{ItemID: "回城卷", MakeIndex: 4},
		{ItemID: "回城卷", MakeIndex: 5},
		{ItemID: "回城卷", MakeIndex: 6},
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleMerchantDlgSelect(server, &ch, activeClient, mir176.Command{Ident: mir176.CMMerchantDlgSelect, Recog: s.world.NPCActorID("仓库-0")}, WireString(t, "@zum_bind3"))
	}()

	cmd1, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() #1 error = %v", err)
	}
	cmd2, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() #2 error = %v", err)
	}
	cmd3, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() #3 error = %v", err)
	}
	cmd4, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() #4 error = %v", err)
	}
	cmd5, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() #5 error = %v", err)
	}
	<-done
	if cmd1.Ident != mir176.SMDelItems {
		t.Fatalf("first ident = %d, want SMDelItems (%d)", cmd1.Ident, mir176.SMDelItems)
	}
	if cmd2.Ident != mir176.SMAddItem {
		t.Fatalf("second ident = %d, want SMAddItem (%d)", cmd2.Ident, mir176.SMAddItem)
	}
	if cmd3.Ident != mir176.SMGoldChanged {
		t.Fatalf("third ident = %d, want SMGoldChanged (%d)", cmd3.Ident, mir176.SMGoldChanged)
	}
	if cmd4.Ident != mir176.SMWeightChanged {
		t.Fatalf("fourth ident = %d, want SMWeightChanged (%d)", cmd4.Ident, mir176.SMWeightChanged)
	}
	if cmd5.Ident != mir176.SMMerchantSay {
		t.Fatalf("fifth ident = %d, want SMMerchantSay (%d)", cmd5.Ident, mir176.SMMerchantSay)
	}
	text, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if got := DecodeString(text); !strings.Contains(got, "已经捆好了") {
		t.Fatalf("message = %q, want bundle success", got)
	}
	if ch.Gold != 0 {
		t.Fatalf("gold = %d, want 0", ch.Gold)
	}
	if len(ch.BagItems) != 1 || ch.BagItems[0].ItemID != "回城卷包" {
		t.Fatalf("bag = %+v, want one 回城卷包", ch.BagItems)
	}
}

func TestHandleMerchantDlgSelectTeleportsFromTeleporterNpc(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Gold = 5000
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleMerchantDlgSelect(server, &ch, activeClient, mir176.Command{Ident: mir176.CMMerchantDlgSelect, Recog: s.world.NPCActorID("传送员-0")}, WireString(t, "@JIANAN"))
	}()

	for {
		cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
		if err != nil {
			t.Fatalf("decodeMessageLikeClient() error = %v", err)
		}
		if cmd.Ident == mir176.SMMerchantDlgClose {
			break
		}
	}
	<-done
	if ch.MapID != "0" || ch.X != 333 || ch.Y != 268 {
		t.Fatalf("character position = %s %d %d, want 0 333 268", ch.MapID, ch.X, ch.Y)
	}
	if ch.Gold != 3000 {
		t.Fatalf("gold = %d, want 3000", ch.Gold)
	}
}

func TestTeleporterTimeMessageUsesGreetingByHour(t *testing.T) {
	got := teleporterTimeMessage(time.Date(2026, time.August, 23, 7, 5, 0, 0, time.Local), "Tester")
	if !strings.Contains(got, "Tester 早上好！") {
		t.Fatalf("message = %q, want morning greeting", got)
	}
	if !strings.Contains(got, "<星期天>") {
		t.Fatalf("message = %q, want weekday", got)
	}
	if !strings.Contains(got, "07:05") {
		t.Fatalf("message = %q, want zero-padded time", got)
	}
}

func TestHandleUserMakeDrugItemConsumesMaterialsAndAddsItem(t *testing.T) {
	s := newTestServerWithMakeDrugNPC(t)
	entity := testMakeDrugNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Gold = 1000
	ch.BagItems = []storage.UserItem{
		{ItemID: "食人树叶", MakeIndex: 1},
		{ItemID: "食人树叶", MakeIndex: 2},
		{ItemID: "食人树叶", MakeIndex: 3},
		{ItemID: "食人树叶", MakeIndex: 4},
		{ItemID: "毒蜘蛛牙齿", MakeIndex: 5},
		{ItemID: "毒蜘蛛牙齿", MakeIndex: 6},
		{ItemID: "食人树果实", MakeIndex: 7},
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleUserMakeDrugItem(server, &ch, activeClient, mir176.Command{
			Ident: mir176.CMUserMakeDrugItem,
			Recog: s.world.NPCActorID("maker"),
		}, WireString(t, "灰色药粉(少量)"))
	}()

	delCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() del error = %v", err)
	}
	addCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() add error = %v", err)
	}
	okCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() ok error = %v", err)
	}
	<-done
	if delCmd.Ident != mir176.SMDelItems {
		t.Fatalf("del ident = %d, want %d", delCmd.Ident, mir176.SMDelItems)
	}
	if addCmd.Ident != mir176.SMAddItem {
		t.Fatalf("add ident = %d, want SMAddItem (%d)", addCmd.Ident, mir176.SMAddItem)
	}
	if okCmd.Ident != mir176.SMMakeDrugSuccess {
		t.Fatalf("ok ident = %d, want SMMakeDrugSuccess (%d)", okCmd.Ident, mir176.SMMakeDrugSuccess)
	}
	if got := len(ch.BagItems); got != 1 {
		t.Fatalf("bag items = %d, want 1", got)
	}
	found := false
	for _, entry := range ch.BagItems {
		if entry.ItemID == "灰色药粉(少量)" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected crafted item in bag")
	}
}

func TestHandleUserStorageItemReturnsStorageOK(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BagItems = []storage.UserItem{{ItemID: testHPItemID, MakeIndex: 77}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleUserStorageItem(server, &ch, activeClient, mir176.Command{Ident: mir176.CMUserStorageItem, Recog: s.world.NPCActorID("guide"), Param: uint16(77)}, WireString(t, testHPItemID))
	}()
	cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	weightCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() weight error = %v", err)
	}
	<-done
	if cmd.Ident != mir176.SMStorageOK {
		t.Fatalf("ident = %d, want SMStorageOK (%d)", cmd.Ident, mir176.SMStorageOK)
	}
	if weightCmd.Ident != mir176.SMWeightChanged {
		t.Fatalf("weight ident = %d, want SMWeightChanged (%d)", weightCmd.Ident, mir176.SMWeightChanged)
	}
	if got := len(ch.StorageItems); got != 1 {
		t.Fatalf("storage items = %d, want 1", got)
	}
}

func TestHandleUserTakeBackStorageItemReturnsTakeBackOK(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.StorageItems = []storage.UserItem{{ItemID: testHPItemID, MakeIndex: 77}}
	beforeBag := len(ch.BagItems)
	beforeStorage := len(ch.StorageItems)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleUserTakeBackStorageItem(server, &ch, activeClient, mir176.Command{Ident: mir176.CMUserTakeBackStorageItem, Recog: s.world.NPCActorID("guide"), Param: uint16(77)}, WireString(t, testHPItemID))
	}()
	cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	addCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() add error = %v", err)
	}
	weightCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() weight error = %v", err)
	}
	<-done
	if cmd.Ident != mir176.SMTakeBackStorageItemOK {
		t.Fatalf("ident = %d, want SMTakeBackStorageItemOK (%d)", cmd.Ident, mir176.SMTakeBackStorageItemOK)
	}
	if addCmd.Ident != mir176.SMAddItem {
		t.Fatalf("add ident = %d, want SMAddItem (%d)", addCmd.Ident, mir176.SMAddItem)
	}
	if weightCmd.Ident != mir176.SMWeightChanged {
		t.Fatalf("weight ident = %d, want SMWeightChanged (%d)", weightCmd.Ident, mir176.SMWeightChanged)
	}
	if got := len(ch.BagItems); got != beforeBag+1 {
		t.Fatalf("bag items = %d, want %d", got, beforeBag+1)
	}
	if got := len(ch.StorageItems); got != beforeStorage-1 {
		t.Fatalf("storage items = %d, want %d", got, beforeStorage-1)
	}
}

func TestSendNPCConversationKeepsMultilineTextInOneMessage(t *testing.T) {
	s := newTestServer(t)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.sendNPCConversation(server, npc.Conversation{
			NPC:  npc.Entity{Name: "Guide"},
			Text: "第一行\\第二行",
		})
	}()

	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	<-done
	if cmd.Ident != mir176.SMMerchantSay {
		t.Fatalf("ident = %d, want SMMerchantSay (%d)", cmd.Ident, mir176.SMMerchantSay)
	}
	text, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if got := DecodeString(text); got != "Guide/第一行\\第二行" {
		t.Fatalf("message = %q, want multiline dialogue", got)
	}
}

func TestHandleSayBroadcastsToPlayersOnSameMap(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch1, err := s.world.CreateCharacter("test", "tester1", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch2, err := s.world.CreateCharacter("test", "tester2", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server1, client1 := net.Pipe()
	defer server1.Close()
	defer client1.Close()
	server2, client2 := net.Pipe()
	defer server2.Close()
	defer client2.Close()
	s.registerClient(server1, ch1)
	defer s.unregisterClient(server1)
	s.registerClient(server2, ch2)
	defer s.unregisterClient(server2)

	done := make(chan struct{})
	read1 := make(chan struct{})
	read2 := make(chan struct{})
	go func() {
		defer close(read1)
		assertHearMessage(t, readFrame(t, client1), "tester1:hello", makeWord(0x00, 0xFF))
	}()
	go func() {
		defer close(read2)
		assertHearMessage(t, readFrame(t, client2), "tester1:hello", makeWord(0x00, 0xFF))
	}()
	go func() {
		defer close(done)
		s.handleSay(server1, &ch1, []byte("hello"))
	}()

	<-read1
	<-read2
	<-done
}

func TestHandleShoutBroadcastsYellowCryToAllPlayers(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch1, err := s.world.CreateCharacter("test", "tester1", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch2, err := s.world.CreateCharacter("test", "tester2", "warrior", "1", 17, 12)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server1, client1 := net.Pipe()
	defer server1.Close()
	defer client1.Close()
	server2, client2 := net.Pipe()
	defer server2.Close()
	defer client2.Close()
	s.registerClient(server1, ch1)
	defer s.unregisterClient(server1)
	s.registerClient(server2, ch2)
	defer s.unregisterClient(server2)

	done := make(chan struct{})
	read1 := make(chan struct{})
	read2 := make(chan struct{})
	go func() {
		defer close(read1)
		assertHearMessage(t, readFrame(t, client1), "(!)tester1: hello", makeWord(0x00, 0x97))
	}()
	go func() {
		defer close(read2)
		assertHearMessage(t, readFrame(t, client2), "(!)tester1: hello", makeWord(0x00, 0x97))
	}()
	go func() {
		defer close(done)
		s.handleSay(server1, &ch1, []byte("!hello"))
	}()

	<-read1
	<-read2
	<-done
}

func TestHandleMobCommandSpawnsMonsterInFront(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Dir = 2
	before, _ := s.world.Snapshot(ch.MapID)
	beforeIDs := map[string]bool{}
	for _, mon := range before {
		beforeIDs[mon.ID] = true
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	text, err := simplifiedchinese.GB18030.NewEncoder().String("@Mob 鹿 2 0")
	if err != nil {
		t.Fatalf("GB18030 encode error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSay(server, &ch, []byte(text))
	}()

	assertHearMessage(t, readFrame(t, client), "spawned 2 鹿", makeWord(0x00, 0xFF))
	<-done

	after, _ := s.world.Snapshot(ch.MapID)
	if len(after) != len(before)+2 {
		t.Fatalf("monster count = %d, want %d", len(after), len(before)+2)
	}
	var spawnedAtFront int
	spawnedPositions := map[[2]int]bool{}
	for _, mon := range after {
		if mon.TemplateID != "鹿" || beforeIDs[mon.ID] {
			continue
		}
		if mon.X == x+1 && mon.Y == y {
			spawnedAtFront++
		}
		pos := [2]int{mon.X, mon.Y}
		spawnedPositions[pos] = true
	}
	if len(spawnedPositions) != 1 {
		t.Fatalf("spawned 鹿 = %d, want 1 shared point", len(spawnedPositions))
	}
	if spawnedAtFront != 2 {
		t.Fatalf("spawned 鹿 at front (%d,%d) = %d, want 2", x+1, y, spawnedAtFront)
	}
}

func TestHandleMobCommandDecodesGBKMonsterName(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Dir = 2
	before, _ := s.world.Snapshot(ch.MapID)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)
	text, err := simplifiedchinese.GB18030.NewEncoder().String("@Mob 白野猪1 1 0")
	if err != nil {
		t.Fatalf("GB18030 encode error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSay(server, &ch, []byte(text))
	}()

	assertHearMessage(t, readFrame(t, client), "spawned 1 白野猪", makeWord(0x00, 0xFF))
	turnCmd, turnBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode monster turn frame error = %v", err)
	}
	if turnCmd.Ident != mir176.SMTurn || int(turnCmd.Param) != x+1 || int(turnCmd.Tag) != y || turnCmd.Series != 4 {
		t.Fatalf("monster turn = %+v, want SM_TURN at (%d,%d) dir 4", turnCmd, x+1, y)
	}
	assertMonsterTurnBody(t, turnBody, "白野猪/255", int32(19|112<<16))

	featureCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode monster feature frame error = %v", err)
	}
	if featureCmd.Ident != mir176.SMFeatureChanged || featureCmd.Recog != turnCmd.Recog || featureCmd.Param != 19 || featureCmd.Tag != 112 {
		t.Fatalf("monster feature = %+v, want feature 19/112 for recog %d", featureCmd, turnCmd.Recog)
	}
	<-done

	after, _ := s.world.Snapshot(ch.MapID)
	if len(after) != len(before)+1 {
		t.Fatalf("monster count = %d, want %d", len(after), len(before)+1)
	}
	var spawned int
	for _, mon := range after {
		if mon.TemplateID == "白野猪1" && mon.X == x+1 && mon.Y == y {
			spawned++
		}
	}
	if spawned != 1 {
		t.Fatalf("spawned 白野猪1 at (%d,%d) = %d, want 1", x+1, y, spawned)
	}
}

func assertMonsterTurnBody(t *testing.T, body []byte, wantName string, wantFeature int32) {
	t.Helper()
	descLen := len(EncodeBuffer(make([]byte, 8)))
	if len(body) <= descLen {
		t.Fatalf("monster turn body len = %d, want > %d", len(body), descLen)
	}
	desc, err := mir176.DecodePlain6Payload(body[:descLen])
	if err != nil {
		t.Fatalf("decode CharDesc error = %v", err)
	}
	if len(desc) != 8 {
		t.Fatalf("CharDesc len = %d, want 8", len(desc))
	}
	if got := int32(binary.LittleEndian.Uint32(desc[0:4])); got != wantFeature {
		t.Fatalf("CharDesc feature = %d, want %d", got, wantFeature)
	}
	name, err := mir176.DecodePlain6Payload(body[descLen:])
	if err != nil {
		t.Fatalf("decode monster name error = %v", err)
	}
	if got := DecodeString(name); got != wantName {
		t.Fatalf("monster turn name = %q, want %q", got, wantName)
	}
}

func TestSendEnterWorldReturnsCharacter(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	want, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	got, ok := s.sendEnterWorld(nil, RunLogin{Account: "test", CharName: "tester"})
	if !ok {
		t.Fatalf("sendEnterWorld() = ok=false, want true")
	}
	if got.Name != want.Name || got.MapID != want.MapID || got.X != want.X || got.Y != want.Y {
		t.Fatalf("sendEnterWorld() = %+v, want %+v", got, want)
	}
}

func TestSendEnterWorldRevivesDeadCharacterAtHome(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	want, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	want.HP = 0
	want.MP = 0
	if err := s.store.SaveCharacter(want); err != nil {
		t.Fatalf("SaveCharacter() error = %v", err)
	}

	got, ok := s.sendEnterWorld(nil, RunLogin{Account: "test", CharName: "tester"})
	if !ok {
		t.Fatalf("sendEnterWorld() = ok=false, want true")
	}
	if got.HP != got.MaxHP || got.MP != got.MaxMP {
		t.Fatalf("sendEnterWorld() vitals = hp=%d mp=%d, want full restore", got.HP, got.MP)
	}
	if got.MapID != want.HomeMap || got.X != want.HomeX || got.Y != want.HomeY {
		t.Fatalf("sendEnterWorld() location = %+v, want home %+v", got, want)
	}
}

func TestSendEnterWorldSendsNearbyMonsters(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	mapID, x, y := s.world.DefaultSpawn()
	if _, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y); err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	if _, err := s.world.SpawnMonsterByName(mapID, x+1, y, "鹿", 1); err != nil {
		t.Fatalf("SpawnMonsterByName() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		ch, ok := s.sendEnterWorld(server, RunLogin{Account: "test", CharName: "tester"})
		if ok {
			s.sendEnterWorldState(server, ch)
		}
	}()

	found := false
	for i := 0; i < 30 && !found; i++ {
		cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
		if err != nil {
			t.Fatalf("decode frame error = %v", err)
		}
		if cmd.Ident == mir176.SMTurn && cmd.Recog >= 100000 {
			if cmd.Series != 4 {
				t.Fatalf("nearby monster SM_TURN series = %d, want 4", cmd.Series)
			}
			assertMonsterTurnBody(t, body, "鹿/255", int32(11|161<<16))
			found = true
		}
	}
	server.Close()
	<-done

	if !found {
		t.Fatalf("nearby monster SM_TURN was not sent during enter-world")
	}
}

func TestHandleTakeOnItemEquipsAndRefreshesAbility(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Gold = 123
	ch.BagItems = []storage.UserItem{{ItemID: testWeaponID, MakeIndex: 1}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleTakeOnItem(server, &ch, mir176.Command{Ident: mir176.CMTakeOnItem, Recog: 1, Param: world.SlotWeapon}, WireString(t, testWeaponID))
	}()

	abilityCmd, abilityBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_ABILITY frame error = %v", err)
	}
	if abilityCmd.Ident != mir176.SMAbility {
		t.Fatalf("first frame ident = %d, want SM_ABILITY (%d)", abilityCmd.Ident, mir176.SMAbility)
	}
	if abilityCmd.Recog != 123 {
		t.Fatalf("SM_ABILITY Recog = %d, want 123", abilityCmd.Recog)
	}
	subAbilityCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_SUBABILITY frame error = %v", err)
	}
	if subAbilityCmd.Ident != mir176.SMSubAbility {
		t.Fatalf("second frame ident = %d, want SM_SUBABILITY (%d)", subAbilityCmd.Ident, mir176.SMSubAbility)
	}
	if subAbilityCmd.Param != makeWord(5, 15) {
		t.Fatalf("SM_SUBABILITY Param = %d, want warrior/wizard default 5/15", subAbilityCmd.Param)
	}
	if subAbilityCmd.Recog != 1 || subAbilityCmd.Param != 0x0f05 || subAbilityCmd.Tag != 0 || subAbilityCmd.Series != 0 {
		t.Fatalf("SM_SUBABILITY = %+v, want default reference values", subAbilityCmd)
	}
	okCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_TAKEON_OK frame error = %v", err)
	}
	if okCmd.Ident != mir176.SMTakeOnOK {
		t.Fatalf("third frame ident = %d, want SM_TAKEON_OK (%d)", okCmd.Ident, mir176.SMTakeOnOK)
	}
	featureCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_FEATURECHANGED frame error = %v", err)
	}
	if featureCmd.Ident != mir176.SMFeatureChanged {
		t.Fatalf("fourth frame ident = %d, want SM_FEATURECHANGED (%d)", featureCmd.Ident, mir176.SMFeatureChanged)
	}
	<-done

	if ch.EquippedItems[SlotWeapon].ItemID != testWeaponID {
		t.Fatalf("EquippedItems[SlotWeapon].ItemID = %q, want %s", ch.EquippedItems[SlotWeapon].ItemID, testWeaponID)
	}
	if okCmd.Recog != int32(s.world.HumanFeatureForCharacter(ch)) {
		t.Fatalf("SM_TAKEON_OK Recog = %d, want feature %d", okCmd.Recog, s.world.HumanFeatureForCharacter(ch))
	}
	body, err := mir176.DecodePlain6Payload(abilityBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	// DC is packed low|high<<8 (min~max): base warrior L1 (1~1) plus 木剑's
	// min_attack/max_attack (2~5) bonus is 3~6.
	dc := binary.LittleEndian.Uint16(body[6:8])
	if lo, hi := byte(dc), byte(dc>>8); lo != 3 || hi != 6 {
		t.Fatalf("DC in ability payload = %d (lo=%d hi=%d), want lo=3 hi=6", dc, lo, hi)
	}
}

func TestHandleTakeOnItemSwapsPreviousItemBackToBag(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	setEquippedItem(&ch, SlotWeapon, storage.UserItem{ItemID: "铁剑"})
	ch.BagItems = []storage.UserItem{{ItemID: testWeaponID, MakeIndex: 1}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleTakeOnItem(server, &ch, mir176.Command{Ident: mir176.CMTakeOnItem, Recog: 1, Param: world.SlotWeapon}, WireString(t, testWeaponID))
	}()

	addCmd, addBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_ADDITEM frame error = %v", err)
	}
	if addCmd.Ident != mir176.SMAddItem {
		t.Fatalf("first frame ident = %d, want SM_ADDITEM (%d)", addCmd.Ident, mir176.SMAddItem)
	}
	if addCmd.Recog != int32(world.CharacterActorID(ch)) {
		t.Fatalf("swap add item recog = %d, want actor id %d", addCmd.Recog, world.CharacterActorID(ch))
	}
	if addCmd.Series != 1 {
		t.Fatalf("swap add item series = %d, want 1", addCmd.Series)
	}
	if got := decodeClientItemName(addBody); got != "铁剑" {
		t.Fatalf("swap add item name = %q, want 铁剑", got)
	}
	abilityCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_ABILITY frame error = %v", err)
	}
	if abilityCmd.Ident != mir176.SMAbility {
		t.Fatalf("second frame ident = %d, want SM_ABILITY (%d)", abilityCmd.Ident, mir176.SMAbility)
	}
	subAbilityCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_SUBABILITY frame error = %v", err)
	}
	if subAbilityCmd.Ident != mir176.SMSubAbility {
		t.Fatalf("third frame ident = %d, want SM_SUBABILITY (%d)", subAbilityCmd.Ident, mir176.SMSubAbility)
	}
	okCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_TAKEON_OK frame error = %v", err)
	}
	if okCmd.Ident != mir176.SMTakeOnOK {
		t.Fatalf("fourth frame ident = %d, want SM_TAKEON_OK (%d)", okCmd.Ident, mir176.SMTakeOnOK)
	}
	featureCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_FEATURECHANGED frame error = %v", err)
	}
	if featureCmd.Ident != mir176.SMFeatureChanged {
		t.Fatalf("fifth frame ident = %d, want SM_FEATURECHANGED (%d)", featureCmd.Ident, mir176.SMFeatureChanged)
	}
	<-done
}

func TestHandleTakeOnItemRejectsUnknownItem(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleTakeOnItem(server, &ch, mir176.Command{Ident: mir176.CMTakeOnItem, Param: world.SlotWeapon}, WireString(t, "does_not_exist"))
	}()

	cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode frame error = %v", err)
	}
	if cmd.Ident != mir176.SMTakeOnFail {
		t.Fatalf("ident = %d, want SM_TAKEON_FAIL (%d)", cmd.Ident, mir176.SMTakeOnFail)
	}
	<-done
	if ch.EquippedItems[SlotWeapon].ItemID != "" {
		t.Fatalf("EquippedItems[SlotWeapon].ItemID = %q, want empty", ch.EquippedItems[SlotWeapon].ItemID)
	}
}

func TestHandleTakeOffItemUnequipsAndRefreshesAbility(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Gold = 456
	setEquippedItem(&ch, SlotWeapon, storage.UserItem{ItemID: testWeaponID})
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleTakeOffItem(server, &ch, mir176.Command{Ident: mir176.CMTakeOffItem, Param: world.SlotWeapon}, WireString(t, testWeaponID))
	}()

	addCmd, addBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_ADDITEM frame error = %v", err)
	}
	if addCmd.Ident != mir176.SMAddItem {
		t.Fatalf("first frame ident = %d, want SM_ADDITEM (%d)", addCmd.Ident, mir176.SMAddItem)
	}
	if addCmd.Recog != int32(world.CharacterActorID(ch)) {
		t.Fatalf("add item recog = %d, want actor id %d", addCmd.Recog, world.CharacterActorID(ch))
	}
	if addCmd.Series != 1 {
		t.Fatalf("add item series = %d, want 1", addCmd.Series)
	}
	if got := decodeClientItemName(addBody); got != testWeaponID {
		t.Fatalf("add item name = %q, want %q", got, testWeaponID)
	}
	abilityCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_ABILITY frame error = %v", err)
	}
	if abilityCmd.Ident != mir176.SMAbility {
		t.Fatalf("second frame ident = %d, want SM_ABILITY (%d)", abilityCmd.Ident, mir176.SMAbility)
	}
	if abilityCmd.Recog != 456 {
		t.Fatalf("SM_ABILITY Recog = %d, want 456", abilityCmd.Recog)
	}
	subAbilityCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_SUBABILITY frame error = %v", err)
	}
	if subAbilityCmd.Ident != mir176.SMSubAbility {
		t.Fatalf("third frame ident = %d, want SM_SUBABILITY (%d)", subAbilityCmd.Ident, mir176.SMSubAbility)
	}
	if subAbilityCmd.Recog != 1 || subAbilityCmd.Param != 0x0f05 || subAbilityCmd.Tag != 0 || subAbilityCmd.Series != 0 {
		t.Fatalf("SM_SUBABILITY = %+v, want default reference values", subAbilityCmd)
	}
	okCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_TAKEOFF_OK frame error = %v", err)
	}
	if okCmd.Ident != mir176.SMTakeOffOK {
		t.Fatalf("fourth frame ident = %d, want SM_TAKEOFF_OK (%d)", okCmd.Ident, mir176.SMTakeOffOK)
	}
	featureCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_FEATURECHANGED frame error = %v", err)
	}
	if featureCmd.Ident != mir176.SMFeatureChanged {
		t.Fatalf("fifth frame ident = %d, want SM_FEATURECHANGED (%d)", featureCmd.Ident, mir176.SMFeatureChanged)
	}
	<-done

	if ch.EquippedItems[SlotWeapon].ItemID != "" {
		t.Fatalf("EquippedItems[SlotWeapon].ItemID = %q, want empty", ch.EquippedItems[SlotWeapon].ItemID)
	}
	if okCmd.Recog != int32(s.world.HumanFeatureForCharacter(ch)) {
		t.Fatalf("SM_TAKEOFF_OK Recog = %d, want feature %d", okCmd.Recog, s.world.HumanFeatureForCharacter(ch))
	}
}

func TestHandleTakeOffItemRejectsMismatchedItemName(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	setEquippedItem(&ch, SlotWeapon, storage.UserItem{ItemID: testWeaponID})
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleTakeOffItem(server, &ch, mir176.Command{Ident: mir176.CMTakeOffItem, Param: world.SlotWeapon}, WireString(t, "not_the_weapon"))
	}()

	cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode frame error = %v", err)
	}
	if cmd.Ident != mir176.SMTakeOffFail {
		t.Fatalf("ident = %d, want SM_TAKEOFF_FAIL (%d)", cmd.Ident, mir176.SMTakeOffFail)
	}
	<-done
	if ch.EquippedItems[SlotWeapon].ItemID != testWeaponID {
		t.Fatalf("EquippedItems[SlotWeapon].ItemID = %q, want %s", ch.EquippedItems[SlotWeapon].ItemID, testWeaponID)
	}
}

func TestSendEnterWorldStateCarriesGoldInAbility(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Gold = 321
	ch.PremiumGold = 654
	ch.SoftVersionDate = 20020522
	ch.AllowGroup = true
	if !s.world.SetMapLight(testMapID, 7) {
		t.Fatalf("SetMapLight(%s) failed", testMapID)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.sendEnterWorldState(server, ch)
	}()

	newMapCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_NEWMAP frame error = %v", err)
	}
	if newMapCmd.Ident != mir176.SMNewMap {
		t.Fatalf("first frame ident = %d, want SM_NEWMAP (%d)", newMapCmd.Ident, mir176.SMNewMap)
	}
	changeLightCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_CHANGELIGHT frame error = %v", err)
	}
	if changeLightCmd.Ident != mir176.SMChangeLight {
		t.Fatalf("second frame ident = %d, want SM_CHANGELIGHT (%d)", changeLightCmd.Ident, mir176.SMChangeLight)
	}
	if changeLightCmd.Param != 7 || changeLightCmd.Tag != 500 {
		t.Fatalf("SM_CHANGELIGHT = %+v, want light=7 clientkey=500", changeLightCmd)
	}
	logonCmd, logonBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_LOGON frame error = %v", err)
	}
	if logonCmd.Ident != mir176.SMLogon {
		t.Fatalf("third frame ident = %d, want SM_LOGON (%d)", logonCmd.Ident, mir176.SMLogon)
	}
	if logonCmd.Series != makeWord(byte(ch.Dir), 7) {
		t.Fatalf("SM_LOGON Series = %d, want direction/light combo", logonCmd.Series)
	}
	assertMessageBodyWL(t, logonBody, s.world.HumanFeatureForCharacter(ch), 0, 1, 0)
	featureCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_FEATURECHANGED frame error = %v", err)
	}
	if featureCmd.Ident != mir176.SMFeatureChanged {
		t.Fatalf("fourth frame ident = %d, want SM_FEATURECHANGED (%d)", featureCmd.Ident, mir176.SMFeatureChanged)
	}
	if featureCmd.Series != 0 {
		t.Fatalf("SM_FEATURECHANGED Series = %d, want 0", featureCmd.Series)
	}
	serverConfigCmd, serverConfigBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_SERVERCONFIG frame error = %v", err)
	}
	if serverConfigCmd.Ident != mir176.SMServerConfig {
		t.Fatalf("fifth frame ident = %d, want SM_SERVERCONFIG (%d)", serverConfigCmd.Ident, mir176.SMServerConfig)
	}
	if serverConfigCmd.Param != 0 {
		t.Fatalf("SM_SERVERCONFIG Param = %d, want 0", serverConfigCmd.Param)
	}
	if serverConfigCmd.Recog != 0 || serverConfigCmd.Tag != 0 || serverConfigCmd.Series != 0 {
		t.Fatalf("SM_SERVERCONFIG header = %+v, want zeroed mirbeta header", serverConfigCmd)
	}
	serverConfigDecoded, err := mir176.DecodePlain6Payload(serverConfigBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload(SM_SERVERCONFIG) error = %v", err)
	}
	if len(serverConfigDecoded) != 18 {
		t.Fatalf("SM_SERVERCONFIG decoded body len = %d, want 18", len(serverConfigDecoded))
	}
	if serverConfigDecoded[0] != 17 || serverConfigDecoded[1] != 1 || serverConfigDecoded[2] != 1 || serverConfigDecoded[3] != 1 || serverConfigDecoded[4] != 0 || serverConfigDecoded[5] != 1 || serverConfigDecoded[6] != 1 || serverConfigDecoded[7] != 1 || serverConfigDecoded[8] != 0 || serverConfigDecoded[9] != 0 || serverConfigDecoded[10] != 0 || serverConfigDecoded[11] != 0 || serverConfigDecoded[12] != 0 || serverConfigDecoded[13] != 0 || serverConfigDecoded[14] != 0 || serverConfigDecoded[15] != 0 || serverConfigDecoded[16] != 1 || serverConfigDecoded[17] != 0 {
		t.Fatalf("SM_SERVERCONFIG decoded body = % x, want reference defaults", serverConfigDecoded)
	}
	userNameCmd, userNameBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_USERNAME frame error = %v", err)
	}
	if userNameCmd.Ident != mir176.SMUserName {
		t.Fatalf("seventh frame ident = %d, want SM_USERNAME (%d)", userNameCmd.Ident, mir176.SMUserName)
	}
	userNameDecoded, err := mir176.DecodePlain6Payload(userNameBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload(SM_USERNAME) error = %v", err)
	}
	if got := DecodeString(userNameDecoded); got != ch.Name {
		t.Fatalf("SM_USERNAME body = %q, want %q", got, ch.Name)
	}
	if userNameCmd.Param != 255 {
		t.Fatalf("SM_USERNAME Param = %d, want name color 255", userNameCmd.Param)
	}
	areaStateCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_AREASTATE frame error = %v", err)
	}
	if areaStateCmd.Ident != mir176.SMAreaState {
		t.Fatalf("eighth frame ident = %d, want SM_AREASTATE (%d)", areaStateCmd.Ident, mir176.SMAreaState)
	}
	if areaStateCmd.Recog != 0 {
		t.Fatalf("SM_AREASTATE Recog = %d, want 0", areaStateCmd.Recog)
	}
	mapDescCmd, mapDescBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_MAPDESCRIPTION frame error = %v", err)
	}
	if mapDescCmd.Ident != mir176.SMMapDescription {
		t.Fatalf("ninth frame ident = %d, want SM_MAPDESCRIPTION (%d)", mapDescCmd.Ident, mir176.SMMapDescription)
	}
	mapDescDecoded, err := mir176.DecodePlain6Payload(mapDescBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload(SM_MAPDESCRIPTION) error = %v", err)
	}
	if got := DecodeString(mapDescDecoded); got != s.world.MapName(ch.MapID) {
		t.Fatalf("SM_MAPDESCRIPTION body = %q, want %q", got, s.world.MapName(ch.MapID))
	}
	goldNameCmd, goldNameBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_GAMEGOLDNAME frame error = %v", err)
	}
	if goldNameCmd.Ident != mir176.SMGameGoldName {
		t.Fatalf("tenth frame ident = %d, want SM_GAMEGOLDNAME (%d)", goldNameCmd.Ident, mir176.SMGameGoldName)
	}
	if goldNameCmd.Recog != int32(ch.PremiumGold) {
		t.Fatalf("SM_GAMEGOLDNAME Recog = %d, want %d", goldNameCmd.Recog, ch.PremiumGold)
	}
	if goldNameCmd.Param != uint16(ch.PremiumPoint) || goldNameCmd.Tag != uint16(uint32(ch.PremiumPoint)>>16) {
		t.Fatalf("SM_GAMEGOLDNAME game point = %d/%d, want %d", goldNameCmd.Param, goldNameCmd.Tag, ch.PremiumPoint)
	}
	goldNameDecoded, err := mir176.DecodePlain6Payload(goldNameBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload(SM_GAMEGOLDNAME) error = %v", err)
	}
	if got := DecodeString(goldNameDecoded); got != "元宝\r游戏点" {
		t.Fatalf("SM_GAMEGOLDNAME body = %q, want reference default names", got)
	}
	_ = client.Close()
	<-done
}

func TestSendEnterWorldStateOmitsVersionOnlyPacketsWhenSoftVersionZero(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.sendEnterWorldState(server, ch)
	}()

	for i := 0; i < 8; i++ {
		if _, _, err := decodeMessageLikeClient(readFrame(t, client)); err != nil {
			t.Fatalf("decode frame %d error = %v", i+1, err)
		}
	}
	_ = client.Close()
	<-done
}

func TestSendSpaceMoveStateSendsMapResetAndShow(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.sendSpaceMoveState(server, ch)
	}()

	hideCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_SPACEMOVE_HIDE frame error = %v", err)
	}
	if hideCmd.Ident != mir176.SMSpacemoveHide {
		t.Fatalf("first frame ident = %d, want SM_SPACEMOVE_HIDE (%d)", hideCmd.Ident, mir176.SMSpacemoveHide)
	}
	clearCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_CLEAROBJECTS frame error = %v", err)
	}
	if clearCmd.Ident != mir176.SMClearObjects {
		t.Fatalf("second frame ident = %d, want SM_CLEAROBJECTS (%d)", clearCmd.Ident, mir176.SMClearObjects)
	}
	changeMapCmd, changeMapBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_CHANGEMAP frame error = %v", err)
	}
	if changeMapCmd.Ident != mir176.SMChangeMap {
		t.Fatalf("third frame ident = %d, want SM_CHANGEMAP (%d)", changeMapCmd.Ident, mir176.SMChangeMap)
	}
	changeMapDecoded, err := mir176.DecodePlain6Payload(changeMapBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload(SM_CHANGEMAP) error = %v", err)
	}
	if got := DecodeString(changeMapDecoded); got != ch.MapID {
		t.Fatalf("SM_CHANGEMAP body = %q, want %q", got, ch.MapID)
	}
	areaCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_AREASTATE frame error = %v", err)
	}
	if areaCmd.Ident != mir176.SMAreaState {
		t.Fatalf("fourth frame ident = %d, want SM_AREASTATE (%d)", areaCmd.Ident, mir176.SMAreaState)
	}
	mapDescCmd, mapDescBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_MAPDESCRIPTION frame error = %v", err)
	}
	if mapDescCmd.Ident != mir176.SMMapDescription {
		t.Fatalf("fifth frame ident = %d, want SM_MAPDESCRIPTION (%d)", mapDescCmd.Ident, mir176.SMMapDescription)
	}
	mapDescDecoded, err := mir176.DecodePlain6Payload(mapDescBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload(SM_MAPDESCRIPTION) error = %v", err)
	}
	if got := DecodeString(mapDescDecoded); got != s.world.MapName(ch.MapID) {
		t.Fatalf("SM_MAPDESCRIPTION body = %q, want %q", got, s.world.MapName(ch.MapID))
	}
	showCmd, showBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_SPACEMOVE_SHOW frame error = %v", err)
	}
	if showCmd.Ident != mir176.SMSpacemoveShow {
		t.Fatalf("sixth frame ident = %d, want SM_SPACEMOVE_SHOW (%d)", showCmd.Ident, mir176.SMSpacemoveShow)
	}
	showDecoded, err := mir176.DecodePlain6Payload(showBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload(SM_SPACEMOVE_SHOW) error = %v", err)
	}
	if len(showDecoded) != 8 {
		t.Fatalf("SM_SPACEMOVE_SHOW body len = %d, want 8", len(showDecoded))
	}
	_ = client.Close()
	<-done
}

func TestSendInitialLoginStateMatchesMirbetaOrder(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Gold = 321
	ch.PremiumGold = 654
	ch.PremiumPoint = 654
	ch.AttackMode = 3
	ch.BonusPoint = 12
	ch.BonusAbil = storage.BonusAbility{DC: 1, MC: 2, SC: 3, AC: 4, MAC: 5, HP: 6, MP: 7, Hit: 8, Speed: 9, Reserved: 10}
	setEquippedItem(&ch, SlotWeapon, storage.UserItem{ItemID: testWeaponID})
	ch.Skills = []string{"火球术"}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.sendInitialLoginState(server, ch)
	}()

	abilityCmd, abilityBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_ABILITY frame error = %v", err)
	}
	if abilityCmd.Ident != mir176.SMAbility {
		t.Fatalf("first frame ident = %d, want SM_ABILITY (%d)", abilityCmd.Ident, mir176.SMAbility)
	}
	abilityDecoded, err := mir176.DecodePlain6Payload(abilityBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload(SM_ABILITY) error = %v", err)
	}
	if got := int(abilityDecoded[0]); got != 1 {
		t.Fatalf("SM_ABILITY level = %d, want 1", got)
	}
	if hp := binary.LittleEndian.Uint16(abilityDecoded[12:14]); hp == 0 {
		t.Fatalf("SM_ABILITY HP = %d, want non-zero", hp)
	}
	if mp := binary.LittleEndian.Uint16(abilityDecoded[14:16]); mp == 0 {
		t.Fatalf("SM_ABILITY MP = %d, want non-zero", mp)
	}

	subAbilityCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_SUBABILITY frame error = %v", err)
	}
	if subAbilityCmd.Ident != mir176.SMSubAbility {
		t.Fatalf("second frame ident = %d, want SM_SUBABILITY (%d)", subAbilityCmd.Ident, mir176.SMSubAbility)
	}
	if subAbilityCmd.Recog != 1 || subAbilityCmd.Param != 0x0f05 || subAbilityCmd.Tag != 0 || subAbilityCmd.Series != 0 {
		t.Fatalf("SM_SUBABILITY = %+v, want default reference values", subAbilityCmd)
	}

	dayCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_DAYCHANGING frame error = %v", err)
	}
	if dayCmd.Ident != mir176.SMDayChanging {
		t.Fatalf("fourth frame ident = %d, want SM_DAYCHANGING (%d)", dayCmd.Ident, mir176.SMDayChanging)
	}
	if dayCmd.Recog != 0 || dayCmd.Param != 0 || dayCmd.Tag != 0 || dayCmd.Series != 0 {
		t.Fatalf("SM_DAYCHANGING header = %+v, want default mirbeta day state", dayCmd)
	}

	useItemsCmd, useItemsBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_SENDUSEITEMS frame error = %v", err)
	}
	if useItemsCmd.Ident != mir176.SMSendUseItems {
		t.Fatalf("fifth frame ident = %d, want SM_SENDUSEITEMS (%d)", useItemsCmd.Ident, mir176.SMSendUseItems)
	}
	if !bytes.Contains(useItemsBody, []byte("1/")) {
		t.Fatalf("SM_SENDUSEITEMS body = %q, want weapon slot entry", useItemsBody)
	}

	bagCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_BAGITEMS frame error = %v", err)
	}
	if bagCmd.Ident != mir176.SMBagItems {
		t.Fatalf("sixth frame ident = %d, want SM_BAGITEMS (%d)", bagCmd.Ident, mir176.SMBagItems)
	}

	useMagicCmd, useMagicBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_SENDMYMAGIC frame error = %v", err)
	}
	if useMagicCmd.Ident != mir176.SMSendMyMagic {
		t.Fatalf("seventh frame ident = %d, want SM_SENDMYMAGIC (%d)", useMagicCmd.Ident, mir176.SMSendMyMagic)
	}
	if useMagicCmd.Series != 1 {
		t.Fatalf("SM_SENDMYMAGIC Series = %d, want 1", useMagicCmd.Series)
	}
	useMagicParts := bytes.Split(useMagicBody, []byte("/"))
	if len(useMagicParts) != 2 {
		t.Fatalf("SM_SENDMYMAGIC parts = %d, want 2 with trailing slash", len(useMagicParts))
	}
	useMagicDecoded, err := mir176.DecodePlain6Payload(useMagicParts[0])
	if err != nil {
		t.Fatalf("DecodePlain6Payload(SM_SENDMYMAGIC) error = %v", err)
	}
	if len(useMagicDecoded) != 82 {
		t.Fatalf("SM_SENDMYMAGIC decoded len = %d, want 82", len(useMagicDecoded))
	}
	if useMagicDecoded[0] != 0 || useMagicDecoded[1] != 0 || useMagicDecoded[2] != 0 || useMagicDecoded[3] != 0 {
		t.Fatalf("SM_SENDMYMAGIC header = % x, want zeroed client magic header", useMagicDecoded[:4])
	}
	if got := binary.LittleEndian.Uint32(useMagicDecoded[4:8]); got != 0 {
		t.Fatalf("SM_SENDMYMAGIC curtrain = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint16(useMagicDecoded[8:10]); got != 1 {
		t.Fatalf("SM_SENDMYMAGIC magic id = %d, want 1", got)
	}
	if got := int(useMagicDecoded[10]); got != 6 {
		t.Fatalf("SM_SENDMYMAGIC name length = %d, want 6", got)
	}
	if name, err := simplifiedchinese.GB18030.NewDecoder().String(string(useMagicDecoded[11 : 11+int(useMagicDecoded[10])])); err != nil || name != "火球术" {
		t.Fatalf("SM_SENDMYMAGIC name = %q err=%v, want 火球术", name, err)
	}
	if useMagicDecoded[25] != 0 || useMagicDecoded[26] != 0 || useMagicDecoded[27] != 0 {
		t.Fatalf("SM_SENDMYMAGIC effect header = % x, want zeroed", useMagicDecoded[25:28])
	}
	if got := binary.LittleEndian.Uint16(useMagicDecoded[28:30]); got != 4 {
		t.Fatalf("SM_SENDMYMAGIC spell = %d, want 4", got)
	}
	if got := binary.LittleEndian.Uint16(useMagicDecoded[30:32]); got != 8 {
		t.Fatalf("SM_SENDMYMAGIC power = %d, want 8", got)
	}
	if got := useMagicDecoded[32]; got != 7 {
		t.Fatalf("SM_SENDMYMAGIC train level = %d, want 7", got)
	}
	if got := binary.LittleEndian.Uint32(useMagicDecoded[36:40]); got != 100 {
		t.Fatalf("SM_SENDMYMAGIC max train = %d, want 100", got)
	}
	if got := useMagicDecoded[52]; got != 3 {
		t.Fatalf("SM_SENDMYMAGIC train lv = %d, want 3", got)
	}
	if got := useMagicDecoded[53]; got != 1 {
		t.Fatalf("SM_SENDMYMAGIC job = %d, want 1", got)
	}

	_ = client.Close()
	<-done
}

func TestSendEquippedItemsSkipsEmptyState(t *testing.T) {
	s := newTestServer(t)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.sendEquippedItems(server, storage.Character{})
	}()

	_ = client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	buf := make([]byte, 1)
	if _, err := client.Read(buf); err == nil {
		t.Fatal("sendEquippedItems() wrote data for empty state, want no packet")
	}
	<-done
}

func TestSendEnterWorldNormalizesBagMakeIndexes(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BagItems = []storage.UserItem{{ItemID: testHPItemID}}
	if err := s.store.SaveCharacter(ch); err != nil {
		t.Fatalf("SaveCharacter() error = %v", err)
	}

	loaded, ok := s.sendEnterWorld(nil, RunLogin{Account: "test", CharName: "tester"})
	if !ok {
		t.Fatal("sendEnterWorld() = false, want true")
	}
	if countBagItems(loaded.BagItems) != 1 {
		t.Fatalf("loaded bag len = %d, want 1", countBagItems(loaded.BagItems))
	}
	if loaded.BagItems[0].MakeIndex == 0 {
		t.Fatalf("loaded bag makeindex = %d, want non-zero", loaded.BagItems[0].MakeIndex)
	}
}

func TestSendLevelUpRefreshesLevelExpAndAbilities(t *testing.T) {
	s := newTestServer(t)
	ch := storage.Character{
		ID:          "player-1",
		Account:     "test",
		Name:        "tester",
		Class:       "warrior",
		Gold:        123,
		PremiumGold: 456,
		Level:       2,
		Experience:  7,
		HP:          31,
		MaxHP:       31,
		MP:          19,
		MaxMP:       19,
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.sendLevelUp(server, ch)
	}()
	stats := s.world.AbilityStats(ch)

	levelCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_LEVELUP frame error = %v", err)
	}
	if levelCmd.Ident != mir176.SMLevelUp || levelCmd.Recog != int32(ch.Experience) || levelCmd.Param != uint16(ch.Level) {
		t.Fatalf("SM_LEVELUP = %+v, want exp=%d level=%d", levelCmd, ch.Experience, ch.Level)
	}

	abilityCmd, abilityBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_ABILITY frame error = %v", err)
	}
	if abilityCmd.Ident != mir176.SMAbility {
		t.Fatalf("second frame ident = %d, want SM_ABILITY (%d)", abilityCmd.Ident, mir176.SMAbility)
	}
	abilityDecoded, err := mir176.DecodePlain6Payload(abilityBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload(SM_ABILITY) error = %v", err)
	}
	if got := int(abilityDecoded[0]); got != ch.Level {
		t.Fatalf("SM_ABILITY level = %d, want %d", got, ch.Level)
	}
	if got := binary.LittleEndian.Uint16(abilityDecoded[12:14]); got != uint16(stats.HP) {
		t.Fatalf("SM_ABILITY HP = %d, want %d", got, stats.HP)
	}

	subAbilityCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_SUBABILITY frame error = %v", err)
	}
	if subAbilityCmd.Ident != mir176.SMSubAbility {
		t.Fatalf("third frame ident = %d, want SM_SUBABILITY (%d)", subAbilityCmd.Ident, mir176.SMSubAbility)
	}

	<-done
}

func TestSubAbilityCommandUsesTaoistSpeed(t *testing.T) {
	cmd := SubAbilityCommand("taoist")
	if cmd.Param != makeWord(5, 18) {
		t.Fatalf("SubAbilityCommand(taoist).Param = %d, want 5/18", cmd.Param)
	}
	if cmd.Recog != 1 || cmd.Tag != 0 || cmd.Series != 0 {
		t.Fatalf("SubAbilityCommand(taoist) = %+v, want mirbeta default header", cmd)
	}
}

func TestHandleQueryUserNameRepliesWithNameOrGhost(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}

	t.Run("name", func(t *testing.T) {
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()

		done := make(chan struct{})
		go func() {
			defer close(done)
			s.handleQueryUserName(server, &ch, mir176.Command{Ident: mir176.CMQueryUserName, Recog: world.CharacterActorID(ch), Param: uint16(ch.X), Tag: uint16(ch.Y)})
		}()

		cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
		if err != nil {
			t.Fatalf("decode reply error = %v", err)
		}
		if cmd.Ident != mir176.SMUserName {
			t.Fatalf("reply ident = %d, want SM_USERNAME (%d)", cmd.Ident, mir176.SMUserName)
		}
		if cmd.Recog != world.CharacterActorID(ch) || cmd.Param != 255 {
			t.Fatalf("reply header = %+v, want recog=%d param=255", cmd, world.CharacterActorID(ch))
		}
		decoded, err := mir176.DecodePlain6Payload(body)
		if err != nil {
			t.Fatalf("DecodePlain6Payload() error = %v", err)
		}
		if got := DecodeString(decoded); got != ch.Name {
			t.Fatalf("reply body = %q, want %q", got, ch.Name)
		}
		<-done
	})

	t.Run("ghost", func(t *testing.T) {
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()

		done := make(chan struct{})
		go func() {
			defer close(done)
			s.handleQueryUserName(server, &ch, mir176.Command{Ident: mir176.CMQueryUserName, Recog: world.CharacterActorID(ch), Param: uint16(ch.X + 2), Tag: uint16(ch.Y + 2)})
		}()

		cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
		if err != nil {
			t.Fatalf("decode reply error = %v", err)
		}
		if cmd.Ident != mir176.SMGhost {
			t.Fatalf("reply ident = %d, want SM_GHOST (%d)", cmd.Ident, mir176.SMGhost)
		}
		if cmd.Recog != world.CharacterActorID(ch) || int(cmd.Param) != ch.X+2 || int(cmd.Tag) != ch.Y+2 {
			t.Fatalf("ghost reply = %+v, want recog=%d x=%d y=%d", cmd, world.CharacterActorID(ch), ch.X+2, ch.Y+2)
		}
		<-done
	})
}

func TestHandleQueryUserStateRepliesWithState(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	setEquippedItem(&ch, SlotWeapon, storage.UserItem{ItemID: testWeaponID})

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleQueryUserState(server, &ch, mir176.Command{Ident: mir176.CMQueryUserState, Recog: world.CharacterActorID(ch), Param: uint16(ch.X), Tag: uint16(ch.Y)})
	}()

	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode reply error = %v", err)
	}
	if cmd.Ident != mir176.SMSendUserState {
		t.Fatalf("reply ident = %d, want SM_SENDUSERSTATE (%d)", cmd.Ident, mir176.SMSendUserState)
	}
	decoded, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if got := int32(binary.LittleEndian.Uint32(decoded[0:4])); got != s.world.HumanFeatureForCharacter(ch) {
		t.Fatalf("feature = %d, want %d", got, s.world.HumanFeatureForCharacter(ch))
	}
	nameLen := int(decoded[4])
	if got := DecodeString(decoded[5 : 5+nameLen]); got != ch.Name {
		t.Fatalf("user name = %q, want %q", got, ch.Name)
	}
	if got := binary.LittleEndian.Uint32(decoded[20:24]); got != 255 {
		t.Fatalf("name color = %d, want 255", got)
	}
	itemBodyLen := len(ClientItemBody(data.StdItem{}, [14]byte{}, 0, 0, 0))
	weaponSlotOffset := 60 + itemBodyLen*world.SlotWeapon
	weaponBody := decoded[weaponSlotOffset:]
	if len(weaponBody) == 0 || int(weaponBody[0]) <= 0 {
		t.Fatalf("weapon slot body missing: %v", weaponBody)
	}
	weaponNameLen := int(weaponBody[0])
	if got := DecodeString(weaponBody[1 : 1+weaponNameLen]); got != testWeaponID {
		t.Fatalf("weapon slot name = %q, want %q", got, testWeaponID)
	}
	<-done
}

func TestHandleDropItemBroadcastsAppearAndRepliesSuccess(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BagItems = []storage.UserItem{{ItemID: testWeaponID, MakeIndex: 1}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleDropItem(server, &ch, mir176.Command{Ident: mir176.CMDropItem, Recog: 1}, WireString(t, testWeaponID))
	}()

	showCmd, showBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode drop show frame error = %v", err)
	}
	if showCmd.Ident != mir176.SMItemShow {
		t.Fatalf("first frame ident = %d, want SM_ITEMSHOW (%d)", showCmd.Ident, mir176.SMItemShow)
	}
	showName, err := mir176.DecodePlain6Payload(showBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if got := DecodeString(showName); got != testWeaponID {
		t.Fatalf("drop show name = %q, want %q", got, testWeaponID)
	}
	weightCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode drop weight frame error = %v", err)
	}
	if weightCmd.Ident != mir176.SMWeightChanged {
		t.Fatalf("second frame ident = %d, want SM_WEIGHTCHANGED (%d)", weightCmd.Ident, mir176.SMWeightChanged)
	}
	dx := int(showCmd.Param) - x
	dy := int(showCmd.Tag) - y
	if dx < -3 || dx > 3 || dy < -3 || dy > 3 {
		t.Fatalf("drop show position = (%d,%d), want near (%d,%d)", showCmd.Param, showCmd.Tag, x, y)
	}
	successCmd, successBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode drop success frame error = %v", err)
	}
	if successCmd.Ident != mir176.SMDropItemSuccess {
		t.Fatalf("third frame ident = %d, want SM_DROPITEM_SUCCESS (%d)", successCmd.Ident, mir176.SMDropItemSuccess)
	}
	successName, err := mir176.DecodePlain6Payload(successBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if got := DecodeString(successName); got != testWeaponID {
		t.Fatalf("drop success body = %q, want %q", got, testWeaponID)
	}
	<-done
	for _, entry := range ch.BagItems {
		if entry.ItemID == testWeaponID {
			t.Fatalf("expected %s removed from bag after drop, got %+v", testWeaponID, ch.BagItems)
		}
	}
	_, drops := s.world.Snapshot(ch.MapID)
	found := false
	for _, drop := range drops {
		if drop.ItemID == testWeaponID && drop.Count == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected dropped %s on the ground, got %+v", testWeaponID, drops)
	}
	stats := s.world.AbilityStats(ch)
	if weightCmd.Recog != int32(stats.Weight) || weightCmd.Param != uint16(stats.WearWeight) || weightCmd.Tag != uint16(stats.HandWeight) {
		t.Fatalf("SM_WEIGHTCHANGED = %+v, want weight=%d wear=%d hand=%d", weightCmd, stats.Weight, stats.WearWeight, stats.HandWeight)
	}
}

func TestHandleDropItemRejectsMismatchedItemName(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleDropItem(server, &ch, mir176.Command{Ident: mir176.CMDropItem, Recog: 1}, WireString(t, "not_the_weapon"))
	}()

	cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode frame error = %v", err)
	}
	if cmd.Ident != mir176.SMDropItemFail {
		t.Fatalf("ident = %d, want SM_DROPITEM_FAIL (%d)", cmd.Ident, mir176.SMDropItemFail)
	}
	<-done
	found := false
	for _, entry := range ch.BagItems {
		if entry.ItemID == testWeaponID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %s to remain in bag, got %+v", testWeaponID, ch.BagItems)
	}
	_, drops := s.world.Snapshot(ch.MapID)
	for _, drop := range drops {
		if drop.ItemID == testWeaponID {
			t.Fatalf("unexpected drop created: %+v", drop)
		}
	}
}

func TestHandlePickupHidesGroundItem(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch, drop, err := s.world.DropItem(ch, testWeaponID)
	if err != nil {
		t.Fatalf("DropItem() error = %v", err)
	}
	ch.X, ch.Y = drop.X, drop.Y
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	clientState := s.registerClient(server, ch)
	clientState.visibleDrops = map[string]world.GroundDrop{drop.ID: drop}

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handlePickup(server, &ch, mir176.Command{Ident: mir176.CMPickup, Param: uint16(drop.X), Tag: uint16(drop.Y)})
	}()

	hideCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode pickup hide frame error = %v", err)
	}
	if hideCmd.Ident != mir176.SMItemHide {
		t.Fatalf("first frame ident = %d, want SM_ITEMHIDE (%d)", hideCmd.Ident, mir176.SMItemHide)
	}
	addCmd, addBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode pickup add item frame error = %v", err)
	}
	if addCmd.Ident != mir176.SMAddItem {
		t.Fatalf("second frame ident = %d, want SM_ADDITEM (%d)", addCmd.Ident, mir176.SMAddItem)
	}
	if got := decodeClientItemName(addBody); got != testWeaponID {
		t.Fatalf("pickup add item name = %q, want %q", got, testWeaponID)
	}
	if got := decodeClientItemMakeIndex(addBody); got != drop.MakeIndex {
		t.Fatalf("pickup add item makeindex = %d, want %d", got, drop.MakeIndex)
	}
	weightCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode pickup weight frame error = %v", err)
	}
	if weightCmd.Ident != mir176.SMWeightChanged {
		t.Fatalf("third frame ident = %d, want SM_WEIGHTCHANGED (%d)", weightCmd.Ident, mir176.SMWeightChanged)
	}
	<-done
	if drop.ID == "" {
		t.Fatalf("expected drop to exist before pickup")
	}
	_, drops := s.world.Snapshot(ch.MapID)
	for _, candidate := range drops {
		if candidate.ID == drop.ID {
			t.Fatalf("drop %s still visible after pickup", drop.ID)
		}
	}
	if ch.EquippedItems[SlotWeapon].ItemID != "" {
		t.Fatalf("EquippedItems[SlotWeapon].ItemID = %q, want unchanged", ch.EquippedItems[SlotWeapon].ItemID)
	}
	found := false
	for _, entry := range ch.BagItems {
		if entry.ItemID == testWeaponID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %s in bag after pickup, got %+v", testWeaponID, ch.BagItems)
	}
	stats := s.world.AbilityStats(ch)
	if weightCmd.Recog != int32(stats.Weight) || weightCmd.Param != uint16(stats.WearWeight) || weightCmd.Tag != uint16(stats.HandWeight) {
		t.Fatalf("SM_WEIGHTCHANGED = %+v, want weight=%d wear=%d hand=%d", weightCmd, stats.Weight, stats.WearWeight, stats.HandWeight)
	}
}

func TestHandlePickupSendsOneAddPerPotionInstance(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BagItems = []storage.UserItem{{ItemID: testHPItemID, MakeIndex: 501}, {ItemID: testHPItemID, MakeIndex: 502}}
	ch, first, err := s.world.DropItemCountByBagIndex(ch, 501, testHPItemID, 1)
	if err != nil {
		t.Fatalf("DropItemCountByBagIndex(first) error = %v", err)
	}
	ch, second, err := s.world.DropItemCountByBagIndex(ch, 502, testHPItemID, 1)
	if err != nil {
		t.Fatalf("DropItemCountByBagIndex(second) error = %v", err)
	}

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	clientState := s.registerClient(server, ch)
	clientState.visibleDrops = map[string]world.GroundDrop{first.ID: first, second.ID: second}

	done := make(chan struct{})
	ch.X, ch.Y = first.X, first.Y
	go func() {
		defer close(done)
		s.handlePickup(server, &ch, mir176.Command{Ident: mir176.CMPickup, Param: uint16(first.X), Tag: uint16(first.Y)})
	}()

	hideCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode first pickup hide frame error = %v", err)
	}
	if hideCmd.Ident != mir176.SMItemHide {
		t.Fatalf("first frame ident = %d, want SM_ITEMHIDE (%d)", hideCmd.Ident, mir176.SMItemHide)
	}
	addCmd1, addBody1, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode first pickup add frame error = %v", err)
	}
	if addCmd1.Ident != mir176.SMAddItem {
		t.Fatalf("second frame ident = %d, want SM_ADDITEM (%d)", addCmd1.Ident, mir176.SMAddItem)
	}
	if got := decodeClientItemName(addBody1); got != testHPItemID {
		t.Fatalf("first pickup add item name = %q, want %q", got, testHPItemID)
	}
	if got := decodeClientItemMakeIndex(addBody1); got != 501 {
		t.Fatalf("first pickup add item makeindex = %d, want 501", got)
	}
	weightCmd1, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode first pickup weight frame error = %v", err)
	}
	if weightCmd1.Ident != mir176.SMWeightChanged {
		t.Fatalf("third frame ident = %d, want SM_WEIGHTCHANGED (%d)", weightCmd1.Ident, mir176.SMWeightChanged)
	}
	<-done

	done2 := make(chan struct{})
	ch.X, ch.Y = second.X, second.Y
	go func() {
		defer close(done2)
		s.handlePickup(server, &ch, mir176.Command{Ident: mir176.CMPickup, Param: uint16(second.X), Tag: uint16(second.Y)})
	}()

	hideCmd2, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode second pickup hide frame error = %v", err)
	}
	if hideCmd2.Ident != mir176.SMItemHide {
		t.Fatalf("first frame ident = %d, want SM_ITEMHIDE (%d)", hideCmd2.Ident, mir176.SMItemHide)
	}
	addCmd2, addBody2, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode second pickup add frame error = %v", err)
	}
	if addCmd2.Ident != mir176.SMAddItem {
		t.Fatalf("second frame ident = %d, want SM_ADDITEM (%d)", addCmd2.Ident, mir176.SMAddItem)
	}
	if got := decodeClientItemName(addBody2); got != testHPItemID {
		t.Fatalf("second pickup add item name = %q, want %q", got, testHPItemID)
	}
	if got := decodeClientItemMakeIndex(addBody2); got != 502 {
		t.Fatalf("second pickup add item makeindex = %d, want 502", got)
	}
	weightCmd2, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode second pickup weight frame error = %v", err)
	}
	if weightCmd2.Ident != mir176.SMWeightChanged {
		t.Fatalf("third frame ident = %d, want SM_WEIGHTCHANGED (%d)", weightCmd2.Ident, mir176.SMWeightChanged)
	}
	<-done2

	if countBagItems(ch.BagItems) != 2 {
		t.Fatalf("bag items = %+v, want 2 potion instances", ch.BagItems)
	}
	if ch.BagItems[0].MakeIndex == ch.BagItems[1].MakeIndex {
		t.Fatalf("bag makeindexes = [%d %d], want distinct identities", ch.BagItems[0].MakeIndex, ch.BagItems[1].MakeIndex)
	}
}

func TestHandleEatItemConsumesPotionAndRefreshesHealth(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	baseStats := s.world.AbilityStats(ch)
	ch.HP = 10
	ch.MaxHP = baseStats.MaxHP
	ch.MP = baseStats.MaxMP / 2
	ch.BagItems = []storage.UserItem{{ItemID: testInstantHPItemID, MakeIndex: 200}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleEatItem(server, &ch, mir176.Command{Ident: mir176.CMEat, Recog: 200}, nil)
	}()

	weightCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode weight frame error = %v", err)
	}
	if weightCmd.Ident != mir176.SMHealthSpellChanged {
		t.Fatalf("first frame ident = %d, want SM_HEALTHSPELLCHANGED (%d)", weightCmd.Ident, mir176.SMHealthSpellChanged)
	}
	weightCmd, _, err = decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode weight frame error = %v", err)
	}
	if weightCmd.Ident != mir176.SMWeightChanged {
		t.Fatalf("second frame ident = %d, want SM_WEIGHTCHANGED (%d)", weightCmd.Ident, mir176.SMWeightChanged)
	}
	stats := s.world.AbilityStats(ch)
	if weightCmd.Recog != int32(stats.Weight) || weightCmd.Param != uint16(stats.WearWeight) || weightCmd.Tag != uint16(stats.HandWeight) {
		t.Fatalf("weight frame = %+v, want weight=%d wear=%d hand=%d", weightCmd, stats.Weight, stats.WearWeight, stats.HandWeight)
	}
	healthStats := s.world.AbilityStats(ch)
	if healthStats.HP != ch.HP || healthStats.MP != ch.MP || healthStats.MaxHP != ch.MaxHP {
		t.Fatalf("post-eat stats = %+v, want hp/mp/maxhp = %d/%d/%d", healthStats, ch.HP, ch.MP, ch.MaxHP)
	}
	if weightCmd, _, err = decodeMessageLikeClient(readFrame(t, client)); err != nil {
		t.Fatalf("decode eat frame error = %v", err)
	}
	if weightCmd.Ident != mir176.SMEatOK {
		t.Fatalf("third frame ident = %d, want SM_EAT_OK (%d)", weightCmd.Ident, mir176.SMEatOK)
	}
	if weightCmd.Recog != 0 {
		t.Fatalf("eat recog = %d, want 0", weightCmd.Recog)
	}
	<-done
	if ch.HP != 19 {
		t.Fatalf("HP = %d, want 19", ch.HP)
	}
	if countBagItems(ch.BagItems) != 0 {
		t.Fatalf("bag = %+v, want empty after potion use", ch.BagItems)
	}
}

func TestApplyWorldTickSendsHealthRefreshForQueuedRecovery(t *testing.T) {
	s := newTestServer(t)
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", "D12", 0, 0)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.HP = 10
	ch.MP = 5
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	updated := ch
	updated.HP = 15
	updated.MP = 10
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.applyWorldTick(world.TickResult{Characters: []storage.Character{updated}})
	}()

	healthCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode health refresh frame error = %v", err)
	}
	if healthCmd.Ident != mir176.SMHealthSpellChanged {
		t.Fatalf("frame ident = %d, want SM_HEALTHSPELLCHANGED (%d)", healthCmd.Ident, mir176.SMHealthSpellChanged)
	}
	if healthCmd.Recog != world.CharacterActorID(updated) || healthCmd.Param != 15 || healthCmd.Tag != 10 {
		t.Fatalf("health refresh = %+v, want actor=%d hp=15 mp=10", healthCmd, world.CharacterActorID(updated))
	}
	<-done
}

func TestHandleUserCommandMakeSendsAddItemFrames(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	before := len(ch.BagItems)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleUserCommand(server, &ch, "@Make 回城卷 2")
	}()

	assertHearMessage(t, readFrame(t, client), "made 2 回城卷", makeWord(0x00, 0xFF))
	addCmd1, addBody1, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode first add frame error = %v", err)
	}
	if addCmd1.Ident != mir176.SMAddItem {
		t.Fatalf("first add frame ident = %d, want SM_ADDITEM (%d)", addCmd1.Ident, mir176.SMAddItem)
	}
	if got := decodeClientItemName(addBody1); got != "回城卷" {
		t.Fatalf("first add item name = %q, want 回城卷", got)
	}
	firstMakeIndex := decodeClientItemMakeIndex(addBody1)
	if firstMakeIndex == 0 {
		t.Fatal("first add item makeindex = 0")
	}
	addCmd2, addBody2, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode second add frame error = %v", err)
	}
	if addCmd2.Ident != mir176.SMAddItem {
		t.Fatalf("second add frame ident = %d, want SM_ADDITEM (%d)", addCmd2.Ident, mir176.SMAddItem)
	}
	if got := decodeClientItemName(addBody2); got != "回城卷" {
		t.Fatalf("second add item name = %q, want 回城卷", got)
	}
	if got := decodeClientItemMakeIndex(addBody2); got == 0 {
		t.Fatal("second add item makeindex = 0")
	} else if got == firstMakeIndex {
		t.Fatalf("add item makeindexes duplicated: %d", got)
	}
	weightCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode weight frame error = %v", err)
	}
	if weightCmd.Ident != mir176.SMWeightChanged {
		t.Fatalf("weight frame ident = %d, want SM_WEIGHTCHANGED (%d)", weightCmd.Ident, mir176.SMWeightChanged)
	}
	<-done
	if got := len(ch.BagItems); got != before+2 {
		t.Fatalf("bag items len = %d, want %d", got, before+2)
	}
}

func TestHandleUserCommandMakeWeaponCarriesDurability(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleUserCommand(server, &ch, "@Make 裁决之杖 1")
	}()

	_ = readFrame(t, client)
	_, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode add frame error = %v", err)
	}
	dura, duraMax := decodeClientItemDura(body)
	if dura == 0 || duraMax == 0 {
		t.Fatalf("weapon durability = %d/%d, want non-zero", dura, duraMax)
	}
	_ = readFrame(t, client)
	<-done
}

func TestHandleUserCommandMakeDragonSlayerCarriesDurability(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleUserCommand(server, &ch, "@Make 屠龙 1")
	}()

	_ = readFrame(t, client)
	_, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode add frame error = %v", err)
	}
	dura, duraMax := decodeClientItemDura(body)
	if dura == 0 || duraMax == 0 {
		t.Fatalf("dragon slayer durability = %d/%d, want non-zero", dura, duraMax)
	}
	_ = readFrame(t, client)
	<-done
}

func TestHandleEatItemUnpacksBundleAndRefreshesWeight(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BagItems = []storage.UserItem{{ItemID: "回城卷包", MakeIndex: 300}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleEatItem(server, &ch, mir176.Command{Ident: mir176.CMEat, Recog: 300}, nil)
	}()

	seen := map[int32]struct{}{}
	for i := 0; i < 6; i++ {
		addCmd, addBody, err := decodeMessageLikeClient(readFrame(t, client))
		if err != nil {
			t.Fatalf("decode add item frame %d error = %v", i, err)
		}
		if addCmd.Ident != mir176.SMAddItem {
			t.Fatalf("add frame %d ident = %d, want SM_ADDITEM (%d)", i, addCmd.Ident, mir176.SMAddItem)
		}
		if got := decodeClientItemName(addBody); got != "回城卷" {
			t.Fatalf("bundle add item %d name = %q, want 回城卷", i, got)
		}
		if got := decodeClientItemMakeIndex(addBody); got == 0 {
			t.Fatalf("bundle add item %d makeindex = 0", i)
		} else if _, ok := seen[got]; ok {
			t.Fatalf("bundle add item %d makeindex = %d duplicated", i, got)
		} else {
			seen[got] = struct{}{}
		}
	}
	weightCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode weight frame error = %v", err)
	}
	if weightCmd.Ident != mir176.SMWeightChanged {
		t.Fatalf("final weight frame ident = %d, want SM_WEIGHTCHANGED (%d)", weightCmd.Ident, mir176.SMWeightChanged)
	}
	eatCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode eat frame error = %v", err)
	}
	if eatCmd.Ident != mir176.SMEatOK {
		t.Fatalf("final frame ident = %d, want SM_EAT_OK (%d)", eatCmd.Ident, mir176.SMEatOK)
	}
	<-done
	found := 0
	for _, entry := range ch.BagItems {
		if entry.ItemID == "回城卷" {
			found++
		}
	}
	if found != 6 {
		t.Fatalf("bag回城卷 count = %d, want 6", found)
	}
}

func TestHandleEatItemShape12RefreshesAbilityBeforeWeight(t *testing.T) {
	bundle, err := data.Load(testConfigsDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	bundle.Items["apple-like"] = data.StdItem{
		ID:      "apple-like",
		Name:    "apple-like",
		Kind:    "consumable",
		StdMode: 3,
		Shape:   12,
		Stats: data.StdItemStats{
			DcMin:  10,
			DcMax:  1,
			McMin:  20,
			ScMin:  30,
			AcMin:  200,
			AcMax:  2,
			MacMin: 200,
			MacMax: 240,
		},
	}
	s := newTestServerWithBundle(t, bundle, config.DefaultGameplay())
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	baseStats := s.world.AbilityStats(ch)
	ch.BagItems = []storage.UserItem{{ItemID: "apple-like", MakeIndex: 401}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleEatItem(server, &ch, mir176.Command{Ident: mir176.CMEat, Recog: 401}, nil)
	}()

	abilityCmd, abilityBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode ability frame error = %v", err)
	}
	if abilityCmd.Ident != mir176.SMAbility {
		t.Fatalf("first frame ident = %d, want SM_ABILITY (%d)", abilityCmd.Ident, mir176.SMAbility)
	}
	decodedAbility, err := mir176.DecodePlain6Payload(abilityBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload(SM_ABILITY) error = %v", err)
	}
	if got := binary.LittleEndian.Uint16(decodedAbility[16:18]); got != uint16(baseStats.MaxHP+200) {
		t.Fatalf("SM_ABILITY MaxHP = %d, want %d", got, baseStats.MaxHP+200)
	}
	if got := binary.LittleEndian.Uint16(decodedAbility[18:20]); got != uint16(baseStats.MaxMP+200) {
		t.Fatalf("SM_ABILITY MaxMP = %d, want %d", got, baseStats.MaxMP+200)
	}

	weightCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode weight frame error = %v", err)
	}
	if weightCmd.Ident != mir176.SMWeightChanged {
		t.Fatalf("second frame ident = %d, want SM_WEIGHTCHANGED (%d)", weightCmd.Ident, mir176.SMWeightChanged)
	}
	eatCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode eat frame error = %v", err)
	}
	if eatCmd.Ident != mir176.SMEatOK {
		t.Fatalf("third frame ident = %d, want SM_EAT_OK (%d)", eatCmd.Ident, mir176.SMEatOK)
	}
	<-done
	if ch.ExtraAbil[0] != 10 || ch.ExtraAbil[3] != 2 || ch.ExtraAbil[4] != 200 || ch.ExtraAbil[5] != 200 {
		t.Fatalf("ExtraAbil = %+v, want shape12 bonuses applied", ch.ExtraAbil)
	}
}

func TestHandleEatItemShape13SendsExperienceAndKeepsWeightOrder(t *testing.T) {
	bundle, err := data.Load(testConfigsDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	bundle.Items["exp-drug"] = data.StdItem{
		ID:      "exp-drug",
		Name:    "exp-drug",
		Kind:    "consumable",
		StdMode: 3,
		Shape:   13,
		DuraMax: 75,
	}
	gameplay := config.DefaultGameplay()
	gameplay.Progression.RequiredExperiencePerLevel = 1000
	s := newTestServerWithBundle(t, bundle, gameplay)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BagItems = []storage.UserItem{{ItemID: "exp-drug", MakeIndex: 402}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleEatItem(server, &ch, mir176.Command{Ident: mir176.CMEat, Recog: 402}, nil)
	}()

	expCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode exp frame error = %v", err)
	}
	if expCmd.Ident != mir176.SMWinExp {
		t.Fatalf("first frame ident = %d, want SM_WINEXP (%d)", expCmd.Ident, mir176.SMWinExp)
	}
	weightCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode weight frame error = %v", err)
	}
	if weightCmd.Ident != mir176.SMWeightChanged {
		t.Fatalf("second frame ident = %d, want SM_WEIGHTCHANGED (%d)", weightCmd.Ident, mir176.SMWeightChanged)
	}
	eatCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode eat frame error = %v", err)
	}
	if eatCmd.Ident != mir176.SMEatOK {
		t.Fatalf("third frame ident = %d, want SM_EAT_OK (%d)", eatCmd.Ident, mir176.SMEatOK)
	}
	<-done
	if ch.Experience != 75 {
		t.Fatalf("Experience = %d, want 75", ch.Experience)
	}
}

func TestHandleEatItemShape13LevelsUpSendsMirbetaSequence(t *testing.T) {
	bundle, err := data.Load(testConfigsDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	bundle.Items["exp-drug-levelup"] = data.StdItem{
		ID:      "exp-drug-levelup",
		Name:    "exp-drug-levelup",
		Kind:    "consumable",
		StdMode: 3,
		Shape:   13,
		DuraMax: 150,
	}
	gameplay := config.DefaultGameplay()
	gameplay.Progression.RequiredExperiencePerLevel = 100
	s := newTestServerWithBundle(t, bundle, gameplay)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.HP = 9
	ch.MP = 4
	ch.BagItems = []storage.UserItem{{ItemID: "exp-drug-levelup", MakeIndex: 403}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleEatItem(server, &ch, mir176.Command{Ident: mir176.CMEat, Recog: 403}, nil)
	}()

	expCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode win exp frame error = %v", err)
	}
	if expCmd.Ident != mir176.SMWinExp {
		t.Fatalf("first frame ident = %d, want SM_WINEXP (%d)", expCmd.Ident, mir176.SMWinExp)
	}
	levelCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode level frame error = %v", err)
	}
	if levelCmd.Ident != mir176.SMLevelUp {
		t.Fatalf("second frame ident = %d, want SM_LEVELUP (%d)", levelCmd.Ident, mir176.SMLevelUp)
	}
	abilityCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode ability frame error = %v", err)
	}
	if abilityCmd.Ident != mir176.SMAbility {
		t.Fatalf("third frame ident = %d, want SM_ABILITY (%d)", abilityCmd.Ident, mir176.SMAbility)
	}
	subAbilityCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode sub ability frame error = %v", err)
	}
	if subAbilityCmd.Ident != mir176.SMSubAbility {
		t.Fatalf("fourth frame ident = %d, want SM_SUBABILITY (%d)", subAbilityCmd.Ident, mir176.SMSubAbility)
	}
	healthCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode health frame error = %v", err)
	}
	if healthCmd.Ident != mir176.SMHealthSpellChanged {
		t.Fatalf("fifth frame ident = %d, want SM_HEALTHSPELLCHANGED (%d)", healthCmd.Ident, mir176.SMHealthSpellChanged)
	}
	weightCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode weight frame error = %v", err)
	}
	if weightCmd.Ident != mir176.SMWeightChanged {
		t.Fatalf("sixth frame ident = %d, want SM_WEIGHTCHANGED (%d)", weightCmd.Ident, mir176.SMWeightChanged)
	}
	eatCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode eat frame error = %v", err)
	}
	if eatCmd.Ident != mir176.SMEatOK {
		t.Fatalf("seventh frame ident = %d, want SM_EAT_OK (%d)", eatCmd.Ident, mir176.SMEatOK)
	}
	<-done
	if ch.Level != 2 {
		t.Fatalf("Level = %d, want 2", ch.Level)
	}
	if ch.Experience != 50 {
		t.Fatalf("Experience = %d, want 50", ch.Experience)
	}
}

func TestHandleAddItemOverridesStdModeMapShape(t *testing.T) {
	s := newTestServer(t)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	drop := world.GroundDrop{ID: "drop-1", MapID: testMapID, X: 1, Y: 1, ItemID: "护身戒指", Count: 1, MakeIndex: 88}
	go s.sendAddItem(server, drop)

	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode add item frame error = %v", err)
	}
	if cmd.Ident != mir176.SMAddItem {
		t.Fatalf("ident = %d, want SM_ADDITEM (%d)", cmd.Ident, mir176.SMAddItem)
	}
	if got := decodeClientItemName(body); got != "护身戒指" {
		t.Fatalf("add item name = %q, want 护身戒指", got)
	}
	if got := decodeClientItemShape(body); got != 0 {
		t.Fatalf("add item shape = %d, want 0 for StdModeMap display", got)
	}
	if got := decodeClientItemMakeIndex(body); got != 88 {
		t.Fatalf("add item makeindex = %d, want 88", got)
	}
}

func TestHandleAddItemUsesShape130WhenDescFlagSet(t *testing.T) {
	s := newTestServer(t)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	var desc [14]byte
	desc[8] = 1
	drop := world.GroundDrop{ID: "drop-1", MapID: testMapID, X: 1, Y: 1, ItemID: "护身戒指", Count: 1, Desc: desc, MakeIndex: 89}
	go s.sendAddItem(server, drop)

	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode add item frame error = %v", err)
	}
	if cmd.Ident != mir176.SMAddItem {
		t.Fatalf("ident = %d, want SM_ADDITEM (%d)", cmd.Ident, mir176.SMAddItem)
	}
	if got := decodeClientItemShape(body); got != 130 {
		t.Fatalf("add item shape = %d, want 130 when desc[8] is set", got)
	}
}

func TestHandleQueryBagItemsSendsNonEmptyBag(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BagItems = []storage.UserItem{
		{ItemID: testWeaponID},
		{ItemID: testArmorID},
	}
	if normalized, changed := s.world.NormalizeCharacterBagItems(ch); changed {
		ch = normalized
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleQueryBagItems(server, &ch)
	}()

	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode bag items frame error = %v", err)
	}
	if cmd.Ident != mir176.SMBagItems {
		t.Fatalf("bag items ident = %d, want SM_BAGITEMS (%d)", cmd.Ident, mir176.SMBagItems)
	}
	if got := cmd.Recog; got != world.CharacterActorID(ch) {
		t.Fatalf("bag items recog = %d, want %d", got, world.CharacterActorID(ch))
	}
	names := decodeBagItemNames(t, body)
	if len(names) != 2 {
		t.Fatalf("bag item count = %d, want 2", len(names))
	}
	if names[0] != testWeaponID || names[1] != testArmorID {
		t.Fatalf("bag item names = %+v, want [%q %q]", names, testWeaponID, testArmorID)
	}
	if ch.BagItems[0].MakeIndex == 0 || ch.BagItems[1].MakeIndex == 0 {
		t.Fatalf("character bag makeindexes = [%d %d], want non-zero indexes", ch.BagItems[0].MakeIndex, ch.BagItems[1].MakeIndex)
	}
	if ch.BagItems[0].MakeIndex == ch.BagItems[1].MakeIndex {
		t.Fatalf("character bag makeindexes = [%d %d], want distinct indexes", ch.BagItems[0].MakeIndex, ch.BagItems[1].MakeIndex)
	}
	<-done
}

func TestHandleCreateGroupSharesMembersList(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	owner, err := s.world.CreateCharacter("test", "leader", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(owner) error = %v", err)
	}
	member, err := s.world.CreateCharacter("test", "member", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(member) error = %v", err)
	}
	member.AllowGroup = true
	if err := s.store.SaveCharacter(member); err != nil {
		t.Fatalf("SaveCharacter(member) error = %v", err)
	}
	ownerServer, ownerClient := net.Pipe()
	defer ownerServer.Close()
	defer ownerClient.Close()
	memberServer, memberClient := net.Pipe()
	defer memberServer.Close()
	defer memberClient.Close()
	s.registerClient(ownerServer, owner)
	s.registerClient(memberServer, member)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleCreateGroup(ownerServer, &owner, WireString(t, member.Name))
	}()

	okCmd, _, err := decodeMessageLikeClient(readFrame(t, ownerClient))
	if err != nil {
		t.Fatalf("decode create ok frame error = %v", err)
	}
	if okCmd.Ident != mir176.SMCreateGroupOK {
		t.Fatalf("create reply ident = %d, want SM_CREATEGROUP_OK (%d)", okCmd.Ident, mir176.SMCreateGroupOK)
	}
	ownerMembersCmd, ownerMembersBody, err := decodeMessageLikeClient(readFrame(t, ownerClient))
	if err != nil {
		t.Fatalf("decode owner group members frame error = %v", err)
	}
	memberMembersCmd, memberMembersBody, err := decodeMessageLikeClient(readFrame(t, memberClient))
	if err != nil {
		t.Fatalf("decode member group members frame error = %v", err)
	}
	if ownerMembersCmd.Ident != mir176.SMGroupMembers || memberMembersCmd.Ident != mir176.SMGroupMembers {
		t.Fatalf("group members idents = %d/%d, want SM_GROUPMEMBERS (%d)", ownerMembersCmd.Ident, memberMembersCmd.Ident, mir176.SMGroupMembers)
	}
	ownerMembersDecoded, err := mir176.DecodePlain6Payload(ownerMembersBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload(owner group members) error = %v", err)
	}
	memberMembersDecoded, err := mir176.DecodePlain6Payload(memberMembersBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload(member group members) error = %v", err)
	}
	if got := DecodeString(ownerMembersDecoded); got != "leader/member/" {
		t.Fatalf("owner group members = %q, want leader/member/", got)
	}
	if got := DecodeString(memberMembersDecoded); got != "leader/member/" {
		t.Fatalf("member group members = %q, want leader/member/", got)
	}
	if owner.GroupOwnerID != owner.ID || !owner.AllowGroup {
		t.Fatalf("owner state = %+v, want group owner self-id and allow group", owner)
	}
	if len(owner.GroupMembers) != 2 || owner.GroupMembers[0] != owner.ID || owner.GroupMembers[1] != member.ID {
		t.Fatalf("owner group members = %+v, want [%q %q]", owner.GroupMembers, owner.ID, member.ID)
	}
	storedMember, ok := s.store.Character(member.ID)
	if !ok {
		t.Fatalf("store.Character(member) missing")
	}
	if storedMember.GroupOwnerID != owner.ID {
		t.Fatalf("stored member state = %+v, want group owner %q", storedMember, owner.ID)
	}
	storedOwner, ok := s.store.Character(owner.ID)
	if !ok {
		t.Fatalf("store.Character(owner) missing")
	}
	if len(storedOwner.GroupMembers) != 2 || storedOwner.GroupMembers[1] != member.ID {
		t.Fatalf("stored owner group members = %+v, want owner/member", storedOwner.GroupMembers)
	}
	<-done
}

func TestHandleDelGroupMemberClearsGroupState(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	owner, err := s.world.CreateCharacter("test", "leader", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(owner) error = %v", err)
	}
	member, err := s.world.CreateCharacter("test", "member", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(member) error = %v", err)
	}
	member.AllowGroup = true
	if err := s.store.SaveCharacter(member); err != nil {
		t.Fatalf("SaveCharacter(member) error = %v", err)
	}
	ownerServer, ownerClient := net.Pipe()
	defer ownerServer.Close()
	defer ownerClient.Close()
	memberServer, memberClient := net.Pipe()
	defer memberServer.Close()
	defer memberClient.Close()
	s.registerClient(ownerServer, owner)
	s.registerClient(memberServer, member)
	owner.GroupOwnerID = owner.ID
	owner.GroupMembers = []string{owner.ID, member.ID}
	member.GroupOwnerID = owner.ID
	if err := s.store.SaveCharacter(owner); err != nil {
		t.Fatalf("SaveCharacter(owner group state) error = %v", err)
	}
	if err := s.store.SaveCharacter(member); err != nil {
		t.Fatalf("SaveCharacter(member group state) error = %v", err)
	}
	s.updateClientByCharacterID(owner)
	s.updateClientByCharacterID(member)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleDelGroupMember(ownerServer, &owner, WireString(t, member.Name))
	}()

	memberCancelCmd, _, err := decodeMessageLikeClient(readFrame(t, memberClient))
	if err != nil {
		t.Fatalf("decode member cancel frame error = %v", err)
	}
	if memberCancelCmd.Ident != mir176.SMGroupCancel {
		t.Fatalf("member cancel ident = %d, want SM_GROUPCANCEL (%d)", memberCancelCmd.Ident, mir176.SMGroupCancel)
	}
	ownerCancelCmd, _, err := decodeMessageLikeClient(readFrame(t, ownerClient))
	if err != nil {
		t.Fatalf("decode owner cancel frame error = %v", err)
	}
	if ownerCancelCmd.Ident != mir176.SMGroupCancel {
		t.Fatalf("owner cancel ident = %d, want SM_GROUPCANCEL (%d)", ownerCancelCmd.Ident, mir176.SMGroupCancel)
	}
	delOKCmd, delOKBody, err := decodeMessageLikeClient(readFrame(t, ownerClient))
	if err != nil {
		t.Fatalf("decode group del ok frame error = %v", err)
	}
	if delOKCmd.Ident != mir176.SMGroupDelMemOK {
		t.Fatalf("del reply ident = %d, want SM_GROUPDELMEM_OK (%d)", delOKCmd.Ident, mir176.SMGroupDelMemOK)
	}
	delOKDecoded, err := mir176.DecodePlain6Payload(delOKBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload(del ok) error = %v", err)
	}
	if got := DecodeString(delOKDecoded); got != member.Name {
		t.Fatalf("group delete body = %q, want %q", got, member.Name)
	}
	storedOwner, ok := s.store.Character(owner.ID)
	if !ok {
		t.Fatalf("store.Character(owner) missing")
	}
	storedMember, ok := s.store.Character(member.ID)
	if !ok {
		t.Fatalf("store.Character(member) missing")
	}
	if storedOwner.GroupOwnerID != "" || storedMember.GroupOwnerID != "" {
		t.Fatalf("group states = owner:%+v member:%+v, want cleared", storedOwner, storedMember)
	}
	if len(storedOwner.GroupMembers) != 0 {
		t.Fatalf("stored owner group members = %+v, want cleared", storedOwner.GroupMembers)
	}
	<-done
}

func TestHandleQueryBagItemsPacksEquipmentStats(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BagItems = []storage.UserItem{{ItemID: testArmorID}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleQueryBagItems(server, &ch)
	}()

	_, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode bag items frame error = %v", err)
	}
	parts := strings.Split(strings.TrimSuffix(string(body), "/"), "/")
	if len(parts) != 1 {
		t.Fatalf("bag item parts = %d, want 1", len(parts))
	}
	decoded, err := mir176.DecodePlain6Payload([]byte(parts[0]))
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if len(decoded) < 28 {
		t.Fatalf("decoded item body too short: %d", len(decoded))
	}
	if got := binary.LittleEndian.Uint16(decoded[26:28]); got != 512 {
		t.Fatalf("packed defense = %d, want 512", got)
	}
	if got := binary.LittleEndian.Uint16(decoded[28:30]); got != 256 {
		t.Fatalf("packed magic defense = %d, want 256", got)
	}
	<-done
}

func TestHandleQueryBagItemsSkipsEmptyBag(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BagItems = []storage.UserItem{}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleQueryBagItems(server, &ch)
	}()

	_ = client.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	buf := make([]byte, 1)
	n, err := client.Read(buf)
	if n != 0 {
		t.Fatalf("empty bag produced %d bytes", n)
	}
	if err == nil {
		t.Fatalf("empty bag unexpectedly produced a frame")
	}
	<-done
}

func TestHandleQueryBagItemsRepliesToRepeatedRequests(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BagItems = []storage.UserItem{{ItemID: testWeaponID}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		s.handleQueryBagItems(server, &ch)
	}()
	if _, _, err := decodeMessageLikeClient(readFrame(t, client)); err != nil {
		t.Fatalf("decode first bag query frame error = %v", err)
	}
	<-done1

	done2 := make(chan struct{})
	go func() {
		defer close(done2)
		s.handleQueryBagItems(server, &ch)
	}()
	if _, _, err := decodeMessageLikeClient(readFrame(t, client)); err != nil {
		t.Fatalf("decode second bag query frame error = %v", err)
	}
	<-done2
}

func TestEquippedItemsBodyCarriesEquippedMakeIndex(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BagItems = []storage.UserItem{{ItemID: testWeaponID, MakeIndex: 7}}
	ch, err = s.world.EquipItemByBagIndex(ch, world.SlotWeapon, 7, testWeaponID)
	if err != nil {
		t.Fatalf("EquipItemByBagIndex() error = %v", err)
	}
	body := EquippedItemsBody(s.world, ch)
	parts := bytes.Split(body, []byte("/"))
	if len(parts) < 2 {
		t.Fatalf("EquippedItemsBody() = %q, want slot/item pairs", body)
	}
	if string(parts[0]) != "1" {
		t.Fatalf("first slot prefix = %q, want 1 for weapon", parts[0])
	}
	decoded, err := mir176.DecodePlain6Payload(parts[1])
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if len(decoded) < 46 {
		t.Fatalf("decoded use-item body too short: %d", len(decoded))
	}
	got := int32(binary.LittleEndian.Uint32(decoded[44:48]))
	if got != 7 {
		t.Fatalf("equipped make index = %d, want 7", got)
	}
}

func TestEquippedItemsBodyCarriesEquippedDurability(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := s.world.DefaultSpawn()
	ch, err := s.world.CreateCharacter("test", "tester", "warrior", mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BagItems = []storage.UserItem{{ItemID: testWeaponID, MakeIndex: 7}}
	ch, err = s.world.EquipItemByBagIndex(ch, world.SlotWeapon, 7, testWeaponID)
	if err != nil {
		t.Fatalf("EquipItemByBagIndex() error = %v", err)
	}
	body := EquippedItemsBody(s.world, ch)
	parts := bytes.Split(body, []byte("/"))
	if len(parts) < 2 {
		t.Fatalf("EquippedItemsBody() = %q, want slot/item pairs", body)
	}
	decoded, err := mir176.DecodePlain6Payload(parts[1])
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if len(decoded) < 50 {
		t.Fatalf("decoded use-item body too short: %d", len(decoded))
	}
	dura := binary.LittleEndian.Uint16(decoded[48:50])
	duraMax := binary.LittleEndian.Uint16(decoded[50:52])
	if dura == 0 || duraMax == 0 {
		t.Fatalf("equipped durability = %d/%d, want non-zero values", dura, duraMax)
	}
}

// assertActionAck verifies a frame is the raw, unencoded "+GOOD/<tick>"
// status-good acknowledgement the original Delphi MirClient/M2Server sends
// to the acting player itself (as opposed to the SM_* command broadcast
// reserved for other nearby observers).
func assertActionAck(t *testing.T, frame []byte) {
	t.Helper()
	body, err := mir176.UnwrapFrame(frame)
	if err != nil {
		t.Fatalf("UnwrapFrame() error = %v", err)
	}
	if !strings.HasPrefix(string(body), "+GOOD/") {
		t.Fatalf("ack body = %q, want +GOOD/ prefix", body)
	}
}

func decodeClientItemName(body []byte) string {
	decoded, err := mir176.DecodePlain6Payload(body)
	if err != nil || len(decoded) == 0 {
		return ""
	}
	n := int(decoded[0])
	if n <= 0 || n > len(decoded)-1 {
		return ""
	}
	return DecodeString(decoded[1 : 1+n])
}

func decodeClientItemShape(body []byte) byte {
	decoded, err := mir176.DecodePlain6Payload(body)
	if err != nil || len(decoded) == 0 {
		return 0
	}
	offset := 1 + itemNameLen + 1
	if offset >= len(decoded) {
		return 0
	}
	return decoded[offset]
}

func decodeClientItemMakeIndex(body []byte) int32 {
	decoded, err := mir176.DecodePlain6Payload(body)
	if err != nil || len(decoded) < 46 {
		return 0
	}
	return int32(binary.LittleEndian.Uint32(decoded[44:48]))
}

func decodeClientItemDura(body []byte) (uint16, uint16) {
	decoded, err := mir176.DecodePlain6Payload(body)
	if err != nil || len(decoded) == 0 {
		return 0, 0
	}
	if len(decoded) < 50 {
		return 0, 0
	}
	dura := binary.LittleEndian.Uint16(decoded[48:50])
	duraMax := binary.LittleEndian.Uint16(decoded[50:52])
	return dura, duraMax
}

func decodeBagItemNames(t *testing.T, body []byte) []string {
	t.Helper()
	parts := strings.Split(strings.TrimSuffix(string(body), "/"), "/")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		name := decodeClientItemName([]byte(part))
		if name == "" {
			t.Fatalf("failed to decode bag item body %q", part)
		}
		names = append(names, name)
	}
	return names
}

func assertHearMessage(t *testing.T, frame []byte, wantText string, wantColor uint16) {
	t.Helper()
	cmd, body, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	if cmd.Ident != mir176.SMHear {
		t.Fatalf("ident = %d, want SM_HEAR (%d)", cmd.Ident, mir176.SMHear)
	}
	if cmd.Param != wantColor || cmd.Tag != 0 || cmd.Series != 1 {
		t.Fatalf("message command = %+v, want color %d tag=0 series=1", cmd, wantColor)
	}
	text, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if got := DecodeString(text); got != wantText {
		t.Fatalf("message = %q, want %q", got, wantText)
	}
}

func readFrame(t *testing.T, r io.Reader) []byte {
	t.Helper()
	buf := make([]byte, 4096)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	return buf[:n]
}
