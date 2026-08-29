package network

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

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

func testDefaultSpawn(t *testing.T) (string, int, int) {
	t.Helper()
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	mp, ok := bundle.Maps[testMapID]
	if !ok {
		t.Fatalf("map %s missing from configs", testMapID)
	}
	if len(mp.StartPoints) > 0 {
		return testMapID, mp.StartPoints[0].X, mp.StartPoints[0].Y
	}
	return testMapID, mp.Width / 2, mp.Height / 2
}

func setWorldRand(t *testing.T, w *world.World, seed int64) {
	t.Helper()
	rv := reflect.ValueOf(w).Elem().FieldByName("rand")
	reflect.NewAt(rv.Type(), unsafe.Pointer(rv.UnsafeAddr())).Elem().Set(reflect.ValueOf(rand.New(rand.NewSource(seed))))
}

type fixedRandSource struct {
	vals []int64
	idx  int
}

func (s *fixedRandSource) Int63() int64 {
	if len(s.vals) == 0 {
		return 0
	}
	v := s.vals[s.idx%len(s.vals)]
	s.idx++
	if v < 0 {
		v = -v
	}
	return v
}

func (s *fixedRandSource) Seed(seed int64) {
	s.idx = 0
}

func setWorldRandSource(t *testing.T, w *world.World, src rand.Source) {
	t.Helper()
	rv := reflect.ValueOf(w).Elem().FieldByName("rand")
	reflect.NewAt(rv.Type(), unsafe.Pointer(rv.UnsafeAddr())).Elem().Set(reflect.ValueOf(rand.New(src)))
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
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
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
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
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
	bundle, _, err := data.LoadConfigsWithReport(dir)
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
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BonusAbil.Hit = 100
	server, client := net.Pipe()
	s.registerClient(server, ch)
	return s, ch, server, client
}

func TestHandleTurnUpdatesCharacterAndAcks(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BonusAbil.Hit = 100
	monsters, _ := s.world.SnapshotAround(ch.MapID, 0, 0, 99999)
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

func TestHandleHitRefreshesMagicListAfterSkillTraining(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BonusAbil.Hit = 100
	ch.Skills = storage.SkillStates{{ID: "基本剑术", Level: 0, Train: 0}}
	ch.Level = 7
	monsters, _ := s.world.SnapshotAround(ch.MapID, 0, 0, 99999)
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

	var ackSeen bool
	for i := 0; i < 5; i++ {
		frame := readFrame(t, client)
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			ackSeen = true
			break
		}
	}
	if !ackSeen {
		t.Fatal("missing caster action ack")
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		frame, ok := readFrameWithTimeout(t, client, 50*time.Millisecond)
		if !ok {
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err == nil && cmd.Ident == mir176.SMSendMyMagic {
			<-done
			t.Fatal("unexpected SMSendMyMagic frame after hit training")
		}
	}
	<-done
}

func TestHandleHitBroadcastsDeathWhenMonsterHPReachesZero(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BonusAbil.Hit = 100
	monsters, _ := s.world.SnapshotAround(ch.MapID, 0, 0, 99999)
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

	monsters, _ := s.world.SnapshotAround(ch.MapID, 0, 0, 99999)
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

	ackSeen := false
	for i := 0; i < 5; i++ {
		frame := readFrame(t, client)
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			ackSeen = true
			break
		}
	}
	if !ackSeen {
		t.Fatal("missing caster action ack")
	}
	var sawStruck, sawWinExp, sawDeath, sawDrop bool
	var showCmd mir176.Command
	var showBody []byte
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		frame, ok := readFrameWithTimeout(t, client, 100*time.Millisecond)
		if !ok {
			break
		}
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			sawStruck = true
		case mir176.SMWinExp:
			sawWinExp = true
		case mir176.SMNowDeath:
			sawDeath = true
		case mir176.SMItemShow:
			sawDrop = true
			showCmd = cmd
			showBody = body
		}
	}
	if !sawStruck || !sawWinExp || !sawDeath || !sawDrop {
		t.Fatalf("frames seen struck=%v winExp=%v death=%v drop=%v", sawStruck, sawWinExp, sawDeath, sawDrop)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BonusAbil.Hit = 100
	monsters, _ := s.world.SnapshotAround(ch.MapID, 0, 0, 99999)
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
	if client := s.clientForConn(attackerServer); client != nil {
		client.mu.Lock()
		client.fireHitArmed = true
		client.fireHitLatestAt = time.Now()
		client.mu.Unlock()
	}

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

func TestFireHitExpiresOnWorldTick(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)
	if client := s.clientForConn(server); client != nil {
		client.mu.Lock()
		client.fireHitArmed = true
		client.fireHitLatestAt = time.Now().Add(-21 * time.Second)
		client.mu.Unlock()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.expireFireHitStates(time.Now())
	}()
	frame := readFrame(t, client)
	assertHearMessage(t, frame, "召唤烈火精灵结束...", makeWord(0x00, 0x97))
	body, err := mir176.UnwrapFrame(readFrame(t, client))
	if err != nil {
		t.Fatalf("UnwrapFrame() error = %v", err)
	}
	if string(body) != "+UFIR" {
		t.Fatalf("body = %q, want +UFIR", body)
	}
	<-done
}

func TestFireHitDoesNotExpireWhenUnarmed(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)
	if client := s.clientForConn(server); client != nil {
		client.mu.Lock()
		client.fireHitArmed = false
		client.fireHitLatestAt = time.Now().Add(-21 * time.Second)
		client.mu.Unlock()
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.expireFireHitStates(time.Now())
	}()
	if err := client.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	buf := make([]byte, 256)
	if n, err := client.Read(buf); err == nil {
		t.Fatalf("unexpected frame when unarmed: %q", buf[:n])
	} else if !errors.Is(err, os.ErrDeadlineExceeded) && !strings.Contains(err.Error(), "i/o timeout") {
		t.Fatalf("Read() error = %v", err)
	}
	<-done
}

func TestHandleSpellFireHitConsumesManaAndArmsState(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.MP = 100
	ch.Skills = storage.SkillStates{{ID: "烈火剑法", Level: 0, Train: 0}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)
	recog := int32(uint32(ch.X) | uint32(ch.Y)<<16)
	skillID, ok := s.world.MagicIDByName("烈火剑法")
	if !ok {
		t.Fatal("MagicIDByName() missing 烈火剑法")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: recog, Param: skillID})
	}()
	assertHearMessage(t, readFrame(t, client), "召唤烈火精灵成功...", makeWord(0x00, 0xFF))
	cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode health frame error = %v", err)
	}
	if cmd.Ident != mir176.SMHealthSpellChanged {
		t.Fatalf("health ident = %d, want SMHealthSpellChanged", cmd.Ident)
	}
	body, err := mir176.UnwrapFrame(readFrame(t, client))
	if err != nil {
		t.Fatalf("UnwrapFrame() error = %v", err)
	}
	if string(body) != "+FIR" {
		t.Fatalf("third frame body = %q, want +FIR", body)
	}
	ackSeen := false
	for i := 0; i < 5; i++ {
		frame := readFrame(t, client)
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			ackSeen = true
			break
		}
	}
	if !ackSeen {
		t.Fatal("missing caster action ack")
	}
	<-done
	if ch.MP >= 100 {
		t.Fatalf("MP = %d, want consumed on fire-hit cast", ch.MP)
	}
	if client := s.clientForConn(server); client != nil {
		client.mu.Lock()
		armed := client.fireHitArmed
		latestAt := client.fireHitLatestAt
		client.mu.Unlock()
		if !armed {
			t.Fatal("fireHitArmed = false, want armed")
		}
		if latestAt.IsZero() {
			t.Fatal("fireHitLatestAt = zero, want armed timestamp recorded")
		}
	}
}

func TestHandleSpellFireHitRejectsRearmWithinCooldown(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.MP = 100
	ch.Skills = storage.SkillStates{{ID: "烈火剑法", Level: 0, Train: 0}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)

	recog := int32(uint32(ch.X) | uint32(ch.Y)<<16)
	skillID, ok := s.world.MagicIDByName("烈火剑法")
	if !ok {
		t.Fatal("MagicIDByName() missing 烈火剑法")
	}

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: recog, Param: skillID})
	}()
	assertHearMessage(t, readFrame(t, client), "召唤烈火精灵成功...", makeWord(0x00, 0xFF))
	cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode health frame error = %v", err)
	}
	if cmd.Ident != mir176.SMHealthSpellChanged {
		t.Fatalf("health ident = %d, want SMHealthSpellChanged", cmd.Ident)
	}
	if body, err := mir176.UnwrapFrame(readFrame(t, client)); err != nil || string(body) != "+FIR" {
		t.Fatalf("first fire frame = %q, err=%v, want +FIR", body, err)
	}
	assertActionAck(t, readFrame(t, client))
	<-firstDone

	beforeMP := ch.MP
	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: recog, Param: skillID})
	}()
	assertHearMessage(t, readFrame(t, client), "召唤烈火精灵失败...", makeWord(0x00, 0x97))
	assertActionAck(t, readFrame(t, client))
	<-secondDone
	if ch.MP != beforeMP {
		t.Fatalf("MP = %d, want unchanged after cooldown rejection", ch.MP)
	}
	if client := s.clientForConn(server); client != nil {
		client.mu.Lock()
		armed := client.fireHitArmed
		client.mu.Unlock()
		if !armed {
			t.Fatal("fireHitArmed = false, want still armed after cooldown rejection")
		}
	}
}

func TestAdvancePowerHitStateKeepsArmedStateAcrossCycleReset(t *testing.T) {
	s := newTestServer(t)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ch := storage.Character{
		ID:     "tester",
		Skills: storage.SkillStates{{ID: "攻杀剑术", Level: 0, Train: 0}},
		EquippedItems: map[int]storage.UserItem{
			SlotWeapon: {Dura: 1},
		},
	}
	clientState := &Client{conn: server, ch: ch, powerHitCount: 1, powerHitPointCount: 0}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.advancePowerHitState(clientState, ch)
	}()
	frame := readFrame(t, client)
	body, err := mir176.UnwrapFrame(frame)
	if err != nil {
		t.Fatalf("UnwrapFrame() error = %v", err)
	}
	if string(body) != "+PWR" {
		t.Fatalf("body = %q, want +PWR", body)
	}
	<-done
	clientState.mu.Lock()
	defer clientState.mu.Unlock()
	if !clientState.powerHitArmed {
		t.Fatal("powerHitArmed = false, want still armed after cycle reset")
	}
	if clientState.powerHitCount != 7 {
		t.Fatalf("powerHitCount = %d, want reset to full cycle length", clientState.powerHitCount)
	}
}

func TestHandleHitAdvancesPowerHitStateBeforeRejectedSwing(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Skills = storage.SkillStates{{ID: "攻杀剑术", Level: 0, Train: 0}}
	ch.EquippedItems = map[int]storage.UserItem{
		SlotWeapon: {Dura: 1},
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)

	clientState := s.clientForConn(server)
	if clientState == nil {
		t.Fatal("client state = nil")
	}
	clientState.mu.Lock()
	clientState.powerHitCount = 3
	clientState.powerHitPointCount = 1
	clientState.powerHitArmed = false
	clientState.ch.Skills = storage.SkillStates{{ID: "攻杀剑术", Level: 0, Train: 0}}
	clientState.mu.Unlock()

	recog := int32(uint32(x+1) | uint32(y)<<16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleHit(server, &ch, mir176.Command{Ident: mir176.CMHit, Recog: recog, Tag: 2})
	}()

	frame := readFrame(t, client)
	cmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	if cmd.Ident != mir176.SMMoveFail {
		t.Fatalf("reply ident = %d, want %d", cmd.Ident, mir176.SMMoveFail)
	}
	<-done

	clientState.mu.Lock()
	defer clientState.mu.Unlock()
	if clientState.powerHitCount != 2 {
		t.Fatalf("powerHitCount = %d, want decremented before rejection", clientState.powerHitCount)
	}
	if clientState.powerHitArmed {
		t.Fatal("powerHitArmed = true, want still false after one decrement")
	}
}

func TestHandleHitClearsPowerHitWhenWeaponMissing(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Skills = storage.SkillStates{{ID: "攻杀剑术", Level: 0, Train: 0}}
	ch.EquippedItems = map[int]storage.UserItem{}
	monsters, _ := s.world.SnapshotAround(ch.MapID, 0, 0, 99999)
	if len(monsters) == 0 {
		t.Fatalf("expected monsters")
	}
	mon := monsters[0]
	ch.X, ch.Y = mon.X-1, mon.Y
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)

	clientState := s.clientForConn(server)
	if clientState == nil {
		t.Fatal("client state = nil")
	}
	clientState.mu.Lock()
	clientState.powerHitArmed = true
	clientState.powerHitCount = 3
	clientState.powerHitPointCount = 1
	clientState.mu.Unlock()

	recog := int32(uint32(ch.X) | uint32(ch.Y)<<16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleHit(server, &ch, mir176.Command{Ident: mir176.CMPowerHit, Recog: recog, Tag: 2})
	}()

	assertActionAck(t, readFrame(t, client))
	if _, _, err := decodeMessageLikeClient(readFrame(t, client)); err != nil {
		t.Fatalf("decode struck frame error = %v", err)
	}
	<-done

	clientState.mu.Lock()
	defer clientState.mu.Unlock()
	if clientState.powerHitArmed {
		t.Fatal("powerHitArmed = true, want cleared when weapon is missing")
	}
}

func TestHandleHitBroadcastsCharacterStruckToTarget(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	attacker, err := s.world.CreateCharacterWithAppearance("test", "attacker", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() attacker error = %v", err)
	}
	attacker.Level = 10
	candidates := []struct {
		x   int
		y   int
		dir int
	}{
		{x + 1, y, 2},
		{x - 1, y, 6},
		{x, y + 1, 4},
		{x, y - 1, 0},
	}
	var target storage.Character
	for _, candidate := range candidates {
		if monsters, _ := s.world.SnapshotAround(mapID, candidate.x, candidate.y, 0); len(monsters) > 0 {
			continue
		}
		target, err = s.world.CreateCharacterWithAppearance("test", "target", "wizard", 0, 0, mapID, candidate.x, candidate.y)
		if err == nil {
			candidates = []struct {
				x   int
				y   int
				dir int
			}{{candidate.x, candidate.y, candidate.dir}}
			break
		}
	}
	if err != nil {
		t.Fatalf("CreateCharacter() target error = %v", err)
	}
	target.HP = 30
	target.MaxHP = s.world.AbilityStats(target).MaxHP
	startingHP := target.HP

	attackerServer, attackerClient := net.Pipe()
	defer attackerServer.Close()
	defer attackerClient.Close()
	targetServer, targetClient := net.Pipe()
	defer targetServer.Close()
	defer targetClient.Close()
	s.registerClient(attackerServer, attacker)
	defer s.unregisterClient(attackerServer)
	s.registerClient(targetServer, target)
	defer s.unregisterClient(targetServer)

	dir := candidates[0].dir
	recog := int32(uint32(attacker.X) | uint32(attacker.Y)<<16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleHit(attackerServer, &attacker, mir176.Command{Ident: mir176.CMHit, Recog: recog, Tag: uint16(dir)})
	}()

	assertActionAck(t, readFrame(t, attackerClient))
	actionCmd, _, err := decodeMessageLikeClient(readFrame(t, targetClient))
	if err != nil {
		t.Fatalf("decode target action frame error = %v", err)
	}
	if actionCmd.Ident != mir176.SMHit {
		t.Fatalf("action ident = %d, want SM_HIT (%d)", actionCmd.Ident, mir176.SMHit)
	}
	if actionCmd.Param != uint16(attacker.X) || actionCmd.Tag != uint16(attacker.Y) || actionCmd.Series != uint16(dir) {
		t.Fatalf("action frame = %+v, want attacker at (%d,%d) dir=%d", actionCmd, attacker.X, attacker.Y, dir)
	}
	struckCmd, struckBody, err := decodeMessageLikeClient(readFrame(t, targetClient))
	if err != nil {
		t.Fatalf("decode target struck frame error = %v", err)
	}
	if struckCmd.Ident != mir176.SMStruck {
		t.Fatalf("struck ident = %d, want SM_STRUCK (%d)", struckCmd.Ident, mir176.SMStruck)
	}
	if struckCmd.Param == 0 || struckCmd.Param >= uint16(startingHP) {
		t.Fatalf("struck hp = %d, want reduced target hp", struckCmd.Param)
	}
	assertMessageBodyWL(t, struckBody, s.world.HumanFeatureForCharacter(target), 0, world.MonsterActorID(world.Monster{ID: attacker.ID}), 0)
	healthCmd, _, err := decodeMessageLikeClient(readFrame(t, targetClient))
	if err != nil {
		t.Fatalf("decode target health frame error = %v", err)
	}
	if healthCmd.Ident != mir176.SMHealthSpellChanged {
		t.Fatalf("health ident = %d, want SM_HEALTHSPELLChanged (%d)", healthCmd.Ident, mir176.SMHealthSpellChanged)
	}
	<-done
}

func TestHandleHitFallsBackToNormalHitWhenSpecialHitUnarmed(t *testing.T) {
	cases := []struct {
		name  string
		ident uint16
	}{
		{name: "power", ident: mir176.CMPowerHit},
		{name: "fire", ident: mir176.CMFireHit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t)
			mapID, x, y := testDefaultSpawn(t)
			attacker, err := s.world.CreateCharacterWithAppearance("test", "attacker", "warrior", 0, 0, mapID, x, y)
			if err != nil {
				t.Fatalf("CreateCharacter() attacker error = %v", err)
			}
			target, err := s.world.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, mapID, x+1, y)
			if err != nil {
				t.Fatalf("CreateCharacter() target error = %v", err)
			}
			attackerServer, attackerClient := net.Pipe()
			defer attackerServer.Close()
			defer attackerClient.Close()
			targetServer, targetClient := net.Pipe()
			defer targetServer.Close()
			defer targetClient.Close()
			s.registerClient(attackerServer, attacker)
			defer s.unregisterClient(attackerServer)
			s.registerClient(targetServer, target)
			defer s.unregisterClient(targetServer)
			recog := int32(uint32(attacker.X) | uint32(attacker.Y)<<16)
			done := make(chan struct{})
			go func() {
				defer close(done)
				s.handleHit(attackerServer, &attacker, mir176.Command{Ident: tc.ident, Recog: recog, Tag: 2})
			}()
			assertActionAck(t, readFrame(t, attackerClient))
			actionCmd, _, err := decodeMessageLikeClient(readFrame(t, targetClient))
			if err != nil {
				t.Fatalf("decode target action frame error = %v", err)
			}
			if actionCmd.Ident != mir176.SMHit {
				t.Fatalf("action ident = %d, want SMHit when unarmed", actionCmd.Ident)
			}
			if _, _, err := decodeMessageLikeClient(readFrame(t, targetClient)); err != nil {
				t.Fatalf("decode target struck frame error = %v", err)
			}
			if _, _, err := decodeMessageLikeClient(readFrame(t, targetClient)); err != nil {
				t.Fatalf("decode target health frame error = %v", err)
			}
			<-done
		})
	}
}

func TestHandleHitBroadcastsSpecialWeaponActionsToTargets(t *testing.T) {
	cases := []struct {
		name       string
		ident      uint16
		wantAction uint16
	}{
		{
			name:       "heavy",
			ident:      mir176.CMHeavyHit,
			wantAction: mir176.SMHeavyHit,
		},
		{
			name:       "big",
			ident:      mir176.CMBigHit,
			wantAction: mir176.SMBigHit,
		},
		{
			name:       "power",
			ident:      mir176.CMPowerHit,
			wantAction: mir176.SMPowerHit,
		},
		{
			name:       "long",
			ident:      mir176.CMLongHit,
			wantAction: mir176.SMLongHit,
		},
		{
			name:       "wide",
			ident:      mir176.CMWideHit,
			wantAction: mir176.SMWideHit,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newDataDirTestServer(t, testConfigsDir)
			mapID, x, y := testDefaultSpawn(t)

			attackPoints := func(baseX, baseY int) [][2]int {
				switch tc.ident {
				case mir176.CMHeavyHit, mir176.CMBigHit, mir176.CMPowerHit:
					fallthrough
				case mir176.CMHit:
					return [][2]int{{baseX + 1, baseY}}
				case mir176.CMLongHit:
					return [][2]int{{baseX + 2, baseY}}
				case mir176.CMWideHit:
					return [][2]int{{baseX + 1, baseY - 1}, {baseX + 1, baseY + 1}, {baseX, baseY + 1}}
				default:
					return [][2]int{{baseX + 1, baseY}}
				}
			}
			var attacker storage.Character
			var target storage.Character
			found := false
			for _, base := range [][2]int{{x + 20, y + 20}, {x + 30, y + 20}, {x + 20, y + 30}, {x + 30, y + 30}} {
				points := attackPoints(base[0], base[1])
				clear := true
				for _, pt := range append([][2]int{{base[0], base[1]}}, points...) {
					monsters, _ := s.world.SnapshotAround(mapID, pt[0], pt[1], 0)
					if len(monsters) > 0 {
						clear = false
						break
					}
				}
				if !clear {
					continue
				}
				var err error
				attacker, err = s.world.CreateCharacterWithAppearance("test", "attacker", "warrior", 0, 0, mapID, base[0], base[1])
				if err != nil {
					t.Fatalf("CreateCharacter() attacker error = %v", err)
				}
				targetX, targetY := points[0][0], points[0][1]
				target, err = s.world.CreateCharacterWithAppearance("test2", "target", "warrior", 0, 0, mapID, targetX, targetY)
				if err != nil {
					t.Fatalf("CreateCharacter() target error = %v", err)
				}
				found = true
				break
			}
			if !found {
				t.Fatal("could not find clear tiles for special hit test")
			}
			attacker.Level = 10
			if tc.ident == mir176.CMPowerHit {
				attacker.Skills = storage.SkillStates{{ID: "攻杀剑术", Level: 0, Train: 0}}
				attacker.EquippedItems = map[int]storage.UserItem{
					SlotWeapon: {ItemID: testWeaponID, Dura: 1},
				}
			}
			target.HP = 1000
			target.MaxHP = s.world.AbilityStats(target).MaxHP
			startingHP := target.HP

			attackerServer, attackerClient := net.Pipe()
			defer attackerServer.Close()
			defer attackerClient.Close()
			targetServer, targetClient := net.Pipe()
			defer targetServer.Close()
			defer targetClient.Close()
			observerServer, observerClient := net.Pipe()
			defer observerServer.Close()
			defer observerClient.Close()
			s.registerClient(attackerServer, attacker)
			defer s.unregisterClient(attackerServer)
			s.registerClient(targetServer, target)
			defer s.unregisterClient(targetServer)
			if tc.ident == mir176.CMPowerHit {
				if client := s.clientForConn(attackerServer); client != nil {
					client.mu.Lock()
					client.powerHitArmed = true
					client.mu.Unlock()
				}
			}
			observer, err := s.world.CreateCharacterWithAppearance("test3", "observer", "wizard", 0, 0, mapID, attacker.X, attacker.Y+2)
			if err != nil {
				t.Fatalf("CreateCharacter() observer error = %v", err)
			}
			s.registerClient(observerServer, observer)
			defer s.unregisterClient(observerServer)

			actionFrameCh := make(chan []byte, 16)
			observerActionFrameCh := make(chan []byte, 16)
			observerStruckFrameCh := make(chan []byte, 16)
			observerHealthFrameCh := make(chan []byte, 16)
			struckFrameCh := make(chan []byte, 16)
			healthFrameCh := make(chan []byte, 16)
			go func() {
				actionFrameCh <- readFrame(t, targetClient)
				struckFrameCh <- readFrame(t, targetClient)
				healthFrameCh <- readFrame(t, targetClient)
			}()
			go func() {
				observerActionFrameCh <- readFrame(t, observerClient)
				observerStruckFrameCh <- readFrame(t, observerClient)
				observerHealthFrameCh <- readFrame(t, observerClient)
			}()

			recog := int32(uint32(attacker.X) | uint32(attacker.Y)<<16)
			done := make(chan struct{})
			go func() {
				defer close(done)
				s.handleHit(attackerServer, &attacker, mir176.Command{Ident: tc.ident, Recog: recog, Tag: uint16(2)})
			}()

			assertActionAck(t, readFrame(t, attackerClient))
			if err := attackerClient.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
				t.Fatalf("SetReadDeadline() error = %v", err)
			}
			buf := make([]byte, 4096)
			if n, err := attackerClient.Read(buf); err == nil {
				winExpCmd, _, err := decodeMessageLikeClient(buf[:n])
				if err != nil {
					t.Fatalf("decode win exp frame error = %v", err)
				}
				if winExpCmd.Ident != mir176.SMWinExp {
					t.Fatalf("win exp command = %+v, want SM_WINEXP", winExpCmd)
				}
			} else if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
				t.Fatalf("Read() error = %v, want timeout or win exp frame", err)
			}
			if err := attackerClient.SetReadDeadline(time.Time{}); err != nil {
				t.Fatalf("SetReadDeadline() error = %v", err)
			}
			actionCmd, _, err := decodeMessageLikeClient(<-actionFrameCh)
			if err != nil {
				t.Fatalf("decode target action frame error = %v", err)
			}
			if actionCmd.Ident != tc.wantAction || actionCmd.Recog != ActorID || int(actionCmd.Param) != attacker.X || int(actionCmd.Tag) != attacker.Y || int(actionCmd.Series) != 2 {
				t.Fatalf("target action = %+v, want %s from attacker at (%d,%d) dir=2", actionCmd, tc.name, attacker.X, attacker.Y)
			}
			observerActionCmd, _, err := decodeMessageLikeClient(<-observerActionFrameCh)
			if err != nil {
				t.Fatalf("decode observer action frame error = %v", err)
			}
			if observerActionCmd.Ident != tc.wantAction || observerActionCmd.Recog != ActorID || int(observerActionCmd.Param) != attacker.X || int(observerActionCmd.Tag) != attacker.Y || int(observerActionCmd.Series) != 2 {
				t.Fatalf("observer action = %+v, want %s from attacker at (%d,%d) dir=2", observerActionCmd, tc.name, attacker.X, attacker.Y)
			}
			observerStruckCmd, _, err := decodeMessageLikeClient(<-observerStruckFrameCh)
			if err != nil {
				t.Fatalf("decode observer struck frame error = %v", err)
			}
			if observerStruckCmd.Ident != mir176.SMStruck {
				t.Fatalf("observer struck ident = %d, want SMStruck (%d)", observerStruckCmd.Ident, mir176.SMStruck)
			}
			observerHealthCmd, _, err := decodeMessageLikeClient(<-observerHealthFrameCh)
			if err != nil {
				t.Fatalf("decode observer health frame error = %v", err)
			}
			if observerHealthCmd.Ident != mir176.SMHealthSpellChanged {
				t.Fatalf("observer health ident = %d, want SMHealthSpellChanged (%d)", observerHealthCmd.Ident, mir176.SMHealthSpellChanged)
			}
			struckCmd, struckBody, err := decodeMessageLikeClient(<-struckFrameCh)
			if err != nil {
				t.Fatalf("decode target struck frame error = %v", err)
			}
			if struckCmd.Ident != mir176.SMStruck {
				t.Fatalf("struck ident = %d, want SM_STRUCK (%d)", struckCmd.Ident, mir176.SMStruck)
			}
			if struckCmd.Param == 0 || struckCmd.Param >= uint16(startingHP) {
				t.Fatalf("struck hp = %d, want reduced target hp", struckCmd.Param)
			}
			assertMessageBodyWL(t, struckBody, s.world.HumanFeatureForCharacter(target), 0, world.MonsterActorID(world.Monster{ID: attacker.ID}), 0)
			healthCmd, _, err := decodeMessageLikeClient(<-healthFrameCh)
			if err != nil {
				t.Fatalf("decode target health frame error = %v", err)
			}
			if healthCmd.Ident != mir176.SMHealthSpellChanged {
				t.Fatalf("health ident = %d, want SMHealthSpellChanged (%d)", healthCmd.Ident, mir176.SMHealthSpellChanged)
			}
			<-done
		})
	}
}

func TestHandleHitDelaysImpactBroadcast(t *testing.T) {
	s := newTestServer(t)
	s.hitImpactDelay = 40 * time.Millisecond
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	monsters, _ := s.world.SnapshotAround(ch.MapID, 0, 0, 99999)
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

func TestHandleSpellConsumesManaAndUpdatesSkillState(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	ch.Level = 7
	skill, ok := s.world.Skill("火球术")
	if !ok {
		t.Fatalf("skill 火球术 missing from config")
	}
	cost := s.world.SpellCost(skill, ch.Skills[0])
	ch.MP = cost + 10
	spawned, err := s.world.SpawnMonsterByNameAt(mapID, x+1, y, "白野猪", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(spawned.Monsters) == 0 {
		t.Fatalf("SpawnMonsterByNameAt() returned no monsters")
	}
	mon := spawned.Monsters[0]

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)
	observer, err := s.world.CreateCharacterWithAppearance("test", "watcher", "wizard", 0, 0, mapID, x+2, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	recog := int32(uint32(mon.X) | uint32(mon.Y)<<16)
	spellFrameCh := make(chan []byte, 1)
	struckFrameCh := make(chan []byte, 1)
	magicFrameCh := make(chan []byte, 1)
	go func() {
		spellFrameCh <- readFrame(t, observerClient)
		struckFrameCh <- readFrame(t, observerClient)
		magicFrameCh <- readFrame(t, observerClient)
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: recog, Param: 1, Tag: uint16(ch.Dir)})
	}()

	assertActionAck(t, readFrame(t, client))
	firstFrame := readFrame(t, client)
	firstCmd, firstBody, err := decodeMessageLikeClient(firstFrame)
	if err != nil {
		t.Fatalf("decode first caster frame error = %v", err)
	}
	if firstCmd.Ident == mir176.SMSendMyMagic {
		firstCmd, firstBody, err = decodeMessageLikeClient(readFrame(t, client))
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
	}
	if firstCmd.Ident != mir176.SMStruck {
		t.Fatalf("first caster frame ident = %d, want SMStruck (%d)", firstCmd.Ident, mir176.SMStruck)
	}
	if firstCmd.Recog != world.MonsterActorID(mon) {
		t.Fatalf("first caster struck recog = %d, want %d", firstCmd.Recog, world.MonsterActorID(mon))
	}
	assertMessageBodyWL(t, firstBody, world.MonsterFeature(mon), 0, ActorID, 0)
	if firstCmd.Param == 0 || firstCmd.Tag == 0 || firstCmd.Series == 0 {
		t.Fatalf("first caster struck cmd = %+v, want non-zero hp/maxhp/damage", firstCmd)
	}
	statsCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode health/spell frame error = %v", err)
	}
	if statsCmd.Ident != mir176.SMHealthSpellChanged {
		t.Fatalf("health/spell ident = %d, want SMHealthSpellChanged (%d)", statsCmd.Ident, mir176.SMHealthSpellChanged)
	}
	magicCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode magic fire frame error = %v", err)
	}
	if magicCmd.Ident != mir176.SMMagicFire {
		t.Fatalf("magic fire ident = %d, want SMMagicFire (%d)", magicCmd.Ident, mir176.SMMagicFire)
	}
	frame := <-spellFrameCh
	spellCmd, spellBody, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode spell frame error = %v", err)
	}
	if spellCmd.Ident != mir176.SMSpell {
		t.Fatalf("spell ident = %d, want SM_SPELL (%d)", spellCmd.Ident, mir176.SMSpell)
	}
	if got := string(spellBody); got != "1" {
		t.Fatalf("spell body = %q, want magic id string", got)
	}
	frame = <-struckFrameCh
	struckCmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode struck frame error = %v", err)
	}
	if struckCmd.Ident != mir176.SMStruck {
		t.Fatalf("struck ident = %d, want SM_STRUCK (%d)", struckCmd.Ident, mir176.SMStruck)
	}
	frame = <-magicFrameCh
	fireCmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode magic fire frame error = %v", err)
	}
	if fireCmd.Ident != mir176.SMMagicFire {
		t.Fatalf("magic fire ident = %d, want SMMagicFire (%d)", fireCmd.Ident, mir176.SMMagicFire)
	}
	<-done
	if ch.MP != cost+10-cost {
		t.Fatalf("MP = %d, want %d after cast", ch.MP, cost+10-cost)
	}
	if got := ch.Skills[0].Train; got < 1 || got > 3 {
		t.Fatalf("skill train = %d, want 1..3", got)
	}
	if ch.Skills[0].LastCastAt == 0 {
		t.Fatalf("skill LastCastAt = 0, want set")
	}
}

func TestHandleSpellBigFireballBroadcastsMonsterHit(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Skills = storage.SkillStates{{ID: "大火球", Level: 0, Train: 0}}
	ch.Level = 19
	skill, ok := s.world.Skill("大火球")
	if !ok {
		t.Fatalf("skill 大火球 missing from config")
	}
	cost := s.world.SpellCost(skill, ch.Skills[0])
	ch.MP = cost + 10
	spawned, err := s.world.SpawnMonsterByNameAt(mapID, x+1, y, "白野猪", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(spawned.Monsters) == 0 {
		t.Fatalf("SpawnMonsterByNameAt() returned no monsters")
	}
	mon := spawned.Monsters[0]

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)
	observer, err := s.world.CreateCharacterWithAppearance("test", "watcher", "wizard", 0, 0, mapID, x+2, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	recog := int32(uint32(mon.X) | uint32(mon.Y)<<16)
	spellFrameCh := make(chan []byte, 1)
	struckFrameCh := make(chan []byte, 1)
	magicFrameCh := make(chan []byte, 1)
	go func() {
		spellFrameCh <- readFrame(t, observerClient)
		struckFrameCh <- readFrame(t, observerClient)
		magicFrameCh <- readFrame(t, observerClient)
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: recog, Param: 5, Tag: uint16(ch.Dir)})
	}()

	assertActionAck(t, readFrame(t, client))
	spellSelfCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode self spell frame error = %v", err)
	}
	if spellSelfCmd.Ident != mir176.SMStruck {
		t.Fatalf("self struck ident = %d, want SM_STRUCK (%d)", spellSelfCmd.Ident, mir176.SMStruck)
	}
	statsCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode health/spell frame error = %v", err)
	}
	if statsCmd.Ident != mir176.SMHealthSpellChanged {
		t.Fatalf("health/spell ident = %d, want SM_HEALTHSPELLChanged (%d)", statsCmd.Ident, mir176.SMHealthSpellChanged)
	}
	magicCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode magic fire frame error = %v", err)
	}
	if magicCmd.Ident != mir176.SMMagicFire {
		t.Fatalf("magic fire ident = %d, want SMMagicFire (%d)", magicCmd.Ident, mir176.SMMagicFire)
	}
	frame := <-spellFrameCh
	spellCmd, spellBody, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode spell frame error = %v", err)
	}
	if spellCmd.Ident != mir176.SMSpell {
		t.Fatalf("spell ident = %d, want SM_SPELL (%d)", spellCmd.Ident, mir176.SMSpell)
	}
	if got := string(spellBody); got != "5" {
		t.Fatalf("spell body = %q, want 5", got)
	}
	frame = <-struckFrameCh
	struckCmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode struck frame error = %v", err)
	}
	if struckCmd.Ident != mir176.SMStruck {
		t.Fatalf("struck ident = %d, want SM_STRUCK (%d)", struckCmd.Ident, mir176.SMStruck)
	}
	frame = <-magicFrameCh
	fireCmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode magic fire frame error = %v", err)
	}
	if fireCmd.Ident != mir176.SMMagicFire {
		t.Fatalf("magic fire ident = %d, want SMMagicFire (%d)", fireCmd.Ident, mir176.SMMagicFire)
	}
	<-done
	if ch.MP != cost+10-cost {
		t.Fatalf("MP = %d, want %d after cast", ch.MP, cost+10-cost)
	}
	if got := ch.Skills[0].Train; got < 1 || got > 3 {
		t.Fatalf("skill train = %d, want 1..3", got)
	}
	if ch.Skills[0].LastCastAt == 0 {
		t.Fatalf("skill LastCastAt = 0, want set")
	}
}

func TestHandleSpellMagicShieldRefreshesAbilityPanel(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Level = 31
	ch.Skills = storage.SkillStates{{ID: "魔法盾", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("魔法盾")
	if !ok {
		t.Fatalf("skill 魔法盾 missing from config")
	}
	cost := s.world.SpellCost(skill, ch.Skills[0])
	ch.MP = cost + 10
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 31, Tag: uint16(ch.Dir)})
	}()

	assertActionAck(t, readFrame(t, client))
	healthCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode health frame error = %v", err)
	}
	if healthCmd.Ident != mir176.SMHealthSpellChanged {
		t.Fatalf("health frame ident = %d, want SMHealthSpellChanged (%d)", healthCmd.Ident, mir176.SMHealthSpellChanged)
	}
	magicCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode magic fire frame error = %v", err)
	}
	if magicCmd.Ident != mir176.SMMagicFire {
		t.Fatalf("magic fire ident = %d, want SMMagicFire (%d)", magicCmd.Ident, mir176.SMMagicFire)
	}
	<-done
	if ch.BubbleDefenceUntil == 0 {
		t.Fatal("BubbleDefenceUntil = 0, want active shield")
	}
}

func TestHandleSpellHealRefreshesCasterAndObservers(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	caster.Level = 20
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("治愈术")
	if !ok {
		t.Fatalf("skill 治愈术 missing from config")
	}
	stats := s.world.AbilityStats(caster)
	caster.MaxHP = stats.MaxHP
	caster.HP = max(1, stats.MaxHP/2)
	caster.MP = s.world.SpellCost(skill, caster.Skills[0]) + 10
	observer, err := s.world.CreateCharacterWithAppearance("test", "watcher", "wizard", 0, 0, mapID, x+2, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	collectFrames := func(conn net.Conn) [][]byte {
		frames := make([][]byte, 0, 4)
		for {
			frame, ok := readFrameWithTimeout(t, conn, time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		return frames
	}
	casterFramesCh := make(chan [][]byte, 1)
	observerFramesCh := make(chan [][]byte, 1)
	go func() { casterFramesCh <- collectFrames(client) }()
	go func() { observerFramesCh <- collectFrames(observerClient) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 2, Tag: uint16(caster.Dir)})
	}()

	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh

	var casterAck, casterHealth bool
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			casterAck = true
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		if cmd.Ident == mir176.SMHealthSpellChanged {
			casterHealth = true
		}
	}
	if !casterAck {
		t.Fatal("missing caster action ack")
	}
	if !casterHealth {
		ids := make([]uint16, 0, len(casterFrames))
		for _, frame := range casterFrames {
			if cmd, _, err := decodeMessageLikeClient(frame); err == nil {
				ids = append(ids, cmd.Ident)
			}
		}
		t.Logf("caster frame ids: %v", ids)
		t.Fatal("missing caster SMHealthSpellChanged frame")
	}

	var observerSpell bool
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		if cmd.Ident == mir176.SMSpell {
			observerSpell = true
		}
	}
	if !observerSpell {
		ids := make([]uint16, 0, len(observerFrames))
		for _, frame := range observerFrames {
			if cmd, _, err := decodeMessageLikeClient(frame); err == nil {
				ids = append(ids, cmd.Ident)
			}
		}
		t.Logf("observer frame ids: %v", ids)
		t.Fatal("missing observer SMSpell frame")
	}
	if got := caster.HP; got <= stats.MaxHP/2 {
		t.Fatalf("caster HP = %d, want healed above %d", got, stats.MaxHP/2)
	}
	<-done
}

func TestHandleSpellHealRefreshesFriendlySummonObservers(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	caster.Level = 20
	caster.Dir = 2
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0, Train: 0}, {ID: "治愈术", Level: 0, Train: 0}}
	summonedResult, err := s.world.CastSkillWithPlayers(caster, "召唤骷髅", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() summon error = %v", err)
	}
	summoned := summonedResult.SummonedMonsters[0]
	if _, err := s.world.HitWithIdent(summonedResult.Character, summonedResult.Character.X, summonedResult.Character.Y, 2, mir176.CMHit); err != nil {
		t.Fatalf("HitWithIdent() damage summon error = %v", err)
	}
	observer, err := s.world.CreateCharacterWithAppearance("test", "watcher", "wizard", 0, 0, mapID, x+2, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	skill, ok := s.world.Skill("治愈术")
	if !ok {
		t.Fatalf("skill 治愈术 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[1])
	caster.MP = cost + 10

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, summonedResult.Character)
	defer s.unregisterClient(server)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	collectFrames := func(conn net.Conn) [][]byte {
		frames := make([][]byte, 0, 6)
		for {
			frame, ok := readFrameWithTimeout(t, conn, time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		return frames
	}
	casterFrames := make(chan [][]byte, 1)
	observerFrames := make(chan [][]byte, 1)
	go func() { casterFrames <- collectFrames(client) }()
	go func() { observerFrames <- collectFrames(observerClient) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &summonedResult.Character, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(summoned.X) | uint32(summoned.Y)<<16), Param: 2, Tag: uint16(caster.Dir)})
	}()

	casterCollected := <-casterFrames
	observerCollected := <-observerFrames
	<-done

	var sawCasterAck, sawCasterHealth, sawObserverSpell bool
	for _, frame := range casterCollected {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			sawCasterAck = true
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		if cmd.Ident == mir176.SMHealthSpellChanged {
			sawCasterHealth = true
		}
	}
	for _, frame := range observerCollected {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			sawObserverSpell = true
		}
	}
	if !sawCasterAck || !sawCasterHealth {
		t.Fatalf("caster frames missing ack/health: ack=%v health=%v", sawCasterAck, sawCasterHealth)
	}
	if !sawObserverSpell {
		ids := make([]uint16, 0, len(observerCollected))
		for _, frame := range observerCollected {
			if cmd, _, err := decodeMessageLikeClient(frame); err == nil {
				ids = append(ids, cmd.Ident)
			}
		}
		t.Fatalf("observer frames missing spell: spell=%v ids=%v", sawObserverSpell, ids)
	}
}

func TestHandleSpellLightningBroadcastsCharacterHit(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	target, err := s.world.CreateCharacterWithAppearance("test", "target", "warrior", 0, 0, mapID, x+4, y)
	if err != nil {
		t.Fatalf("CreateCharacter() target error = %v", err)
	}
	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, x+6, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "雷电术", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("雷电术")
	if !ok {
		t.Fatalf("skill 雷电术 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10
	target.HP = 1000
	target.MaxHP = 1000
	startingHP := target.HP

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	targetServer, targetClient := net.Pipe()
	defer targetServer.Close()
	defer targetClient.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(targetServer, target)
	defer s.unregisterClient(targetServer)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	collectFrames := func(conn net.Conn) [][]byte {
		frames := make([][]byte, 0, 4)
		for {
			frame, ok := readFrameWithTimeout(t, conn, time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		return frames
	}
	casterFramesCh := make(chan [][]byte, 1)
	targetFramesCh := make(chan [][]byte, 1)
	observerFramesCh := make(chan [][]byte, 1)
	go func() { casterFramesCh <- collectFrames(client) }()
	go func() { targetFramesCh <- collectFrames(targetClient) }()
	go func() { observerFramesCh <- collectFrames(observerClient) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(target.X) | uint32(target.Y)<<16), Param: 11, Tag: uint16(caster.Dir)})
	}()

	casterFrames := <-casterFramesCh
	targetFrames := <-targetFramesCh
	observerFrames := <-observerFramesCh

	var casterAck, casterHealth bool
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			casterAck = true
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		if cmd.Ident == mir176.SMHealthSpellChanged {
			casterHealth = true
		}
	}
	if !casterAck {
		t.Fatal("missing caster action ack")
	}
	if !casterHealth {
		t.Fatal("missing caster SMHealthSpellChanged frame")
	}

	var targetStruck, targetHealth bool
	for _, frame := range targetFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode target frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			targetStruck = true
			if cmd.Param == 0 || cmd.Param >= uint16(startingHP) {
				t.Fatalf("target struck hp = %d, want reduced hp", cmd.Param)
			}
		case mir176.SMHealthSpellChanged:
			targetHealth = true
		}
	}
	if !targetStruck {
		t.Fatal("missing target SMStruck frame")
	}
	if !targetHealth {
		t.Fatal("missing target SMHealthSpellChanged frame")
	}

	var observerSpell bool
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		if cmd.Ident == mir176.SMSpell {
			observerSpell = true
		}
	}
	if !observerSpell {
		t.Fatal("missing observer SMSpell frame")
	}

	<-done
}

func TestHandleSpellExplosionBroadcastsMonsterHits(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	mapID, x, y := testDefaultSpawn(t)
	mp, ok := bundle.Maps[mapID]
	if !ok {
		t.Fatalf("map %s missing from configs", mapID)
	}
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, x+4, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "爆裂火焰", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("爆裂火焰")
	if !ok {
		t.Fatalf("skill 爆裂火焰 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10

	targetX, targetY := -1, -1
	for dx := 6; dx < 20 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := x + dx
			ty := y + dy
			if !mp.Walkable(tx, ty) || !mp.Walkable(tx+1, ty) {
				continue
			}
			clear := true
			for _, pt := range [][2]int{{tx, ty}, {tx + 1, ty}, {tx - 1, ty}, {tx, ty + 1}, {tx, ty - 1}} {
				if monsters, _ := s.world.SnapshotAround(mapID, pt[0], pt[1], 0); len(monsters) > 0 {
					clear = false
					break
				}
			}
			if clear {
				targetX, targetY = tx, ty
				break
			}
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for explosion test")
	}
	first, err := s.world.SpawnMonsterByNameAt(mapID, targetX, targetY, "白野猪", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() first error = %v", err)
	}
	if len(first.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() first monsters = %d, want 1", len(first.Monsters))
	}
	second, err := s.world.SpawnMonsterByNameAt(mapID, targetX+1, targetY, "白野猪", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() second error = %v", err)
	}
	if len(second.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() second monsters = %d, want 1", len(second.Monsters))
	}

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	collectFrames := func(conn net.Conn) [][]byte {
		frames := make([][]byte, 0, 4)
		for {
			frame, ok := readFrameWithTimeout(t, conn, time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		return frames
	}
	casterFramesCh := make(chan [][]byte, 1)
	observerFramesCh := make(chan [][]byte, 1)
	go func() { casterFramesCh <- collectFrames(client) }()
	go func() { observerFramesCh <- collectFrames(observerClient) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 23, Tag: uint16(caster.Dir)})
	}()

	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh

	var casterAck int
	casterMonsterHits := map[int32]struct{}{}
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			casterAck++
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		if cmd.Ident == mir176.SMStruck {
			casterMonsterHits[cmd.Recog] = struct{}{}
		}
	}
	if casterAck != 1 {
		for i, frame := range casterFrames {
			if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
				t.Logf("caster frame %d: GOOD %q", i, body)
				continue
			}
			cmd, _, err := decodeMessageLikeClient(frame)
			if err != nil {
				t.Logf("caster frame %d decode error: %v", i, err)
				continue
			}
			t.Logf("caster frame %d: ident=%d recog=%d param=%d tag=%d series=%d", i, cmd.Ident, cmd.Recog, cmd.Param, cmd.Tag, cmd.Series)
		}
		t.Fatalf("caster ack count = %d, want 1", casterAck)
	}
	if _, ok := casterMonsterHits[100001]; !ok {
		t.Fatalf("caster missing hit for monster actor %d", 100001)
	}
	if _, ok := casterMonsterHits[100002]; !ok {
		t.Fatalf("caster missing hit for monster actor %d", 100002)
	}

	var observerStruck int
	observerMonsterHits := map[int32]struct{}{}
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			observerStruck++
			observerMonsterHits[cmd.Recog] = struct{}{}
		}
	}
	if observerStruck < 2 {
		t.Fatalf("observer SMStruck count = %d, want at least 2", observerStruck)
	}
	if _, ok := observerMonsterHits[100001]; !ok {
		t.Fatalf("observer missing hit for monster actor %d", 100001)
	}
	if _, ok := observerMonsterHits[100002]; !ok {
		t.Fatalf("observer missing hit for monster actor %d", 100002)
	}

	<-done
}

func TestHandleSpellHellfireBroadcastsMonsterHits(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	mapID, x, y := testDefaultSpawn(t)
	mp, ok := bundle.Maps[mapID]
	if !ok {
		t.Fatalf("map %s missing from configs", mapID)
	}
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, x+4, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "地狱火", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("地狱火")
	if !ok {
		t.Fatalf("skill 地狱火 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10

	targetX, targetY := -1, -1
	for dx := 6; dx < 20 && targetX < 0; dx++ {
		tx := x + dx
		ty := y
		clear := true
		for step := 1; step <= 5; step++ {
			if !mp.Walkable(x+step, y) {
				clear = false
				break
			}
		}
		if !clear || !mp.Walkable(tx, ty) {
			continue
		}
		targetX, targetY = tx, ty
	}
	if targetX < 0 {
		t.Fatal("could not find clear line for hellfire test")
	}
	first, err := s.world.SpawnMonsterByNameAt(mapID, x+1, y, "白野猪", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() first error = %v", err)
	}
	if len(first.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() first monsters = %d, want 1", len(first.Monsters))
	}
	second, err := s.world.SpawnMonsterByNameAt(mapID, x+4, y, "白野猪", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() second error = %v", err)
	}
	if len(second.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() second monsters = %d, want 1", len(second.Monsters))
	}

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	collectFrames := func(conn net.Conn) [][]byte {
		frames := make([][]byte, 0, 4)
		for {
			frame, ok := readFrameWithTimeout(t, conn, time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		return frames
	}
	casterFramesCh := make(chan [][]byte, 1)
	observerFramesCh := make(chan [][]byte, 1)
	go func() { casterFramesCh <- collectFrames(client) }()
	go func() { observerFramesCh <- collectFrames(observerClient) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 9, Tag: uint16(caster.Dir)})
	}()

	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh

	var casterAck int
	casterMonsterHits := map[int32]struct{}{}
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			casterAck++
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		if cmd.Ident == mir176.SMStruck {
			casterMonsterHits[cmd.Recog] = struct{}{}
		}
	}
	if casterAck != 1 {
		t.Fatalf("caster ack count = %d, want 1", casterAck)
	}
	if _, ok := casterMonsterHits[100001]; !ok {
		t.Fatalf("caster missing hit for monster actor %d", 100001)
	}
	if _, ok := casterMonsterHits[100002]; !ok {
		t.Fatalf("caster missing hit for monster actor %d", 100002)
	}

	var observerStruck int
	observerMonsterHits := map[int32]struct{}{}
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			observerStruck++
			observerMonsterHits[cmd.Recog] = struct{}{}
		}
	}
	if observerStruck < 2 {
		t.Fatalf("observer SMStruck count = %d, want at least 2", observerStruck)
	}
	if _, ok := observerMonsterHits[100001]; !ok {
		t.Fatalf("observer missing hit for monster actor %d", 100001)
	}
	if _, ok := observerMonsterHits[100002]; !ok {
		t.Fatalf("observer missing hit for monster actor %d", 100002)
	}

	<-done
}

func TestHandleSpellLightningLineBroadcastsHits(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	mapID, x, y := testDefaultSpawn(t)
	mp, ok := bundle.Maps[mapID]
	if !ok {
		t.Fatalf("map %s missing from configs", mapID)
	}
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "疾光电影", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("疾光电影")
	if !ok {
		t.Fatalf("skill 疾光电影 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10

	targetX, targetY := -1, -1
	for dx := 8; dx < 16 && targetX < 0; dx++ {
		tx := x + dx
		ty := y
		clear := true
		for step := 1; step <= 8; step++ {
			if !mp.Walkable(x+step, y) {
				clear = false
				break
			}
		}
		if !clear || !mp.Walkable(tx, ty) {
			continue
		}
		targetX, targetY = tx, ty
	}
	if targetX < 0 {
		t.Fatal("could not find clear line for lightning test")
	}
	first, err := s.world.SpawnMonsterByNameAt(mapID, x+1, y, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() first error = %v", err)
	}
	if len(first.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() first monsters = %d, want 1", len(first.Monsters))
	}
	second, err := s.world.SpawnMonsterByNameAt(mapID, x+4, y, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() second error = %v", err)
	}
	if len(second.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() second monsters = %d, want 1", len(second.Monsters))
	}
	target, err := s.world.CreateCharacterWithAppearance("test", "target", "warrior", 0, 0, mapID, x+6, y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() target error = %v", err)
	}
	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, x+8, y)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() observer error = %v", err)
	}

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	targetServer, targetClient := net.Pipe()
	defer targetServer.Close()
	defer targetClient.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(targetServer, target)
	defer s.unregisterClient(targetServer)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	collectFrames := func(conn net.Conn) [][]byte {
		frames := make([][]byte, 0, 6)
		for {
			frame, ok := readFrameWithTimeout(t, conn, time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		return frames
	}
	casterFramesCh := make(chan [][]byte, 1)
	targetFramesCh := make(chan [][]byte, 1)
	observerFramesCh := make(chan [][]byte, 1)
	go func() { casterFramesCh <- collectFrames(client) }()
	go func() { targetFramesCh <- collectFrames(targetClient) }()
	go func() { observerFramesCh <- collectFrames(observerClient) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 10, Tag: uint16(caster.Dir)})
	}()

	casterFrames := <-casterFramesCh
	targetFrames := <-targetFramesCh
	observerFrames := <-observerFramesCh

	var casterAck, casterHealth, casterStruck int
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			casterAck++
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			casterStruck++
		case mir176.SMHealthSpellChanged:
			casterHealth++
		}
	}
	if casterAck != 1 {
		t.Fatalf("caster ack count = %d, want 1", casterAck)
	}
	if casterStruck != 5 {
		t.Fatalf("caster SMStruck count = %d, want 5", casterStruck)
	}
	if casterHealth != 4 {
		t.Fatalf("caster SMHealthSpellChanged count = %d, want 4", casterHealth)
	}

	var targetSpell, targetStruck, targetHealth bool
	for _, frame := range targetFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode target frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			targetSpell = true
		case mir176.SMStruck:
			targetStruck = true
		case mir176.SMHealthSpellChanged:
			targetHealth = true
		}
	}
	if !targetSpell {
		t.Fatal("missing target SMSpell frame")
	}
	if !targetStruck {
		t.Fatal("missing target SMStruck frame")
	}
	if !targetHealth {
		t.Fatal("missing target SMHealthSpellChanged frame")
	}

	var observerStruck, observerHealth bool
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			observerStruck = true
		case mir176.SMHealthSpellChanged:
			observerHealth = true
		}
	}
	if !observerStruck {
		t.Fatal("missing observer SMStruck frame")
	}
	if !observerHealth {
		t.Fatal("missing observer SMHealthSpellChanged frame")
	}

	<-done
}

func TestHandleSpellTrapBroadcastsMonsterHits(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	mapID, x, y := testDefaultSpawn(t)
	mp, ok := bundle.Maps[mapID]
	if !ok {
		t.Fatalf("map %s missing from configs", mapID)
	}
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "困魔咒", Level: 5, Train: 0}}
	skill, ok := s.world.Skill("困魔咒")
	if !ok {
		t.Fatalf("skill 困魔咒 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10

	targetX, targetY := -1, -1
	for dx := 6; dx < 20 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := x + dx
			ty := y + dy
			if !mp.Walkable(tx, ty) || !mp.Walkable(tx+1, ty) {
				continue
			}
			clear := true
			for _, pt := range [][2]int{{tx, ty}, {tx + 1, ty}, {tx - 1, ty}, {tx, ty + 1}, {tx, ty - 1}} {
				if monsters, _ := s.world.SnapshotAround(mapID, pt[0], pt[1], 0); len(monsters) > 0 {
					clear = false
					break
				}
			}
			if clear {
				targetX, targetY = tx, ty
				break
			}
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for trap test")
	}
	firstPack, err := s.world.SpawnMonsterByNameAt(mapID, targetX, targetY, "白野猪", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() first error = %v", err)
	}
	if len(firstPack.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() first monsters = %d, want 1", len(firstPack.Monsters))
	}
	secondPack, err := s.world.SpawnMonsterByNameAt(mapID, targetX+1, targetY, "白野猪", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() second error = %v", err)
	}
	if len(secondPack.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() second monsters = %d, want 1", len(secondPack.Monsters))
	}
	firstMonster := firstPack.Monsters[0]
	secondMonster := secondPack.Monsters[0]

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	observer, err := s.world.CreateCharacterWithAppearance("test", "watcher", "wizard", 0, 0, mapID, x+4, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	collectFrames := func(conn net.Conn) [][]byte {
		frames := make([][]byte, 0, 8)
		for {
			frame, ok := readFrameWithTimeout(t, conn, 3*time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		return frames
	}
	casterFramesCh := make(chan [][]byte, 1)
	observerFramesCh := make(chan [][]byte, 1)
	go func() { casterFramesCh <- collectFrames(client) }()
	go func() { observerFramesCh <- collectFrames(observerClient) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 16, Tag: uint16(caster.Dir)})
	}()

	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh

	var casterAck, casterHealth int
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			casterAck++
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMHealthSpellChanged:
			casterHealth++
		}
	}
	if casterAck != 1 {
		t.Fatalf("caster ack count = %d, want 1", casterAck)
	}
	if casterHealth != 1 {
		t.Fatalf("caster SMHealthSpellChanged count = %d, want 1", casterHealth)
	}

	var observerSpell, observerStruck int
	seenMonsterHits := map[int32]struct{}{}
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			observerSpell++
		case mir176.SMStruck:
			observerStruck++
			seenMonsterHits[cmd.Recog] = struct{}{}
		}
	}
	if observerSpell != 1 {
		t.Fatalf("observer SMSpell count = %d, want 1", observerSpell)
	}
	if observerStruck < 2 {
		t.Fatalf("observer SMStruck count = %d, want at least 2", observerStruck)
	}
	if _, ok := seenMonsterHits[world.MonsterActorID(firstMonster)]; !ok {
		t.Fatalf("missing first monster hit frame for actor %d", world.MonsterActorID(firstMonster))
	}
	if _, ok := seenMonsterHits[world.MonsterActorID(secondMonster)]; !ok {
		t.Fatalf("missing second monster hit frame for actor %d", world.MonsterActorID(secondMonster))
	}

	<-done
}

func TestHandleSpellSummonRefreshesMonsterAppearBroadcast(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Level = 19
	ch.Dir = 2
	ch.Skills = storage.SkillStates{{ID: "召唤骷髅", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("召唤骷髅")
	if !ok {
		t.Fatalf("skill 召唤骷髅 missing from config")
	}
	cost := s.world.SpellCost(skill, ch.Skills[0])
	ch.MP = cost + 10
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)
	observer, err := s.world.CreateCharacterWithAppearance("test", "watcher", "wizard", 0, 0, mapID, x+4, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	collectFrames := func(conn net.Conn) [][]byte {
		frames := make([][]byte, 0, 8)
		for {
			frame, ok := readFrameWithTimeout(t, conn, time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		return frames
	}
	casterFramesCh := make(chan [][]byte, 1)
	observerFramesCh := make(chan [][]byte, 1)
	go func() { casterFramesCh <- collectFrames(client) }()
	go func() { observerFramesCh <- collectFrames(observerClient) }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 17, Tag: uint16(ch.Dir)})
	}()
	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh
	descLen := len(EncodeBuffer(make([]byte, 8)))
	var casterAck, casterTurn, casterFeature, casterHealth, casterFire bool
	var observerSpell, observerTurn, observerFeature, observerFire bool
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && len(body) > 0 && body[0] == '+' {
			if strings.HasPrefix(string(body), "+GOOD/") {
				casterAck = true
			}
			continue
		}
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			if got := string(body); got != "17" {
				t.Fatalf("caster spell body = %q, want magic id string", got)
			}
		case mir176.SMTurn:
			casterTurn = true
			if len(body) <= descLen {
				t.Fatalf("caster summon turn body len = %d, want > %d", len(body), descLen)
			}
			namePayload, err := mir176.DecodePlain6Payload(body[descLen:])
			if err != nil {
				t.Fatalf("decode caster summon monster name error = %v", err)
			}
			if got := DecodeString(namePayload); got != "骷髅/255" {
				t.Fatalf("caster summon monster name = %q, want %q", got, "骷髅/255")
			}
		case mir176.SMFeatureChanged:
			casterFeature = true
		case mir176.SMHealthSpellChanged:
			casterHealth = true
		case mir176.SMMagicFire:
			casterFire = true
		}
	}
	for _, frame := range observerFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && len(body) > 0 && body[0] == '+' {
			continue
		}
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			observerSpell = true
			if got := string(body); got != "17" {
				t.Fatalf("observer spell body = %q, want magic id string", got)
			}
		case mir176.SMTurn:
			observerTurn = true
			if len(body) <= descLen {
				t.Fatalf("observer summon turn body len = %d, want > %d", len(body), descLen)
			}
			namePayload, err := mir176.DecodePlain6Payload(body[descLen:])
			if err != nil {
				t.Fatalf("decode observer summon monster name error = %v", err)
			}
			if got := DecodeString(namePayload); got != "骷髅/255" {
				t.Fatalf("observer summon monster name = %q, want %q", got, "骷髅/255")
			}
		case mir176.SMFeatureChanged:
			observerFeature = true
		case mir176.SMMagicFire:
			observerFire = true
		}
	}
	if !casterAck || !casterTurn || !casterFeature || !casterHealth || !casterFire {
		t.Fatalf("caster frames missing: ack=%v turn=%v feature=%v health=%v fire=%v", casterAck, casterTurn, casterFeature, casterHealth, casterFire)
	}
	if !observerSpell || !observerTurn || !observerFeature || !observerFire {
		t.Fatalf("observer frames missing: spell=%v turn=%v feature=%v fire=%v", observerSpell, observerTurn, observerFeature, observerFire)
	}
	<-done
	if ch.MP != cost+10-cost {
		t.Fatalf("MP = %d, want %d after summon", ch.MP, cost+10-cost)
	}
}

func TestHandleSpellTamingRefreshesMonsterFeatureBroadcast(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "tester", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	caster.Level = 100
	caster.Dir = 2
	caster.Skills = storage.SkillStates{{ID: "诱惑之光", Level: 10, Train: 0}}
	skill, ok := s.world.Skill("诱惑之光")
	if !ok {
		t.Fatalf("skill 诱惑之光 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10
	spawned, err := s.world.SpawnMonsterByNameAt(mapID, x+1, y, "鸡", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(spawned.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 1", len(spawned.Monsters))
	}
	mon := spawned.Monsters[0]
	setWorldRandSource(t, s.world, &fixedRandSource{vals: []int64{0, 0, 0, 0}})

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	observer, err := s.world.CreateCharacterWithAppearance("test", "watcher", "wizard", 0, 0, mapID, x+4, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	collectFrames := func(conn net.Conn) [][]byte {
		frames := make([][]byte, 0, 6)
		for {
			frame, ok := readFrameWithTimeout(t, conn, time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		return frames
	}
	casterFramesCh := make(chan [][]byte, 1)
	observerFramesCh := make(chan [][]byte, 1)
	go func() { casterFramesCh <- collectFrames(client) }()
	go func() { observerFramesCh <- collectFrames(observerClient) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x+1) | uint32(y)<<16), Param: 20, Tag: uint16(caster.Dir)})
	}()

	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh

	if len(casterFrames) == 0 {
		t.Fatal("missing caster frames")
	}
	assertActionAck(t, casterFrames[0])
	var casterFeatureSeen, casterHealthSeen bool
	for _, frame := range casterFrames[1:] {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMFeatureChanged:
			if cmd.Recog != world.MonsterActorID(mon) {
				t.Fatalf("caster feature recog = %d, want monster actor %d", cmd.Recog, world.MonsterActorID(mon))
			}
			casterFeatureSeen = true
		case mir176.SMHealthSpellChanged:
			casterHealthSeen = true
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	if !casterFeatureSeen || !casterHealthSeen {
		ids := make([]uint16, 0, len(casterFrames))
		for _, frame := range casterFrames {
			if cmd, _, err := decodeMessageLikeClient(frame); err == nil {
				ids = append(ids, cmd.Ident)
			}
		}
		t.Fatalf("caster frames missing feature/health: feature=%v health=%v ids=%v", casterFeatureSeen, casterHealthSeen, ids)
	}
	if len(observerFrames) == 0 {
		t.Fatal("missing observer frames")
	}
	observerCmd, _, err := decodeMessageLikeClient(observerFrames[0])
	if err != nil {
		t.Fatalf("decode observer frame error = %v", err)
	}
	if observerCmd.Ident != mir176.SMSpell {
		t.Fatalf("observer spell ident = %d, want SMSpell (%d)", observerCmd.Ident, mir176.SMSpell)
	}
	var observerFeatureCmd mir176.Command
	var observerFeatureSeen bool
	for _, frame := range observerFrames[1:] {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMFeatureChanged:
			observerFeatureCmd = cmd
			observerFeatureSeen = true
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected observer frame ident=%d", cmd.Ident)
		}
	}
	if !observerFeatureSeen {
		t.Fatal("missing observer feature frame")
	}
	if observerFeatureCmd.Recog != world.MonsterActorID(mon) {
		t.Fatalf("observer feature recog = %d, want monster actor %d", observerFeatureCmd.Recog, world.MonsterActorID(mon))
	}

	<-done
	if caster.MP != cost+10-cost {
		t.Fatalf("MP = %d, want %d after taming", caster.MP, cost+10-cost)
	}
	monsters, _ := s.world.SnapshotAround(mapID, x, y, 4)
	var controlled bool
	for _, current := range monsters {
		if current.ID != mon.ID {
			continue
		}
		if current.MasterID != caster.ID {
			t.Fatalf("controlled.MasterID = %q, want %q", current.MasterID, caster.ID)
		}
		controlled = true
	}
	if !controlled {
		t.Fatalf("tamed monster %s not found in snapshot", mon.ID)
	}
}

func TestHandleSpellInsightSendsTargetStateToCaster(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	target, err := s.world.CreateCharacterWithAppearance("test", "target", "wizard", 0, 0, mapID, x+4, y)
	if err != nil {
		t.Fatalf("CreateCharacter() target error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "心灵启示", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("心灵启示")
	if !ok {
		t.Fatalf("skill 心灵启示 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	targetServer, targetClient := net.Pipe()
	defer targetServer.Close()
	defer targetClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(targetServer, target)
	defer s.unregisterClient(targetServer)

	casterFrames := make(chan []byte, 8)
	targetFrames := make(chan []byte, 4)
	go func() {
		casterFrames <- readFrame(t, client)
		casterFrames <- readFrame(t, client)
		casterFrames <- readFrame(t, client)
		casterFrames <- readFrame(t, client)
	}()
	go func() {
		targetFrames <- readFrame(t, targetClient)
		targetFrames <- readFrame(t, targetClient)
		targetFrames <- readFrame(t, targetClient)
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(target.X) | uint32(target.Y)<<16), Param: 28, Tag: uint16(caster.Dir)})
	}()

	sequence := make([]uint16, 0, 5)
	for i := 0; i < 4; i++ {
		frame := <-casterFrames
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			sequence = append(sequence, 0xffff)
			continue
		}
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame %d error = %v", i, err)
		}
		sequence = append(sequence, cmd.Ident)
		switch cmd.Ident {
		case mir176.SMSendMyMagic:
		case mir176.SMHealthSpellChanged:
		case mir176.SMSendUserState:
			decoded, err := mir176.DecodePlain6Payload(body)
			if err != nil {
				t.Fatalf("DecodePlain6Payload() error = %v", err)
			}
			if len(decoded) < 5 {
				t.Fatalf("state body too short: %d", len(decoded))
			}
			nameLen := int(decoded[4])
			if len(decoded) < 5+nameLen {
				t.Fatalf("state body name len = %d beyond payload %d", nameLen, len(decoded))
			}
			if got := DecodeString(decoded[5 : 5+nameLen]); got != target.Name {
				t.Fatalf("user state name = %q, want %q", got, target.Name)
			}
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected caster frame ident=%d body=%q", cmd.Ident, string(body))
		}
	}
	if len(sequence) != 4 {
		t.Fatalf("caster frame sequence len = %d, want 4", len(sequence))
	}
	if sequence[0] != 0xffff {
		t.Fatalf("first caster frame = %d, want action ack", sequence[0])
	}
	if sequence[1] != mir176.SMSendUserState {
		t.Fatalf("second caster frame = %d, want SMSendUserState", sequence[1])
	}
	if sequence[2] != mir176.SMHealthSpellChanged {
		t.Fatalf("third caster frame = %d, want SMHealthSpellChanged", sequence[2])
	}
	if sequence[3] != mir176.SMMagicFire {
		t.Fatalf("fourth caster frame = %d, want SMMagicFire", sequence[3])
	}
	var sawTargetSpell, sawTargetHealth bool
	for i := 0; i < 3; i++ {
		cmd, _, err := decodeMessageLikeClient(<-targetFrames)
		if err != nil {
			t.Fatalf("decode target frame %d error = %v", i, err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			sawTargetSpell = true
		case mir176.SMHealthSpellChanged:
			sawTargetHealth = true
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected target frame ident=%d", cmd.Ident)
		}
	}
	if !sawTargetSpell {
		t.Fatal("missing target SMSpell frame")
	}
	if !sawTargetHealth {
		t.Fatal("missing target SMHealthSpellChanged frame")
	}
	<-done
	if caster.MP != cost+10-cost {
		t.Fatalf("MP = %d, want %d after insight", caster.MP, cost+10-cost)
	}
}

func TestHandleSpellInsightRejectsImmediateReuse(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	targetA, err := s.world.CreateCharacterWithAppearance("test", "target-a", "wizard", 0, 0, mapID, x+4, y)
	if err != nil {
		t.Fatalf("CreateCharacter() targetA error = %v", err)
	}
	targetB, err := s.world.CreateCharacterWithAppearance("test", "target-b", "warrior", 0, 0, mapID, x+5, y)
	if err != nil {
		t.Fatalf("CreateCharacter() targetB error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "心灵启示", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("心灵启示")
	if !ok {
		t.Fatalf("skill 心灵启示 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 20
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	targetAServer, targetAClient := net.Pipe()
	defer targetAServer.Close()
	defer targetAClient.Close()
	targetBServer, targetBClient := net.Pipe()
	defer targetBServer.Close()
	defer targetBClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(targetAServer, targetA)
	defer s.unregisterClient(targetAServer)
	s.registerClient(targetBServer, targetB)
	defer s.unregisterClient(targetBServer)

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetA.X) | uint32(targetA.Y)<<16), Param: 28, Tag: uint16(caster.Dir)})
	}()
	targetAFrames := make(chan []byte, 3)
	targetBFrames := make(chan []byte, 2)
	go func() {
		targetAFrames <- readFrame(t, targetAClient)
		targetAFrames <- readFrame(t, targetAClient)
		targetAFrames <- readFrame(t, targetAClient)
	}()
	go func() {
		targetBFrames <- readFrame(t, targetBClient)
		targetBFrames <- readFrame(t, targetBClient)
	}()
	for i := 0; i < 4; i++ {
		_ = readFrame(t, client)
	}
	for i := 0; i < 3; i++ {
		_ = <-targetAFrames
	}
	for i := 0; i < 2; i++ {
		_ = <-targetBFrames
	}
	<-firstDone
	caster.Skills[0].LastCastAt = time.Now().UnixMilli()

	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetB.X) | uint32(targetB.Y)<<16), Param: 28, Tag: uint16(caster.Dir)})
	}()
	frame := readFrame(t, client)
	cmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode second frame error = %v", err)
	}
	if cmd.Ident != mir176.SMMagicFireFail {
		t.Fatalf("second frame ident = %v, want SMMagicFireFail", cmd.Ident)
	}
	assertActionFail(t, readFrame(t, client))
	<-secondDone
}

func TestApplyWorldTickSendsCloseHealthForExpiredShowHP(t *testing.T) {
	s := newTestServer(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, "D12", 0, 0)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	updated := ch
	updated.ShowHPUntil = 0
	now := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.applyWorldTick(world.TickResult{
			ShowHPExpiredCharacters: []storage.Character{updated},
		}, now)
	}()

	closeCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode close health frame error = %v", err)
	}
	if closeCmd.Ident != mir176.SMCloseHealth {
		t.Fatalf("frame ident = %d, want SM_CLOSEHEALTH (%d)", closeCmd.Ident, mir176.SMCloseHealth)
	}
	if closeCmd.Recog != world.CharacterActorID(updated) {
		t.Fatalf("close health recog = %d, want %d", closeCmd.Recog, world.CharacterActorID(updated))
	}
	<-done
}

func TestApplyWorldTickSendsOpenHealthForPendingShowHP(t *testing.T) {
	s := newTestServer(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, "D12", 0, 0)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	updated := ch
	now := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.applyWorldTick(world.TickResult{
			ShowHPOpenedCharacters: []storage.Character{updated},
		}, now)
	}()

	openCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode open health frame error = %v", err)
	}
	if openCmd.Ident != mir176.SMOpenHealth {
		t.Fatalf("frame ident = %d, want SM_OPENHEALTH (%d)", openCmd.Ident, mir176.SMOpenHealth)
	}
	if openCmd.Recog != world.CharacterActorID(updated) {
		t.Fatalf("open health recog = %d, want %d", openCmd.Recog, world.CharacterActorID(updated))
	}
	if openCmd.Param != uint16(updated.HP) || openCmd.Tag != uint16(updated.MaxHP) {
		t.Fatalf("open health payload = %+v, want hp=%d max=%d", openCmd, updated.HP, updated.MaxHP)
	}
	<-done
}

func TestHandleSpellStealthRefreshesStateForCasterAndObservers(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, x+4, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "隐身术", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("隐身术")
	if !ok {
		t.Fatalf("skill 隐身术 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.Level = 20
	caster.MP = cost + 10
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	collectFrames := func(conn net.Conn, capHint int) [][]byte {
		frames := make([][]byte, 0, capHint)
		for {
			frame, ok := readFrameWithTimeout(t, conn, time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		return frames
	}
	casterFramesCh := make(chan [][]byte, 1)
	observerFramesCh := make(chan [][]byte, 1)
	go func() { casterFramesCh <- collectFrames(client, 5) }()
	go func() { observerFramesCh <- collectFrames(observerClient, 3) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(caster.X) | uint32(caster.Y)<<16), Param: 18, Tag: uint16(caster.Dir)})
	}()

	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh
	var sawAck, sawState, sawHealth bool
	for i, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			sawAck = true
			continue
		}
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame %d error = %v", i, err)
		}
		switch cmd.Ident {
		case mir176.SMSendMyMagic:
		case mir176.SMSpacemoveShow:
			sawState = true
			assertCharDesc(t, body, s.world.HumanFeatureForCharacter(caster), 2)
		case mir176.SMHealthSpellChanged:
			sawHealth = true
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected caster frame ident=%d body=%q", cmd.Ident, string(body))
		}
	}
	if !sawAck {
		t.Fatal("missing action ack frame")
	}
	if !sawState {
		t.Fatal("missing SMSpacemoveShow frame")
	}
	if !sawHealth {
		t.Fatal("missing SMHealthSpellChanged frame")
	}

	var sawObserverSpell, sawObserverState bool
	for i, frame := range observerFrames {
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame %d error = %v", i, err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			sawObserverSpell = true
		case mir176.SMSpacemoveShow:
			sawObserverState = true
			assertCharDesc(t, body, s.world.HumanFeatureForCharacter(caster), 2)
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected observer frame ident=%d body=%q", cmd.Ident, string(body))
		}
	}
	if !sawObserverSpell {
		t.Fatal("missing observer SMSpell frame")
	}
	if !sawObserverState {
		t.Fatal("missing observer SMSpacemoveShow frame")
	}
	<-done
	if caster.TransparentUntil == 0 {
		t.Fatal("TransparentUntil = 0, want active stealth")
	}
}

func TestHandleSpellGroupStealthRefreshesStateForGroupMembers(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	friend, err := s.world.CreateCharacterWithAppearance("test", "friend", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() friend error = %v", err)
	}
	caster.AllowGroup = true
	friend.AllowGroup = true
	caster, friend, err = s.world.CreateGroup(caster, friend, 2)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "集体隐身术", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("集体隐身术")
	if !ok {
		t.Fatalf("skill 集体隐身术 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.Level = 21
	caster.MP = cost + 10

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	friendServer, friendClient := net.Pipe()
	defer friendServer.Close()
	defer friendClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(friendServer, friend)
	defer s.unregisterClient(friendServer)

	collectFrames := func(conn net.Conn, capHint int) [][]byte {
		frames := make([][]byte, 0, capHint)
		for {
			frame, ok := readFrameWithTimeout(t, conn, time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		return frames
	}
	casterFramesCh := make(chan [][]byte, 1)
	friendFramesCh := make(chan [][]byte, 1)
	go func() { casterFramesCh <- collectFrames(client, 7) }()
	go func() { friendFramesCh <- collectFrames(friendClient, 5) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 19, Tag: uint16(caster.Dir)})
	}()

	casterFrames := <-casterFramesCh
	friendFrames := <-friendFramesCh
	var casterAck bool
	casterState := map[int32]bool{}
	for i, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			casterAck = true
			continue
		}
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame %d error = %v", i, err)
		}
		switch cmd.Ident {
		case mir176.SMSendMyMagic:
		case mir176.SMSpacemoveShow:
			casterState[cmd.Recog] = true
			switch cmd.Recog {
			case world.CharacterActorID(caster):
				assertCharDesc(t, body, s.world.HumanFeatureForCharacter(caster), 2)
			case world.CharacterActorID(friend):
				assertCharDesc(t, body, s.world.HumanFeatureForCharacter(friend), 2)
			default:
				t.Fatalf("unexpected caster state actor=%d", cmd.Recog)
			}
		case mir176.SMHealthSpellChanged:
			// expected: self and friend both receive health refreshes, caster gets one extra final refresh
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	if !casterAck {
		t.Fatal("missing action ack frame")
	}
	if !casterState[world.CharacterActorID(friend)] {
		t.Fatalf("caster state frames = %+v, want friend state", casterState)
	}

	var friendSpell bool
	friendState := map[int32]bool{}
	for i, frame := range friendFrames {
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode friend frame %d error = %v", i, err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			friendSpell = true
		case mir176.SMSpacemoveShow:
			friendState[cmd.Recog] = true
			switch cmd.Recog {
			case world.CharacterActorID(caster):
				assertCharDesc(t, body, s.world.HumanFeatureForCharacter(caster), 2)
			case world.CharacterActorID(friend):
				assertCharDesc(t, body, s.world.HumanFeatureForCharacter(friend), 2)
			default:
				t.Fatalf("unexpected friend state actor=%d", cmd.Recog)
			}
		case mir176.SMHealthSpellChanged:
			// expected health refresh
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected friend frame ident=%d", cmd.Ident)
		}
	}
	if !friendSpell {
		t.Fatal("missing friend SMSpell frame")
	}
	if !friendState[world.CharacterActorID(friend)] {
		t.Fatalf("friend state frames = %+v, want friend state", friendState)
	}
	<-done
	if caster.TransparentUntil == 0 {
		t.Fatal("caster TransparentUntil = 0, want active stealth")
	}
	if groupClient, ok := s.ClientByCharacterID(friend.ID); !ok || groupClient.ch.TransparentUntil == 0 {
		t.Fatal("friend TransparentUntil = 0, want active stealth")
	}
}

func TestHandleSpellProtectionRefreshesGroupMembers(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	friend, err := s.world.CreateCharacterWithAppearance("test", "friend", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() friend error = %v", err)
	}
	caster.AllowGroup = true
	friend.AllowGroup = true
	caster, friend, err = s.world.CreateGroup(caster, friend, 2)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "神圣战甲术", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("神圣战甲术")
	if !ok {
		t.Fatalf("skill 神圣战甲术 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.Level = 20
	caster.MP = cost + 10

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	friendServer, friendClient := net.Pipe()
	defer friendServer.Close()
	defer friendClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(friendServer, friend)
	defer s.unregisterClient(friendServer)

	casterFrames := make(chan []byte, 8)
	friendFrames := make(chan []byte, 4)
	go func() {
		for i := 0; i < 5; i++ {
			casterFrames <- readFrame(t, client)
		}
	}()
	go func() {
		for i := 0; i < 4; i++ {
			friendFrames <- readFrame(t, friendClient)
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 15, Tag: uint16(caster.Dir)})
	}()

	var casterHealthCount, casterAbilityCount int
	for i := 0; i < 4; i++ {
		frame := <-casterFrames
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame %d error = %v", i, err)
		}
		switch cmd.Ident {
		case mir176.SMHealthSpellChanged:
			casterHealthCount++
		case mir176.SMAbility:
			casterAbilityCount++
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	if casterHealthCount != 2 {
		t.Fatalf("caster SMHealthSpellChanged count = %d, want 2", casterHealthCount)
	}
	if casterAbilityCount != 1 {
		t.Fatalf("caster SMAbility count = %d, want 1", casterAbilityCount)
	}

	friendSpellCount, friendHealthCount, friendAbilityCount := 0, 0, 0
	for i := 0; i < 4; i++ {
		cmd, body, err := decodeMessageLikeClient(<-friendFrames)
		if err != nil {
			t.Fatalf("decode friend frame %d error = %v", i, err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			friendSpellCount++
			if got := string(body); got != "15" {
				t.Fatalf("friend spell body = %q, want 15", got)
			}
		case mir176.SMHealthSpellChanged:
			friendHealthCount++
		case mir176.SMAbility:
			friendAbilityCount++
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected friend frame ident=%d", cmd.Ident)
		}
	}
	if friendSpellCount != 1 {
		t.Fatalf("friend SMSpell count = %d, want 1", friendSpellCount)
	}
	if friendHealthCount != 1 {
		t.Fatalf("friend SMHealthSpellChanged count = %d, want 1", friendHealthCount)
	}
	if friendAbilityCount != 1 {
		t.Fatalf("friend SMAbility count = %d, want 1", friendAbilityCount)
	}
	<-done
	if caster.DefenceUpUntil == 0 {
		t.Fatal("caster DefenceUpUntil = 0, want active armour")
	}
	groupClient, ok := s.ClientByCharacterID(friend.ID)
	if !ok {
		t.Fatal("friend client missing")
	}
	if groupClient.ch.DefenceUpUntil == 0 {
		t.Fatal("friend DefenceUpUntil = 0, want active armour")
	}
}

func TestHandleSpellGhostShieldRefreshesGroupMembers(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	friend, err := s.world.CreateCharacterWithAppearance("test", "friend", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() friend error = %v", err)
	}
	caster.AllowGroup = true
	friend.AllowGroup = true
	caster, friend, err = s.world.CreateGroup(caster, friend, 2)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "幽灵盾", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("幽灵盾")
	if !ok {
		t.Fatalf("skill 幽灵盾 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.Level = 20
	caster.MP = cost + 10

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	friendServer, friendClient := net.Pipe()
	defer friendServer.Close()
	defer friendClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(friendServer, friend)
	defer s.unregisterClient(friendServer)

	casterFrames := make(chan []byte, 8)
	friendFrames := make(chan []byte, 4)
	go func() {
		for i := 0; i < 5; i++ {
			casterFrames <- readFrame(t, client)
		}
	}()
	go func() {
		for i := 0; i < 4; i++ {
			friendFrames <- readFrame(t, friendClient)
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 14, Tag: uint16(caster.Dir)})
	}()

	var casterHealthCount, casterAbilityCount int
	for i := 0; i < 4; i++ {
		frame := <-casterFrames
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame %d error = %v", i, err)
		}
		switch cmd.Ident {
		case mir176.SMHealthSpellChanged:
			casterHealthCount++
		case mir176.SMAbility:
			casterAbilityCount++
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	if casterHealthCount != 2 {
		t.Fatalf("caster SMHealthSpellChanged count = %d, want 2", casterHealthCount)
	}
	if casterAbilityCount != 1 {
		t.Fatalf("caster SMAbility count = %d, want 1", casterAbilityCount)
	}

	friendSpellCount, friendHealthCount, friendAbilityCount := 0, 0, 0
	for i := 0; i < 4; i++ {
		cmd, body, err := decodeMessageLikeClient(<-friendFrames)
		if err != nil {
			t.Fatalf("decode friend frame %d error = %v", i, err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			friendSpellCount++
			if got := string(body); got != "14" {
				t.Fatalf("friend spell body = %q, want 14", got)
			}
		case mir176.SMHealthSpellChanged:
			friendHealthCount++
		case mir176.SMAbility:
			friendAbilityCount++
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected friend frame ident=%d", cmd.Ident)
		}
	}
	if friendSpellCount != 1 {
		t.Fatalf("friend SMSpell count = %d, want 1", friendSpellCount)
	}
	if friendHealthCount != 1 {
		t.Fatalf("friend SMHealthSpellChanged count = %d, want 1", friendHealthCount)
	}
	if friendAbilityCount != 1 {
		t.Fatalf("friend SMAbility count = %d, want 1", friendAbilityCount)
	}
	<-done
	if caster.MagDefenceUpUntil == 0 {
		t.Fatal("caster MagDefenceUpUntil = 0, want active shield")
	}
	groupClient, ok := s.ClientByCharacterID(friend.ID)
	if !ok {
		t.Fatal("friend client missing")
	}
	if groupClient.ch.MagDefenceUpUntil == 0 {
		t.Fatal("friend MagDefenceUpUntil = 0, want active shield")
	}
}

func TestHandleSpellGroupHealingRefreshesGroupMembers(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	friend, err := s.world.CreateCharacterWithAppearance("test", "friend", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() friend error = %v", err)
	}
	caster.AllowGroup = true
	friend.AllowGroup = true
	caster, friend, err = s.world.CreateGroup(caster, friend, 2)
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "群体治疗术", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("群体治疗术")
	if !ok {
		t.Fatalf("skill 群体治疗术 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.Level = 20
	caster.MP = cost + 10
	caster.HP = 10
	friend.HP = 12

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	friendServer, friendClient := net.Pipe()
	defer friendServer.Close()
	defer friendClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(friendServer, friend)
	defer s.unregisterClient(friendServer)

	casterFrames := make(chan []byte, 8)
	friendFrames := make(chan []byte, 4)
	go func() {
		for i := 0; i < 4; i++ {
			casterFrames <- readFrame(t, client)
		}
	}()
	go func() {
		for i := 0; i < 3; i++ {
			friendFrames <- readFrame(t, friendClient)
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 29, Tag: uint16(caster.Dir)})
	}()

	var casterHealthCount int
	for i := 0; i < 4; i++ {
		frame := <-casterFrames
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame %d error = %v", i, err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
		case mir176.SMHealthSpellChanged:
			casterHealthCount++
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	if casterHealthCount != 2 {
		t.Fatalf("caster SMHealthSpellChanged count = %d, want 2", casterHealthCount)
	}

	friendSpellCount, friendHealthCount := 0, 0
	for i := 0; i < 2; i++ {
		cmd, _, err := decodeMessageLikeClient(<-friendFrames)
		if err != nil {
			t.Fatalf("decode friend frame %d error = %v", i, err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			friendSpellCount++
		case mir176.SMHealthSpellChanged:
			friendHealthCount++
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected friend frame ident=%d", cmd.Ident)
		}
	}
	if friendSpellCount != 1 {
		t.Fatalf("friend SMSpell count = %d, want 1", friendSpellCount)
	}
	if friendHealthCount != 1 {
		t.Fatalf("friend SMHealthSpellChanged count = %d, want 1", friendHealthCount)
	}
	<-done
	if caster.HP <= 10 {
		t.Fatalf("caster HP = %d, want increased", caster.HP)
	}
	groupClient, ok := s.ClientByCharacterID(friend.ID)
	if !ok {
		t.Fatal("friend client missing")
	}
	if groupClient.ch.HP <= 12 {
		t.Fatalf("friend HP = %d, want increased", groupClient.ch.HP)
	}
}

func TestHandleSpellFireWallBroadcastsSpellToObservers(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, x+4, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "火墙", Level: 10, Train: 0}}
	skill, ok := s.world.Skill("火墙")
	if !ok {
		t.Fatalf("skill 火墙 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.Level = 20
	caster.MP = cost + 10

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	casterFrames := make(chan []byte, 4)
	observerFrames := make(chan []byte, 2)
	go func() {
		for i := 0; i < 3; i++ {
			casterFrames <- readFrame(t, client)
		}
	}()
	go func() {
		observerFrames <- readFrame(t, observerClient)
		observerFrames <- readFrame(t, observerClient)
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x+2) | uint32(y)<<16), Param: 22, Tag: uint16(caster.Dir)})
	}()

	var sawAck bool
	for i := 0; i < 3; i++ {
		frame := <-casterFrames
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			sawAck = true
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame %d error = %v", i, err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
		case mir176.SMHealthSpellChanged:
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	if !sawAck {
		t.Fatal("missing action ack frame")
	}
	cmd, body, err := decodeMessageLikeClient(<-observerFrames)
	if err != nil {
		t.Fatalf("decode observer frame error = %v", err)
	}
	if cmd.Ident != mir176.SMSpell {
		t.Fatalf("observer frame ident = %d, want SMSpell (%d)", cmd.Ident, mir176.SMSpell)
	}
	if got := string(body); got != "22" {
		t.Fatalf("observer spell body = %q, want 22", got)
	}
	<-done
	if caster.MP != cost+10-cost {
		t.Fatalf("MP = %d, want %d after fire wall", caster.MP, cost+10-cost)
	}
}

func TestHandleSpellIceStormBroadcastsMonsterHitsToObservers(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, x+4, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "冰咆哮", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("冰咆哮")
	if !ok {
		t.Fatalf("skill 冰咆哮 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10

	mp, ok := bundle.Maps[mapID]
	if !ok {
		t.Fatalf("map %s missing from configs", mapID)
	}
	targetX, targetY := -1, -1
	for dx := 6; dx < 24 && targetX < 0; dx++ {
		for dy := -4; dy <= 4; dy++ {
			tx := x + dx
			ty := y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			clear := true
			for _, pt := range [][2]int{{tx, ty}, {tx + 1, ty}, {tx - 1, ty}, {tx, ty + 1}, {tx, ty - 1}} {
				if monsters, _ := s.world.SnapshotAround(mapID, pt[0], pt[1], 0); len(monsters) > 0 {
					clear = false
					break
				}
			}
			if clear {
				targetX, targetY = tx, ty
				break
			}
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for ice storm test")
	}
	result, err := s.world.SpawnMonsterByNameAt(mapID, targetX, targetY, "白野猪", 2)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(result.Monsters) != 2 {
		t.Fatalf("SpawnMonsterByNameAt() monsters = %d, want 2", len(result.Monsters))
	}

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	casterFrames := make(chan []byte, 8)
	observerFrames := make(chan []byte, 8)
	go func() {
		for i := 0; i < 5; i++ {
			casterFrames <- readFrame(t, client)
		}
	}()
	go func() {
		for i := 0; i < 4; i++ {
			observerFrames <- readFrame(t, observerClient)
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 33, Tag: uint16(caster.Dir)})
	}()

	assertActionAck(t, <-casterFrames)
	var casterStruck int
	for i := 0; i < 3; i++ {
		cmd, _, err := decodeMessageLikeClient(<-casterFrames)
		if err != nil {
			t.Fatalf("decode caster hit frame %d error = %v", i, err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			casterStruck++
		case mir176.SMHealthSpellChanged:
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	if casterStruck != 2 {
		t.Fatalf("caster SMStruck count = %d, want 2", casterStruck)
	}

	cmd, _, err := decodeMessageLikeClient(<-observerFrames)
	if err != nil {
		t.Fatalf("decode observer spell frame error = %v", err)
	}
	if cmd.Ident != mir176.SMSpell {
		t.Fatalf("observer frame ident = %d, want SMSpell (%d)", cmd.Ident, mir176.SMSpell)
	}
	var observerStruck int
	for i := 0; i < 2; i++ {
		cmd, _, err := decodeMessageLikeClient(<-observerFrames)
		if err != nil {
			t.Fatalf("decode observer hit frame %d error = %v", i, err)
		}
		if cmd.Ident != mir176.SMStruck {
			t.Fatalf("observer frame ident = %d, want SMStruck (%d)", cmd.Ident, mir176.SMStruck)
		}
		observerStruck++
	}
	if observerStruck != 2 {
		t.Fatalf("observer SMStruck count = %d, want 2", observerStruck)
	}

	<-done
	if caster.MP != cost+10-cost {
		t.Fatalf("MP = %d, want %d after ice storm", caster.MP, cost+10-cost)
	}
}

func TestHandleSpellElectricBlizzardBroadcastsMixedHits(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	mapID, x, y := testDefaultSpawn(t)
	mp, ok := bundle.Maps[mapID]
	if !ok {
		t.Fatalf("map %s missing from configs", mapID)
	}
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "地狱雷光", Level: 10, Train: 0}}
	skill, ok := s.world.Skill("地狱雷光")
	if !ok {
		t.Fatalf("skill 地狱雷光 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.Level = 100
	caster.MP = cost + 10

	casterX, casterY := -1, -1
	for dx := 6; dx < 24 && casterX < 0; dx++ {
		for dy := -4; dy <= 4; dy++ {
			tx := x + dx
			ty := y + dy
			if !mp.Walkable(tx-1, ty) || !mp.Walkable(tx+1, ty) || !mp.Walkable(tx, ty+1) {
				continue
			}
			clear := true
			for _, pt := range [][2]int{{tx - 1, ty}, {tx + 1, ty}, {tx, ty + 1}} {
				if monsters, _ := s.world.SnapshotAround(mapID, pt[0], pt[1], 0); len(monsters) > 0 {
					clear = false
					break
				}
			}
			if clear {
				casterX, casterY = tx, ty
				break
			}
		}
	}
	if casterX < 0 {
		t.Fatal("could not find clear tile for electric blizzard test")
	}
	target, err := s.world.CreateCharacterWithAppearance("test", "target", "warrior", 0, 0, mapID, casterX, casterY+1)
	if err != nil {
		t.Fatalf("CreateCharacterWithAppearance() target error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	targetServer, targetClient := net.Pipe()
	defer targetServer.Close()
	defer targetClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(targetServer, target)
	defer s.unregisterClient(targetServer)

	collectFrames := func(conn net.Conn) [][]byte {
		frames := make([][]byte, 0, 8)
		for {
			frame, ok := readFrameWithTimeout(t, conn, time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		return frames
	}
	casterFramesCh := make(chan [][]byte, 1)
	targetFramesCh := make(chan [][]byte, 1)
	go func() { casterFramesCh <- collectFrames(client) }()
	go func() { targetFramesCh <- collectFrames(targetClient) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(casterX) | uint32(casterY)<<16), Param: 24, Tag: uint16(caster.Dir)})
	}()

	casterFrames := <-casterFramesCh
	targetFrames := <-targetFramesCh

	var casterAck, casterHealth int
	casterStruck := 0
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			casterAck++
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			casterStruck++
		case mir176.SMHealthSpellChanged:
			casterHealth++
		}
	}
	if casterAck != 1 {
		t.Fatalf("caster ack count = %d, want 1", casterAck)
	}
	if casterHealth < 1 {
		t.Fatalf("caster SMHealthSpellChanged count = %d, want at least 1", casterHealth)
	}
	if casterStruck < 1 {
		t.Fatalf("caster SMStruck count = %d, want at least 1", casterStruck)
	}

	var targetStruck, targetHealth bool
	for _, frame := range targetFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode target frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			targetStruck = true
		case mir176.SMHealthSpellChanged:
			targetHealth = true
		}
	}
	if !targetStruck {
		t.Fatal("missing target SMStruck frame")
	}
	if !targetHealth {
		t.Fatal("missing target SMHealthSpellChanged frame")
	}

	<-done
}

func TestHandleSpellRepelBroadcastsStateRefreshToTargets(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	mapID, x, y := testDefaultSpawn(t)
	mp, ok := bundle.Maps[mapID]
	if !ok {
		t.Fatalf("map %s missing from configs", mapID)
	}
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	caster.Level = 100
	target := storage.Character{}
	observer := storage.Character{}
	targetX, targetY := -1, -1
	for dx := 1; dx < 8 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := x + dx
			ty := y + dy
			if !mp.Walkable(tx, ty) || !mp.Walkable(tx+1, ty) {
				continue
			}
			targetX, targetY = tx, ty
			break
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for repel test")
	}
	target, err = s.world.CreateCharacterWithAppearance("test", "target", "warrior", 0, 0, mapID, targetX, targetY)
	if err != nil {
		t.Fatalf("CreateCharacter() target error = %v", err)
	}
	observer, err = s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, targetX+3, targetY)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "抗拒火环", Level: 10, Train: 0}}
	skill, ok := s.world.Skill("抗拒火环")
	if !ok {
		t.Fatalf("skill 抗拒火环 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	targetServer, targetClient := net.Pipe()
	defer targetServer.Close()
	defer targetClient.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(targetServer, target)
	defer s.unregisterClient(targetServer)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	collectFrames := func(conn net.Conn) [][]byte {
		frames := make([][]byte, 0, 4)
		for {
			frame, ok := readFrameWithTimeout(t, conn, time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		return frames
	}
	casterFramesCh := make(chan [][]byte, 1)
	targetFramesCh := make(chan [][]byte, 1)
	observerFramesCh := make(chan [][]byte, 1)
	go func() { casterFramesCh <- collectFrames(client) }()
	go func() { targetFramesCh <- collectFrames(targetClient) }()
	go func() { observerFramesCh <- collectFrames(observerClient) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 8, Tag: uint16(caster.Dir)})
	}()

	casterFrames := <-casterFramesCh
	targetFrames := <-targetFramesCh
	observerFrames := <-observerFramesCh

	var casterAck, casterHealth bool
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			casterAck = true
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMHealthSpellChanged:
			casterHealth = true
		}
	}
	if !casterAck {
		t.Fatal("missing caster action ack")
	}
	if !casterHealth {
		t.Fatal("missing caster SMHealthSpellChanged frame")
	}

	var sawObserverSpell bool
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		if cmd.Ident == mir176.SMSpell {
			sawObserverSpell = true
		}
	}
	if !sawObserverSpell {
		t.Fatal("missing observer SMSpell frame")
	}

	var targetSpell bool
	for _, frame := range targetFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode target frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			targetSpell = true
		}
	}
	if !targetSpell {
		t.Fatal("missing target SMSpell frame")
	}

	<-done
	if caster.MP != cost+10-cost {
		t.Fatalf("MP = %d, want %d after repel", caster.MP, cost+10-cost)
	}
}

func TestHandleSpellChargeBroadcastsMovementAndSpell(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	mapID, x, y := testDefaultSpawn(t)
	mp, ok := bundle.Maps[mapID]
	if !ok {
		t.Fatalf("map %s missing from configs", mapID)
	}
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	caster.Level = 100
	target := storage.Character{}
	observer := storage.Character{}
	targetX, targetY := -1, -1
	for dx := 1; dx < 8 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := x + dx
			ty := y + dy
			if !mp.Walkable(tx, ty) || !mp.Walkable(tx+1, ty) || !mp.Walkable(tx+2, ty) {
				continue
			}
			targetX, targetY = tx, ty
			break
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for charge test")
	}
	target, err = s.world.CreateCharacterWithAppearance("test", "target", "warrior", 0, 0, mapID, targetX, targetY)
	if err != nil {
		t.Fatalf("CreateCharacter() target error = %v", err)
	}
	observer, err = s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, targetX+3, targetY)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "野蛮冲撞", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("野蛮冲撞")
	if !ok {
		t.Fatalf("skill 野蛮冲撞 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	targetServer, targetClient := net.Pipe()
	defer targetServer.Close()
	defer targetClient.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(targetServer, target)
	defer s.unregisterClient(targetServer)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	collectFrames := func(conn net.Conn) [][]byte {
		frames := make([][]byte, 0, 4)
		var pending []byte
		for {
			chunk, ok := readFrameWithTimeout(t, conn, time.Second)
			if !ok {
				break
			}
			pending = append(pending, chunk...)
			split, rest := mir176.SplitFrames(pending)
			frames = append(frames, split...)
			pending = append(pending[:0], rest...)
		}
		return frames
	}
	casterFramesCh := make(chan [][]byte, 1)
	targetFramesCh := make(chan [][]byte, 1)
	observerFramesCh := make(chan [][]byte, 1)
	go func() { casterFramesCh <- collectFrames(client) }()
	go func() { targetFramesCh <- collectFrames(targetClient) }()
	go func() { observerFramesCh <- collectFrames(observerClient) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 27, Tag: uint16(caster.Dir)})
	}()

	casterFrames := <-casterFramesCh
	targetFrames := <-targetFramesCh
	observerFrames := <-observerFramesCh

	var casterAck, casterHealth bool
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			casterAck = true
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMHealthSpellChanged:
			casterHealth = true
		}
	}
	if !casterAck {
		t.Fatal("missing caster action ack")
	}
	if !casterHealth {
		t.Fatal("missing caster SMHealthSpellChanged frame")
	}

	var targetSpell bool
	for _, frame := range targetFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode target frame error = %v", err)
		}
		if cmd.Ident == mir176.SMSpell {
			targetSpell = true
		}
	}
	if !targetSpell {
		t.Fatal("missing target SMSpell frame")
	}

	var observerSpell bool
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		if cmd.Ident == mir176.SMSpell {
			observerSpell = true
		}
	}
	if !observerSpell {
		t.Fatal("missing observer SMSpell frame")
	}

	<-done
	if caster.MP != cost+10-cost {
		t.Fatalf("MP = %d, want %d after charge", caster.MP, cost+10-cost)
	}
	if caster.X == x && caster.Y == y {
		t.Fatal("caster position unchanged, want charge movement")
	}
}

func TestHandleSpellSpiritFireBroadcastsSpellAndStruck(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	mapID, x, y := testDefaultSpawn(t)
	mp, ok := bundle.Maps[mapID]
	if !ok {
		t.Fatalf("map %s missing from configs", mapID)
	}
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	caster.Level = 100
	caster.Skills = storage.SkillStates{{ID: "灵魂火符", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("灵魂火符")
	if !ok {
		t.Fatalf("skill 灵魂火符 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10

	targetX, targetY := -1, -1
	for dx := 4; dx <= 8 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := x + dx
			ty := y + dy
			if !mp.Walkable(tx, ty) || !mp.Walkable(tx+1, ty) {
				continue
			}
			targetX, targetY = tx, ty
			break
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for spirit fire target")
	}

	target, err := s.world.CreateCharacterWithAppearance("test", "target", "warrior", 0, 0, mapID, targetX, targetY)
	if err != nil {
		t.Fatalf("CreateCharacter() target error = %v", err)
	}
	target.Level = 100
	target.HP = 4000
	target.MaxHP = 4000

	targetObserver, err := s.world.CreateCharacterWithAppearance("test", "target_observer", "wizard", 0, 0, mapID, targetX+1, targetY)
	if err != nil {
		t.Fatalf("CreateCharacter() target observer error = %v", err)
	}

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	targetServer, targetClient := net.Pipe()
	defer targetServer.Close()
	defer targetClient.Close()
	targetObserverServer, targetObserverClient := net.Pipe()
	defer targetObserverServer.Close()
	defer targetObserverClient.Close()
	spellObserverServer, spellObserverClient := net.Pipe()
	defer spellObserverServer.Close()
	defer spellObserverClient.Close()

	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(targetServer, target)
	defer s.unregisterClient(targetServer)
	s.registerClient(targetObserverServer, targetObserver)
	defer s.unregisterClient(targetObserverServer)

	collectFrames := func(conn net.Conn) [][]byte {
		frames := make([][]byte, 0, 4)
		for {
			frame, ok := readFrameWithTimeout(t, conn, time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		return frames
	}
	casterFramesCh := make(chan [][]byte, 1)
	targetFramesCh := make(chan [][]byte, 1)
	targetObserverFramesCh := make(chan [][]byte, 1)
	go func() { casterFramesCh <- collectFrames(client) }()
	go func() { targetFramesCh <- collectFrames(targetClient) }()
	go func() { targetObserverFramesCh <- collectFrames(targetObserverClient) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 13, Tag: uint16(caster.Dir)})
	}()

	casterFrames := <-casterFramesCh
	targetFrames := <-targetFramesCh
	targetObserverFrames := <-targetObserverFramesCh
	var casterAck, casterHealth bool
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			casterAck = true
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMHealthSpellChanged:
			casterHealth = true
		}
	}
	if !casterAck {
		t.Fatal("missing caster action ack")
	}
	if !casterHealth {
		t.Fatal("missing caster SMHealthSpellChanged frame")
	}

	var targetSpell, targetHealth bool
	for _, frame := range targetFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode target frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			targetSpell = true
		case mir176.SMHealthSpellChanged:
			targetHealth = true
		}
	}
	if !targetSpell {
		t.Fatal("missing target SMStruck frame")
	}
	if !targetHealth {
		t.Fatal("missing target SMHealthSpellChanged frame")
	}

	var targetObserverSpell, targetObserverHealth bool
	for _, frame := range targetObserverFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode target observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			targetObserverSpell = true
		case mir176.SMHealthSpellChanged:
			targetObserverHealth = true
		}
	}
	if !targetObserverSpell {
		t.Fatal("missing target observer SMStruck frame")
	}
	if !targetObserverHealth {
		t.Fatal("missing target observer SMHealthSpellChanged frame")
	}
}

func TestHandleSpellSpiritFireBroadcastsMagicFireWithoutTarget(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	mapID, x, y := testDefaultSpawn(t)
	mp, ok := bundle.Maps[mapID]
	if !ok {
		t.Fatalf("map %s missing from configs", mapID)
	}
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	caster.Level = 100
	caster.Skills = storage.SkillStates{{ID: "灵魂火符", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("灵魂火符")
	if !ok {
		t.Fatalf("skill 灵魂火符 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10

	targetX, targetY := -1, -1
	for dx := 4; dx <= 8 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := x + dx
			ty := y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			targetX, targetY = tx, ty
			break
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for spirit fire target")
	}

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)

	framesCh := make(chan [][]byte, 1)
	go func() {
		frames := make([][]byte, 0, 4)
		for {
			frame, ok := readFrameWithTimeout(t, client, 3*time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		framesCh <- frames
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 13, Tag: uint16(caster.Dir)})
	}()

	frames := <-framesCh
	var casterAck, casterMagicFire bool
	for _, frame := range frames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			casterAck = true
			continue
		}
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMMagicFire:
			decoded, err := mir176.DecodePlain6Payload(body)
			if err != nil {
				t.Fatalf("DecodePlain6Payload() error = %v", err)
			}
			if len(decoded) != 4 {
				t.Fatalf("SM_MAGICFIRE body len = %d, want 4", len(decoded))
			}
			if got := int32(binary.LittleEndian.Uint32(decoded)); got != 0 {
				t.Fatalf("SM_MAGICFIRE target = %d, want 0", got)
			}
			if cmd.Recog != world.CharacterActorID(caster) {
				t.Fatalf("SM_MAGICFIRE recog = %d, want %d", cmd.Recog, world.CharacterActorID(caster))
			}
			if cmd.Param != uint16(targetX) || cmd.Tag != uint16(targetY) {
				t.Fatalf("SM_MAGICFIRE position = (%d,%d), want (%d,%d)", cmd.Param, cmd.Tag, targetX, targetY)
			}
			if cmd.Series != makeWord(byte(skill.EffectType), byte(skill.Effect)) {
				t.Fatalf("SM_MAGICFIRE series = %d, want %d", cmd.Series, makeWord(byte(skill.EffectType), byte(skill.Effect)))
			}
			casterMagicFire = true
		}
	}
	<-done
	if !casterAck {
		t.Fatal("missing caster action ack")
	}
	if !casterMagicFire {
		t.Fatal("missing caster SMMagicFire frame")
	}
}

func TestHandleSpellSpiritFireBroadcastsSpellEffectToObservers(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	mapID, x, y := testDefaultSpawn(t)
	mp, ok := bundle.Maps[mapID]
	if !ok {
		t.Fatalf("map %s missing from configs", mapID)
	}
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, x+2, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	caster.Level = 100
	caster.Skills = storage.SkillStates{{ID: "灵魂火符", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("灵魂火符")
	if !ok {
		t.Fatalf("skill 灵魂火符 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10

	targetX, targetY := -1, -1
	for dx := 4; dx <= 8 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := x + dx
			ty := y + dy
			if !mp.Walkable(tx, ty) {
				continue
			}
			targetX, targetY = tx, ty
			break
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tile for spirit fire target")
	}

	server, casterClient := net.Pipe()
	defer server.Close()
	defer casterClient.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	casterFramesCh := make(chan [][]byte, 1)
	go func() {
		frames := make([][]byte, 0, 4)
		for {
			frame, ok := readFrameWithTimeout(t, casterClient, time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		casterFramesCh <- frames
	}()

	observerFramesCh := make(chan [][]byte, 1)
	go func() {
		frames := make([][]byte, 0, 4)
		for {
			frame, ok := readFrameWithTimeout(t, observerClient, 3*time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		observerFramesCh <- frames
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 13, Tag: uint16(caster.Dir)})
	}()

	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh

	var casterAck, casterMagicFire bool
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			casterAck = true
			continue
		}
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		if cmd.Ident != mir176.SMMagicFire {
			continue
		}
		decoded, err := mir176.DecodePlain6Payload(body)
		if err != nil {
			t.Fatalf("DecodePlain6Payload() error = %v", err)
		}
		if len(decoded) != 4 {
			t.Fatalf("SM_MAGICFIRE body len = %d, want 4", len(decoded))
		}
		if got := int32(binary.LittleEndian.Uint32(decoded)); got != 0 {
			t.Fatalf("SM_MAGICFIRE target = %d, want 0", got)
		}
		if cmd.Recog != world.CharacterActorID(caster) {
			t.Fatalf("SM_MAGICFIRE recog = %d, want %d", cmd.Recog, world.CharacterActorID(caster))
		}
		if cmd.Param != uint16(targetX) || cmd.Tag != uint16(targetY) {
			t.Fatalf("SM_MAGICFIRE position = (%d,%d), want (%d,%d)", cmd.Param, cmd.Tag, targetX, targetY)
		}
		if cmd.Series != makeWord(byte(skill.EffectType), byte(skill.Effect)) {
			t.Fatalf("SM_MAGICFIRE series = %d, want %d", cmd.Series, makeWord(byte(skill.EffectType), byte(skill.Effect)))
		}
		casterMagicFire = true
	}
	if !casterAck {
		t.Fatal("missing caster action ack")
	}
	if !casterMagicFire {
		t.Fatal("missing caster SMMagicFire frame")
	}

	var observerSpell bool
	for _, frame := range observerFrames {
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		if cmd.Ident != mir176.SMSpell {
			continue
		}
		if cmd.Param != uint16(targetX) || cmd.Tag != uint16(targetY) {
			t.Fatalf("SM_SPELL position = (%d,%d), want (%d,%d)", cmd.Param, cmd.Tag, targetX, targetY)
		}
		if got := string(body); got != "13" {
			t.Fatalf("SM_SPELL body = %q, want magic id 13", got)
		}
		observerSpell = true
	}
	if !observerSpell {
		t.Fatal("missing observer SMSpell frame")
	}

	<-done
}

func TestHandleSpellPoisonBroadcastsSpellAndHealth(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	mapID, x, y := testDefaultSpawn(t)
	mp, ok := bundle.Maps[mapID]
	if !ok {
		t.Fatalf("map %s missing from configs", mapID)
	}
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	caster.Level = 14
	caster.Dir = 2
	caster.Skills = storage.SkillStates{{ID: "施毒术", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("施毒术")
	if !ok {
		t.Fatalf("skill 施毒术 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10
	caster.EquippedItems = map[int]storage.UserItem{
		world.SlotBujuk: {ItemID: "灰色药粉(少量)", Dura: 5000},
	}

	targetX, targetY := -1, -1
	for dx := 4; dx < 10 && targetX < 0; dx++ {
		for dy := -2; dy <= 2; dy++ {
			tx := x + dx
			ty := y + dy
			ox := tx + 2
			if !mp.Walkable(tx, ty) || !mp.Walkable(ox, ty) {
				continue
			}
			targetX, targetY = tx, ty
			break
		}
	}
	if targetX < 0 {
		t.Fatal("could not find clear tiles for poison test")
	}

	target, err := s.world.CreateCharacterWithAppearance("test", "target", "warrior", 0, 0, mapID, targetX, targetY)
	if err != nil {
		t.Fatalf("CreateCharacter() target error = %v", err)
	}
	target.HP = 1000
	target.MaxHP = 1000

	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, targetX+2, targetY)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	targetServer, targetClient := net.Pipe()
	defer targetServer.Close()
	defer targetClient.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(targetServer, target)
	defer s.unregisterClient(targetServer)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	collectFrames := func(conn net.Conn) [][]byte {
		frames := make([][]byte, 0, 4)
		var pending []byte
		for {
			chunk, ok := readFrameWithTimeout(t, conn, time.Second)
			if !ok {
				break
			}
			pending = append(pending, chunk...)
			split, rest := mir176.SplitFrames(pending)
			frames = append(frames, split...)
			pending = append(pending[:0], rest...)
		}
		return frames
	}
	casterFramesCh := make(chan [][]byte, 1)
	targetFramesCh := make(chan [][]byte, 1)
	observerFramesCh := make(chan [][]byte, 1)
	go func() { casterFramesCh <- collectFrames(client) }()
	go func() { targetFramesCh <- collectFrames(targetClient) }()
	go func() { observerFramesCh <- collectFrames(observerClient) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 6, Tag: uint16(caster.Dir)})
	}()

	casterFrames := <-casterFramesCh
	targetFrames := <-targetFramesCh
	observerFrames := <-observerFramesCh

	var casterAck, casterHealth bool
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			casterAck = true
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		if cmd.Ident == mir176.SMHealthSpellChanged {
			casterHealth = true
		}
	}
	if !casterAck {
		t.Fatal("missing caster action ack")
	}
	if !casterHealth {
		t.Fatal("missing caster SMHealthSpellChanged frame")
	}

	var targetSpell, targetHealth bool
	for _, frame := range targetFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode target frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			targetSpell = true
		case mir176.SMHealthSpellChanged:
			targetHealth = true
		}
	}
	if !targetSpell {
		t.Fatal("missing target SMSpell frame")
	}
	if !targetHealth {
		t.Fatal("missing target SMHealthSpellChanged frame")
	}

	var observerSpell bool
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		if cmd.Ident == mir176.SMSpell {
			observerSpell = true
		}
	}
	if !observerSpell {
		t.Fatal("missing observer SMSpell frame")
	}

	<-done
}

func TestHandleSpellTurnUndeadBroadcastsMonsterDeath(t *testing.T) {
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	mp, ok := bundle.Maps[testMapID]
	if !ok {
		t.Fatalf("map %s missing from configs", testMapID)
	}
	dropTableID := "test-undead-dropper"
	bundle.Drops[dropTableID] = data.StdDropTable{
		ID: dropTableID,
		Entries: []data.StdDropEntry{{
			ItemID:   testWeaponID,
			Chance:   1,
			MinCount: 1,
			MaxCount: 1,
		}},
	}
	mon := bundle.Monsters["僵尸"]
	mon.ID = dropTableID
	mon.Name = "test-undead-dropper"
	mon.Undead = 1
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
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, x+4, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	caster.Level = 100
	caster.Class = "taoist"
	caster.Skills = storage.SkillStates{{ID: "圣言术", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("圣言术")
	if !ok {
		t.Fatalf("skill 圣言术 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	monsters, _ := s.world.SnapshotAround(mapID, x, y, 4)
	if len(monsters) == 0 {
		t.Fatal("expected monster for turn undead test")
	}
	target := monsters[0]
	if target.Undead != 1 {
		target.Undead = 1
	}

	casterFrames := make(chan []byte, 8)
	observerFrames := make(chan []byte, 8)
	go func() {
		for i := 0; i < 7; i++ {
			casterFrames <- readFrame(t, client)
		}
	}()
	go func() {
		for i := 0; i < 5; i++ {
			observerFrames <- readFrame(t, observerClient)
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x+2) | uint32(y)<<16), Param: 32, Tag: uint16(caster.Dir)})
	}()

	var sawAck, sawStruck, sawDeath, sawDrop, sawWinExp, sawHealth bool
	for i := 0; i < 6; i++ {
		frame := <-casterFrames
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			sawAck = true
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame %d error = %v", i, err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			sawStruck = true
		case mir176.SMNowDeath:
			sawDeath = true
		case mir176.SMItemShow:
			sawDrop = true
		case mir176.SMWinExp:
			sawWinExp = true
		case mir176.SMHealthSpellChanged:
			sawHealth = true
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	if !sawAck {
		t.Fatal("missing action ack frame")
	}
	if !sawStruck {
		t.Fatal("missing SMStruck frame")
	}
	if !sawDeath {
		t.Fatal("missing SMNowDeath frame")
	}
	if !sawDrop {
		t.Fatal("missing SMItemShow frame")
	}
	if !sawWinExp {
		t.Fatal("missing SMWinExp frame")
	}
	if !sawHealth {
		t.Fatal("missing SMHealthSpellChanged frame")
	}

	var sawSpell, sawObserverStruck, sawObserverDeath, sawObserverDrop bool
	for i := 0; i < 4; i++ {
		cmd, _, err := decodeMessageLikeClient(<-observerFrames)
		if err != nil {
			t.Fatalf("decode observer frame %d error = %v", i, err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			sawSpell = true
		case mir176.SMStruck:
			sawObserverStruck = true
		case mir176.SMNowDeath:
			sawObserverDeath = true
		case mir176.SMItemShow:
			sawObserverDrop = true
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected observer frame ident=%d", cmd.Ident)
		}
	}
	if !sawSpell {
		t.Fatal("missing observer SMSpell frame")
	}
	if !sawObserverStruck {
		t.Fatal("missing observer SMStruck frame")
	}
	if !sawObserverDeath {
		t.Fatal("missing observer SMNowDeath frame")
	}
	if !sawObserverDrop {
		t.Fatal("missing observer SMItemShow frame")
	}
	<-done
	snapshot, _ := s.world.SnapshotAround(mapID, x, y, 4)
	for _, current := range snapshot {
		if current.ID == target.ID {
			t.Fatalf("monster %s still visible after 圣言术", target.ID)
		}
	}
	if caster.MP != cost+10-cost {
		t.Fatalf("MP = %d, want %d after turn undead", caster.MP, cost+10-cost)
	}
}

func TestBroadcastTeleportMoveSendsDisappearAndAppear(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	from, err := s.world.CreateCharacterWithAppearance("test", "caster", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	to := from
	to.X = x + 2
	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "warrior", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(server, from)
	defer s.unregisterClient(server)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	frames := make(chan []byte, 4)
	go func() {
		for i := 0; i < 4; i++ {
			frames <- readFrame(t, observerClient)
		}
	}()

	s.broadcastTeleportMove(server, from, to)

	disappearCmd, _, err := decodeMessageLikeClient(<-frames)
	if err != nil {
		t.Fatalf("decode disappear frame error = %v", err)
	}
	if disappearCmd.Ident != mir176.SMDisappear {
		t.Fatalf("disappear ident = %d, want SM_DISAPPEAR (%d)", disappearCmd.Ident, mir176.SMDisappear)
	}
	if disappearCmd.Recog != world.CharacterActorID(from) {
		t.Fatalf("disappear recog = %d, want actor %d", disappearCmd.Recog, world.CharacterActorID(from))
	}

	logonCmd, _, err := decodeMessageLikeClient(<-frames)
	if err != nil {
		t.Fatalf("decode logon frame error = %v", err)
	}
	if logonCmd.Ident != mir176.SMLogon {
		t.Fatalf("logon ident = %d, want SM_LOGON (%d)", logonCmd.Ident, mir176.SMLogon)
	}
	if logonCmd.Recog != world.CharacterActorID(from) || int(logonCmd.Param) != to.X || int(logonCmd.Tag) != to.Y {
		t.Fatalf("logon frame = %+v, want moved character at (%d,%d)", logonCmd, to.X, to.Y)
	}

	featureCmd, featureBody, err := decodeMessageLikeClient(<-frames)
	if err != nil {
		t.Fatalf("decode feature frame error = %v", err)
	}
	if featureCmd.Ident != mir176.SMFeatureChanged {
		t.Fatalf("feature ident = %d, want SM_FEATURECHANGED (%d)", featureCmd.Ident, mir176.SMFeatureChanged)
	}
	if featureCmd.Recog != world.CharacterActorID(from) {
		t.Fatalf("feature recog = %d, want actor %d", featureCmd.Recog, world.CharacterActorID(from))
	}
	if len(featureBody) != 0 {
		t.Fatalf("feature body len = %d, want 0", len(featureBody))
	}

	nameCmd, nameBody, err := decodeMessageLikeClient(<-frames)
	if err != nil {
		t.Fatalf("decode name frame error = %v", err)
	}
	if nameCmd.Ident != mir176.SMUserName {
		t.Fatalf("name ident = %d, want SM_USERNAME (%d)", nameCmd.Ident, mir176.SMUserName)
	}
	if nameCmd.Recog != world.CharacterActorID(from) {
		t.Fatalf("name recog = %d, want actor %d", nameCmd.Recog, world.CharacterActorID(from))
	}
	decodedName, err := mir176.DecodePlain6Payload(nameBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if got := DecodeString(decodedName); got != s.world.CharacterDisplayName(from) {
		t.Fatalf("name body = %q, want %q", got, s.world.CharacterDisplayName(from))
	}
}

func TestHandleSpellInstantTeleportBroadcastsMove(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	caster.Skills = storage.SkillStates{{ID: "瞬息移动", Level: 5, Train: 0}}
	skill, ok := s.world.Skill("瞬息移动")
	if !ok {
		t.Fatalf("skill 瞬息移动 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10

	probeServer := newDataDirTestServer(t, testConfigsDir)
	setWorldRand(t, probeServer.world, 1)
	preview := caster
	previewResult, err := probeServer.world.CastSkillWithPlayers(preview, "瞬息移动", caster.X, caster.Y, 0, nil)
	if err != nil {
		t.Fatalf("CastSkillWithPlayers() preview error = %v", err)
	}
	if previewResult.Character.X == caster.X && previewResult.Character.Y == caster.Y {
		t.Fatal("preview teleport did not move caster")
	}
	dest := previewResult.Character
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	sourceServer, sourceClient := net.Pipe()
	defer sourceServer.Close()
	defer sourceClient.Close()
	destServer, destClient := net.Pipe()
	defer destServer.Close()
	defer destClient.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	sourceObserver, err := s.world.CreateCharacterWithAppearance("test", "source_observer", "warrior", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() source observer error = %v", err)
	}
	destObserver, err := s.world.CreateCharacterWithAppearance("test", "dest_observer", "warrior", 0, 0, mapID, dest.X, dest.Y)
	if err != nil {
		t.Fatalf("CreateCharacter() dest observer error = %v", err)
	}
	s.registerClient(sourceServer, sourceObserver)
	defer s.unregisterClient(sourceServer)
	s.registerClient(destServer, destObserver)
	defer s.unregisterClient(destServer)

	collectFrames := func(conn net.Conn) [][]byte {
		frames := make([][]byte, 0, 8)
		for {
			frame, ok := readFrameWithTimeout(t, conn, 3*time.Second)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		return frames
	}
	sourceFramesCh := make(chan [][]byte, 1)
	destFramesCh := make(chan [][]byte, 1)
	go func() { sourceFramesCh <- collectFrames(sourceClient) }()
	go func() { destFramesCh <- collectFrames(destClient) }()

	s.broadcastTeleportMove(server, caster, dest)

	sourceFrames := <-sourceFramesCh
	destFrames := <-destFramesCh

	var sourceDisappear bool
	for _, frame := range sourceFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode source frame error = %v", err)
		}
		if cmd.Ident == mir176.SMDisappear {
			sourceDisappear = true
		}
	}
	if !sourceDisappear {
		t.Fatal("missing source SMDisappear frame")
	}

	var destLogon, destFeature, destName bool
	for _, frame := range destFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode dest frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMLogon:
			destLogon = true
			if int(cmd.Param) != dest.X || int(cmd.Tag) != dest.Y {
				t.Fatalf("dest logon frame = %+v, want moved character at (%d,%d)", cmd, dest.X, dest.Y)
			}
		case mir176.SMFeatureChanged:
			destFeature = true
		case mir176.SMUserName:
			destName = true
		}
	}
	if !destLogon {
		t.Fatal("missing dest SMLogon frame")
	}
	if !destFeature {
		t.Fatal("missing dest SMFeatureChanged frame")
	}
	if !destName {
		t.Fatal("missing dest SMUserName frame")
	}
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

func readFrameSkippingMagicFire(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	for {
		frame, ok := readFrameWithTimeout(t, conn, time.Second)
		if !ok {
			t.Fatal("timed out waiting for frame")
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode frame error = %v", err)
		}
		if cmd.Ident == mir176.SMMagicFire {
			continue
		}
		return frame
	}
}

func TestHandleSaySendsHearMessage(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	decodedBody, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	parts := bytes.Split(decodedBody, []byte{'/'})
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
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	if len(stocks) != 2 {
		t.Fatalf("merchant stocks = %+v, want 2 stock entries", stocks)
	}
}

func TestHandleUserBuyItemFailsWhenBagIsFull(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	if len(stocks) != 4 {
		t.Fatalf("merchant stocks = %+v, want 4 stock entries", stocks)
	}
}

func TestHandleUserRepairItemNormalRepairReducesMaxDurability(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Gold = 100000
	ch.BagItems = []storage.UserItem{{ItemID: testWeaponID, MakeIndex: 77, Dura: 30, DuraMax: 60}}
	if entry, item, ok := merchantBagItemByMakeIndex(ch, 77, testWeaponID, s.world); !ok {
		t.Fatalf("merchantBagItemByMakeIndex() failed for %s", testWeaponID)
	} else if price := merchantRepairPrice(item, entry, false, s.world.Gameplay().Castle.SuperRepairPriceRate); price <= 0 {
		t.Fatalf("repair price = %d, item=%+v entry=%+v", price, item, entry)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleUserRepairItem(server, &ch, activeClient, mir176.Command{
			Ident: mir176.CMUserRepairItem,
			Recog: s.world.NPCActorID("guide"),
			Param: uint16(77),
		}, WireString(t, testWeaponID))
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
	if cmd.Ident != mir176.SMUserRepairItemOK {
		t.Fatalf("ident = %d, want SMUserRepairItemOK (%d)", cmd.Ident, mir176.SMUserRepairItemOK)
	}
	if goldCmd.Ident != mir176.SMGoldChanged {
		t.Fatalf("gold ident = %d, want SMGoldChanged (%d)", goldCmd.Ident, mir176.SMGoldChanged)
	}
	if got, want := ch.BagItems[0].DuraMax, uint16(59); got != want {
		t.Fatalf("duraMax = %d, want %d", got, want)
	}
	if got, want := ch.BagItems[0].Dura, uint16(59); got != want {
		t.Fatalf("dura = %d, want %d", got, want)
	}
	if ch.Gold >= 100000 {
		t.Fatalf("gold = %d, want reduced", ch.Gold)
	}
}

func TestHandleUserRepairItemSpecialRepairKeepsMaxDurability(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Gold = 100000
	ch.BagItems = []storage.UserItem{{ItemID: testWeaponID, MakeIndex: 77, Dura: 30, DuraMax: 60}}
	if entry, item, ok := merchantBagItemByMakeIndex(ch, 77, testWeaponID, s.world); !ok {
		t.Fatalf("merchantBagItemByMakeIndex() failed for %s", testWeaponID)
	} else if price := merchantRepairPrice(item, entry, true, s.world.Gameplay().Castle.SuperRepairPriceRate); price <= 0 {
		t.Fatalf("special repair price = %d, item=%+v entry=%+v", price, item, entry)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	activeClient := s.registerClient(server, ch)
	defer s.unregisterClient(server)
	activeClient.mu.Lock()
	activeClient.merchantCurrentLabel = "@s_repair"
	activeClient.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleUserRepairItem(server, &ch, activeClient, mir176.Command{
			Ident: mir176.CMUserRepairItem,
			Recog: s.world.NPCActorID("guide"),
			Param: uint16(77),
		}, WireString(t, testWeaponID))
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
	if cmd.Ident != mir176.SMUserRepairItemOK {
		t.Fatalf("ident = %d, want SMUserRepairItemOK (%d)", cmd.Ident, mir176.SMUserRepairItemOK)
	}
	if goldCmd.Ident != mir176.SMGoldChanged {
		t.Fatalf("gold ident = %d, want SMGoldChanged (%d)", goldCmd.Ident, mir176.SMGoldChanged)
	}
	if got, want := ch.BagItems[0].DuraMax, uint16(60); got != want {
		t.Fatalf("duraMax = %d, want %d", got, want)
	}
	if got, want := ch.BagItems[0].Dura, uint16(60); got != want {
		t.Fatalf("dura = %d, want %d", got, want)
	}
	if ch.Gold >= 100000 {
		t.Fatalf("gold = %d, want reduced", ch.Gold)
	}
}

func TestHandleMerchantDlgSelectOpensStorageWindow(t *testing.T) {
	s := newTestServer(t)
	entity := testGuideNPC()
	mapID, x, y := entity.MapID, entity.X, entity.Y
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch1, err := s.world.CreateCharacterWithAppearance("test", "tester1", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch2, err := s.world.CreateCharacterWithAppearance("test", "tester2", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch1, err := s.world.CreateCharacterWithAppearance("test", "tester1", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch2, err := s.world.CreateCharacterWithAppearance("test", "tester2", "warrior", 0, 0, "1", 17, 12)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Dir = 2
	before, _ := s.world.SnapshotAround(ch.MapID, 0, 0, 99999)
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

	after, _ := s.world.SnapshotAround(ch.MapID, 0, 0, 99999)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Dir = 2
	before, _ := s.world.SnapshotAround(ch.MapID, 0, 0, 99999)
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

	after, _ := s.world.SnapshotAround(ch.MapID, 0, 0, 99999)
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
	mapID, x, y := testDefaultSpawn(t)
	want, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	want, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	if _, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y); err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	if _, err := s.world.SpawnMonsterByNameAt(mapID, x+1, y, "鹿", 1); err != nil {
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Gold = 321
	ch.PremiumGold = 654
	ch.SoftVersionDate = 20020522
	ch.AllowGroup = true
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
	if changeLightCmd.Tag != 500 {
		t.Fatalf("SM_CHANGELIGHT = %+v, want clientkey=500", changeLightCmd)
	}
	logonCmd, logonBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SM_LOGON frame error = %v", err)
	}
	if logonCmd.Ident != mir176.SMLogon {
		t.Fatalf("third frame ident = %d, want SM_LOGON (%d)", logonCmd.Ident, mir176.SMLogon)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Gold = 321
	ch.PremiumGold = 654
	ch.PremiumPoint = 654
	ch.AttackMode = 3
	ch.BonusPoint = 12
	ch.BonusAbil = storage.BonusAbility{DC: 1, MC: 2, SC: 3, AC: 4, MAC: 5, HP: 6, MP: 7, Hit: 8, Speed: 9}
	setEquippedItem(&ch, SlotWeapon, storage.UserItem{ItemID: testWeaponID})
	ch.Skills = storage.SkillStates{{ID: "火球术", Level: 3, Train: 100, Hotkey: '1'}}
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
	if useMagicDecoded[0] != '1' || useMagicDecoded[1] != 3 || useMagicDecoded[2] != 0 || useMagicDecoded[3] != 0 {
		t.Fatalf("SM_SENDMYMAGIC header = % x, want key/level header", useMagicDecoded[:4])
	}
	if got := binary.LittleEndian.Uint32(useMagicDecoded[4:8]); got != 100 {
		t.Fatalf("SM_SENDMYMAGIC curtrain = %d, want 100", got)
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
	if useMagicDecoded[25] != 1 || useMagicDecoded[26] != 1 || useMagicDecoded[27] != 0 {
		t.Fatalf("SM_SENDMYMAGIC effect header = % x, want 01 01 00", useMagicDecoded[25:28])
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
	if got := binary.LittleEndian.Uint32(useMagicDecoded[54:58]); got != 60 {
		t.Fatalf("SM_SENDMYMAGIC delay = %d, want 60", got)
	}

	_ = client.Close()
	<-done
}

func TestHandleMagicKeyChangeUpdatesSkillHotkey(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Skills = storage.SkillStates{{ID: "精神力战法", Level: 0, Train: 0}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleMagicKeyChange(server, &ch, mir176.Command{Ident: mir176.CMMagicKeyChange, Param: 4, Tag: uint16('7')})
	}()

	if _, ok := readFrameWithTimeout(t, client, time.Second); ok {
		t.Fatal("unexpected frame after magic key change")
	}
	<-done
	if ch.Skills[0].Hotkey != '7' {
		t.Fatalf("character hotkey = %q, want '7'", ch.Skills[0].Hotkey)
	}
}

func TestSendUseMagicAppendsAttackSkillFlags(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Skills = storage.SkillStates{
		{ID: "火球术", Level: 3, Train: 100, Hotkey: '1'},
		{ID: "刺杀剑术", Level: 0, Train: 0, Hotkey: '2'},
		{ID: "半月弯刀", Level: 0, Train: 0, Hotkey: '3'},
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.sendUseMagic(server, ch)
	}()

	magicCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode SMSendMyMagic frame error = %v", err)
	}
	if magicCmd.Ident != mir176.SMSendMyMagic {
		t.Fatalf("first frame ident = %d, want SMSendMyMagic (%d)", magicCmd.Ident, mir176.SMSendMyMagic)
	}
	longFrame := readFrame(t, client)
	longBody, err := mir176.UnwrapFrame(longFrame)
	if err != nil {
		t.Fatalf("decode +LNG frame error = %v", err)
	}
	if string(longBody) != "+LNG" {
		t.Fatalf("long frame body = %q, want +LNG", longBody)
	}
	wideFrame := readFrame(t, client)
	wideBody, err := mir176.UnwrapFrame(wideFrame)
	if err != nil {
		t.Fatalf("decode +WID frame error = %v", err)
	}
	if string(wideBody) != "+WID" {
		t.Fatalf("wide frame body = %q, want +WID", wideBody)
	}
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	_, drops := s.world.SnapshotAround(ch.MapID, 0, 0, 99999)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	_, drops := s.world.SnapshotAround(ch.MapID, 0, 0, 99999)
	for _, drop := range drops {
		if drop.ItemID == testWeaponID {
			t.Fatalf("unexpected drop created: %+v", drop)
		}
	}
}

func TestHandlePickupHidesGroundItem(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch, drop, err := s.world.DropItemCountByBagIndex(ch, 0, testWeaponID)
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
	_, drops := s.world.SnapshotAround(ch.MapID, 0, 0, 99999)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BagItems = []storage.UserItem{{ItemID: testHPItemID, MakeIndex: 501}, {ItemID: testHPItemID, MakeIndex: 502}}
	ch, first, err := s.world.DropItemCountByBagIndex(ch, 501, testHPItemID)
	if err != nil {
		t.Fatalf("DropItemCountByBagIndex(first) error = %v", err)
	}
	ch, second, err := s.world.DropItemCountByBagIndex(ch, 502, testHPItemID)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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

	delCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode del frame error = %v", err)
	}
	if delCmd.Ident != mir176.SMDelItems {
		t.Fatalf("first frame ident = %d, want SMDelItems (%d)", delCmd.Ident, mir176.SMDelItems)
	}
	healthCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode health frame error = %v", err)
	}
	if healthCmd.Ident != mir176.SMHealthSpellChanged {
		t.Fatalf("second frame ident = %d, want SM_HEALTHSPELLCHANGED (%d)", healthCmd.Ident, mir176.SMHealthSpellChanged)
	}
	weightCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode weight frame error = %v", err)
	}
	if weightCmd.Ident != mir176.SMWeightChanged {
		t.Fatalf("third frame ident = %d, want SM_WEIGHTCHANGED (%d)", weightCmd.Ident, mir176.SMWeightChanged)
	}
	stats := s.world.AbilityStats(ch)
	if weightCmd.Recog != int32(stats.Weight) || weightCmd.Param != uint16(stats.WearWeight) || weightCmd.Tag != uint16(stats.HandWeight) {
		t.Fatalf("weight frame = %+v, want weight=%d wear=%d hand=%d", weightCmd, stats.Weight, stats.WearWeight, stats.HandWeight)
	}
	healthStats := s.world.AbilityStats(ch)
	if healthStats.HP != ch.HP || healthStats.MP != ch.MP || healthStats.MaxHP != ch.MaxHP {
		t.Fatalf("post-eat stats = %+v, want hp/mp/maxhp = %d/%d/%d", healthStats, ch.HP, ch.MP, ch.MaxHP)
	}
	eatCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode eat frame error = %v", err)
	}
	if eatCmd.Ident != mir176.SMEatOK {
		t.Fatalf("fourth frame ident = %d, want SM_EAT_OK (%d)", eatCmd.Ident, mir176.SMEatOK)
	}
	if eatCmd.Recog != 0 {
		t.Fatalf("eat recog = %d, want 0", eatCmd.Recog)
	}
	<-done
	if ch.HP != 19 {
		t.Fatalf("HP = %d, want 19", ch.HP)
	}
	if countBagItems(ch.BagItems) != 0 {
		t.Fatalf("bag = %+v, want empty after potion use", ch.BagItems)
	}
}

func TestHandleEatItemLearningBookRefreshesMagicList(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Level = 100
	ch.BagItems = []storage.UserItem{{ItemID: "火球术", MakeIndex: 300}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleEatItem(server, &ch, mir176.Command{Ident: mir176.CMEat, Recog: 0}, nil)
	}()

	delCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode del frame error = %v", err)
	}
	if delCmd.Ident != mir176.SMDelItems {
		t.Fatalf("first frame ident = %d, want SMDelItems (%d)", delCmd.Ident, mir176.SMDelItems)
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
	if !ch.Skills.Has("火球术") {
		t.Fatalf("skills = %+v, want learned 火球术", ch.Skills)
	}
	if countBagItems(ch.BagItems) != 0 {
		t.Fatalf("bag = %+v, want consumed skill book", ch.BagItems)
	}
}

func TestApplyWorldTickSendsHealthRefreshForQueuedRecovery(t *testing.T) {
	s := newTestServer(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, "D12", 0, 0)
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
	now := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.applyWorldTick(world.TickResult{Characters: []storage.Character{updated}}, now)
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

func TestApplyWorldTickSendsStateRefreshForExpiredStealth(t *testing.T) {
	s := newTestServer(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "taoist", 0, 0, "D12", 0, 0)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	updated := ch
	updated.TransparentUntil = 0
	now := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.applyWorldTick(world.TickResult{
			Characters:             []storage.Character{updated},
			StateRefreshCharacters: []storage.Character{updated},
		}, now)
	}()

	refreshCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode state refresh frame error = %v", err)
	}
	if refreshCmd.Ident != mir176.SMSpacemoveShow {
		t.Fatalf("frame ident = %d, want SM_SPACEMOVE_SHOW (%d)", refreshCmd.Ident, mir176.SMSpacemoveShow)
	}
	<-done
}

func TestApplyWorldTickSendsAbilityRefreshForExpiredProtection(t *testing.T) {
	s := newTestServer(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, "D12", 0, 0)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	updated := ch
	now := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.applyWorldTick(world.TickResult{
			Characters:               []storage.Character{updated},
			AbilityRefreshCharacters: []storage.Character{updated},
		}, now)
	}()

	abilityCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode ability refresh frame error = %v", err)
	}
	if abilityCmd.Ident != mir176.SMAbility {
		t.Fatalf("frame ident = %d, want SM_ABILITY (%d)", abilityCmd.Ident, mir176.SMAbility)
	}
	<-done
}

func TestHandleUserCommandMakeSendsAddItemFrames(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
		result, ok := s.world.HandleUserCommand(ch, "@Make 回城卷 2")
		if !ok {
			return
		}
		s.handleUserCommandResult(server, &ch, result)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		result, ok := s.world.HandleUserCommand(ch, "@Make 裁决之杖 1")
		if !ok {
			return
		}
		s.handleUserCommandResult(server, &ch, result)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		result, ok := s.world.HandleUserCommand(ch, "@Make 屠龙 1")
		if !ok {
			return
		}
		s.handleUserCommandResult(server, &ch, result)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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

	delCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode del frame error = %v", err)
	}
	if delCmd.Ident != mir176.SMDelItems {
		t.Fatalf("first frame ident = %d, want SMDelItems (%d)", delCmd.Ident, mir176.SMDelItems)
	}
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
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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

	delCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode del frame error = %v", err)
	}
	if delCmd.Ident != mir176.SMDelItems {
		t.Fatalf("first frame ident = %d, want SMDelItems (%d)", delCmd.Ident, mir176.SMDelItems)
	}
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
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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

	delCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode del frame error = %v", err)
	}
	if delCmd.Ident != mir176.SMDelItems {
		t.Fatalf("first frame ident = %d, want SMDelItems (%d)", delCmd.Ident, mir176.SMDelItems)
	}
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
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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

	delCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode del frame error = %v", err)
	}
	if delCmd.Ident != mir176.SMDelItems {
		t.Fatalf("first frame ident = %d, want SMDelItems (%d)", delCmd.Ident, mir176.SMDelItems)
	}
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
	item, ok := s.world.Item(drop.ItemID)
	if !ok {
		t.Fatalf("Item(%s) missing", drop.ItemID)
	}
	item = world.UpgradeClientItemForDisplay(item, storage.UserItem{Desc: drop.Desc}, true)
	dura := world.ItemDuraForEquip(item)
	body := EncodeBuffer(ClientItemBody(item, drop.Desc, drop.MakeIndex, dura, dura))
	go s.sendCommand(server, mir176.Command{Ident: mir176.SMAddItem}, body)

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
	item, ok := s.world.Item(drop.ItemID)
	if !ok {
		t.Fatalf("Item(%s) missing", drop.ItemID)
	}
	item = world.UpgradeClientItemForDisplay(item, storage.UserItem{Desc: drop.Desc}, true)
	dura := world.ItemDuraForEquip(item)
	body := EncodeBuffer(ClientItemBody(item, drop.Desc, drop.MakeIndex, dura, dura))
	go s.sendCommand(server, mir176.Command{Ident: mir176.SMAddItem}, body)

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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.BagItems = []storage.UserItem{
		{ItemID: testWeaponID, MakeIndex: 1},
		{ItemID: testArmorID, MakeIndex: 2},
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
	mapID, x, y := testDefaultSpawn(t)
	owner, err := s.world.CreateCharacterWithAppearance("test", "leader", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(owner) error = %v", err)
	}
	member, err := s.world.CreateCharacterWithAppearance("test", "member", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	owner, err := s.world.CreateCharacterWithAppearance("test", "leader", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(owner) error = %v", err)
	}
	member, err := s.world.CreateCharacterWithAppearance("test", "member", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
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
		if cmd, _, decErr := decodeMessageLikeClient(frame); decErr == nil {
			t.Fatalf("ack body = %q, want +GOOD/ prefix (first frame ident=%d)", body, cmd.Ident)
		}
		t.Fatalf("ack body = %q, want +GOOD/ prefix", body)
	}
}

func assertActionFail(t *testing.T, frame []byte) {
	t.Helper()
	body, err := mir176.UnwrapFrame(frame)
	if err != nil {
		t.Fatalf("UnwrapFrame() error = %v", err)
	}
	if !strings.HasPrefix(string(body), "+FAIL/") {
		if cmd, _, decErr := decodeMessageLikeClient(frame); decErr == nil {
			t.Fatalf("fail body = %q, want +FAIL/ prefix (first frame ident=%d)", body, cmd.Ident)
		}
		t.Fatalf("fail body = %q, want +FAIL/ prefix", body)
	}
}

func isActionAckFrame(frame []byte) bool {
	body, err := mir176.UnwrapFrame(frame)
	return err == nil && strings.HasPrefix(string(body), "+GOOD/")
}

func isActionFailFrame(frame []byte) bool {
	body, err := mir176.UnwrapFrame(frame)
	return err == nil && strings.HasPrefix(string(body), "+FAIL/")
}

func collectFramesUntilActionAck(t *testing.T, conn net.Conn, max int) [][]byte {
	t.Helper()
	frames := make([][]byte, 0, max)
	for i := 0; i < max; i++ {
		frame := readFrame(t, conn)
		frames = append(frames, frame)
		if isActionAckFrame(frame) {
			return frames
		}
	}
	t.Fatal("missing action ack frame")
	return frames
}

func collectFramesUntilActionFail(t *testing.T, conn net.Conn, max int) [][]byte {
	t.Helper()
	frames := make([][]byte, 0, max)
	for i := 0; i < max; i++ {
		frame := readFrame(t, conn)
		frames = append(frames, frame)
		if isActionFailFrame(frame) {
			return frames
		}
	}
	t.Fatal("missing action fail frame")
	return frames
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

func readFrameWithTimeout(t *testing.T, c net.Conn, d time.Duration) ([]byte, bool) {
	t.Helper()
	if err := c.SetReadDeadline(time.Now().Add(d)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	defer c.SetReadDeadline(time.Time{})
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return nil, false
		}
		t.Fatalf("Read() error = %v", err)
	}
	return append([]byte(nil), buf[:n]...), true
}
