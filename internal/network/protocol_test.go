package network

import (
	"encoding/binary"
	"fmt"
	"testing"

	"openmir2/internal/data"
	"openmir2/internal/protocol/mir176"
	"openmir2/internal/storage"
	"openmir2/internal/world"
)

func TestDecodeRunLogin(t *testing.T) {
	frame := mir176.WrapFrame(append([]byte{'1'}, mir176.EncodePlain6Payload([]byte("**test/hero/1/176/2022080300"))...))
	got, ok := decodeRunLogin(frame)
	if !ok {
		t.Fatalf("decodeRunLogin() ok = false")
	}
	if got.Account != "test" || got.CharName != "hero" || got.SessionID != 1 || got.Version != 176 || got.Code != 2022080300 {
		t.Fatalf("decodeRunLogin() = %#v", got)
	}
}

func TestExtendedSoftVersionMatchesReferenceExtraction(t *testing.T) {
	tests := []struct {
		version int
		want    int
	}{
		{version: 0, want: 0},
		{version: 21158117, want: 0},
		{version: 100000000, want: 0},
		{version: 121158117, want: 100000000},
		{version: 221158117, want: 200000000},
	}
	for _, tt := range tests {
		if got := extendedSoftVersion(tt.version); got != tt.want {
			t.Fatalf("extendedSoftVersion(%d) = %d, want %d", tt.version, got, tt.want)
		}
	}
}

func TestValidateRunLoginMatchesCaseInsensitiveCharacterLookup(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.world.CreateCharacterWithAppearance("account", "Hero", "warrior", 0, 0, testMapID, 10, 10); err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	sessionID := s.newSessionID("account")

	if !s.validateRunLogin(RunLogin{Account: "account", CharName: "hero", SessionID: sessionID}) {
		t.Fatal("validateRunLogin() rejected case-insensitive character name")
	}
}

func TestSessionPlayLifecycleMatchesSelectConnectionState(t *testing.T) {
	s := &Server{sessions: map[int32]string{}, sessionPlaying: map[int32]bool{}}
	sessionID := s.newSessionID("account")
	if s.sessionPlayingState(sessionID) {
		t.Fatal("new session is playing, want no-play")
	}
	s.closeSession(sessionID)
	if _, ok := s.sessionAccount(sessionID); ok {
		t.Fatal("no-play session remains after select connection closes")
	}

	sessionID = s.newSessionID("account")
	s.setSessionPlaying(sessionID, true)
	if !s.sessionPlayingState(sessionID) {
		t.Fatal("selected session is no-play, want playing")
	}
	if account, ok := s.sessionAccount(sessionID); !ok || account != "account" {
		t.Fatalf("playing session lookup = %q, %t", account, ok)
	}
}

func TestPlain6ClassFromJobUsesReferenceIntegerFallback(t *testing.T) {
	tests := []struct {
		job  string
		want string
	}{
		{job: "0", want: "warrior"},
		{job: "1", want: "wizard"},
		{job: "2", want: "taoist"},
		{job: "wizard", want: "warrior"},
	}
	for _, tt := range tests {
		if got := plain6ClassFromJob(tt.job); got != tt.want {
			t.Fatalf("plain6ClassFromJob(%q) = %q, want %q", tt.job, got, tt.want)
		}
	}
}

func TestDecodeRunLoginLegacyPayload(t *testing.T) {
	legacyPayload := []byte{
		0x5e, 0x5e, 0x54, 0x71, 0x6d, 0x73, 0x54, 0x77, 0x5f, 0x6c,
		0x45, 0x6d, 0x72, 0x6f, 0x3f, 0x68, 0x61, 0x5f, 0x59, 0x68,
		0x63, 0x62, 0x3f, 0x66, 0x62, 0x60, 0x5a, 0x6b, 0x62, 0x60,
		0x50, 0x6b, 0x60, 0x63, 0x58, 0x6b, 0x60, 0x3f,
	}
	frame := mir176.WrapFrame(append([]byte{'1'}, legacyPayload...))
	if _, ok := decodeRunLogin(frame); ok {
		t.Fatal("decodeRunLogin() accepted legacy XOR payload")
	}
}

func TestDecodeCapturedPlain6RunLoginFrame(t *testing.T) {
	frame := []byte{0x23, 0x34, 0x46, 0x5e, 0x65, 0x70, 0x55, 0x53, 0x49, 0x70, 0x47, 0x72, 0x4d, 0x60, 0x55, 0x42, 0x4c, 0x6e, 0x47, 0x6f, 0x40, 0x6b, 0x48, 0x5f, 0x40, 0x6d, 0x49, 0x4f, 0x5c, 0x6d, 0x48, 0x4f, 0x58, 0x6b, 0x48, 0x3c, 0x21}
	got, ok := decodeRunLogin(frame)
	if !ok {
		t.Fatalf("decodeRunLogin() ok = false")
	}
	if got.Account != "test" || got.CharName != "dddd2" || got.SessionID != 1 || got.Version != 21158117 || got.Code != 0 {
		t.Fatalf("decodeRunLogin() = %#v", got)
	}
}

func TestAbilityLength(t *testing.T) {
	if got := len(Ability(world.AbilityStats{Level: 1, HP: 35, MaxHP: 35})); got != 40 {
		t.Fatalf("Ability length = %d, want 40", got)
	}
}

func TestAbilityEncodesFieldsAtReferenceOffsets(t *testing.T) {
	body := Ability(world.AbilityStats{
		Level: 3, AC: 0x01020304, MAC: 0x05060708, DC: 0x090a0b0c, MC: 0x0d0e0f10, SC: 0x11121314,
		HP: 30, MP: 20, MaxHP: 35, MaxMP: 25,
		Exp: 100, MaxExp: 200,
	})
	if got := body[0]; got != 3 {
		t.Fatalf("Level = %d, want 3", got)
	}
	checks := []struct {
		name   string
		offset int
		want   uint32
	}{
		{"AC", 2, 0xffff}, {"MAC", 4, 0xffff}, {"DC", 6, 0xffff}, {"MC", 8, 0xffff}, {"SC", 10, 0xffff},
	}
	for _, c := range checks {
		if got := uint32(binary.LittleEndian.Uint16(body[c.offset : c.offset+2])); got != c.want {
			t.Fatalf("%s at offset %d = %d, want %d", c.name, c.offset, got, c.want)
		}
	}
	if got := binary.LittleEndian.Uint16(body[12:14]); got != 30 {
		t.Fatalf("HP = %d, want 30", got)
	}
	if got := binary.LittleEndian.Uint16(body[14:16]); got != 20 {
		t.Fatalf("MP = %d, want 20", got)
	}
	if got := binary.LittleEndian.Uint16(body[16:18]); got != 35 {
		t.Fatalf("MaxHP = %d, want 35", got)
	}
	if got := binary.LittleEndian.Uint16(body[18:20]); got != 25 {
		t.Fatalf("MaxMP = %d, want 25", got)
	}
	if got := binary.LittleEndian.Uint32(body[24:28]); got != 100 {
		t.Fatalf("Exp = %d, want 100", got)
	}
	if got := binary.LittleEndian.Uint32(body[28:32]); got != 200 {
		t.Fatalf("MaxExp = %d, want 200", got)
	}
}

func TestModernAbilityEncodesReference50ByteLayout(t *testing.T) {
	body := ModernAbility(world.AbilityStats{
		Level: 3, AC: 0x01020304, MAC: 0x05060708, DC: 0x090a0b0c, MC: 0x0d0e0f10, SC: 0x11121314,
		HP: 30, MP: 20, MaxHP: 35, MaxMP: 25, Exp: 100, MaxExp: 200,
		Weight: 1, MaxWeight: 2, WearWeight: 3, MaxWearWeight: 4, HandWeight: 5, MaxHandWeight: 6,
	})
	if len(body) != 50 {
		t.Fatalf("ModernAbility length = %d, want 50", len(body))
	}
	if got := binary.LittleEndian.Uint32(body[2:6]); got != 0x01020304 {
		t.Fatalf("modern AC = %#x, want %#x", got, 0x01020304)
	}
	if got := binary.LittleEndian.Uint16(body[22:24]); got != 30 {
		t.Fatalf("modern HP = %d, want 30", got)
	}
	if got := binary.LittleEndian.Uint32(body[30:34]); got != 100 {
		t.Fatalf("modern Exp = %d, want 100", got)
	}
	if got := binary.LittleEndian.Uint16(body[48:50]); got != 6 {
		t.Fatalf("modern MaxHandWeight = %d, want 6", got)
	}
}

func TestMessageBodyWLLength(t *testing.T) {
	if got := len(MessageBodyWL(0, 0, 0, 0)); got != 16 {
		t.Fatalf("MessageBodyWL length = %d, want 16", got)
	}
}

func TestEncodeString(t *testing.T) {
	got, err := mir176.DecodePlain6Payload(EncodeString("0"))
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if string(got) != "0" {
		t.Fatalf("EncodeString decode = %q, want %q", got, "0")
	}
}

func TestEncodeBuffer(t *testing.T) {
	body := MessageBodyWL(1, 2, 3, 4)
	got, err := mir176.DecodePlain6Payload(EncodeBuffer(body))
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("EncodeBuffer decode = %v, want %v", got, body)
	}
}

func TestClientItemBodyUsesItemName(t *testing.T) {
	item := data.StdItem{Name: "Letter", StdMode: 5, Weight: 1}
	body := ClientItemBody(item, [14]byte{}, 7, 1234, 4321)
	if got := int(body[0]); got != len("Letter") {
		t.Fatalf("name length = %d, want %d", got, len("Letter"))
	}
	if got := string(body[1 : 1+int(body[0])]); got != "Letter" {
		t.Fatalf("name = %q, want %q", got, "Letter")
	}
}

func TestClientItemBodyAppendsDurabilityForReferenceStdMode50(t *testing.T) {
	item := data.StdItem{Name: "Timed item", StdMode: 50}
	body := ClientItemBody(item, [14]byte{}, 7, 12, 34)
	if got := string(body[1 : 1+int(body[0])]); got != "Timed item #12" {
		t.Fatalf("name = %q, want %q", got, "Timed item #12")
	}
}

func TestClientBagItemBodyMatchesReferenceModernStdMode50Branch(t *testing.T) {
	item := data.StdItem{Name: "Timed item", StdMode: 50}
	modern := ClientBagItemBody(item, [14]byte{}, 7, 12, 34)
	if got := string(modern[1 : 1+int(modern[0])]); got != "Timed item" {
		t.Fatalf("modern bag name = %q, want %q", got, "Timed item")
	}
	legacy := LegacyBagItemBody(item, 7, 12, 34)
	if got := string(legacy[1 : 1+int(legacy[0])]); got != "Timed item #12" {
		t.Fatalf("legacy bag name = %q, want %q", got, "Timed item #12")
	}
}

func TestEquippedItemBodiesMatchReferenceStdMode50Branches(t *testing.T) {
	item := data.StdItem{Name: "Timed item", StdMode: 50}
	modern := ClientEquippedItemBody(item, [14]byte{}, 7, 12, 34)
	if got := string(modern[1 : 1+int(modern[0])]); got != "Timed item" {
		t.Fatalf("modern equipped name = %q, want %q", got, "Timed item")
	}
	legacy := LegacyEquippedItemBody(item, 7, 12, 34)
	if got := string(legacy[1 : 1+int(legacy[0])]); got != "Timed item" {
		t.Fatalf("legacy equipped name = %q, want %q", got, "Timed item")
	}
}

func TestNonEquipmentItemListsOmitReferenceStdMode50Suffix(t *testing.T) {
	item := data.StdItem{Name: "Timed item", StdMode: 50}
	modern := ClientNoTimedDurabilityItemBody(item, [14]byte{}, 7, 12, 34)
	if got := string(modern[1 : 1+int(modern[0])]); got != "Timed item" {
		t.Fatalf("modern list name = %q, want %q", got, "Timed item")
	}
	legacy := LegacyNoTimedDurabilityItemBody(item, 7, 12, 34)
	if got := string(legacy[1 : 1+int(legacy[0])]); got != "Timed item" {
		t.Fatalf("legacy list name = %q, want %q", got, "Timed item")
	}
}

func TestClientItemBodyMatchesReferenceLayout(t *testing.T) {
	item := data.StdItem{
		Name:         "Sword",
		StdMode:      1,
		Shape:        2,
		Weight:       3,
		AniCount:     4,
		SpecialPwr:   5,
		ItemDesc:     6,
		NeedIdentify: 7,
		Looks:        7,
		DuraMax:      8,
		Stats: data.StdItemStats{
			AcMin:  1,
			AcMax:  2,
			MacMin: 3,
			MacMax: 4,
			DcMin:  5,
			DcMax:  6,
			McMin:  7,
			McMax:  8,
			ScMin:  9,
			ScMax:  10,
		},
		Need:      11,
		NeedLevel: 12,
		Price:     13,
	}
	body := ClientItemBody(item, [14]byte{}, 38, 39, 40)
	if got := len(body); got != 56 {
		t.Fatalf("ClientItemBody length = %d, want 56", got)
	}
	if got := binary.LittleEndian.Uint16(body[24:26]); got != 8 {
		t.Fatalf("base DuraMax = %d, want 8", got)
	}
	if got := binary.LittleEndian.Uint32(body[44:48]); got != 38 {
		t.Fatalf("MakeIndex = %d, want 38", got)
	}
	if got := binary.LittleEndian.Uint16(body[48:50]); got != 39 {
		t.Fatalf("Dura = %d, want 39", got)
	}
	if got := binary.LittleEndian.Uint16(body[50:52]); got != 40 {
		t.Fatalf("DuraMax = %d, want 40", got)
	}
	if got := binary.LittleEndian.Uint32(body[52:56]); got != 0 {
		t.Fatalf("UpgradeOpt = %d, want 0", got)
	}
}

func TestItemBodyUsesCurrentLayout(t *testing.T) {
	item := data.StdItem{Name: "Sword", StdMode: 1, DuraMax: 40}

	if got := len(itemBodyForAdd(storage.Character{}, item, [14]byte{}, 1, 2, 3)); got != 56 {
		t.Fatalf("add item body length = %d, want 56", got)
	}
	if got := len(itemBodyForBag(storage.Character{}, item, [14]byte{}, 1, 2, 3)); got != 56 {
		t.Fatalf("bag item body length = %d, want 56", got)
	}
	if got := len(itemBodyForEquipped(storage.Character{}, item, [14]byte{}, 1, 2, 3)); got != 56 {
		t.Fatalf("ticked equipped item body length = %d, want 56", got)
	}
}

func TestLegacyItemBodyUsesReferenceLayout(t *testing.T) {
	item := data.StdItem{Name: "Sword", StdMode: 1, DuraMax: 40}
	if got := len(LegacyClientItemBody(item, 1, 2, 3)); got != 50 {
		t.Fatalf("legacy item body length = %d, want 50", got)
	}
}

func TestResponseUsesPlain6Message(t *testing.T) {
	body := EncodeBuffer(MessageBodyWL(1, 2, 3, 4))
	frame := encodeMessage(mir176.Command{Ident: mir176.SMLogon, Recog: 1, Param: 330, Tag: 330}, body)
	cmd, got, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	if cmd.Ident != mir176.SMLogon || string(got) != string(body) {
		t.Fatalf("decoded game response = ident %d body %v, want ident %d body %v", cmd.Ident, got, mir176.SMLogon, body)
	}
}

func TestNewMapBodyDecodesOnceToMapID(t *testing.T) {
	body := EncodeString("0")
	frame := encodeMessage(mir176.Command{Ident: mir176.SMNewMap, Recog: 1, Param: 330, Tag: 330}, body)
	cmd, got, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	if cmd.Ident != mir176.SMNewMap {
		t.Fatalf("decoded ident = %d, want %d", cmd.Ident, mir176.SMNewMap)
	}
	mapID, err := mir176.DecodePlain6Payload(got)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if string(mapID) != "0" {
		t.Fatalf("decoded map id = %q, want %q", mapID, "0")
	}
}

func TestShowEventMessageMatchesReferenceLayout(t *testing.T) {
	eventID := int32(400001)
	eventX, eventY := 12, 34
	eventType, eventParam := 4, 7
	body := make([]byte, 4)
	binary.LittleEndian.PutUint16(body, uint16(eventParam))
	frame := encodeMessage(mir176.Command{
		Ident:  mir176.SMShowEvent,
		Recog:  eventID,
		Param:  uint16(eventType),
		Tag:    uint16(eventX),
		Series: uint16(eventY),
	}, EncodeBuffer(body))
	cmd, encodedBody, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	if cmd.Ident != mir176.SMShowEvent || cmd.Recog != eventID || cmd.Param != uint16(eventType) || cmd.Tag != uint16(eventX) || cmd.Series != uint16(eventY) {
		t.Fatalf("show event command = %+v, want id=%d type=%d x=%d y=%d", cmd, eventID, eventType, eventX, eventY)
	}
	decodedBody, err := mir176.DecodePlain6Payload(encodedBody)
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if len(decodedBody) != 4 || binary.LittleEndian.Uint16(decodedBody) != uint16(eventParam) {
		t.Fatalf("show event body = %v, want event param %d", decodedBody, eventParam)
	}
}

func TestNoticeBody(t *testing.T) {
	got, err := mir176.DecodePlain6Payload(NoticeBody())
	if err != nil {
		t.Fatalf("DecodePlain6Payload() error = %v", err)
	}
	if string(got) != "OpenMir2 \x1b" {
		t.Fatalf("NoticeBody decode = %q, want %q", got, "OpenMir2 \x1b")
	}
}

func TestDecodeLoginNoticeOK(t *testing.T) {
	frame := mir176.EncodePlain6ClientMessage(mir176.Command{Ident: mir176.CMLoginNoticeOK, Series: 1}, nil)
	cmd, _, err := mir176.DecodePlain6ClientMessage(frame)
	if err != nil {
		t.Fatalf("DecodePlain6ClientMessage() error = %v", err)
	}
	if cmd.Ident != mir176.CMLoginNoticeOK {
		t.Fatalf("decoded ident = %d, want %d", cmd.Ident, mir176.CMLoginNoticeOK)
	}
}

func TestDecodeTurnCommandPacksCoordinatesAndDirection(t *testing.T) {
	x, y, dir := 10, 20, 3
	recog := int32(uint32(x) | uint32(y)<<16)
	frame := mir176.EncodePlain6ClientMessage(mir176.Command{Ident: mir176.CMTurn, Recog: recog, Tag: uint16(dir)}, nil)
	cmd, _, err := mir176.DecodePlain6ClientMessage(frame)
	if err != nil {
		t.Fatalf("DecodePlain6ClientMessage() error = %v", err)
	}
	if cmd.Ident != mir176.CMTurn {
		t.Fatalf("decoded ident = %d, want %d", cmd.Ident, mir176.CMTurn)
	}
	gotX := int(uint32(cmd.Recog) & 0xFFFF)
	gotY := int(uint32(cmd.Recog) >> 16)
	if gotX != x || gotY != y || int(cmd.Tag) != dir {
		t.Fatalf("decoded turn = (%d,%d,%d), want (%d,%d,%d)", gotX, gotY, cmd.Tag, x, y, dir)
	}
}

func decodeMessageLikeClient(frame []byte) (mir176.Command, []byte, error) {
	encoded, err := mir176.UnwrapFrame(frame)
	if err != nil {
		return mir176.Command{}, nil, err
	}
	headLen := len(mir176.EncodePlain6Command(mir176.Command{}))
	if len(encoded) < headLen {
		return mir176.Command{}, nil, fmt.Errorf("message shorter than command header: %d < %d", len(encoded), headLen)
	}
	cmd, err := mir176.DecodePlain6Command(encoded[:headLen])
	if err != nil {
		return mir176.Command{}, nil, err
	}
	return cmd, encoded[headLen:], nil
}
