package network

import (
	"net"
	"testing"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"openmir2/internal/config"
	"openmir2/internal/protocol/mir176"
	"openmir2/internal/storage"
	"openmir2/internal/world"
)

func TestDealItemBodyUsesRecipientClientLayout(t *testing.T) {
	s := newTestServer(t)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	ch := storage.Character{ID: "deal-layout-recipient", Name: "recipient"}
	s.registerClient(serverConn, ch)
	defer s.unregisterClient(serverConn)
	entry := storage.UserItem{ItemID: "木剑", MakeIndex: 1, Dura: 1000, DuraMax: 1000}
	if got := len(s.dealItemBody(serverConn, entry, true)); got != 50 {
		t.Fatalf("legacy deal item body length = %d, want 50", got)
	}
	client := s.clientForConn(serverConn)
	client.mu.Lock()
	client.softVersion = 1
	client.mu.Unlock()
	if got := len(s.dealItemBody(serverConn, entry, true)); got != 56 {
		t.Fatalf("modern deal item body length = %d, want 56", got)
	}
}

func TestDealTryRejectsWhenDealIsDisabled(t *testing.T) {
	gameplay := config.DefaultGameplay()
	gameplay.Deal.DisableDeal = true
	s := newTestServerWithGameplay(t, gameplay)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "deal-disabled", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	s.registerClient(serverConn, ch)

	s.handleDealTry(serverConn, &ch)
	assertMenuOK(t, readFrame(t, clientConn), ch, "交易功能暂时关闭！！！")
}

func TestDealTryChecksDisabledBeforeActiveDeal(t *testing.T) {
	gameplay := config.DefaultGameplay()
	gameplay.Deal.DisableDeal = true
	s := newTestServerWithGameplay(t, gameplay)
	ch := storage.Character{ID: "deal-disabled-active", Name: "deal-disabled-active"}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	s.registerClient(serverConn, ch)
	client := s.clientForConn(serverConn)
	client.mu.Lock()
	client.dealPeerID = "already-dealing"
	client.mu.Unlock()

	s.handleDealTry(serverConn, &ch)
	assertMenuOK(t, readFrame(t, clientConn), ch, "交易功能暂时关闭！！！")
}

func TestDealCancelDoesNothingWithoutActiveDeal(t *testing.T) {
	s := newTestServer(t)
	ch := storage.Character{ID: "deal-cancel-idle", Name: "idle"}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	s.registerClient(serverConn, ch)

	s.handleDealCancel(serverConn)
	if _, ok := readFrameWithTimeout(t, clientConn, 25*time.Millisecond); ok {
		t.Fatal("idle deal cancellation emitted a response")
	}
}

func TestDealChangeGoldRejectsNegativeAmountBeforePeerLookup(t *testing.T) {
	s := newTestServer(t)
	ch := storage.Character{ID: "deal-negative-gold", Name: "deal-negative-gold", Gold: 100}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	s.registerClient(serverConn, ch)

	s.handleDealChangeGold(serverConn, &ch, mir176.Command{Recog: ^int32(0)})
	cmd, _, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode negative gold response: %v", err)
	}
	if cmd.Ident != mir176.SMDealChangeGoldFail || cmd.Recog != 0 || cmd.Param != 100 {
		t.Fatalf("negative gold response = %+v, want immediate change-gold failure", cmd)
	}
}

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
	assertDealOpened(t, firstClient, first, second)
	assertDealOpened(t, secondClient, second, first)
	for _, conn := range []net.Conn{firstServer, secondServer} {
		client := s.clientForConn(conn)
		client.mu.Lock()
		client.dealLastAt = time.Now().Add(-4 * time.Second)
		client.mu.Unlock()
	}

	s.handleDealEnd(firstServer, &first)
	assertSystemMessage(t, readFrame(t, firstClient), first, "你已经确认交易了.", makeWord(0xDB, 0xFF))
	assertSystemMessage(t, readFrame(t, secondClient), second, "对方已经确认交易了.", makeWord(0xDB, 0xFF))
	s.handleDealEnd(secondServer, &second)
	assertDealIdent(t, readFrame(t, firstClient), mir176.SMDealSuccess)
	assertSystemMessage(t, readFrame(t, firstClient), first, "交易成功...", makeWord(0xDB, 0xFF))
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealSuccess)
	assertSystemMessage(t, readFrame(t, secondClient), second, "交易成功...", makeWord(0xDB, 0xFF))

	first.Dir = 2
	second.Dir = 6
	s.updateClient(firstServer, first)
	s.updateClient(secondServer, second)
	s.handleDealTry(firstServer, &first)
	assertDealOpened(t, firstClient, first, second)
	assertDealOpened(t, secondClient, second, first)
	s.handleDealCancel(firstServer)
	assertDealIdent(t, readFrame(t, firstClient), mir176.SMDealCancel)
	assertSystemMessage(t, readFrame(t, firstClient), first, "交易取消！！！", makeWord(0xDB, 0xFF))
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealCancel)
	assertSystemMessage(t, readFrame(t, secondClient), second, "交易取消！！！", makeWord(0xDB, 0xFF))
	s.handleDealTry(firstServer, &first)
	assertMenuOK(t, readFrame(t, firstClient), first, "请稍候再交易！！！")
}

func TestDealTryRestoresStaleOfferBeforeOpening(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	first, err := s.world.CreateCharacterWithAppearance("test", "deal-stale-first", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(first) error = %v", err)
	}
	second, err := s.world.CreateCharacterWithAppearance("test", "deal-stale-second", "warrior", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter(second) error = %v", err)
	}
	first.Dir = 2
	second.Dir = 6
	item := storage.UserItem{ItemID: "木剑", MakeIndex: 321, Dura: 1000, DuraMax: 1000}
	firstServer, firstClient := net.Pipe()
	secondServer, secondClient := net.Pipe()
	defer firstServer.Close()
	defer firstClient.Close()
	defer secondServer.Close()
	defer secondClient.Close()
	s.registerClient(firstServer, first)
	s.registerClient(secondServer, second)
	firstClientState := s.clientForConn(firstServer)
	firstClientState.mu.Lock()
	firstClientState.dealItems = []storage.UserItem{item}
	firstClientState.dealGold = 50
	firstClientState.ch.Gold = 100
	firstClientState.mu.Unlock()
	first.Gold = 100

	s.handleDealTry(firstServer, &first)
	assertDealOpened(t, firstClient, first, second)
	assertDealOpened(t, secondClient, second, first)
	if len(first.BagItems) < 1 || first.Gold != 150 {
		t.Fatalf("restored offer = bag:%+v gold:%d, want item and 150 gold", first.BagItems, first.Gold)
	}
	found := false
	for _, entry := range first.BagItems {
		if entry.MakeIndex == item.MakeIndex && entry.ItemID == item.ItemID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("restored offer missing item %d: %+v", item.MakeIndex, first.BagItems)
	}
}

func TestDealChangeGoldRejectsWithoutActivePeer(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "deal-no-peer", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	s.registerClient(serverConn, ch)
	s.handleDealChangeGold(serverConn, &ch, mir176.Command{Recog: 100})
	message, _, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil || message.Ident != mir176.SMDealChangeGoldFail || message.Recog != 0 || message.Param != uint16(ch.Gold) {
		t.Fatalf("no-peer gold response = %+v, err=%v, want reference failure", message, err)
	}
}

func TestDealEndTooFastNotifiesAndCancels(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	first, err := s.world.CreateCharacterWithAppearance("test", "deal-fast-first", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(first) error = %v", err)
	}
	second, err := s.world.CreateCharacterWithAppearance("test", "deal-fast-second", "warrior", 0, 0, mapID, x+1, y)
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
	assertDealOpened(t, firstClient, first, second)
	assertDealOpened(t, secondClient, second, first)
	s.handleDealEnd(firstServer, &first)
	assertSystemMessage(t, readFrame(t, firstClient), first, "过早按了成交按钮。", makeWord(0xFF, 0x38))
	assertDealIdent(t, readFrame(t, firstClient), mir176.SMDealCancel)
	assertSystemMessage(t, readFrame(t, firstClient), first, "交易取消！！！", makeWord(0xDB, 0xFF))
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealCancel)
	assertSystemMessage(t, readFrame(t, secondClient), second, "交易取消！！！", makeWord(0xDB, 0xFF))
	if s.clientForConn(firstServer).dealPeerID != "" || s.clientForConn(secondServer).dealPeerID != "" {
		t.Fatal("too-fast deal remained active after cancellation")
	}
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
	if err := s.store.SaveCharacter(first); err != nil {
		t.Fatalf("SaveCharacter(first baseline) error = %v", err)
	}
	firstServer, firstClient := net.Pipe()
	secondServer, secondClient := net.Pipe()
	defer firstServer.Close()
	defer firstClient.Close()
	defer secondServer.Close()
	defer secondClient.Close()
	s.registerClient(firstServer, first)
	s.registerClient(secondServer, second)
	s.handleDealTry(firstServer, &first)
	assertDealOpened(t, firstClient, first, second)
	assertDealOpened(t, secondClient, second, first)
	itemName, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("木剑 extra"))
	if err != nil {
		t.Fatalf("encode item name: %v", err)
	}
	firstLastBefore := s.clientForConn(firstServer).dealLastAt
	secondLastBefore := s.clientForConn(secondServer).dealLastAt
	s.handleDealAddItem(firstServer, &first, mir176.Command{Recog: 123}, itemName)
	assertDealIdent(t, readFrame(t, firstClient), mir176.SMDealAddItemOK)
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealRemoteAddItem)
	if got := s.clientForConn(firstServer).dealLastAt; !got.After(firstLastBefore) {
		t.Fatalf("deal add timestamp = %v, want after %v", got, firstLastBefore)
	}
	if got := s.clientForConn(secondServer).dealLastAt; !got.After(secondLastBefore) {
		t.Fatalf("remote deal add timestamp = %v, want after %v", got, secondLastBefore)
	}
	if len(first.BagItems) != 0 {
		t.Fatalf("bag after add = %d items, want 0", len(first.BagItems))
	}
	s.handleDealDelItem(firstServer, &first, mir176.Command{Recog: 123}, itemName)
	assertMenuOK(t, readFrame(t, firstClient), first, "交易的金币或物品不可以取回，要取回请取消再重新交易！！！")
	assertDealIdent(t, readFrame(t, firstClient), mir176.SMDealDelItemFail)
	if len(first.BagItems) != 0 {
		t.Fatalf("bag after rejected delete = %d items, want 0", len(first.BagItems))
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
	s.handleDealChangeGold(firstServer, &first, mir176.Command{Recog: 100})
	assertMenuOK(t, readFrame(t, firstClient), first, "交易的金币或物品不可以取回，要取回请取消再重新交易！！！")
	assertDealIdent(t, readFrame(t, firstClient), mir176.SMDealDelItemFail)
	if first.Gold != 300 {
		t.Fatalf("gold after rejected return = %d, want 300", first.Gold)
	}
	s.handleDealCancel(firstServer)
	assertDealIdent(t, readFrame(t, firstClient), mir176.SMDealCancel)
	assertSystemMessage(t, readFrame(t, firstClient), first, "交易取消！！！", makeWord(0xDB, 0xFF))
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealCancel)
	assertSystemMessage(t, readFrame(t, secondClient), second, "交易取消！！！", makeWord(0xDB, 0xFF))
	restored := s.clientForConn(firstServer).character()
	if len(restored.BagItems) != 1 || restored.Gold != 500 {
		t.Fatalf("state after cancel = items:%d gold:%d, want items:1 gold:500", len(restored.BagItems), restored.Gold)
	}
	stored, ok := s.store.Character(first.ID)
	if !ok || stored.Gold != 500 || len(stored.BagItems) != 1 || stored.BagItems[0].MakeIndex != 123 {
		t.Fatalf("trade cancel persisted runtime state: stored=%+v, want baseline gold/items", stored)
	}
}

func TestDealChangeGoldRequiresCurrentFrontPosition(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	first, err := s.world.CreateCharacterWithAppearance("test", "deal-gold-first", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(first) error = %v", err)
	}
	second, err := s.world.CreateCharacterWithAppearance("test", "deal-gold-second", "warrior", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter(second) error = %v", err)
	}
	first.Dir = 2
	second.Dir = 6
	first.Gold = 500
	firstServer, firstClient := net.Pipe()
	secondServer, secondClient := net.Pipe()
	defer firstServer.Close()
	defer firstClient.Close()
	defer secondServer.Close()
	defer secondClient.Close()
	s.registerClient(firstServer, first)
	s.registerClient(secondServer, second)
	s.handleDealTry(firstServer, &first)
	assertDealOpened(t, firstClient, first, second)
	assertDealOpened(t, secondClient, second, first)
	second.X += 5
	s.updateClient(secondServer, second)
	s.handleDealChangeGold(firstServer, &first, mir176.Command{Recog: 200})
	command, _, err := decodeMessageLikeClient(readFrame(t, firstClient))
	if err != nil || command.Ident != mir176.SMDealChangeGoldFail || command.Recog != 0 {
		t.Fatalf("gold response = %+v, err=%v, want failure after movement", command, err)
	}
	if first.Gold != 500 {
		t.Fatalf("gold after rejected offer = %d, want 500", first.Gold)
	}
	if _, ok := readFrameWithTimeout(t, secondClient, 25*time.Millisecond); ok {
		t.Fatal("peer received a gold update after rejected offer")
	}
}

func TestDealSuccessSendsReceivedItemsAndGoldBeforeSuccess(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	first, err := s.world.CreateCharacterWithAppearance("test", "deal-success-first", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(first) error = %v", err)
	}
	second, err := s.world.CreateCharacterWithAppearance("test", "deal-success-second", "warrior", 0, 0, mapID, x+1, y)
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
	assertDealOpened(t, firstClient, first, second)
	assertDealOpened(t, secondClient, second, first)
	itemName, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("木剑"))
	if err != nil {
		t.Fatalf("encode item name: %v", err)
	}
	s.handleDealAddItem(firstServer, &first, mir176.Command{Recog: 123}, itemName)
	assertDealIdent(t, readFrame(t, firstClient), mir176.SMDealAddItemOK)
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealRemoteAddItem)
	s.handleDealChangeGold(firstServer, &first, mir176.Command{Recog: 200})
	_, _, _ = decodeMessageLikeClient(readFrame(t, firstClient))
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealRemoteChangeGold)
	firstClientState := s.clientForConn(firstServer)
	firstClientState.mu.Lock()
	firstClientState.dealOK = true
	firstClientState.dealLastAt = time.Now().Add(-2 * time.Second)
	firstClientState.mu.Unlock()
	secondClientState := s.clientForConn(secondServer)
	secondClientState.mu.Lock()
	secondClientState.dealLastAt = time.Now().Add(-2 * time.Second)
	secondClientState.mu.Unlock()
	s.handleDealEnd(firstServer, &first)
	assertSystemMessage(t, readFrame(t, firstClient), first, "你已经确认交易了.", makeWord(0xDB, 0xFF))
	assertSystemMessage(t, readFrame(t, secondClient), second, "对方已经确认交易了.", makeWord(0xDB, 0xFF))
	s.handleDealEnd(secondServer, &second)
	assertDealIdent(t, readFrame(t, firstClient), mir176.SMDealSuccess)
	assertSystemMessage(t, readFrame(t, firstClient), first, "交易成功...", makeWord(0xDB, 0xFF))
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMWeightChanged)
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMAddItem)
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMGoldChanged)
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealSuccess)
	assertSystemMessage(t, readFrame(t, secondClient), second, "交易成功...", makeWord(0xDB, 0xFF))
}

func TestDealReturnGateCanBeDisabled(t *testing.T) {
	gameplay := config.DefaultGameplay()
	gameplay.Deal.CanNotGetBackDeal = false
	s := newTestServerWithGameplay(t, gameplay)
	mapID, x, y := testDefaultSpawn(t)
	first, err := s.world.CreateCharacterWithAppearance("test", "deal-override-first", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(first) error = %v", err)
	}
	second, err := s.world.CreateCharacterWithAppearance("test", "deal-override-second", "warrior", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter(second) error = %v", err)
	}
	first.Dir = 2
	second.Dir = 6
	first.Gold = 300
	first.BagItems = []storage.UserItem{{ItemID: "木剑", MakeIndex: 321, Dura: 1000, DuraMax: 1000}}
	firstServer, firstClient := net.Pipe()
	secondServer, secondClient := net.Pipe()
	defer firstServer.Close()
	defer firstClient.Close()
	defer secondServer.Close()
	defer secondClient.Close()
	s.registerClient(firstServer, first)
	s.registerClient(secondServer, second)
	s.handleDealTry(firstServer, &first)
	assertDealOpened(t, firstClient, first, second)
	assertDealOpened(t, secondClient, second, first)
	itemName, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("木剑"))
	if err != nil {
		t.Fatalf("encode item name: %v", err)
	}
	s.handleDealAddItem(firstServer, &first, mir176.Command{Recog: 321}, itemName)
	assertDealIdent(t, readFrame(t, firstClient), mir176.SMDealAddItemOK)
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealRemoteAddItem)
	s.handleDealDelItem(firstServer, &first, mir176.Command{Recog: 321}, itemName)
	assertDealIdent(t, readFrame(t, firstClient), mir176.SMDealDelItemOK)
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealRemoteDelItem)
	if len(first.BagItems) != 1 {
		t.Fatalf("bag after configured return = %d, want 1", len(first.BagItems))
	}
	s.handleDealChangeGold(firstServer, &first, mir176.Command{Recog: 200})
	goldOK, _, err := decodeMessageLikeClient(readFrame(t, firstClient))
	if err != nil || goldOK.Ident != mir176.SMDealChangeGoldOK || goldOK.Recog != 200 {
		t.Fatalf("configured gold offer = %+v, err=%v", goldOK, err)
	}
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealRemoteChangeGold)
	s.handleDealChangeGold(firstServer, &first, mir176.Command{Recog: 100})
	changedGold, _, err := decodeMessageLikeClient(readFrame(t, firstClient))
	if err != nil || changedGold.Ident != mir176.SMDealChangeGoldOK || changedGold.Recog != 100 {
		t.Fatalf("configured gold return = %+v, err=%v", changedGold, err)
	}
	assertDealIdent(t, readFrame(t, secondClient), mir176.SMDealRemoteChangeGold)
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

func TestDealTryStopsSilentlyWhenFrontTileHasMonster(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	first, err := s.world.CreateCharacterWithAppearance("test", "deal-monster-front", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	first.Dir = 2
	if _, err := s.world.SpawnMonsterByNameAt(mapID, x+1, y, testMonsterID, 1); err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	s.registerClient(serverConn, first)

	s.handleDealTry(serverConn, &first)
	if _, ok := readFrameWithTimeout(t, clientConn, 25*time.Millisecond); ok {
		t.Fatal("non-character front object emitted a deal response")
	}
}

func TestFrontMovingActorSkipsDeadObjects(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	dead, err := s.world.CreateCharacterWithAppearance("test", "front-dead", "warrior", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter(dead) error = %v", err)
	}
	live, err := s.world.CreateCharacterWithAppearance("test", "front-live", "warrior", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter(live) error = %v", err)
	}
	dead.HP = 0
	dead.Ghost = true
	dead.Dir = 6
	live.Dir = 6
	observer := storage.Character{ID: "front-observer", Name: "front-observer", MapID: mapID, X: x, Y: y, Dir: 2, HP: 100, MaxHP: 100}
	observerServer, observerClient := net.Pipe()
	deadServer, deadClient := net.Pipe()
	liveServer, liveClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	defer deadServer.Close()
	defer deadClient.Close()
	defer liveServer.Close()
	defer liveClient.Close()
	s.registerClient(observerServer, observer)
	s.registerClient(deadServer, dead)
	s.registerClient(liveServer, live)

	actor, ok := s.frontMovingActor(observer)
	if !ok || !actor.isCharacter || actor.character.ID != live.ID {
		t.Fatalf("frontMovingActor() = %+v, %t; want live character %q after dead target", actor, ok, live.ID)
	}
}

func TestFrontMovingActorClampsAtMapBoundary(t *testing.T) {
	s := newTestServer(t)
	mapID, _, y := testDefaultSpawn(t)
	observer := storage.Character{ID: "front-edge-observer", Name: "front-edge-observer", MapID: mapID, X: 0, Y: y, Dir: 6, HP: 100, MaxHP: 100}
	target := storage.Character{ID: "front-edge-target", Name: "front-edge-target", MapID: mapID, X: 0, Y: y, Dir: 2, HP: 100, MaxHP: 100}
	observerServer, observerClient := net.Pipe()
	targetServer, targetClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	defer targetServer.Close()
	defer targetClient.Close()
	s.registerClient(observerServer, observer)
	s.registerClient(targetServer, target)

	actor, ok := s.frontMovingActor(observer)
	if !ok || !actor.isCharacter || actor.character.ID != target.ID {
		t.Fatalf("frontMovingActor() at boundary = %+v, %t; want %q", actor, ok, target.ID)
	}
}

func TestDealTryRejectsGhostTarget(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	first, err := s.world.CreateCharacterWithAppearance("test", "deal-ghost-first", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(first) error = %v", err)
	}
	second, err := s.world.CreateCharacterWithAppearance("test", "deal-ghost-target", "warrior", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter(second) error = %v", err)
	}
	first.Dir = 2
	second.Dir = 6
	second.Ghost = true
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

func TestDealVisibilitySyncKeepsValidDealAndCancelsAfterMove(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	first, err := s.world.CreateCharacterWithAppearance("test", "deal-position-first", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(first) error = %v", err)
	}
	second, err := s.world.CreateCharacterWithAppearance("test", "deal-position-second", "warrior", 0, 0, mapID, x+1, y)
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
	assertDealOpened(t, firstClient, first, second)
	assertDealOpened(t, secondClient, second, first)
	s.syncVisibleNPCs()
	if s.clientForConn(firstServer).character().ID == "" {
		t.Fatal("first character disappeared during valid deal sync")
	}
	first.X++
	firstState := s.clientForConn(firstServer)
	firstState.mu.Lock()
	firstState.ch = first
	firstState.mu.Unlock()
	s.syncVisibleNPCs()
	if s.clientForConn(firstServer).dealPeerID != "" || s.clientForConn(secondServer).dealPeerID != "" {
		t.Fatal("deal remained active after the participants stopped facing each other")
	}
}

func TestDealPositionValidRejectsFrontBlocker(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	first := storage.Character{ID: "deal-blocker-first", Name: "deal-blocker-first", MapID: mapID, X: x, Y: y, Dir: 2, HP: 100, MaxHP: 100, ObjectOrder: 10}
	second := storage.Character{ID: "deal-blocker-second", Name: "deal-blocker-second", MapID: mapID, X: x + 1, Y: y, Dir: 6, HP: 100, MaxHP: 100, ObjectOrder: 20}
	blocker := storage.Character{ID: "deal-blocker-obstacle", Name: "deal-blocker-obstacle", MapID: mapID, X: x + 1, Y: y, Dir: 6, HP: 100, MaxHP: 100, ObjectOrder: 1}
	firstServer, firstClient := net.Pipe()
	secondServer, secondClient := net.Pipe()
	blockerServer, blockerClient := net.Pipe()
	defer firstServer.Close()
	defer firstClient.Close()
	defer secondServer.Close()
	defer secondClient.Close()
	defer blockerServer.Close()
	defer blockerClient.Close()
	firstState := s.registerClient(firstServer, first)
	secondState := s.registerClient(secondServer, second)
	s.registerClient(blockerServer, blocker)
	firstState.mu.Lock()
	firstState.dealPeerID = second.ID
	firstState.mu.Unlock()
	secondState.mu.Lock()
	secondState.dealPeerID = first.ID
	secondState.mu.Unlock()

	if s.dealPositionValid(firstState, first) {
		t.Fatal("dealPositionValid() = true with a live front blocker")
	}
}

func assertDealOpened(t *testing.T, conn net.Conn, self, other storage.Character) {
	t.Helper()
	assertSystemMessage(t, readFrame(t, conn), self, other.Name+"开始交易.", makeWord(0xDB, 0xFF))
	assertDealMenu(t, readFrame(t, conn), other.Name)
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

func assertMenuOK(t *testing.T, frame []byte, ch storage.Character, want string) {
	t.Helper()
	cmd, body, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode menu ok: %v", err)
	}
	decoded, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("decode menu ok body: %v", err)
	}
	if cmd.Ident != mir176.SMMenuOK || cmd.Recog != world.CharacterActorID(ch) || DecodeString(decoded) != want {
		t.Fatalf("menu ok = %+v body=%q, want actor=%d text=%q", cmd, DecodeString(decoded), world.CharacterActorID(ch), want)
	}
}
