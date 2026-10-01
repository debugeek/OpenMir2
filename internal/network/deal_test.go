package network

import (
	"net"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"openmir2/internal/protocol/mir176"
	"openmir2/internal/storage"
)

func TestDealTryEndAndCancelLifecycle(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	first, err := s.world.CreateCharacterWithAppearance("test", "deal-first", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(first) error = %v", err)
	}
	second, err := s.world.CreateCharacterWithAppearance("test", "deal-second", "warrior", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter(second) error = %v", err)
	}
	first.Dir = 2
	second.Dir = 6
	firstServer, firstClient := net.Pipe()
	secondServer, secondClient := net.Pipe()
	defer firstServer.Close()
	defer firstClient.Close()
	defer secondServer.Close()
	defer secondClient.Close()
	s.registerClient(firstServer, first)
	s.registerClient(secondServer, second)

	s.handleDealTry(firstServer, &first)
	assertDealMenu(t, readFrame(t, firstClient), second.Name)
	assertDealMenu(t, readFrame(t, secondClient), first.Name)
	for _, conn := range []net.Conn{firstServer, secondServer} {
		client := s.clientForConn(conn)
		client.mu.Lock()
		client.dealLastAt = time.Now().Add(-4 * time.Second)
		client.mu.Unlock()
	}

	s.handleDealEnd(firstServer, &first)
	s.handleDealEnd(secondServer, &second)
	assertDealIdent(t, readFrame(t, firstClient), mir176.SMDealSuccess)
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealSuccess)

	first.Dir = 2
	second.Dir = 6
	s.updateClient(firstServer, first)
	s.updateClient(secondServer, second)
	s.handleDealTry(firstServer, &first)
	assertDealMenu(t, readFrame(t, firstClient), second.Name)
	assertDealMenu(t, readFrame(t, secondClient), first.Name)
	s.handleDealCancel(firstServer)
	assertDealIdent(t, readFrame(t, firstClient), mir176.SMDealCancel)
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealCancel)
}

func TestDealItemAndGoldReturnOnCancel(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	first, err := s.world.CreateCharacterWithAppearance("test", "deal-item-first", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(first) error = %v", err)
	}
	second, err := s.world.CreateCharacterWithAppearance("test", "deal-item-second", "warrior", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter(second) error = %v", err)
	}
	first.Dir = 2
	second.Dir = 6
	first.Gold = 500
	first.BagItems = []storage.UserItem{{ItemID: "木剑", MakeIndex: 123, Dura: 1000, DuraMax: 1000}}
	firstServer, firstClient := net.Pipe()
	secondServer, secondClient := net.Pipe()
	defer firstServer.Close()
	defer firstClient.Close()
	defer secondServer.Close()
	defer secondClient.Close()
	s.registerClient(firstServer, first)
	s.registerClient(secondServer, second)
	s.handleDealTry(firstServer, &first)
	assertDealMenu(t, readFrame(t, firstClient), second.Name)
	assertDealMenu(t, readFrame(t, secondClient), first.Name)

	itemName, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("木剑"))
	if err != nil {
		t.Fatalf("encode item name: %v", err)
	}
	s.handleDealAddItem(firstServer, &first, mir176.Command{Recog: 123}, itemName)
	assertDealIdent(t, readFrame(t, firstClient), mir176.SMDealAddItemOK)
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealRemoteAddItem)
	if len(first.BagItems) != 0 {
		t.Fatalf("bag after add = %d items, want 0", len(first.BagItems))
	}

	s.handleDealChangeGold(firstServer, &first, mir176.Command{Recog: 200})
	goldOK, _, err := decodeMessageLikeClient(readFrame(t, firstClient))
	if err != nil || goldOK.Ident != mir176.SMDealChangeGoldOK || goldOK.Recog != 200 {
		t.Fatalf("gold response = %+v, err=%v", goldOK, err)
	}
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealRemoteChangeGold)
	if first.Gold != 300 {
		t.Fatalf("gold after offer = %d, want 300", first.Gold)
	}
	firstClientState := s.clientForConn(firstServer)
	firstClientState.mu.Lock()
	firstClientState.dealOK = true
	firstClientState.dealLastAt = time.Now().Add(-2 * time.Second)
	firstClientState.mu.Unlock()
	s.handleDealChangeGold(firstServer, &first, mir176.Command{Recog: 100})
	changedGold, _, err := decodeMessageLikeClient(readFrame(t, firstClient))
	if err != nil || changedGold.Ident != mir176.SMDealChangeGoldOK || changedGold.Recog != 100 {
		t.Fatalf("confirmed-side gold response = %+v, err=%v", changedGold, err)
	}
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealRemoteChangeGold)

	s.handleDealCancel(firstServer)
	assertDealIdent(t, readFrame(t, firstClient), mir176.SMDealCancel)
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealCancel)
	restored := s.clientForConn(firstServer).character()
	if len(restored.BagItems) != 1 || restored.Gold != 500 {
		t.Fatalf("state after cancel = items:%d gold:%d, want items:1 gold:500", len(restored.BagItems), restored.Gold)
	}
}

func TestDealTryRequiresTheFrontTile(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	first, err := s.world.CreateCharacterWithAppearance("test", "deal-front", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(first) error = %v", err)
	}
	second, err := s.world.CreateCharacterWithAppearance("test", "deal-side", "warrior", 0, 0, mapID, x, y+1)
	if err != nil {
		t.Fatalf("CreateCharacter(second) error = %v", err)
	}
	first.Dir = 2
	firstServer, firstClient := net.Pipe()
	secondServer, secondClient := net.Pipe()
	defer firstServer.Close()
	defer firstClient.Close()
	defer secondServer.Close()
	defer secondClient.Close()
	s.registerClient(firstServer, first)
	s.registerClient(secondServer, second)
	s.handleDealTry(firstServer, &first)
	assertDealIdent(t, readFrame(t, firstClient), mir176.SMDealTryFail)
}

func assertDealMenu(t *testing.T, frame []byte, name string) {
	t.Helper()
	cmd, body, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode deal menu: %v", err)
	}
	decoded, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("decode deal menu body: %v", err)
	}
	if cmd.Ident != mir176.SMDealMenu || DecodeString(decoded) != name {
		t.Fatalf("deal menu = ident:%d body:%q, want ident:%d body:%q", cmd.Ident, DecodeString(decoded), mir176.SMDealMenu, name)
	}
}

func assertDealIdent(t *testing.T, frame []byte, ident uint16) {
	t.Helper()
	cmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode deal response: %v", err)
	}
	if cmd.Ident != ident {
		t.Fatalf("deal response ident = %d, want %d", cmd.Ident, ident)
	}
}
