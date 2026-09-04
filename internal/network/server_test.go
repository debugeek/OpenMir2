package network

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
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

func TestCharacterStruckCommandUsesTargetActor(t *testing.T) {
	hit := world.CharacterHit{Character: storage.Character{ID: "target-7", HP: 80, MaxHP: 100}, Damage: 12}

	cmd := CharacterStruckCommand(hit)
	if cmd.Recog != world.CharacterActorID(hit.Character) {
		t.Fatalf("SM_STRUCK recog = %d, want target actor %d", cmd.Recog, world.CharacterActorID(hit.Character))
	}
}

func TestCharacterHitCommandUsesAttackerActor(t *testing.T) {
	ch := storage.Character{ID: "attacker-27", X: 12, Y: 34, Dir: 5}

	cmd := CharacterHitCommand(ch, mir176.CMFireHit)
	if cmd.Recog != world.CharacterActorID(ch) || cmd.Param != 12 || cmd.Tag != 34 || cmd.Series != 5 {
		t.Fatalf("hit command = %+v, want attacker actor %d and position/direction", cmd, world.CharacterActorID(ch))
	}
}

func TestCharacterStruckUsesCapturedAttackerActorAfterDisconnect(t *testing.T) {
	s := newTestServer(t)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	client := s.registerClient(serverConn, storage.Character{ID: "target", MapID: testMapID, X: 10, Y: 10})
	defer s.unregisterClient(serverConn)
	hit := world.CharacterHit{
		Character:     client.character(),
		AttackerID:    "attacker",
		AttackerActor: 123456,
		Damage:        1,
	}
	s.sendCharacterStruck([]*Client{client}, hit)
	_, body, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode struck frame error = %v", err)
	}
	assertMessageBodyWL(t, body, s.world.HumanFeatureForCharacter(hit.Character), s.world.CharacterStatus(hit.Character), 123456, 0)
}

func TestCharacterSpellStruckCommandUsesTargetActor(t *testing.T) {
	hit := world.CharacterHit{Character: storage.Character{ID: "target-7", HP: 80, MaxHP: 100}, Damage: 12}

	cmd := CharacterSpellStruckCommand(hit)
	if cmd.Recog != world.CharacterActorID(hit.Character) || cmd.Param != 80 || cmd.Tag != 100 || cmd.Series != 12 {
		t.Fatalf("magic SM_STRUCK command = %+v, want target=%d hp=80 maxhp=100 damage=12", cmd, world.CharacterActorID(hit.Character))
	}
}

func TestMagicStruckBodyUsesTargetStateAndAttackerID(t *testing.T) {
	s := newTestServer(t)
	caster := storage.Character{ID: "magic-caster", MapID: testMapID, X: 10, Y: 10}
	target := storage.Character{ID: "magic-target", MapID: testMapID, X: 10, Y: 11}
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()
	client := s.registerClient(server, target)
	defer s.unregisterClient(server)

	go s.sendCharacterSpellStruck([]*Client{client}, caster, world.CharacterHit{Character: target, Damage: 4, AttackerID: caster.ID, Magic: true})
	var cmd mir176.Command
	var body []byte
	var err error
	for i := 0; i < 12; i++ {
		frame := readFrame(t, clientConn)
		cmd, body, err = decodeMessageLikeClient(frame)
		if err == nil && cmd.Ident == mir176.SMStruck {
			break
		}
	}
	if err != nil {
		t.Fatalf("decode magic struck frame error = %v", err)
	}
	if cmd.Recog != world.CharacterActorID(target) {
		t.Fatalf("magic struck recog = %d, want target actor %d", cmd.Recog, world.CharacterActorID(target))
	}
	assertMessageBodyWL(t, body, s.world.HumanFeatureForCharacter(target), s.world.CharacterStatus(target), world.CharacterActorID(caster), 1)
}

func TestMonsterStruckAndDeathBodiesIncludeStatus(t *testing.T) {
	s := newTestServer(t)
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()
	client := s.registerClient(server, storage.Character{ID: "monster-status-observer", MapID: testMapID, X: 10, Y: 10})
	defer s.unregisterClient(server)
	result := world.AttackResult{
		MonsterID: "status-monster", MonsterRaceImg: 1, MonsterStatus: 0x00800000,
		MonsterHP: 8, MonsterMaxHP: 10, Damage: 2,
	}

	struckDone := make(chan struct{})
	go func() {
		s.broadcastMonsterStruck([]*Client{client}, result)
		close(struckDone)
	}()
	_, struckBody, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode monster struck frame error = %v", err)
	}
	assertMessageBodyWL(t, struckBody, world.MonsterFeature(world.Monster{RaceImg: 1}), result.MonsterStatus, ActorID, 0)
	<-struckDone

	deathDone := make(chan struct{})
	go func() {
		s.broadcastMonsterDeath([]*Client{client}, result)
		close(deathDone)
	}()
	_, deathBody, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode monster death frame error = %v", err)
	}
	assertCharDesc(t, deathBody, world.MonsterFeature(world.Monster{RaceImg: 1}), result.MonsterStatus)
	<-deathDone
}

func TestSecondaryMonsterHitUsesReferenceImpactDelay(t *testing.T) {
	s := newTestServer(t)
	s.SetHitImpactDelay(0)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	client := s.registerClient(serverConn, storage.Character{ID: "secondary-hit-observer", MapID: testMapID, X: 10, Y: 10})
	defer s.unregisterClient(serverConn)
	result := world.AttackResult{
		MonsterID: "secondary-monster", MonsterRaceImg: 1, MonsterHP: 80, MonsterMaxHP: 100,
		Damage: 20, ImpactDelay: 40 * time.Millisecond, Character: storage.Character{ID: "attacker", MapID: testMapID},
	}
	s.broadcastHitImpact([]*Client{client}, result)
	if _, ok := readFrameWithTimeout(t, clientConn, 10*time.Millisecond); ok {
		t.Fatal("secondary monster hit was sent before reference delay")
	}
	frame, ok := readFrameWithTimeout(t, clientConn, 100*time.Millisecond)
	if !ok {
		t.Fatal("secondary monster hit was not sent after reference delay")
	}
	cmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode delayed monster hit error = %v", err)
	}
	if cmd.Ident != mir176.SMStruck {
		t.Fatalf("delayed monster hit ident = %d, want SM_STRUCK", cmd.Ident)
	}
}

func TestMonsterHitWithImmediateImpactSendsImmediateAndDelayedStruck(t *testing.T) {
	s := newTestServer(t)
	s.SetHitImpactDelay(0)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	client := s.registerClient(serverConn, storage.Character{ID: "immediate-monster-hit-observer", MapID: testMapID, X: 10, Y: 10})
	defer s.unregisterClient(serverConn)
	result := world.AttackResult{
		MonsterID: "immediate-monster", MonsterRaceImg: 1, MonsterHP: 80, MonsterMaxHP: 100,
		Damage: 20, ImmediateImpact: true, ImpactDelay: 40 * time.Millisecond,
		Character: storage.Character{ID: "attacker", MapID: testMapID},
	}
	s.broadcastHitImpact([]*Client{client}, result)
	if _, ok := readFrameWithTimeout(t, clientConn, 10*time.Millisecond); !ok {
		t.Fatal("immediate monster hit was not sent immediately")
	}
	if _, ok := readFrameWithTimeout(t, clientConn, 100*time.Millisecond); !ok {
		t.Fatal("delayed monster hit was not sent after reference delay")
	}
}

func TestMagicMonsterZeroDamageOmitsImpactPackets(t *testing.T) {
	s := newTestServer(t)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	client := s.registerClient(serverConn, storage.Character{ID: "zero-magic-observer", MapID: testMapID, X: 10, Y: 10})
	defer s.unregisterClient(serverConn)

	s.broadcastHitImpact([]*Client{client}, world.AttackResult{
		MonsterID: "zero-magic-monster", MonsterHP: 100, MonsterMaxHP: 100,
		MonsterHealthChanged: true, Magic: true, Character: storage.Character{ID: "caster"},
	})
	if _, ok := readFrameWithTimeout(t, clientConn, 20*time.Millisecond); ok {
		t.Fatal("zero-damage magic impact emitted a packet")
	}
}

func TestSpellEventSecondaryMonsterHitUsesReferenceImpactDelay(t *testing.T) {
	s := newTestServer(t)
	s.SetHitImpactDelay(0)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	caster := storage.Character{ID: "secondary-event-caster", MapID: testMapID, X: 10, Y: 10}
	client := s.registerClient(serverConn, caster)
	defer s.unregisterClient(serverConn)
	result := world.AttackResult{
		MonsterID: "secondary-event-monster", MonsterRaceImg: 1, MonsterHP: 80, MonsterMaxHP: 100,
		Damage: 20, ImpactDelay: 40 * time.Millisecond, Character: caster,
		MonsterX: caster.X, MonsterY: caster.Y,
	}
	s.handleSpellEvent(serverConn, &caster, "半月弯刀", data.StdSkill{}, world.SpellEvent{Kind: world.SpellEventMonsterHit, MonsterHit: result})
	if _, ok := readFrameWithTimeout(t, clientConn, 10*time.Millisecond); ok {
		t.Fatal("spell event secondary monster hit was sent before reference delay")
	}
	frame, ok := readFrameWithTimeout(t, clientConn, 100*time.Millisecond)
	if !ok {
		t.Fatal("spell event secondary monster hit was not sent after reference delay")
	}
	cmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode delayed spell event monster hit error = %v", err)
	}
	if cmd.Ident != mir176.SMStruck {
		t.Fatalf("spell event delayed monster hit ident = %d, want SM_STRUCK", cmd.Ident)
	}
	_ = client
}

func TestSpellEventSecondaryCharacterHitUsesReferenceImpactDelay(t *testing.T) {
	s := newTestServer(t)
	s.SetHitImpactDelay(0)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	caster := storage.Character{ID: "secondary-character-caster", MapID: testMapID, X: 10, Y: 10, HP: 80, MaxHP: 100}
	s.registerClient(serverConn, caster)
	defer s.unregisterClient(serverConn)
	hit := world.CharacterHit{Character: caster, Damage: 20, ImpactDelay: 40 * time.Millisecond}
	s.handleSpellEvent(serverConn, &caster, "半月弯刀", data.StdSkill{}, world.SpellEvent{Kind: world.SpellEventCharacterHit, Character: caster, CharacterHit: hit})
	if _, ok := readFrameWithTimeout(t, clientConn, 10*time.Millisecond); ok {
		t.Fatal("spell event secondary character hit was sent before reference delay")
	}
	frame, ok := readFrameWithTimeout(t, clientConn, 100*time.Millisecond)
	if !ok {
		t.Fatal("spell event secondary character hit was not sent after reference delay")
	}
	cmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode delayed spell event character hit error = %v", err)
	}
	if cmd.Ident != mir176.SMStruck {
		t.Fatalf("spell event delayed character hit ident = %d, want SM_STRUCK", cmd.Ident)
	}
}

func TestMonsterOpenHealthUsesMonsterActor(t *testing.T) {
	s := newTestServer(t)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	ch := storage.Character{ID: "monster-health-observer", MapID: testMapID, X: 10, Y: 10}
	client := s.registerClient(serverConn, ch)
	defer s.unregisterClient(serverConn)
	mon := world.Monster{ID: "monster-health-7", MapID: testMapID, X: 10, Y: 10, HP: 42, MaxHP: 100}

	go client.sendOpenHealthMonster(s, mon)
	cmd, _, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode open health frame error = %v", err)
	}
	if cmd.Ident != mir176.SMOpenHealth || cmd.Recog != world.MonsterActorID(mon) || cmd.Param != 42 || cmd.Tag != 100 {
		t.Fatalf("SM_OPENHEALTH command = %+v, want monster actor and HP fields", cmd)
	}

	go client.sendCloseHealthMonster(s, mon)
	cmd, _, err = decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode close health frame error = %v", err)
	}
	if cmd.Ident != mir176.SMCloseHealth || cmd.Recog != world.MonsterActorID(mon) {
		t.Fatalf("SM_CLOSEHEALTH command = %+v, want monster actor", cmd)
	}
}

func TestMagicMonsterHitSendsHealthBeforeStruck(t *testing.T) {
	s := newTestServer(t)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	client := s.registerClient(serverConn, storage.Character{ID: "magic-health-observer", MapID: testMapID, X: 10, Y: 10})
	defer s.unregisterClient(serverConn)
	result := world.AttackResult{
		MonsterID:            "magic-health-monster",
		MonsterHP:            42,
		MonsterMaxHP:         100,
		MonsterMP:            7,
		MonsterMaxMP:         13,
		MonsterHealthChanged: true,
		Magic:                true,
		Damage:               8,
		Character:            storage.Character{ID: "caster", MapID: testMapID},
	}
	done := make(chan struct{})
	go func() {
		s.broadcastHitImpact([]*Client{client}, result)
		close(done)
	}()

	health, _, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode monster health frame error = %v", err)
	}
	if health.Ident != mir176.SMHealthSpellChanged || health.Recog != world.MonsterActorID(world.Monster{ID: result.MonsterID}) || health.Param != 42 || health.Tag != 7 || health.Series != 100 {
		t.Fatalf("monster health frame = %+v, want HP/MP/MaxHP 42/7/100", health)
	}
	struck, struckBody, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode monster struck frame error = %v", err)
	}
	if struck.Ident != mir176.SMStruck {
		t.Fatalf("monster struck ident = %d, want SM_STRUCK", struck.Ident)
	}
	assertMessageBodyWL(t, struckBody, world.MonsterFeature(world.Monster{RaceImg: result.MonsterRaceImg, MonsterWeapon: result.MonsterWeapon, Appr: result.MonsterAppr}), 0, world.CharacterActorID(result.Character), 1)
	normalStruck, normalBody, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode normal monster struck frame error = %v", err)
	}
	if normalStruck.Ident != mir176.SMStruck {
		t.Fatalf("normal monster struck ident = %d, want SM_STRUCK", normalStruck.Ident)
	}
	assertMessageBodyWL(t, normalBody, world.MonsterFeature(world.Monster{RaceImg: result.MonsterRaceImg, MonsterWeapon: result.MonsterWeapon, Appr: result.MonsterAppr}), 0, world.CharacterActorID(result.Character), 0)
	<-done
}

func TestCharacterDeathCommandUsesTargetActor(t *testing.T) {
	ch := storage.Character{ID: "dead-target-7", X: 12, Y: 13, Dir: 4}
	cmd := CharacterDeathCommand(ch)
	if cmd.Ident != mir176.SMNowDeath || cmd.Recog != world.CharacterActorID(ch) || cmd.Param != 4 || cmd.Tag != 12 || cmd.Series != 1 {
		t.Fatalf("SM_NOWDEATH command = %+v, want target actor, direction, x, and immediate marker", cmd)
	}
}

func TestMonsterDeathCommandUsesDirectionAndImmediateMarker(t *testing.T) {
	result := world.AttackResult{MonsterID: "dead-monster-7", MonsterX: 12, MonsterDir: 4}
	cmd := MonsterDeathCommand(result)
	if cmd.Ident != mir176.SMNowDeath || cmd.Recog != world.MonsterActorID(world.Monster{ID: result.MonsterID}) || cmd.Param != 4 || cmd.Tag != 12 || cmd.Series != 1 {
		t.Fatalf("monster SM_NOWDEATH command = %+v, want target actor, direction, x, and immediate marker", cmd)
	}
}

func TestMonsterTurnSendsFeatureRefreshAfterTurn(t *testing.T) {
	s := newTestServer(t)
	mon := world.Monster{ID: "turn-monster", MapID: testMapID, X: 10, Y: 10, Dir: 3, RaceImg: 1, Appr: 2}
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()
	ch := storage.Character{ID: "turn-observer", MapID: testMapID, X: 10, Y: 10}
	client := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		client.sendMonsterAction(s, world.MonsterAction{
			Kind: world.MonsterActionTurn, MonsterID: mon.ID, MapID: mon.MapID,
			X: mon.X, Y: mon.Y, Dir: mon.Dir, RaceImg: mon.RaceImg, Appr: mon.Appr,
		})
	}()
	turnCmd, _, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode monster turn frame error = %v", err)
	}
	if turnCmd.Ident != mir176.SMTurn {
		t.Fatalf("first frame ident = %d, want SM_TURN (%d)", turnCmd.Ident, mir176.SMTurn)
	}
	featureCmd, featureBody, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode monster feature frame error = %v", err)
	}
	if featureCmd.Ident != mir176.SMFeatureChanged || featureCmd.Recog != world.MonsterActorID(mon) {
		t.Fatalf("feature command = %+v, want SM_FEATURECHANGED for monster", featureCmd)
	}
	if len(featureBody) != 0 {
		t.Fatalf("feature body len = %d, want 0", len(featureBody))
	}
	<-done
}

func TestMonsterTurnCommandIncludesLight(t *testing.T) {
	mon := world.Monster{ID: "turn-monster-light", X: 10, Y: 11, Dir: 3}
	cmd := MonsterTurnCommand(mon, 7)
	if cmd.Series != makeWord(byte(mon.Dir), 7) {
		t.Fatalf("monster turn series = %d, want dir/light %d", cmd.Series, makeWord(byte(mon.Dir), 7))
	}
}

func TestMonsterWalkCommandIncludesLight(t *testing.T) {
	action := world.MonsterAction{MonsterID: "walk-monster-light", X: 10, Y: 11, Dir: 3}
	cmd := MonsterWalkCommand(action, 7)
	if cmd.Series != makeWord(byte(action.Dir), 7) {
		t.Fatalf("monster walk series = %d, want dir/light %d", cmd.Series, makeWord(byte(action.Dir), 7))
	}
}

func TestMonsterWalkBodyIncludesCharacterDescription(t *testing.T) {
	mon := world.Monster{ID: "walk-monster-body", Appr: 12, RaceImg: 3}
	body := MonsterWalkBody(mon)
	assertCharDesc(t, body, world.MonsterFeature(mon), 0)
}

func TestMonsterActionBodiesPreserveStatusSnapshot(t *testing.T) {
	mon := world.Monster{ID: "action-monster-status", Appr: 12, RaceImg: 3}
	status := int32(0x00800000)
	assertCharDesc(t, MonsterWalkBodyWithStatus(mon, status), world.MonsterFeature(mon), status)
	turnBody, err := mir176.DecodePlain6Payload(MonsterTurnBodyWithStatus(mon, status))
	if err != nil {
		t.Fatalf("decode monster turn body error = %v", err)
	}
	if len(turnBody) < 8 || int32(binary.LittleEndian.Uint32(turnBody[4:8])) != status {
		t.Fatalf("monster turn status = %d, want %d", binary.LittleEndian.Uint32(turnBody[4:8]), status)
	}
	decoded, err := mir176.DecodePlain6Payload(MonsterDigUpBodyWithStatus(mon, status))
	if err != nil {
		t.Fatalf("decode monster dig-up body error = %v", err)
	}
	if len(decoded) != 16 || int32(binary.LittleEndian.Uint32(decoded[4:8])) != status {
		t.Fatalf("monster dig-up status = %d, want %d", binary.LittleEndian.Uint32(decoded[4:8]), status)
	}
}

func TestMonsterDigUpCommandIncludesLight(t *testing.T) {
	mon := world.Monster{ID: "dig-monster-light", X: 10, Y: 11, Dir: 3}
	cmd := MonsterDigUpCommand(mon, 7)
	if cmd.Series != makeWord(byte(mon.Dir), 7) {
		t.Fatalf("monster dig up series = %d, want dir/light %d", cmd.Series, makeWord(byte(mon.Dir), 7))
	}
}

func TestSpellRushIncludesCharacterDescription(t *testing.T) {
	s := newTestServer(t)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ch := storage.Character{ID: "rush-caster", MapID: testMapID, X: 10, Y: 10, Dir: 3}
	rush := world.SpellRush{Character: ch, X: 11, Y: 10, Dir: 2}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.sendSpellRush(server, rush)
	}()
	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode rush frame error = %v", err)
	}
	if cmd.Ident != mir176.SMRush || cmd.Recog != world.CharacterActorID(ch) || cmd.Param != 11 || cmd.Tag != 10 || byte(cmd.Series) != 2 {
		t.Fatalf("rush command = %+v", cmd)
	}
	assertCharDesc(t, body, s.world.HumanFeatureForCharacter(ch), s.world.CharacterStatus(ch))
	<-done
}

func TestSpellRushKungSendsFailureMessageAfterMovement(t *testing.T) {
	s := newTestServer(t)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ch := storage.Character{ID: "rush-caster-kung", MapID: testMapID, X: 10, Y: 10, Dir: 3}
	rush := world.SpellRush{Character: ch, X: 10, Y: 10, Dir: 2, Kung: true}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpellEvent(server, &ch, "野蛮冲撞", data.StdSkill{}, world.SpellEvent{
			Kind: world.SpellEventRush,
			Rush: rush,
		})
	}()
	first, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode rush failure frame error = %v", err)
	}
	if first.Ident != mir176.SMRushKung || first.Recog != world.CharacterActorID(ch) {
		t.Fatalf("rush failure movement command = %+v", first)
	}
	second, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode rush failure message error = %v", err)
	}
	if second.Ident != mir176.SMSystemMessage || second.Recog != world.CharacterActorID(ch) || second.Param != makeWord(0xFF, 0x38) || second.Series != 1 {
		t.Fatalf("rush failure system command = %+v", second)
	}
	decoded, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("decode rush failure message body error = %v", err)
	}
	if got := DecodeString(decoded); got != "冲撞力不够..." {
		t.Fatalf("rush failure message = %q, want %q", got, "冲撞力不够...")
	}
	<-done
}

func TestSpellHealingGaugeUsesTargetCharacterState(t *testing.T) {
	s := newTestServer(t)
	casterServer, casterClient := net.Pipe()
	targetServer, targetClient := net.Pipe()
	defer casterServer.Close()
	defer casterClient.Close()
	defer targetServer.Close()
	defer targetClient.Close()
	caster := storage.Character{ID: "caster-3", HP: 1, MaxHP: 2, MapID: testMapID}
	target := storage.Character{ID: "target-2", HP: 80, MaxHP: 100, MapID: testMapID}
	s.registerClient(casterServer, caster)
	s.registerClient(targetServer, target)
	defer s.unregisterClient(casterServer)
	defer s.unregisterClient(targetServer)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpellEvent(casterServer, &caster, "治愈术", data.StdSkill{}, world.SpellEvent{
			Kind:      world.SpellEventHealingGauge,
			Caster:    caster,
			Character: target,
		})
	}()
	cmd, _, err := decodeMessageLikeClient(readFrame(t, casterClient))
	if err != nil {
		t.Fatalf("decode healing gauge frame error = %v", err)
	}
	if cmd.Ident != mir176.SMInstanceHealGauge || cmd.Recog != world.CharacterActorID(target) || cmd.Param != uint16(target.HP) || cmd.Tag != uint16(target.MaxHP) {
		t.Fatalf("healing gauge command = %+v, want target actor=%d hp=%d/%d", cmd, world.CharacterActorID(target), target.HP, target.MaxHP)
	}
	if _, ok := readFrameWithTimeout(t, targetClient, 50*time.Millisecond); ok {
		t.Fatal("target received its own healing gauge")
	}
	<-done
}

func TestSpellHealingGaugeUsesTargetMonsterState(t *testing.T) {
	s := newTestServer(t)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	caster := storage.Character{ID: "caster-monster-gauge", HP: 1, MaxHP: 2, MapID: testMapID}
	target := world.Monster{ID: "target-monster-gauge", MapID: testMapID, HP: 80, MaxHP: 100, Alive: true}
	s.registerClient(server, caster)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpellEvent(server, &caster, "治愈术", data.StdSkill{}, world.SpellEvent{
			Kind:    world.SpellEventHealingGauge,
			Caster:  caster,
			Monster: target,
		})
	}()
	cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode monster healing gauge frame error = %v", err)
	}
	if cmd.Ident != mir176.SMInstanceHealGauge || cmd.Recog != world.MonsterActorID(target) || cmd.Param != uint16(target.HP) || cmd.Tag != uint16(target.MaxHP) {
		t.Fatalf("monster healing gauge command = %+v, want target actor=%d hp=%d/%d", cmd, world.MonsterActorID(target), target.HP, target.MaxHP)
	}
	<-done
}

func TestSpellMessageConversionDoesNotWaitForSocketWrite(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	client := &Client{
		conn:              serverConn,
		ch:                storage.Character{ID: "observer"},
		spellMessages:     make(chan spellObjectMessage, 1),
		spellMessagesDone: make(chan struct{}),
		output:            make(chan []byte, 1),
	}
	s := &Server{}
	go client.runSpellMessages(s)
	go client.runOutput()

	done := make(chan struct{})
	go func() {
		client.enqueueSpellMessage(s, spellObjectMessage{kind: spellObjectMessageStart, start: spellStartEvent{
			caster:  storage.Character{ID: "caster"},
			magicID: 1,
		}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		close(client.spellMessagesDone)
		t.Fatal("spell message conversion waited for socket write")
	}
	close(client.spellMessagesDone)
}

func TestSpellMessageQueueAcceptsBurstWithoutBlocking(t *testing.T) {
	client := &Client{
		spellMessagesDone: make(chan struct{}),
	}
	client.spellQueueCond = sync.NewCond(&client.mu)

	for i := 0; i < 64; i++ {
		client.enqueueSpellMessage(&Server{}, spellObjectMessage{kind: spellObjectMessageStart, start: spellStartEvent{magicID: uint16(i)}})
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.spellQueue) != 64 {
		t.Fatalf("queued spell messages = %d, want 64", len(client.spellQueue))
	}
	for i, message := range client.spellQueue {
		if message.start.magicID != uint16(i) {
			t.Fatalf("queued spell message %d = %d, want %d", i, message.start.magicID, i)
		}
	}
}

func TestSendCommandDoesNotWriteAfterClientUnregister(t *testing.T) {
	s := newTestServer(t)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, storage.Character{ID: "closed-client", MapID: testMapID})
	s.unregisterClient(server)

	s.sendCommand(server, mir176.Command{Ident: mir176.SMAddItem}, nil)
	s.sendRawFrame(server, "+FAIL")
	if _, ok := readFrameWithTimeout(t, client, 50*time.Millisecond); ok {
		t.Fatal("unregistered client received a command")
	}
}

func TestPoisonSystemMessageUsesReferenceRedStyle(t *testing.T) {
	s := newTestServer(t)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ch := storage.Character{ID: "poison-target", MapID: testMapID}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.sendSystemMessage(server, ch, "你中毒了[时间:5秒，点数:2点].")
	}()
	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode poison message error = %v", err)
	}
	if cmd.Ident != mir176.SMSystemMessage || cmd.Recog != world.CharacterActorID(ch) || cmd.Param != makeWord(0xFF, 0x38) || cmd.Series != 1 {
		t.Fatalf("poison system command = %+v, want red system message", cmd)
	}
	decoded, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("decode poison system body payload error = %v", err)
	}
	if got := DecodeString(decoded); got != "你中毒了[时间:5秒，点数:2点]." {
		t.Fatalf("poison system body = %q", got)
	}
	<-done
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

func TestSpellRefClientsUsesReferenceVisibilityCacheBounds(t *testing.T) {
	s := newTestServer(t)
	casterConn, casterPeer := net.Pipe()
	defer casterConn.Close()
	defer casterPeer.Close()
	observerConn, observerPeer := net.Pipe()
	defer observerConn.Close()
	defer observerPeer.Close()

	caster := storage.Character{ID: "spell-cache-caster", MapID: testMapID, X: 10, Y: 10}
	s.clientMu.Lock()
	s.clients[casterConn] = &Client{conn: casterConn, ch: caster}
	s.clients[observerConn] = &Client{conn: observerConn, ch: storage.Character{ID: "spell-cache-observer", MapID: testMapID, X: 20, Y: 10}}
	s.clientMu.Unlock()

	first := s.spellRefClients(caster)
	if !containsClientID(first, "spell-cache-observer") {
		t.Fatalf("first spell visibility = %v, want observer at 10 cells", clientIDs(first))
	}

	s.clientMu.Lock()
	s.clients[observerConn].ch.X = 21
	s.clientMu.Unlock()
	second := s.spellRefClients(caster)
	if containsClientID(second, "spell-cache-observer") {
		t.Fatalf("cached spell visibility = %v, want observer excluded at 11 cells", clientIDs(second))
	}
}

func TestSpellRefClientsUsesReconnectedObserverConnection(t *testing.T) {
	s := newTestServer(t)
	casterConn, casterPeer := net.Pipe()
	defer casterConn.Close()
	defer casterPeer.Close()
	oldObserverConn, oldObserverPeer := net.Pipe()
	defer oldObserverConn.Close()
	defer oldObserverPeer.Close()
	newObserverConn, newObserverPeer := net.Pipe()
	defer newObserverConn.Close()
	defer newObserverPeer.Close()

	caster := storage.Character{ID: "reconnect-cache-caster", MapID: testMapID, X: 10, Y: 10}
	observer := storage.Character{ID: "reconnect-cache-observer", MapID: testMapID, X: 15, Y: 10}
	s.clientMu.Lock()
	s.clients[casterConn] = &Client{conn: casterConn, ch: caster}
	s.clients[oldObserverConn] = &Client{conn: oldObserverConn, ch: observer}
	s.clientMu.Unlock()
	if !containsClientID(s.spellRefClients(caster), observer.ID) {
		t.Fatal("initial spell visibility does not include observer")
	}

	s.clientMu.Lock()
	delete(s.clients, oldObserverConn)
	s.clients[newObserverConn] = &Client{conn: newObserverConn, ch: observer}
	s.clientMu.Unlock()

	clients := s.spellRefClients(caster)
	if len(clients) != 2 {
		t.Fatalf("reconnected spell visibility = %d clients, want caster and observer", len(clients))
	}
	for _, client := range clients {
		if client.ch.ID == observer.ID && client.conn != newObserverConn {
			t.Fatal("spell visibility returned stale observer connection")
		}
	}
}

func TestCharacterHitClientsReuseReferenceVisibilityCache(t *testing.T) {
	s := newTestServer(t)
	targetConn, targetPeer := net.Pipe()
	defer targetConn.Close()
	defer targetPeer.Close()
	observerConn, observerPeer := net.Pipe()
	defer observerConn.Close()
	defer observerPeer.Close()

	target := storage.Character{ID: "hit-cache-target", MapID: testMapID, X: 10, Y: 10}
	s.clientMu.Lock()
	s.clients[targetConn] = &Client{conn: targetConn, ch: target}
	s.clients[observerConn] = &Client{conn: observerConn, ch: storage.Character{ID: "hit-cache-observer", MapID: testMapID, X: 20, Y: 10}}
	s.clientMu.Unlock()

	if !containsClientID(s.clientsForCharacterHit(target), "hit-cache-observer") {
		t.Fatal("initial character hit visibility does not include observer")
	}
	s.clientMu.Lock()
	s.clients[observerConn].ch.X = 21
	s.clientMu.Unlock()
	if containsClientID(s.clientsForCharacterHit(target), "hit-cache-observer") {
		t.Fatal("cached character hit visibility includes observer at 11 cells")
	}
}

func TestMonsterDeathClientsReuseReferenceVisibilityCache(t *testing.T) {
	s := newTestServer(t)
	monster := world.AttackResult{MonsterID: "death-cache-monster", MonsterMapID: testMapID, MonsterX: 10, MonsterY: 10}
	monsterConn, monsterPeer := net.Pipe()
	defer monsterConn.Close()
	defer monsterPeer.Close()
	observerConn, observerPeer := net.Pipe()
	defer observerConn.Close()
	defer observerPeer.Close()

	s.clientMu.Lock()
	s.clients[monsterConn] = &Client{conn: monsterConn, ch: storage.Character{ID: "death-cache-owner", MapID: testMapID, X: 10, Y: 10}}
	s.clients[observerConn] = &Client{conn: observerConn, ch: storage.Character{ID: "death-cache-observer", MapID: testMapID, X: 20, Y: 10}}
	s.clientMu.Unlock()

	if !containsClientID(s.monsterDeathClients(monster), "death-cache-observer") {
		t.Fatal("initial monster death visibility does not include observer")
	}
	s.clientMu.Lock()
	s.clients[observerConn].ch.X = 21
	s.clientMu.Unlock()
	if containsClientID(s.monsterDeathClients(monster), "death-cache-observer") {
		t.Fatal("cached monster death visibility includes observer at 11 cells")
	}
}

func TestForgetMissingDeadMonsterSendsReferenceDeathRefresh(t *testing.T) {
	s := newTestServer(t)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	client := &Client{
		conn: serverConn,
		ch:   storage.Character{ID: "dead-refresh-observer", MapID: testMapID, X: 10, Y: 10},
		visibleMonsters: map[string]world.Monster{
			"dead-refresh-monster": {ID: "dead-refresh-monster", MapID: testMapID, X: 11, Y: 12, Dir: 3, RaceImg: 1, Alive: false},
		},
	}
	done := make(chan struct{})
	go func() {
		client.forgetMissingMonsters(s, map[string]struct{}{})
		close(done)
	}()
	cmd, body, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode dead monster refresh error = %v", err)
	}
	if cmd.Ident != mir176.SMDeath || cmd.Recog != world.MonsterActorID(world.Monster{ID: "dead-refresh-monster"}) || cmd.Param != 3 || cmd.Tag != 11 || cmd.Series != 0 {
		t.Fatalf("dead monster refresh command = %+v, want SM_DEATH with direction and coordinates", cmd)
	}
	assertCharDesc(t, body, world.MonsterFeature(world.Monster{RaceImg: 1}), world.MonsterStatus(world.Monster{RaceImg: 1}, time.Now()))
	<-done
}

func TestSpellRefClientsRebuildsAfterOwnerMapChange(t *testing.T) {
	s := newTestServer(t)
	casterConn, casterPeer := net.Pipe()
	defer casterConn.Close()
	defer casterPeer.Close()
	observerConn, observerPeer := net.Pipe()
	defer observerConn.Close()
	defer observerPeer.Close()

	s.clientMu.Lock()
	s.clients[casterConn] = &Client{conn: casterConn, ch: storage.Character{ID: "spell-map-caster", MapID: testMapID, X: 10, Y: 10}}
	s.clients[observerConn] = &Client{conn: observerConn, ch: storage.Character{ID: "spell-map-observer", MapID: testMapID, X: 15, Y: 10}}
	s.clientMu.Unlock()

	oldPosition := storage.Character{ID: "spell-map-caster", MapID: testMapID, X: 10, Y: 10}
	if !containsClientID(s.spellRefClients(oldPosition), "spell-map-observer") {
		t.Fatal("initial spell visibility does not include observer")
	}

	s.clientMu.Lock()
	s.clients[observerConn].ch.X = 45
	s.clientMu.Unlock()
	s.invalidateSpellRef(oldPosition.ID)
	newPosition := storage.Character{ID: oldPosition.ID, MapID: testMapID, X: 40, Y: 10}
	s.clientMu.Lock()
	s.clients[casterConn].ch = newPosition
	s.clientMu.Unlock()
	if !containsClientID(s.spellRefClients(newPosition), "spell-map-observer") {
		t.Fatal("rebuilt spell visibility does not include observer at new position")
	}
}

func containsClientID(clients []*Client, id string) bool {
	for _, client := range clients {
		if client.ch.ID == id {
			return true
		}
	}
	return false
}

func clientIDs(clients []*Client) []string {
	ids := make([]string, 0, len(clients))
	for _, client := range clients {
		ids = append(ids, client.ch.ID)
	}
	return ids
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

	recog := int32(uint32(ch.X) | uint32(ch.Y)<<16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleHit(server, &ch, mir176.Command{Ident: mir176.CMHit, Recog: recog, Tag: uint16(dir)})
	}()

	struckCmd, struckBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode monster struck frame error = %v", err)
	}
	if struckCmd.Ident != mir176.SMStruck || struckCmd.Recog != world.MonsterActorID(mon) || struckCmd.Param != uint16(mon.HP-4) || struckCmd.Tag != uint16(mon.MaxHP) || struckCmd.Series != 4 {
		t.Fatalf("monster struck = %+v, want recog=%d hp=%d/%d damage=4", struckCmd, world.MonsterActorID(mon), mon.HP-4, mon.MaxHP)
	}
	assertMessageBodyWL(t, struckBody, world.MonsterFeature(mon), 0, world.CharacterActorID(ch), 0)
	assertActionAck(t, readFrame(t, client))
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

func TestDelayedAttackSkillExpUsesReconnectedCharacterClient(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	oldServer, oldClient := net.Pipe()
	s.registerClient(oldServer, ch)
	adapter := attackSyncAdapter{s: s, conn: oldServer, characterID: ch.ID}
	adapter.SendSkillExp(7, 2, 13, 10*time.Millisecond)
	s.unregisterClient(oldServer)
	oldServer.Close()
	oldClient.Close()

	newServer, newClient := net.Pipe()
	defer newServer.Close()
	defer newClient.Close()
	s.registerClient(newServer, ch)
	frame, ok := readFrameWithTimeout(t, newClient, time.Second)
	if !ok {
		t.Fatal("timed out waiting for delayed skill experience")
	}
	cmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode delayed skill experience error = %v", err)
	}
	if cmd.Ident != mir176.SMMagicLvExp || cmd.Recog != 7 || cmd.Param != 2 || cmd.Tag != 13 || cmd.Series != 0 {
		t.Fatalf("delayed skill experience command = %+v, want magic=7 level=2 train=13", cmd)
	}
}

func TestSkillLevelUpReplacesPendingSkillExperience(t *testing.T) {
	s := newTestServer(t)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	ch := storage.Character{ID: "skill-exp-replace", MapID: testMapID}
	s.registerClient(serverConn, ch)
	s.scheduleSkillExp(ch.ID, 7, 1, 10, 40*time.Millisecond, false)
	s.scheduleSkillExp(ch.ID, 7, 2, 3, 80*time.Millisecond, true)
	frame, ok := readFrameWithTimeout(t, clientConn, time.Second)
	if !ok {
		t.Fatal("timed out waiting for replacement skill experience")
	}
	cmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode skill experience error = %v", err)
	}
	if cmd.Ident != mir176.SMMagicLvExp || cmd.Recog != 7 || cmd.Param != 2 || cmd.Tag != 3 || cmd.Series != 0 {
		t.Fatalf("skill experience command = %+v, want latest level/train values", cmd)
	}
	if extra, ok := readFrameWithTimeout(t, clientConn, 120*time.Millisecond); ok {
		t.Fatalf("received stale replacement frame: %v", extra)
	}
}

func TestSpellPushUsesReferenceBackstepFields(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	target, err := s.world.CreateCharacterWithAppearance("test", "target", "warrior", 0, 0, mapID, x+2, y+1)
	if err != nil {
		t.Fatalf("CreateCharacter() target error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	push := world.CharacterPush{Character: target, Dir: 6}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpellEvent(server, &caster, "抗拒火环", data.StdSkill{}, world.SpellEvent{Kind: world.SpellEventCharacterPush, CharacterPush: push})
	}()
	frame := readFrame(t, client)
	cmd, body, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode backstep error = %v", err)
	}
	if cmd.Ident != mir176.SMBackStep || cmd.Recog != world.CharacterActorID(target) || cmd.Param != uint16(target.X) || cmd.Tag != uint16(target.Y) || cmd.Series != uint16(makeWord(byte(push.Dir), byte(s.world.MapLight(target.MapID)))) {
		t.Fatalf("backstep command = %+v, want target=%d position=(%d,%d) dir=%d", cmd, world.CharacterActorID(target), target.X, target.Y, push.Dir)
	}
	assertCharDesc(t, body, s.world.HumanFeatureForCharacter(target), s.world.CharacterStatus(target))
	<-done
}

func TestSpellPushBroadcastsOnceToNearbyObserver(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "warrior", 0, 0, mapID, x+3, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	target := observer
	target.ID = "target"
	target.X = x + 1
	target.Y = y
	casterServer, casterClient := net.Pipe()
	observerServer, observerClient := net.Pipe()
	defer casterServer.Close()
	defer casterClient.Close()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(casterServer, caster)
	defer s.unregisterClient(casterServer)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpellEvent(casterServer, &caster, "抗拒火环", data.StdSkill{}, world.SpellEvent{Kind: world.SpellEventCharacterPush, CharacterPush: world.CharacterPush{Character: target, Dir: 2}})
	}()
	if _, ok := readFrameWithTimeout(t, casterClient, time.Second); !ok {
		t.Fatal("timed out waiting for caster push")
	}
	if _, ok := readFrameWithTimeout(t, observerClient, time.Second); !ok {
		t.Fatal("timed out waiting for observer push")
	}
	if _, ok := readFrameWithTimeout(t, observerClient, 100*time.Millisecond); ok {
		t.Fatal("observer received duplicate push")
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

		if i == 2 {
			var sawStruck, sawAck bool
			for frameNo := 0; frameNo < 2; frameNo++ {
				frame := readFrame(t, client)
				if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
					sawAck = true
					continue
				}
				cmd, _, err := decodeMessageLikeClient(frame)
				if err != nil {
					t.Fatalf("decode frame %d error: %v", frameNo, err)
				}
				switch cmd.Ident {
				case mir176.SMStruck:
					sawStruck = true
					if cmd.Recog != world.MonsterActorID(mon) || cmd.Param != 0 {
						t.Fatalf("struck command = %+v, want dead monster", cmd)
					}
				}
			}
			if !sawStruck || !sawAck {
				t.Fatalf("deferred death frames seen struck=%v ack=%v", sawStruck, sawAck)
			}
		} else {
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
			assertActionAck(t, readFrame(t, client))
		}
		<-done
		if i == 2 {
			tick, err := s.world.Tick(s.PlayerSnapshots(), time.Now())
			if err != nil {
				t.Fatalf("Tick() error = %v", err)
			}
			s.applyWorldTick(tick, time.Now())
			var sawWinExp, sawDeath, sawDrop bool
			for frameNo := 0; frameNo < 3; frameNo++ {
				frame := readFrame(t, client)
				if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
					continue
				}
				cmd, body, err := decodeMessageLikeClient(frame)
				if err != nil {
					t.Fatalf("deferred death frame %d error: %v", frameNo, err)
				}
				switch cmd.Ident {
				case mir176.SMWinExp:
					sawWinExp = true
				case mir176.SMNowDeath:
					sawDeath = true
					assertCharDesc(t, body, world.MonsterFeature(mon), 0)
				case mir176.SMItemShow:
					sawDrop = true
				}
			}
			if !sawWinExp || !sawDeath || !sawDrop {
				t.Fatalf("deferred death frames seen winExp=%v death=%v drop=%v", sawWinExp, sawDeath, sawDrop)
			}
		}
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
	initialFrames := make([][]byte, 0, 2)
	for i := 0; i < 2; i++ {
		frame := readFrame(t, client)
		initialFrames = append(initialFrames, frame)
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			ackSeen = true
		}
	}
	if !ackSeen {
		t.Fatal("missing caster action ack")
	}
	tick, err := s.world.Tick(s.PlayerSnapshots(), time.Now())
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	s.applyWorldTick(tick, time.Now())
	var sawStruck, sawWinExp, sawDeath, sawDrop bool
	var showCmd mir176.Command
	var showBody []byte
	for _, frame := range initialFrames {
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			continue
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
	ended, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode fire expiry message error = %v", err)
	}
	if ended.Ident != mir176.SMSystemMessage {
		t.Fatalf("fire expiry message ident = %d, want SM_SYSTEMMESSAGE", ended.Ident)
	}
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

func TestHandleSpellRejectsBlockedCharacter(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.SpellBlocked = true
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell})
	}()
	assertActionFail(t, readFrame(t, client))
	<-done
}

func TestHandleSpellKeepsStartedFailureStateInSession(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "started-failure", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Skills = storage.SkillStates{{ID: "神圣战甲术", Level: 0}}
	skill, ok := s.world.Skill("神圣战甲术")
	if !ok {
		t.Fatal("skill 神圣战甲术 missing from config")
	}
	ch.MP = s.world.SpellCost(skill, ch.Skills[0]) + 10
	initialMP := ch.MP
	magicID, ok := s.world.MagicIDByName("神圣战甲术")
	if !ok {
		t.Fatal("MagicIDByName() missing 神圣战甲术")
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{
			Ident: mir176.CMSpell,
			Recog: int32(uint32(x) | uint32(y)<<16),
			Tag:   magicID,
		})
	}()
	frames := collectFramesUntilActionAck(t, client, 8)
	<-done
	if ch.MP >= initialMP {
		t.Fatalf("session MP = %d, want consumed resource from %d", ch.MP, initialMP)
	}
	for _, frame := range frames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err == nil && cmd.Ident == mir176.SMMagicFire {
			t.Fatalf("started failure emitted SM_MAGICFIRE: %+v", cmd)
		}
	}
}

func TestHandleSpellQueuesMultipleDelayedRequestsLikeReference(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "queued-spells", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Skills = storage.SkillStates{{ID: "火球术", Level: 0}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)
	spellClient := s.clientForConn(server)
	spellClient.mu.Lock()
	spellClient.spellActionAt = time.Now()
	spellClient.spellActionInterval = time.Second
	spellClient.mu.Unlock()

	request := spellRequest{x: x + 1, y: y, magicID: 1}
	s.processSpellDelivery(server, &ch, request, false)
	s.processSpellDelivery(server, &ch, request, false)

	spellClient.mu.Lock()
	pending := spellClient.pendingSpellMessages
	spellClient.mu.Unlock()
	if pending != 2 {
		t.Fatalf("pending delayed spells = %d, want 2", pending)
	}
}

func TestHandleSpellConsumesManaBeforeOutOfRangeFailure(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Skills = storage.SkillStates{{ID: "火球术", Level: 0}}
	ch.MP = 100
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{
			Ident: mir176.CMSpell,
			Recog: int32(uint32(x+9) | uint32(y)<<16),
			Tag:   1,
		})
	}()
	frames := collectFramesUntilActionAck(t, client, 4)
	var sawFail, sawHealth, sawStart bool
	for _, frame := range frames {
		if strings.HasPrefix(string(frame), "+FAIL/") || isActionFailFrame(frame) {
			sawFail = true
			continue
		}
		if strings.HasPrefix(string(frame), "+GOOD/") || isActionAckFrame(frame) {
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode out-of-range frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMHealthSpellChanged:
			sawHealth = true
		case mir176.SMMagicFireFail:
			sawFail = true
		case mir176.SMSpell:
			sawStart = true
		}
	}
	<-done
	if !sawFail || !sawHealth || sawStart {
		t.Fatalf("out-of-range frames = %+v, want fail+health without spell start", frames)
	}
	if ch.MP >= 100 {
		t.Fatalf("caster MP = %d, want consumed resource", ch.MP)
	}
}

func TestHandleSpellUpdatesCasterDirectionBeforeWorldSpell(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Skills = storage.SkillStates{{ID: "火球术", Level: 0}}
	ch.MP = 100
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)

	targetX, targetY := x+1, y
	wantDir := world.Direction(x, y, targetX, targetY)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{
			Ident: mir176.CMSpell,
			Recog: int32(uint32(targetX) | uint32(targetY)<<16),
			Tag:   1,
		})
	}()
	for i := 0; i < 8; i++ {
		if body, err := mir176.UnwrapFrame(readFrame(t, client)); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			break
		}
		if i == 7 {
			t.Fatal("missing caster action ack")
		}
	}
	<-done
	if ch.Dir != wantDir {
		t.Fatalf("caster direction = %d, want %d", ch.Dir, wantDir)
	}
}

func TestHandleSpellAllowsParalyzedCharacterWhenConfigured(t *testing.T) {
	bundle, _, err := data.LoadConfigsWithReport(testConfigsDir)
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	gameplay := config.DefaultGameplay()
	gameplay.Combat.ParalyCanSpell = true
	s := newTestServerWithBundle(t, bundle, gameplay)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	ch.MP = 100
	ch.ParalyzedUntil = time.Now().Add(time.Minute).UnixNano()
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x)&0xFFFF | uint32(y)<<16), Tag: 1})
	}()
	frame := readFrame(t, client)
	if strings.HasPrefix(string(frame), "+FAIL") {
		t.Fatalf("paralyzed spell was rejected: %q", frame)
	}
	<-done
}

func TestHandleSpellKeepsParalyzedStateUntilTickClearsIt(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Skills = storage.SkillStates{{ID: "火球术", Level: 0}}
	ch.MP = 100
	ch.ParalyzedUntil = time.Now().Add(-time.Minute).UnixNano()
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x)&0xFFFF | uint32(y)<<16), Tag: 1})
	}()
	assertActionFail(t, readFrame(t, client))
	<-done
}

func TestHandleSpellFireHitArmsState(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Level = 100
	ch.Skills = storage.SkillStates{{ID: "烈火剑法", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("烈火剑法")
	if !ok {
		t.Fatal("Skill() missing 烈火剑法")
	}
	cost := s.world.SpellCost(skill, ch.Skills[0])
	ch.MP = cost + 100
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
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: recog, Tag: skillID})
	}()
	firstCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode fire message error = %v", err)
	}
	if firstCmd.Ident != mir176.SMSystemMessage {
		t.Fatalf("first fire frame ident = %d, want SM_SYSTEMMESSAGE", firstCmd.Ident)
	}
	body, err := mir176.UnwrapFrame(readFrame(t, client))
	if err != nil {
		t.Fatalf("UnwrapFrame() error = %v", err)
	}
	if string(body) != "+FIR" {
		t.Fatalf("second fire frame body = %q, want +FIR", body)
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
	if ch.MP != 100 {
		t.Fatalf("MP = %d, want unchanged for zero-cost fire-hit cast", ch.MP)
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
	ch.Level = 100
	ch.Skills = storage.SkillStates{{ID: "烈火剑法", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("烈火剑法")
	if !ok {
		t.Fatal("Skill() missing 烈火剑法")
	}
	cost := s.world.SpellCost(skill, ch.Skills[0])
	ch.MP = cost + 100
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
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: recog, Tag: skillID})
	}()
	if cmd, _, err := decodeMessageLikeClient(readFrame(t, client)); err != nil || cmd.Ident != mir176.SMSystemMessage {
		t.Fatalf("first fire system frame = %+v, err=%v", cmd, err)
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
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: recog, Tag: skillID})
	}()
	if cmd, _, err := decodeMessageLikeClient(readFrame(t, client)); err != nil || cmd.Ident != mir176.SMSystemMessage {
		t.Fatalf("second fire system frame = %+v, err=%v", cmd, err)
	}
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
	attacker.BonusAbil.Hit = 100
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
	target.Sitting = true
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
	var struckCmd mir176.Command
	var struckBody []byte
	for {
		struckCmd, struckBody, err = decodeMessageLikeClient(readFrame(t, targetClient))
		if err != nil {
			t.Fatalf("decode target struck frame error = %v", err)
		}
		if struckCmd.Ident != mir176.SMChangeNameColor {
			break
		}
	}
	if struckCmd.Ident != mir176.SMStruck {
		t.Fatalf("struck ident = %d, want SM_STRUCK (%d)", struckCmd.Ident, mir176.SMStruck)
	}
	if struckCmd.Param == 0 || struckCmd.Param >= uint16(startingHP) {
		t.Fatalf("struck hp = %d, want reduced target hp", struckCmd.Param)
	}
	assertMessageBodyWL(t, struckBody, s.world.HumanFeatureForCharacter(target), s.world.CharacterStatus(target), world.CharacterActorID(attacker), 0)
	var attackerStruckCmd mir176.Command
	var attackerStruckBody []byte
	for {
		attackerStruckCmd, attackerStruckBody, err = decodeMessageLikeClient(readFrame(t, attackerClient))
		if err != nil {
			t.Fatalf("decode attacker struck frame error = %v", err)
		}
		if attackerStruckCmd.Ident != mir176.SMChangeNameColor {
			break
		}
	}
	if attackerStruckCmd.Ident != mir176.SMStruck || attackerStruckCmd.Recog != world.CharacterActorID(target) {
		t.Fatalf("attacker struck frame = %+v, want target actor %d", attackerStruckCmd, world.CharacterActorID(target))
	}
	assertMessageBodyWL(t, attackerStruckBody, s.world.HumanFeatureForCharacter(target), s.world.CharacterStatus(target), world.CharacterActorID(attacker), 0)
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
			attacker.BonusAbil.Hit = 100
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
			<-done
		})
	}
}

func TestHandleHitBroadcastsSpecialWeaponActionsToTargets(t *testing.T) {
	cases := []struct {
		name       string
		ident      uint16
		wantAction uint16
		mp         int
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
			mp:         100,
		},
		{
			name:       "wide with insufficient mp",
			ident:      mir176.CMWideHit,
			wantAction: mir176.SMWideHit,
			mp:         1,
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
			attacker.BonusAbil.Hit = 100
			if tc.ident == mir176.CMPowerHit {
				attacker.Skills = storage.SkillStates{{ID: "攻杀剑术", Level: 0, Train: 0}}
				attacker.EquippedItems = map[int]storage.UserItem{
					SlotWeapon: {ItemID: testWeaponID, Dura: 1},
				}
			}
			if tc.ident == mir176.CMLongHit {
				attacker.Skills = storage.SkillStates{{ID: "刺杀剑术", Level: 0, Train: 0}}
			}
			if tc.ident == mir176.CMWideHit {
				attacker.Skills = storage.SkillStates{{ID: "半月弯刀", Level: 0, Train: 0}}
				attacker.MP = tc.mp
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
			struckFrameCh := make(chan []byte, 16)
			go func() {
				actionFrameCh <- readFrame(t, targetClient)
				for {
					frame := readFrame(t, targetClient)
					cmd, _, err := decodeMessageLikeClient(frame)
					if err != nil {
						t.Errorf("decode target follow-up frame error = %v", err)
						return
					}
					if cmd.Ident != mir176.SMChangeNameColor {
						struckFrameCh <- frame
						return
					}
				}
			}()
			go func() {
				observerActionFrameCh <- readFrame(t, observerClient)
				for {
					frame := readFrame(t, observerClient)
					cmd, _, err := decodeMessageLikeClient(frame)
					if err != nil {
						t.Errorf("decode observer follow-up frame error = %v", err)
						return
					}
					if cmd.Ident != mir176.SMChangeNameColor {
						observerStruckFrameCh <- frame
						return
					}
				}
			}()

			recog := int32(uint32(attacker.X) | uint32(attacker.Y)<<16)
			done := make(chan struct{})
			go func() {
				defer close(done)
				s.handleHit(attackerServer, &attacker, mir176.Command{Ident: tc.ident, Recog: recog, Tag: uint16(2)})
			}()

			wideHealthCount := 0
			for {
				frame := readFrame(t, attackerClient)
				if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
					break
				}
				cmd, _, err := decodeMessageLikeClient(frame)
				if err != nil {
					t.Fatalf("decode attacker frame error = %v", err)
				}
				if cmd.Ident == mir176.SMStruck {
					continue
				}
				if cmd.Ident == mir176.SMChangeNameColor {
					continue
				}
				if cmd.Ident != mir176.SMHealthSpellChanged {
					t.Fatalf("unexpected attacker frame ident = %d", cmd.Ident)
				}
				wideHealthCount++
			}
			if tc.ident == mir176.CMWideHit && wideHealthCount != 1 {
				t.Fatalf("wide-hit health count = %d, want 1", wideHealthCount)
			}
			if err := attackerClient.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
				t.Fatalf("SetReadDeadline() error = %v", err)
			}
			buf := make([]byte, 4096)
			if n, err := attackerClient.Read(buf); err == nil {
				nameColorOnly := false
				winExpCmd, _, err := decodeMessageLikeClient(buf[:n])
				if err != nil {
					t.Fatalf("decode win exp frame error = %v", err)
				}
				for winExpCmd.Ident == mir176.SMChangeNameColor {
					n, err = attackerClient.Read(buf)
					if err != nil {
						if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
							nameColorOnly = true
							break
						}
						t.Fatalf("read win exp frame after name color error = %v", err)
					}
					winExpCmd, _, err = decodeMessageLikeClient(buf[:n])
					if err != nil {
						t.Fatalf("decode win exp frame after name color error = %v", err)
					}
				}
				if winExpCmd.Ident != mir176.SMWinExp && !nameColorOnly {
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
			var observerStruckCmd mir176.Command
			for {
				observerStruckCmd, _, err = decodeMessageLikeClient(<-observerStruckFrameCh)
				if err != nil {
					t.Fatalf("decode observer struck frame error = %v", err)
				}
				if observerStruckCmd.Ident != mir176.SMChangeNameColor {
					break
				}
			}
			if observerStruckCmd.Ident != mir176.SMStruck {
				t.Fatalf("observer struck ident = %d, want SMStruck (%d)", observerStruckCmd.Ident, mir176.SMStruck)
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
			assertMessageBodyWL(t, struckBody, s.world.HumanFeatureForCharacter(target), 0, world.CharacterActorID(attacker), 0)
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
	ch.BonusAbil.Hit = 100
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
	spawned, err := s.world.SpawnMonsterByNameAt(mapID, x+1, y, "黑色恶蛆1", 1)
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
	targetID := world.MonsterActorID(mon)
	observerFramesCh := make(chan [][]byte, 1)
	go func() {
		frames := make([][]byte, 0, 3)
		for len(frames) < 3 {
			frames = append(frames, readFrame(t, observerClient))
		}
		observerFramesCh <- frames
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: recog, Param: uint16(targetID), Series: uint16(uint32(targetID) >> 16), Tag: 1})
	}()

	frames := collectFramesUntilActionAck(t, client, 8)
	<-done
	if cost > 0 {
		healthIndex, magicIndex, ackIndex := -1, -1, -1
		for i, frame := range frames {
			if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
				ackIndex = i
				continue
			}
			command, _, err := decodeMessageLikeClient(frame)
			if err == nil {
				switch command.Ident {
				case mir176.SMHealthSpellChanged:
					if healthIndex < 0 {
						healthIndex = i
					}
				case mir176.SMMagicFire:
					if magicIndex < 0 {
						magicIndex = i
					}
				}
			}
		}
		if healthIndex < 0 || magicIndex < 0 || ackIndex < 0 || healthIndex >= magicIndex || magicIndex >= ackIndex {
			t.Fatalf("resource/effect/ack order = health %d, magic %d, ack %d; want health, effect, ack", healthIndex, magicIndex, ackIndex)
		}
	}
	tickResult, err := s.world.Tick(s.PlayerSnapshots(), time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	s.hitImpactDelay = 0
	for _, hit := range tickResult.MonsterHits {
		hit.Magic = true
		s.broadcastHitImpact(s.ClientsAround(hit.Character.MapID, hit.MonsterX, hit.MonsterY, playerViewRange), hit)
	}
	if len(tickResult.Characters) > 0 {
		ch = tickResult.Characters[0]
		s.updateClientByCharacterID(ch)
		s.sendHealthSpellChanged(server, world.CharacterActorID(ch), s.world.AbilityStats(ch))
	}
	var sawStruck, sawHealth, sawMagic bool
	for _, frame := range frames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			continue
		}
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSendMyMagic:
		case mir176.SMStruck:
			sawStruck = true
			if cmd.Recog != world.MonsterActorID(mon) {
				t.Fatalf("struck recog = %d, want %d", cmd.Recog, world.MonsterActorID(mon))
			}
			assertMessageBodyWL(t, body, world.MonsterFeature(mon), 0, world.CharacterActorID(ch), 1)
			if cmd.Param == 0 || cmd.Tag == 0 || cmd.Series == 0 {
				t.Fatalf("caster struck cmd = %+v, want non-zero hp/maxhp/damage", cmd)
			}
		case mir176.SMHealthSpellChanged:
			sawHealth = true
		case mir176.SMMagicFire:
			sawMagic = true
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	for !sawMagic || !sawStruck || !sawHealth {
		frame := readFrame(t, client)
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode late caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			sawStruck = true
		case mir176.SMHealthSpellChanged:
			sawHealth = true
		case mir176.SMSendMyMagic, mir176.SMMagicLvExp:
		case mir176.SMMagicFire:
			sawMagic = true
		default:
			t.Fatalf("unexpected late caster frame ident=%d", cmd.Ident)
		}
	}
	if !sawStruck || !sawHealth || !sawMagic {
		t.Fatalf("missing spell frames: struck=%v health=%v magic=%v", sawStruck, sawHealth, sawMagic)
	}
	observerFrames := <-observerFramesCh
	frame := observerFrames[0]
	spellCmd, spellBody, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode spell frame error = %v", err)
	}
	if spellCmd.Ident != mir176.SMSpell {
		t.Fatalf("spell ident = %d, want SM_SPELL (%d)", spellCmd.Ident, mir176.SMSpell)
	}
	if got := string(spellBody); got != "1" {
		t.Fatalf("spell body = %q, want magic id 1", got)
	}
	if spellCmd.Recog != world.CharacterActorID(ch) || spellCmd.Param != uint16(mon.X) || spellCmd.Tag != uint16(mon.Y) {
		t.Fatalf("spell command = %+v, want caster=%d target=(%d,%d)", spellCmd, world.CharacterActorID(ch), mon.X, mon.Y)
	}
	if spellCmd.Series != 1 {
		t.Fatalf("spell series = %d, want effect 1", spellCmd.Series)
	}
	magicCmd, magicBody, err := decodeMessageLikeClient(observerFrames[1])
	if err != nil {
		t.Fatalf("decode magic fire frame error = %v", err)
	}
	if magicCmd.Ident != mir176.SMMagicFire {
		t.Fatalf("magic fire ident = %d, want SM_MAGICFIRE (%d)", magicCmd.Ident, mir176.SMMagicFire)
	}
	if magicCmd.Series != uint16(makeWord(1, 1)) {
		t.Fatalf("magic fire series = %d, want effect type/effect 1/1", magicCmd.Series)
	}
	decodedMagicBody, err := mir176.DecodePlain6Payload(magicBody)
	if err != nil {
		t.Fatalf("decode magic fire target body error = %v", err)
	}
	if len(decodedMagicBody) != 4 || binary.LittleEndian.Uint32(decodedMagicBody) != uint32(world.MonsterActorID(mon)) {
		t.Fatalf("magic fire target body = %v, want actor id %d", decodedMagicBody, world.MonsterActorID(mon))
	}
	frame = observerFrames[2]
	struckCmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode struck frame error = %v", err)
	}
	if struckCmd.Ident != mir176.SMStruck {
		t.Fatalf("struck ident = %d, want SM_STRUCK (%d)", struckCmd.Ident, mir176.SMStruck)
	}
	if ch.MP != cost+10-cost {
		t.Fatalf("MP = %d, want %d after cast", ch.MP, cost+10-cost)
	}
	if got := ch.Skills[0].Train; got < 1 || got > 3 {
		t.Fatalf("skill train = %d, want 1..3", got)
	}
}

func TestRecordSpellActionUpdatesSharedMagicTime(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "spell_time", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)

	connected := s.clientForConn(server)
	connected.mu.Lock()
	connected.spellActionAt = time.Time{}
	connected.spellActionCount = 3
	connected.mu.Unlock()
	s.recordSpellAction(server)
	connected.mu.Lock()
	actionAt := connected.spellActionAt
	actionCount := connected.spellActionCount
	connected.mu.Unlock()
	if actionAt.IsZero() {
		t.Fatal("recordSpellAction() left spell action time unset")
	}
	if actionCount != 0 {
		t.Fatalf("spell action count = %d, want 0", actionCount)
	}
}

func TestRecordSpellCastTimestampPreservesMagicInterval(t *testing.T) {
	s := newTestServer(t)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	state := &Client{conn: server, spellActionInterval: 1700 * time.Millisecond, spellActionCount: 3}
	s.clientMu.Lock()
	s.clients[server] = state
	s.clientMu.Unlock()

	s.recordSpellCastTimestamp(server)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.spellActionAt.IsZero() {
		t.Fatal("spellActionAt is zero after special spell")
	}
	if state.spellActionInterval != 1700*time.Millisecond || state.spellActionCount != 3 {
		t.Fatalf("special spell changed throttle state: interval=%s count=%d", state.spellActionInterval, state.spellActionCount)
	}
}

func TestHandleSpellRejectsUnlearnedSkillWithActionFail(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.MP = 100
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	skillID, ok := s.world.MagicIDByName("火球术")
	if !ok {
		t.Fatal("MagicIDByName() missing 火球术")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x+1) | uint32(y)<<16), Tag: skillID})
	}()

	frame := readFrame(t, client)
	assertActionFail(t, frame)
	if extra, ok := readFrameWithTimeout(t, client, 200*time.Millisecond); ok {
		cmd, _, err := decodeMessageLikeClient(extra)
		if err != nil {
			t.Fatalf("decode extra frame error = %v", err)
		}
		t.Fatalf("unexpected extra frame after action fail: ident=%d", cmd.Ident)
	}
	<-done
	if ch.MP != 100 {
		t.Fatalf("MP = %d, want unchanged after rejection", ch.MP)
	}
}

func TestHandleSpellQueuesSecondPendingDelayedCast(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.MP = 100
	ch.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	spellClient := s.clientForConn(server)
	spellClient.mu.Lock()
	spellClient.spellActionAt = time.Now()
	spellClient.spellActionInterval = time.Second
	spellClient.mu.Unlock()

	request := spellRequest{x: x + 1, y: y, magicID: 1}
	s.processSpellDelivery(server, &ch, request, false)

	spellClient.mu.Lock()
	pending := spellClient.pendingSpellMessages
	spellClient.mu.Unlock()
	if pending != 1 {
		t.Fatalf("pending delayed spells = %d, want 1", pending)
	}
	spellClient.mu.Lock()
	if spellClient.actionIdent != mir176.CMSpell || spellClient.actionDir != world.Direction(ch.X, ch.Y, x+1, y) {
		t.Fatalf("delayed spell action snapshot = ident:%d dir:%d, want CMSpell and current direction", spellClient.actionIdent, spellClient.actionDir)
	}
	spellClient.mu.Unlock()

	s.processSpellDelivery(server, &ch, request, false)
	spellClient.mu.Lock()
	pending = spellClient.pendingSpellMessages
	spellClient.mu.Unlock()
	if pending != 2 {
		t.Fatalf("pending delayed spells after second request = %d, want 2", pending)
	}
	s.unregisterClient(server)
}

func TestHandleSpellDelaysAfterRunAction(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.MP = 100
	ch.SpellTick = 700
	ch.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	spellClient := s.clientForConn(server)
	spellClient.mu.Lock()
	initialActionAt := time.Now()
	initialSpellActionAt := time.Now().Add(-2 * time.Second)
	spellClient.actionAt = initialActionAt
	spellClient.actionIdent = mir176.CMRun
	spellClient.actionDir = 4
	spellClient.spellActionAt = initialSpellActionAt
	spellClient.spellActionInterval = time.Second
	spellClient.mu.Unlock()

	s.processSpellDelivery(server, &ch, spellRequest{x: x + 1, y: y, magicID: 1}, false)

	spellClient.mu.Lock()
	pending := spellClient.pendingSpellMessages
	actionIdent := spellClient.actionIdent
	actionDir := spellClient.actionDir
	actionAt := spellClient.actionAt
	spellActionAt := spellClient.spellActionAt
	spellClient.mu.Unlock()
	if pending != 1 {
		t.Fatalf("pending spells after non-spell action = %d, want 1", pending)
	}
	if actionIdent != mir176.CMSpell || actionDir != world.Direction(ch.X, ch.Y, ch.X+1, ch.Y) {
		t.Fatalf("delayed action snapshot = %d/%d, want spell/%d", actionIdent, actionDir, world.Direction(ch.X, ch.Y, ch.X+1, ch.Y))
	}
	if !actionAt.Equal(initialActionAt) || !spellActionAt.Equal(initialSpellActionAt) {
		t.Fatalf("delayed action times changed: action=%v/%v spell=%v/%v", actionAt, initialActionAt, spellActionAt, initialSpellActionAt)
	}
	if ch.SpellTick != 700 {
		t.Fatalf("SpellTick after delayed enqueue = %d, want unchanged 700", ch.SpellTick)
	}
	s.processSpellDelivery(server, &ch, spellRequest{x: x + 1, y: y, magicID: 1}, true)
	if ch.SpellTick != 250 {
		t.Fatalf("SpellTick after delayed delivery = %d, want 250", ch.SpellTick)
	}
	s.unregisterClient(server)
}

func TestHandleSpellStruckDelayDoesNotAdvanceMagicThrottle(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.MP = 100
	ch.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	spellClient := s.clientForConn(server)
	spellAt := time.Now()
	spellClient.mu.Lock()
	spellClient.struckAt = spellAt
	spellClient.spellActionAt = spellAt.Add(-time.Second)
	spellClient.spellActionInterval = 2 * time.Second
	spellClient.spellActionCount = 3
	spellClient.mu.Unlock()

	s.processSpellDelivery(server, &ch, spellRequest{x: x + 1, y: y, magicID: 1}, false)

	spellClient.mu.Lock()
	count := spellClient.spellActionCount
	gotSpellAt := spellClient.spellActionAt
	spellClient.mu.Unlock()
	if count != 3 {
		t.Fatalf("spell action count = %d, want unchanged 3 during struck delay", count)
	}
	if !gotSpellAt.Equal(spellAt.Add(-time.Second)) {
		t.Fatalf("spell action time = %v, want unchanged %v", gotSpellAt, spellAt.Add(-time.Second))
	}
	s.unregisterClient(server)
}

func TestHandleSpellDelaysAfterWalkAction(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.MP = 100
	ch.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	spellClient := s.clientForConn(server)
	spellClient.mu.Lock()
	spellClient.actionAt = time.Now()
	spellClient.actionIdent = mir176.CMWalk
	spellClient.actionDir = world.Direction(ch.X, ch.Y, x+1, y)
	spellClient.spellActionAt = time.Now().Add(-2 * time.Second)
	spellClient.spellActionInterval = time.Second
	spellClient.mu.Unlock()

	s.processSpellDelivery(server, &ch, spellRequest{x: x + 1, y: y, magicID: 1}, false)

	spellClient.mu.Lock()
	pending := spellClient.pendingSpellMessages
	spellClient.mu.Unlock()
	if pending != 1 {
		t.Fatalf("pending spells after walk action = %d, want 1", pending)
	}
	s.unregisterClient(server)
}

func TestHandleSpellDelaysAfterOrdinaryActionUpdatesSpellSnapshot(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.MP = 100
	ch.Skills = storage.SkillStates{{ID: "火球术", Level: 0, Train: 0}}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	spellClient := s.clientForConn(server)
	initialActionAt := time.Now()
	spellClient.mu.Lock()
	spellClient.actionAt = initialActionAt
	spellClient.actionIdent = mir176.CMHit
	spellClient.actionDir = 2
	spellClient.spellActionAt = time.Now().Add(-2 * time.Second)
	spellClient.spellActionInterval = time.Second
	spellClient.mu.Unlock()

	s.processSpellDelivery(server, &ch, spellRequest{x: x + 1, y: y, magicID: 1}, false)

	spellClient.mu.Lock()
	pending := spellClient.pendingSpellMessages
	actionIdent := spellClient.actionIdent
	actionDir := spellClient.actionDir
	actionAt := spellClient.actionAt
	spellClient.mu.Unlock()
	if pending != 1 {
		t.Fatalf("pending spells after ordinary action = %d, want 1", pending)
	}
	if actionIdent != mir176.CMSpell || actionDir != world.Direction(ch.X, ch.Y, x+1, y) {
		t.Fatalf("delayed action snapshot = %d/%d, want spell/%d", actionIdent, actionDir, world.Direction(ch.X, ch.Y, x+1, y))
	}
	if !actionAt.Equal(initialActionAt) {
		t.Fatalf("delayed action time = %v, want unchanged %v", actionAt, initialActionAt)
	}
	s.unregisterClient(server)
}

func TestHandleSpellPassiveWarriorSkillsOnlyAcknowledge(t *testing.T) {
	for _, skillID := range []string{"基本剑术", "精神力战法", "攻杀剑术"} {
		t.Run(skillID, func(t *testing.T) {
			s := newTestServer(t)
			mapID, x, y := testDefaultSpawn(t)
			ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
			if err != nil {
				t.Fatalf("CreateCharacter() error = %v", err)
			}
			ch.Skills = storage.SkillStates{{ID: skillID, Level: 0, Train: 0}}
			magicID, ok := s.world.MagicIDByName(skillID)
			if !ok {
				t.Fatalf("MagicIDByName() missing %s", skillID)
			}
			server, client := net.Pipe()
			defer server.Close()
			defer client.Close()
			s.registerClient(server, ch)
			defer s.unregisterClient(server)
			done := make(chan struct{})
			go func() {
				defer close(done)
				s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Tag: magicID})
			}()
			frame := readFrame(t, client)
			body, err := mir176.UnwrapFrame(frame)
			if err != nil || !strings.HasPrefix(string(body), "+GOOD/") {
				t.Fatalf("ack frame = %q, %v; want +GOOD/", body, err)
			}
			if extra, ok := readFrameWithTimeout(t, client, 50*time.Millisecond); ok {
				t.Fatalf("unexpected extra frame: %x", extra)
			}
			<-done
			spellClient := s.clientForConn(server)
			spellClient.mu.Lock()
			defer spellClient.mu.Unlock()
			if spellClient.actionIdent != mir176.CMSpell {
				t.Fatalf("passive skill action ident = %d, want CMSpell", spellClient.actionIdent)
			}
		})
	}
}

func TestHandleSpellWarriorToggleSkills(t *testing.T) {
	for _, tc := range []struct {
		name        string
		firstFrame  string
		secondFrame string
	}{
		{name: "刺杀剑术", firstFrame: "+LNG", secondFrame: "+ULNG"},
		{name: "半月弯刀", firstFrame: "+WID", secondFrame: "+UWID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t)
			mapID, x, y := testDefaultSpawn(t)
			ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "warrior", 0, 0, mapID, x, y)
			if err != nil {
				t.Fatalf("CreateCharacter() error = %v", err)
			}
			ch.Skills = storage.SkillStates{{ID: tc.name, Level: 0, Train: 0}}
			ch.ThrustingDisabled = true
			ch.HalfMoonDisabled = true
			magicID, ok := s.world.MagicIDByName(tc.name)
			if !ok {
				t.Fatalf("MagicIDByName() missing %s", tc.name)
			}
			server, client := net.Pipe()
			defer server.Close()
			defer client.Close()
			call := func(wantToggle string) {
				done := make(chan struct{})
				go func() {
					defer close(done)
					s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Tag: magicID})
				}()
				frame := readFrame(t, client)
				body, err := mir176.UnwrapFrame(frame)
				if err != nil || string(body) != wantToggle {
					t.Fatalf("toggle response = %q, %v; want %q", body, err, wantToggle)
				}
				body, err = mir176.UnwrapFrame(readFrame(t, client))
				if err != nil || !strings.HasPrefix(string(body), "+GOOD/") {
					t.Fatalf("ack response = %q, %v", body, err)
				}
				<-done
			}
			call(tc.firstFrame)
			call(tc.secondFrame)
		})
	}
}

func TestHandleSpellMonsterMagicHitSendsVisibleHealthBeforeStruck(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "watcher", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)

	result := world.AttackResult{
		MonsterID:            "monster-visible-health",
		MonsterHP:            37,
		MonsterMaxHP:         100,
		MonsterMP:            12,
		MonsterX:             x,
		MonsterY:             y,
		MonsterHealthChanged: true,
		Magic:                true,
		Character:            ch,
	}
	skill, ok := s.world.Skill("雷电术")
	if !ok {
		t.Fatal("雷电术 missing from config")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpellEvent(server, &ch, "雷电术", skill, world.SpellEvent{Kind: world.SpellEventMonsterHit, MonsterHit: result})
	}()

	first := readFrame(t, client)
	second := readFrame(t, client)
	<-done
	firstCmd, _, err := decodeMessageLikeClient(first)
	if err != nil {
		t.Fatalf("decode first frame error = %v", err)
	}
	secondCmd, _, err := decodeMessageLikeClient(second)
	if err != nil {
		t.Fatalf("decode second frame error = %v", err)
	}
	if firstCmd.Ident != mir176.SMHealthSpellChanged || firstCmd.Recog != world.MonsterActorID(world.Monster{ID: result.MonsterID}) {
		t.Fatalf("first monster magic frame = %+v, want visible health", firstCmd)
	}
	if firstCmd.Param != uint16(result.MonsterHP) || firstCmd.Tag != uint16(result.MonsterMP) || firstCmd.Series != uint16(result.MonsterMaxHP) {
		t.Fatalf("monster health frame = %+v, want hp/mp/maxhp %d/%d/%d", firstCmd, result.MonsterHP, result.MonsterMP, result.MonsterMaxHP)
	}
	if secondCmd.Ident != mir176.SMStruck || secondCmd.Recog != world.MonsterActorID(world.Monster{ID: result.MonsterID}) {
		t.Fatalf("second monster magic frame = %+v, want target struck", secondCmd)
	}
}

func TestHandleSpellReportsInsufficientMPWithMagicFireFailAndActionOK(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.HP = ch.MaxHP
	ch.Skills = storage.SkillStates{{ID: "雷电术", Level: 10, Train: 0}}
	skill, ok := s.world.Skill("雷电术")
	if !ok {
		t.Fatal("skill 雷电术 missing from config")
	}
	cost := s.world.SpellCost(skill, ch.Skills[0])
	if cost < 1 {
		t.Fatalf("lightning cost = %d, want positive", cost)
	}
	ch.MP = cost - 1
	ch.SpellTick = 700
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	defer s.unregisterClient(server)

	skillID, ok := s.world.MagicIDByName("雷电术")
	if !ok {
		t.Fatal("MagicIDByName() missing 雷电术")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x+1) | uint32(y)<<16), Tag: skillID})
	}()

	seenMagicFail, seenActionOK := false, false
	for !seenMagicFail || !seenActionOK {
		frame := readFrame(t, client)
		command, _, err := decodeMessageLikeClient(frame)
		if err == nil {
			if command.Ident != mir176.SMMagicFireFail {
				t.Fatalf("unexpected command frame = %+v", command)
			}
			seenMagicFail = true
			continue
		}
		if len(frame) < 2 || !strings.HasPrefix(string(frame[1:len(frame)-1]), "+GOOD/") {
			t.Fatalf("action confirmation frame = %x, want +GOOD", frame)
		}
		seenActionOK = true
	}
	<-done
	if ch.MP != cost-1 {
		t.Fatalf("MP = %d, want unchanged after rejection", ch.MP)
	}
	if ch.SpellTick != 250 {
		t.Fatalf("SpellTick = %d, want 250 after rejected cast", ch.SpellTick)
	}
}

func TestHandleSpellLegacyHighMagicSendsStartThenMagicFireFail(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "legacy", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.SoftVersionDate = 0
	ch.ClientTick = 0
	ch.Skills = storage.SkillStates{{ID: "困魔咒", Level: 1, Train: 0}}
	skill, ok := s.world.Skill("困魔咒")
	if !ok {
		t.Fatal("skill 困魔咒 missing from config")
	}
	ch.MP = s.world.SpellCost(skill, ch.Skills[0]) + 1
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	observer, observerClient := net.Pipe()
	defer observer.Close()
	defer observerClient.Close()
	observerCh, err := s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	s.registerClient(server, ch)
	defer s.unregisterClient(server)
	s.registerClient(observer, observerCh)
	defer s.unregisterClient(observer)

	magicID, ok := s.world.MagicIDByName("困魔咒")
	if !ok {
		t.Fatal("MagicIDByName() missing 困魔咒")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Tag: magicID})
	}()
	frames := collectFramesUntilActionAck(t, client, 8)
	<-done
	observerFirst := readFrame(t, observerClient)
	observerCommand, observerBody, err := decodeMessageLikeClient(observerFirst)
	if err != nil {
		t.Fatalf("decode observer spell frame error = %v", err)
	}
	if observerCommand.Ident != mir176.SMSpell || string(observerBody) != fmt.Sprint(magicID) {
		t.Fatalf("observer first frame = %+v body=%q, want SM_SPELL magic %d", observerCommand, observerBody, magicID)
	}
	observerSecond := readFrame(t, observerClient)
	observerFailure, _, err := decodeMessageLikeClient(observerSecond)
	if err != nil {
		t.Fatalf("decode observer failure frame error = %v", err)
	}
	if observerFailure.Ident != mir176.SMMagicFireFail {
		t.Fatalf("observer second frame = %+v, want SM_MAGICFIRE_FAIL", observerFailure)
	}

	failIndex := -1
	decodedFrames := make([]string, 0, len(frames))
	for i, frame := range frames {
		if isActionAckFrame(frame) {
			decodedFrames = append(decodedFrames, "action-ok")
			continue
		}
		command, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode frame error = %v", err)
		}
		decodedFrames = append(decodedFrames, fmt.Sprintf("ident=%d", command.Ident))
		switch command.Ident {
		case mir176.SMMagicFire:
			t.Fatalf("legacy high magic unexpectedly emitted SM_MAGICFIRE: %+v", command)
		case mir176.SMMagicFireFail:
			failIndex = i
		}
	}
	if failIndex < 0 {
		t.Fatalf("legacy high magic frames = %v, want SM_MAGICFIRE_FAIL before action-ok", decodedFrames)
	}
}

func TestSpellMagicFireFailReachesVisibleClients(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	casterConn, casterClient := net.Pipe()
	defer casterConn.Close()
	defer casterClient.Close()
	observerConn, observerClient := net.Pipe()
	defer observerConn.Close()
	defer observerClient.Close()
	s.registerClient(casterConn, caster)
	defer s.unregisterClient(casterConn)
	s.registerClient(observerConn, observer)
	defer s.unregisterClient(observerConn)

	readFail := func(conn net.Conn, done chan<- error) {
		frame, ok := readFrameWithTimeout(t, conn, time.Second)
		if !ok {
			done <- errors.New("missing magic fire fail frame")
			return
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			done <- err
			return
		}
		if cmd.Ident != mir176.SMMagicFireFail || cmd.Recog != world.CharacterActorID(caster) {
			done <- fmt.Errorf("magic fire fail command = %+v", cmd)
			return
		}
		done <- nil
	}
	casterDone := make(chan error, 1)
	go readFail(casterClient, casterDone)
	handleDone := make(chan struct{})
	go func() {
		s.handleSpellEvent(casterConn, &caster, "雷电术", data.StdSkill{}, world.SpellEvent{Kind: world.SpellEventMagicFireFail, Character: caster})
		close(handleDone)
	}()
	if err := <-casterDone; err != nil {
		t.Fatal(err)
	}
	<-handleDone
	observerFrame, ok := readFrameWithTimeout(t, observerClient, time.Second)
	if !ok {
		t.Fatal("missing observer magic fire failure frame")
	}
	observerCmd, _, err := decodeMessageLikeClient(observerFrame)
	if err != nil {
		t.Fatal(err)
	}
	if observerCmd.Ident != mir176.SMMagicFireFail || observerCmd.Recog != world.CharacterActorID(caster) {
		t.Fatalf("observer magic fire fail command = %+v", observerCmd)
	}
}

func TestCharacterStatusMessageMatchesReferenceFields(t *testing.T) {
	s := newTestServer(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "status", "warrior", 0, 0, testMapID, 10, 10)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.ExtraAbil[3] = 7
	ch.Sitting = true
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	done := make(chan struct{})
	go func() {
		s.handleCharacterStatusChanged(serverConn, ch)
		close(done)
	}()
	cmd, _, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode status message = %v", err)
	}
	if cmd.Ident != mir176.SMCharStatusChanged {
		t.Fatalf("status ident = %d, want %d", cmd.Ident, mir176.SMCharStatusChanged)
	}
	if cmd.Param != 1 || cmd.Tag != 0 {
		t.Fatalf("status fields = (%d, %d), want (1, 0)", cmd.Param, cmd.Tag)
	}
	if cmd.Series != 7 {
		t.Fatalf("hit speed field = %d, want 7", cmd.Series)
	}
	<-done
}

func TestSpellStartEffectUsesReferenceByteField(t *testing.T) {
	s := newTestServer(t)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	c := &Client{conn: serverConn, ch: storage.Character{ID: "observer"}}
	event := spellStartEvent{caster: storage.Character{ID: "caster", MapID: testMapID, X: 10, Y: 10}, magicID: 7, effect: 0x1234, targetX: 11, targetY: 12}
	done := make(chan struct{})
	go func() {
		c.handleSpellStart(s, event)
		close(done)
	}()
	cmd, body, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode spell start = %v", err)
	}
	if cmd.Ident != mir176.SMSpell || cmd.Series != 0x34 {
		t.Fatalf("spell start command = %+v, want SM_SPELL with low-byte effect", cmd)
	}
	if string(body) != "7" {
		t.Fatalf("spell start body = %q, want magic id", body)
	}
	<-done
}

func TestMonsterStatusMessageUsesReferenceFields(t *testing.T) {
	s := newTestServer(t)
	mon := world.Monster{ID: "summon-7", MapID: testMapID, X: 10, Y: 10, TransparentUntil: time.Now().Add(time.Minute)}
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	done := make(chan struct{})
	go func() {
		(&Client{conn: serverConn}).sendMonsterStatusChanged(s, mon)
		close(done)
	}()
	cmd, _, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode monster status message = %v", err)
	}
	if cmd.Ident != mir176.SMCharStatusChanged || cmd.Recog != world.MonsterActorID(mon) || cmd.Param != 0 || cmd.Tag != 0x80 || cmd.Series != 0 {
		t.Fatalf("monster status command = %+v, want actor=%d status=0x00800000", cmd, world.MonsterActorID(mon))
	}
	<-done
}

func TestSpellDurabilityMessageMatchesReferenceFields(t *testing.T) {
	s := newTestServer(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "durability", "wizard", 0, 0, "D12", 0, 0)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()
	client := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		client.handleQueuedSpellMessage(s, spellObjectMessage{
			kind:       spellObjectMessageDurability,
			durability: world.SpellDurability{Slot: world.SlotBujuk, Dura: 37, DuraMax: 99},
		})
	}()
	cmd, body, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode durability frame error = %v", err)
	}
	if cmd.Ident != mir176.SMDuraChange || cmd.Recog != 37 || cmd.Param != uint16(world.SlotBujuk) || cmd.Tag != 99 || cmd.Series != 0 {
		t.Fatalf("durability command = %+v, want dura=37 slot=%d duramax=99", cmd, world.SlotBujuk)
	}
	if len(body) != 0 {
		t.Fatalf("durability body len = %d, want 0", len(body))
	}
	<-done
}

func TestSpellCharacterFeatureRefreshPrecedesDurabilityAndStruck(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	target, err := s.world.CreateCharacterWithAppearance("feature-target", "feature-target", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, target)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpellEvent(server, &target, "火球术", data.StdSkill{}, world.SpellEvent{
			Kind: world.SpellEventCharacterHit,
			CharacterHit: world.CharacterHit{
				Character:      target,
				FeatureChanged: true,
				DeletedItems:   []storage.UserItem{{ItemID: "护身符", MakeIndex: 9, Dura: 0, DuraMax: 100}},
				Durability:     []world.SpellDurability{{Slot: world.SlotDress, Dura: 37, DuraMax: 99}},
				Damage:         1,
			},
		})
	}()

	deleted, deletedBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode deleted item frame error = %v", err)
	}
	if deleted.Ident != mir176.SMDelItem || deleted.Recog != world.CharacterActorID(target) || deleted.Series != 1 {
		t.Fatalf("first frame command = %+v, want SM_DELITEM actor and count 1", deleted)
	}
	if got := decodeClientItemName(deletedBody); got != "护身符" {
		t.Fatalf("deleted item name = %q, want %q", got, "护身符")
	}
	feature, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode feature frame error = %v", err)
	}
	if feature.Ident != mir176.SMFeatureChanged {
		t.Fatalf("first frame ident = %d, want SM_FEATURECHANGED (%d)", feature.Ident, mir176.SMFeatureChanged)
	}
	durability, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode durability frame error = %v", err)
	}
	if durability.Ident != mir176.SMDuraChange {
		t.Fatalf("second frame ident = %d, want SM_DURACHANGE (%d)", durability.Ident, mir176.SMDuraChange)
	}
	struck, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode struck frame error = %v", err)
	}
	if struck.Ident != mir176.SMStruck {
		t.Fatalf("third frame ident = %d, want SM_STRUCK (%d)", struck.Ident, mir176.SMStruck)
	}
	<-done
}

func TestSpellItemDeleteMessageMatchesReferenceFields(t *testing.T) {
	s := newTestServer(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "delete", "wizard", 0, 0, "D12", 0, 0)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()
	client := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpellEvent(server, &ch, "隐身术", data.StdSkill{}, world.SpellEvent{
			Kind:        world.SpellEventItemDelete,
			Character:   ch,
			DeletedItem: storage.UserItem{ItemID: "护身符", MakeIndex: 42, Dura: 0, DuraMax: 100},
		})
	}()
	cmd, body, err := decodeMessageLikeClient(readFrame(t, clientConn))
	if err != nil {
		t.Fatalf("decode item delete frame error = %v", err)
	}
	if cmd.Ident != mir176.SMDelItem || cmd.Recog != world.CharacterActorID(ch) || cmd.Series != 1 {
		t.Fatalf("item delete command = %+v, want SM_DELITEM actor and count 1", cmd)
	}
	if got := decodeClientItemName(body); got != "护身符" {
		t.Fatalf("item delete name = %q, want %q", got, "护身符")
	}
	if got := decodeClientItemMakeIndex(body); got != 42 {
		t.Fatalf("item delete make index = %d, want 42", got)
	}
	if got, gotMax := decodeClientItemDura(body); got != 0 || gotMax != 100 {
		t.Fatalf("item delete durability = (%d, %d), want (0, 100)", got, gotMax)
	}
	_ = client
	<-done
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
	spawned, err := s.world.SpawnMonsterByNameAt(mapID, x+1, y, "黑色恶蛆1", 1)
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
	observerFramesCh := make(chan [][]byte, 1)
	go func() {
		frames := make([][]byte, 0, 3)
		for len(frames) < 3 {
			frames = append(frames, readFrame(t, observerClient))
		}
		observerFramesCh <- frames
	}()
	done := make(chan struct{})
	targetID := world.MonsterActorID(mon)
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: recog, Param: uint16(targetID), Series: uint16(uint32(targetID) >> 16), Tag: 5})
	}()

	frames := collectFramesUntilActionAck(t, client, 8)
	for len(frames) < 3 {
		frames = append(frames, readFrame(t, client))
	}
	<-done
	tickResult, err := s.world.Tick(s.PlayerSnapshots(), time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	s.hitImpactDelay = 0
	delayedCasterFrames := make(chan [][]byte, 1)
	go func() {
		frames := make([][]byte, 0, 2)
		for len(frames) < 2 {
			frames = append(frames, readFrame(t, client))
		}
		delayedCasterFrames <- frames
	}()
	for _, hit := range tickResult.MonsterHits {
		hit.Magic = true
		s.broadcastHitImpact(s.ClientsAround(hit.Character.MapID, hit.MonsterX, hit.MonsterY, playerViewRange), hit)
	}
	if len(tickResult.Characters) > 0 {
		ch = tickResult.Characters[0]
		s.updateClientByCharacterID(ch)
		s.sendHealthSpellChanged(server, world.CharacterActorID(ch), s.world.AbilityStats(ch))
	}
	var sawStruck, sawHealth, sawMagic bool
	for _, frame := range frames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			sawStruck = true
		case mir176.SMHealthSpellChanged:
			sawHealth = true
		case mir176.SMMagicFire:
			sawMagic = true
		case mir176.SMSendMyMagic:
		case mir176.SMMagicLvExp:
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	if !sawMagic {
		t.Fatalf("missing immediate frames: magic=%v", sawMagic)
	}
	for _, frame := range <-delayedCasterFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode delayed caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			sawStruck = true
		case mir176.SMHealthSpellChanged:
			sawHealth = true
		}
	}
	if !sawStruck || !sawHealth {
		t.Fatalf("missing delayed frames: struck=%v health=%v", sawStruck, sawHealth)
	}
	observerFrames := <-observerFramesCh
	frame := observerFrames[0]
	spellCmd, spellBody, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode spell frame error = %v", err)
	}
	if spellCmd.Ident != mir176.SMSpell {
		t.Fatalf("spell ident = %d, want SM_SPELL (%d)", spellCmd.Ident, mir176.SMSpell)
	}
	if got := string(spellBody); got != "5" {
		t.Fatalf("spell body = %q, want magic id 5", got)
	}
	if spellCmd.Recog != world.CharacterActorID(ch) || spellCmd.Param != uint16(mon.X) || spellCmd.Tag != uint16(mon.Y) {
		t.Fatalf("spell command = %+v, want caster=%d target=(%d,%d)", spellCmd, world.CharacterActorID(ch), mon.X, mon.Y)
	}
	if spellCmd.Series != 3 {
		t.Fatalf("spell series = %d, want effect 3", spellCmd.Series)
	}
	magicCmd, _, err := decodeMessageLikeClient(observerFrames[1])
	if err != nil {
		t.Fatalf("decode magic fire frame error = %v", err)
	}
	if magicCmd.Ident != mir176.SMMagicFire {
		t.Fatalf("magic fire ident = %d, want SM_MAGICFIRE (%d)", magicCmd.Ident, mir176.SMMagicFire)
	}
	frame = observerFrames[2]
	struckCmd, _, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode struck frame error = %v", err)
	}
	if struckCmd.Ident != mir176.SMStruck {
		t.Fatalf("struck ident = %d, want SM_STRUCK (%d)", struckCmd.Ident, mir176.SMStruck)
	}
	<-done
	if ch.MP != cost+10-cost {
		t.Fatalf("MP = %d, want %d after cast", ch.MP, cost+10-cost)
	}
	if got := ch.Skills[0].Train; got < 1 || got > 3 {
		t.Fatalf("skill train = %d, want 1..3", got)
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
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 0, Tag: 31})
	}()

	frames := collectFramesUntilActionAck(t, client, 8)
	for len(frames) < 4 {
		frames = append(frames, readFrame(t, client))
	}
	var sawHealth, sawMagic, sawStatus bool
	for _, frame := range frames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMHealthSpellChanged:
			sawHealth = true
		case mir176.SMMagicFire:
			sawMagic = true
		case mir176.SMCharStatusChanged:
			sawStatus = true
		case mir176.SMSendMyMagic:
		case mir176.SMMagicLvExp:
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	if (cost > 0 && !sawHealth) || !sawMagic || !sawStatus {
		t.Fatalf("missing frames: health=%v magic=%v status=%v", sawHealth, sawMagic, sawStatus)
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
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 0, Tag: 2})
	}()

	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh
	tickResult, err := s.world.Tick([]world.PlayerSnapshot{{Character: caster}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	for _, updated := range tickResult.Characters {
		if updated.ID == caster.ID {
			caster = updated
		}
	}

	var casterAck, casterMagicFire bool
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
		case mir176.SMMagicFire:
			casterMagicFire = true
		}
	}
	if !casterAck {
		t.Fatal("missing caster action ack")
	}
	if !casterMagicFire {
		t.Fatal("missing caster SMMagicFire frame")
	}

	var observerSpell bool
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
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
	if got := caster.HP; got <= max(1, stats.MaxHP/2) {
		t.Fatalf("caster HP = %d, want recovery after delayed healing is consumed", got)
	}
	if caster.IncHealing != 0 {
		t.Fatalf("caster IncHealing = %d, want recovery queue consumed", caster.IncHealing)
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
	caster.EquippedItems = map[int]storage.UserItem{world.SlotBujuk: {ItemID: "护身符", Dura: 10000}}
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
		targetID := world.MonsterActorID(summoned)
		s.handleSpell(server, &summonedResult.Character, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(summoned.X) | uint32(summoned.Y)<<16), Param: uint16(targetID), Series: uint16(uint32(targetID) >> 16), Tag: 2})
	}()

	casterCollected := <-casterFrames
	observerCollected := <-observerFrames
	<-done

	var sawCasterAck, sawCasterMagicFire, sawObserverSpell bool
	for _, frame := range casterCollected {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			sawCasterAck = true
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMMagicFire:
			sawCasterMagicFire = true
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
	if !sawCasterAck || !sawCasterMagicFire {
		t.Fatalf("caster frames missing ack/magicfire: ack=%v magicfire=%v", sawCasterAck, sawCasterMagicFire)
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

func TestHandleSpellHealToMonsterDoesNotFireMagicEffect(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	caster.Dir = 2
	caster.MP = 100
	caster.Skills = storage.SkillStates{{ID: "治愈术", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("治愈术")
	if !ok {
		t.Fatalf("skill 治愈术 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 10
	spawned, err := s.world.SpawnMonsterByNameAt(mapID, x+1, y, "白野猪", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() error = %v", err)
	}
	if len(spawned.Monsters) == 0 {
		t.Fatalf("SpawnMonsterByNameAt() returned no monsters")
	}
	mon := spawned.Monsters[0]
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
		targetID := world.MonsterActorID(spawned.Monsters[0])
		s.handleSpell(server, &caster, mir176.Command{
			Ident:  mir176.CMSpell,
			Recog:  int32(uint32(mon.X) | uint32(mon.Y)<<16),
			Param:  uint16(uint32(targetID) & 0xFFFF),
			Tag:    2,
			Series: uint16(uint32(targetID) >> 16),
		})
	}()

	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh
	<-done

	var sawAck, sawHealth, sawMagicFire bool
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			sawAck = true
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMHealthSpellChanged:
			sawHealth = true
		case mir176.SMMagicFire:
			sawMagicFire = true
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	if !sawAck {
		t.Fatal("missing caster action ack")
	}
	if cost > 0 && !sawHealth {
		t.Fatal("missing caster SMHealthSpellChanged frame")
	}
	if !sawMagicFire {
		t.Fatal("missing caster SMMagicFire frame")
	}
	var sawObserverSpell bool
	for _, frame := range observerFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			continue
		}
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		if cmd.Ident == mir176.SMSpell {
			sawObserverSpell = true
			if got := string(body); got != "2" {
				t.Fatalf("observer spell body = %q, want magic id 2", got)
			}
		}
	}
	if !sawObserverSpell {
		t.Fatal("missing observer SMSpell frame")
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
		targetID := world.CharacterActorID(target)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(target.X) | uint32(target.Y)<<16), Param: uint16(targetID), Series: uint16(uint32(targetID) >> 16), Tag: 11})
	}()
	<-done
	s.hitImpactDelay = 0
	tickResult, err := s.world.Tick(s.PlayerSnapshots(), time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	s.applyWorldTick(tickResult, time.Now())

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
	if cost > 0 && !casterHealth {
		t.Fatal("missing caster SMHealthSpellChanged frame")
	}

	var targetStruck, targetHealth bool
	targetHealthIndex, targetStruckIndex := -1, -1
	for index, frame := range targetFrames {
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode target frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			targetStruck = true
			targetStruckIndex = index
			assertMessageBodyWL(t, body, s.world.HumanFeatureForCharacter(target), 0, world.CharacterActorID(caster), 1)
			if cmd.Param == 0 || cmd.Param >= uint16(startingHP) {
				t.Fatalf("target struck hp = %d, want reduced hp", cmd.Param)
			}
		case mir176.SMHealthSpellChanged:
			targetHealth = true
			targetHealthIndex = index
		}
	}
	if !targetStruck {
		t.Logf("missing target SMStruck frame, frames=%d", len(targetFrames))
	}
	if !targetHealth {
		t.Logf("missing target SMHealthSpellChanged frame, frames=%d", len(targetFrames))
	}
	if targetHealth && targetStruck && targetHealthIndex > targetStruckIndex {
		t.Fatalf("target health frame index = %d, struck frame index = %d; want health first", targetHealthIndex, targetStruckIndex)
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
	first, err := s.world.SpawnMonsterByNameAt(mapID, targetX, targetY, "黑色恶蛆1", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() first error = %v", err)
	}
	if len(first.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() first monsters = %d, want 1", len(first.Monsters))
	}
	second, err := s.world.SpawnMonsterByNameAt(mapID, targetX+1, targetY, "黑色恶蛆1", 1)
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
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 0, Tag: 23})
		tickResult, err := s.world.Tick(s.PlayerSnapshots(), time.Now().Add(time.Second))
		if err != nil {
			t.Errorf("Tick() error = %v", err)
			return
		}
		s.applyWorldTick(tickResult, time.Now())
	}()

	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh

	var casterAck int
	var casterMonsterHits int
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
			casterMonsterHits++
			if cmd.Recog != world.MonsterActorID(first.Monsters[0]) && cmd.Recog != world.MonsterActorID(second.Monsters[0]) {
				t.Fatalf("caster struck recog = %d, want one of target actors", cmd.Recog)
			}
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
	if casterMonsterHits != 2 {
		t.Fatalf("caster struck count = %d, want 2", casterMonsterHits)
	}

	var observerStruck int
	var observerMonsterHits int
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			observerStruck++
			observerMonsterHits++
			if cmd.Recog != world.MonsterActorID(first.Monsters[0]) && cmd.Recog != world.MonsterActorID(second.Monsters[0]) {
				t.Fatalf("observer struck recog = %d, want one of target actors", cmd.Recog)
			}
		}
	}
	if observerStruck < 2 {
		t.Fatalf("observer SMStruck count = %d, want at least 2", observerStruck)
	}
	if observerMonsterHits != 2 {
		t.Fatalf("observer struck count = %d, want 2", observerMonsterHits)
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
	first, err := s.world.SpawnMonsterByNameAt(mapID, x+1, y, "黑色恶蛆1", 1)
	if err != nil {
		t.Fatalf("SpawnMonsterByNameAt() first error = %v", err)
	}
	if len(first.Monsters) != 1 {
		t.Fatalf("SpawnMonsterByNameAt() first monsters = %d, want 1", len(first.Monsters))
	}
	second, err := s.world.SpawnMonsterByNameAt(mapID, x+4, y, "黑色恶蛆1", 1)
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
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 0, Tag: 9})
	}()
	<-done
	tickResult, err := s.world.Tick(s.PlayerSnapshots(), time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	s.applyWorldTick(tickResult, time.Now())

	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh

	var casterAck int
	var casterMonsterHits int
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
			casterMonsterHits++
			if cmd.Recog != world.MonsterActorID(first.Monsters[0]) && cmd.Recog != world.MonsterActorID(second.Monsters[0]) {
				t.Fatalf("caster struck recog = %d, want one of target actors", cmd.Recog)
			}
		}
	}
	if casterAck != 1 {
		t.Fatalf("caster ack count = %d, want 1", casterAck)
	}
	if casterMonsterHits != 2 {
		t.Fatalf("caster struck count = %d, want 2", casterMonsterHits)
	}

	var observerStruck int
	var observerMonsterHits int
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			observerStruck++
			observerMonsterHits++
			if cmd.Recog != world.MonsterActorID(first.Monsters[0]) && cmd.Recog != world.MonsterActorID(second.Monsters[0]) {
				t.Fatalf("observer struck recog = %d, want one of target actors", cmd.Recog)
			}
		}
	}
	if observerStruck < 2 {
		t.Fatalf("observer SMStruck count = %d, want at least 2", observerStruck)
	}
	if observerMonsterHits != 2 {
		t.Fatalf("observer struck count = %d, want 2", observerMonsterHits)
	}

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
	target.ShowHPUntil = time.Now().Add(time.Minute).UnixNano()
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
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 0, Tag: 10})
	}()
	<-done
	tickResult, err := s.world.Tick(s.PlayerSnapshots(), time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	s.applyWorldTick(tickResult, time.Now())

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
	if casterStruck != 3 {
		t.Fatalf("caster SMStruck count = %d, want 3 for two monsters and one character", casterStruck)
	}
	wantCasterHealth := 1
	if cost > 0 {
		wantCasterHealth = 2
	}
	if casterHealth < wantCasterHealth {
		t.Fatalf("caster SMHealthSpellChanged count = %d, want at least %d", casterHealth, wantCasterHealth)
	}

	var targetSpell, targetStruck, targetHealth bool
	for _, frame := range targetFrames {
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode target frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			targetSpell = true
			if int(cmd.Param) != targetX || int(cmd.Tag) != targetY || string(body) != fmt.Sprint(10) {
				t.Fatalf("target SMSpell coordinates = (%d,%d), want requested (%d,%d)", cmd.Param, cmd.Tag, targetX, targetY)
			}
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

	var observerStruck bool
	var observerHealth int
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			observerStruck = true
		case mir176.SMHealthSpellChanged:
			if cmd.Recog == world.CharacterActorID(target) {
				observerHealth++
			}
		}
	}
	if !observerStruck {
		t.Fatal("missing observer SMStruck frame")
	}
	if observerHealth != 1 {
		t.Fatalf("observer target SMHealthSpellChanged count = %d, want one", observerHealth)
	}

}

func TestHandleSpellParalysisBroadcastsSpellOnly(t *testing.T) {
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
	caster.Skills = storage.SkillStates{{ID: "困魔咒", Level: 5, Train: 0}}
	caster.EquippedItems = map[int]storage.UserItem{world.SlotBujuk: {ItemID: "护身符", Dura: 10000}}
	skill, ok := s.world.Skill("困魔咒")
	if !ok {
		t.Fatalf("skill 困魔咒 missing from config")
	}
	magicID, ok := s.world.MagicIDByName("困魔咒")
	if !ok {
		t.Fatal("MagicIDByName() missing 困魔咒")
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
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 0, Tag: magicID})
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
	wantCasterHealth := 0
	if cost > 0 {
		wantCasterHealth = 1
	}
	if casterHealth != wantCasterHealth {
		t.Fatalf("caster SMHealthSpellChanged count = %d, want %d", casterHealth, wantCasterHealth)
	}

	var observerSpell, observerNameColor int
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			observerSpell++
		case mir176.SMChangeNameColor:
			if cmd.Param != 0x7D {
				t.Fatalf("observer SMChangeNameColor param = %d, want 125", cmd.Param)
			}
			observerNameColor++
		}
	}
	if observerSpell != 1 {
		t.Fatalf("observer SMSpell count = %d, want 1", observerSpell)
	}
	if observerNameColor != 0 {
		t.Fatalf("observer SMChangeNameColor count = %d, want 0", observerNameColor)
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
	ch.EquippedItems = map[int]storage.UserItem{world.SlotBujuk: {ItemID: "护身符", Dura: 10000, DuraMax: 10000}}
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
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 0, Tag: 17})
	}()
	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh
	descLen := len(EncodeBuffer(make([]byte, 8)))
	var casterAck, casterTurn, casterFeature, casterHealth, casterFire bool
	casterOrder := make([]uint16, 0, len(casterFrames))
	durabilityIndex := -1
	turnIndex := -1
	var observerSpell, observerTurn, observerFeature bool
	for index, frame := range casterFrames {
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
		casterOrder = append(casterOrder, cmd.Ident)
		switch cmd.Ident {
		case mir176.SMSpell:
			if got := string(body); got != "17" {
				t.Fatalf("caster spell body = %q, want magic id 17", got)
			}
		case mir176.SMTurn:
			casterTurn = true
			turnIndex = index
			if len(body) <= descLen {
				t.Fatalf("caster summon turn body len = %d, want > %d", len(body), descLen)
			}
			namePayload, err := mir176.DecodePlain6Payload(body[descLen:])
			if err != nil {
				t.Fatalf("decode caster summon monster name error = %v", err)
			}
			if got := DecodeString(namePayload); got != "骷髅(tester)/255" {
				t.Fatalf("caster summon monster name = %q, want %q", got, "骷髅(tester)/255")
			}
		case mir176.SMFeatureChanged:
			casterFeature = true
		case mir176.SMHealthSpellChanged:
			casterHealth = true
		case mir176.SMDuraChange:
			durabilityIndex = index
			if cmd.Recog != 9900 || cmd.Param != uint16(world.SlotBujuk) || cmd.Tag != 10000 || cmd.Series != 0 {
				t.Fatalf("caster summon durability command = %+v, want dura=9900 slot=%d duramax=10000", cmd, world.SlotBujuk)
			}
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
				t.Fatalf("observer spell body = %q, want magic id 17", got)
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
			if got := DecodeString(namePayload); got != "骷髅(tester)/255" {
				t.Fatalf("observer summon monster name = %q, want %q", got, "骷髅(tester)/255")
			}
		case mir176.SMFeatureChanged:
			observerFeature = true
		}
	}
	if !casterAck || !casterTurn || !casterFeature || (cost > 0 && !casterHealth) || !casterFire {
		t.Fatalf("caster frames missing: ack=%v turn=%v feature=%v health=%v fire=%v", casterAck, casterTurn, casterFeature, casterHealth, casterFire)
	}
	if !observerSpell || !observerTurn || !observerFeature {
		t.Fatalf("observer frames missing: spell=%v turn=%v feature=%v", observerSpell, observerTurn, observerFeature)
	}
	if durabilityIndex < 0 || turnIndex < 0 || durabilityIndex >= turnIndex {
		t.Fatalf("caster summon packet order = %+v, want durability before summon turn", casterOrder)
	}
	<-done
	if ch.MP != cost+10-cost {
		t.Fatalf("MP = %d, want %d after summon", ch.MP, cost+10-cost)
	}
}

func TestHandleSpellSummonFailureKeepsSpellStartForVisibleClients(t *testing.T) {
	s := newDataDirTestServer(t, testConfigsDir)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	caster.Level = 19
	caster.Skills = storage.SkillStates{{ID: "召唤神兽", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("召唤神兽")
	if !ok {
		t.Fatal("skill 召唤神兽 missing from config")
	}
	caster.MP = s.world.SpellCost(skill, caster.Skills[0]) + 10
	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, x+4, y)
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

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Tag: 30})
	}()

	observerCommands := make([]mir176.Command, 0, 1)
	for len(observerCommands) < 1 {
		frame := readFrame(t, observerClient)
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		observerCommands = append(observerCommands, cmd)
	}
	if observerCommands[0].Ident != mir176.SMSpell {
		t.Fatalf("observer failure commands = %+v, want spell only", observerCommands)
	}
	failureFrame, ok := readFrameWithTimeout(t, observerClient, time.Second)
	if !ok {
		t.Fatal("observer missing magic fire failure")
	}
	failureCommand, _, err := decodeMessageLikeClient(failureFrame)
	if err != nil {
		t.Fatalf("decode observer failure frame error = %v", err)
	}
	if failureCommand.Ident != mir176.SMMagicFireFail || failureCommand.Recog != world.CharacterActorID(caster) {
		t.Fatalf("observer failure command = %+v, want SM_MAGICFIRE_FAIL for caster", failureCommand)
	}

	seenFail, seenAck := false, false
	for !seenAck {
		frame := readFrame(t, client)
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			seenAck = true
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		if cmd.Ident == mir176.SMMagicFireFail {
			seenFail = true
		}
		if cmd.Ident == mir176.SMMagicFire {
			t.Fatal("summon failure emitted magic fire")
		}
	}
	if !seenFail {
		t.Fatal("caster did not receive magic fire failure")
	}
	<-done
}

func TestHandleSpellTamingRefreshesMonsterUsernameBroadcast(t *testing.T) {
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
		targetID := world.MonsterActorID(mon)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x+1) | uint32(y)<<16), Param: uint16(targetID), Series: uint16(uint32(targetID) >> 16), Tag: 20})
	}()

	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh

	var casterAck, casterNameColorSeen, casterUsernameSeen, casterHealthSeen, casterMagicSeen bool
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
		case mir176.SMChangeNameColor:
			if cmd.Recog != world.MonsterActorID(mon) {
				t.Fatalf("caster name color recog = %d, want monster actor %d", cmd.Recog, world.MonsterActorID(mon))
			}
			casterNameColorSeen = true
		case mir176.SMUserName:
			if cmd.Recog != world.MonsterActorID(mon) {
				t.Fatalf("caster username recog = %d, want monster actor %d", cmd.Recog, world.MonsterActorID(mon))
			}
			casterUsernameSeen = true
		case mir176.SMHealthSpellChanged:
			casterHealthSeen = true
		case mir176.SMMagicFire:
			casterMagicSeen = true
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	if !casterAck || casterNameColorSeen || !casterUsernameSeen || (cost > 0 && !casterHealthSeen) || !casterMagicSeen {
		ids := make([]uint16, 0, len(casterFrames))
		for _, frame := range casterFrames {
			if cmd, _, err := decodeMessageLikeClient(frame); err == nil {
				ids = append(ids, cmd.Ident)
			}
		}
		t.Fatalf("caster frames mismatch ack/no-name-color/username/health/magic: ack=%v name-color=%v username=%v health=%v magic=%v ids=%v", casterAck, casterNameColorSeen, casterUsernameSeen, casterHealthSeen, casterMagicSeen, ids)
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
	var observerUsernameCmd mir176.Command
	var observerUsernameBody []byte
	var observerUsernameSeen bool
	for _, frame := range observerFrames[1:] {
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMChangeNameColor:
		case mir176.SMUserName:
			observerUsernameCmd = cmd
			observerUsernameBody = body
			observerUsernameSeen = true
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected observer frame ident=%d", cmd.Ident)
		}
	}
	if !observerUsernameSeen {
		t.Fatal("missing observer username frame")
	}
	if observerUsernameCmd.Recog != world.MonsterActorID(mon) {
		t.Fatalf("observer username recog = %d, want monster actor %d", observerUsernameCmd.Recog, world.MonsterActorID(mon))
	}
	name, err := mir176.DecodePlain6Payload(observerUsernameBody)
	if err != nil {
		t.Fatalf("decode observer username error = %v", err)
	}
	decodedName, err := simplifiedchinese.GB18030.NewDecoder().String(string(name))
	if err != nil {
		t.Fatalf("decode observer username GB18030 error = %v", err)
	}
	if decodedName != mon.Name+"("+caster.Name+")" {
		t.Fatalf("observer username = %q, want %q", decodedName, mon.Name+"("+caster.Name+")")
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
	casterFrames := make(chan [][]byte, 1)
	targetFrames := make(chan [][]byte, 1)
	go func() { casterFrames <- collectFrames(client, 6) }()
	go func() { targetFrames <- collectFrames(targetClient, 4) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		targetID := world.CharacterActorID(target)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(target.X) | uint32(target.Y)<<16), Param: uint16(targetID), Series: uint16(uint32(targetID) >> 16), Tag: 28})
	}()

	casterCollected := <-casterFrames
	targetCollected := <-targetFrames

	var sawCasterAck, sawCasterHealth, sawCasterMagic bool
	for _, frame := range casterCollected {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			sawCasterAck = true
			continue
		}
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSendMyMagic:
		case mir176.SMHealthSpellChanged:
			sawCasterHealth = true
		case mir176.SMMagicFire:
			sawCasterMagic = true
		default:
			t.Fatalf("unexpected caster frame ident=%d body=%q", cmd.Ident, string(body))
		}
	}
	if !sawCasterAck {
		t.Fatal("missing action ack")
	}
	if cost > 0 && !sawCasterHealth {
		t.Fatal("missing caster SMHealthSpellChanged frame")
	}
	if !sawCasterMagic {
		t.Fatal("missing caster SMMagicFire frame")
	}
	var sawTargetSpell, sawTargetHealth bool
	for _, frame := range targetCollected {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode target frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			sawTargetSpell = true
		case mir176.SMMagicFire:
		case mir176.SMHealthSpellChanged:
			sawTargetHealth = true
		default:
			t.Fatalf("unexpected target frame ident=%d", cmd.Ident)
		}
	}
	if !sawTargetSpell {
		t.Fatal("missing target SMSpell frame")
	}
	if sawTargetHealth {
		t.Fatal("unexpected target SMHealthSpellChanged frame before delayed SMOpenHealth")
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

	collectFrames := func(conn net.Conn, capHint int) [][]byte {
		frames := make([][]byte, 0, capHint)
		for {
			frame, ok := readFrameWithTimeout(t, conn, 200*time.Millisecond)
			if !ok {
				break
			}
			frames = append(frames, frame)
		}
		return frames
	}

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		targetID := world.CharacterActorID(targetA)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetA.X) | uint32(targetA.Y)<<16), Param: uint16(targetID), Series: uint16(uint32(targetID) >> 16), Tag: 28})
	}()
	casterFramesCh := make(chan [][]byte, 1)
	targetAFramesCh := make(chan [][]byte, 1)
	targetBFramesCh := make(chan [][]byte, 1)
	go func() { casterFramesCh <- collectFrames(client, 8) }()
	go func() { targetAFramesCh <- collectFrames(targetAClient, 3) }()
	go func() { targetBFramesCh <- collectFrames(targetBClient, 2) }()
	<-firstDone
	_ = <-casterFramesCh
	_ = <-targetAFramesCh
	_ = <-targetBFramesCh
	spellClient := s.clientForConn(server)
	spellClient.mu.Lock()
	spellActionAt := spellClient.spellActionAt
	spellActionInterval := spellClient.spellActionInterval
	spellClient.mu.Unlock()
	if spellActionAt.IsZero() || spellActionInterval <= 0 {
		t.Fatalf("spell action state = %v, %v; want initialized after first cast", spellActionAt, spellActionInterval)
	}
	caster.Skills[0].LastCastAt = time.Now().UnixMilli()

	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		targetID := world.CharacterActorID(targetB)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetB.X) | uint32(targetB.Y)<<16), Param: uint16(targetID), Series: uint16(uint32(targetID) >> 16), Tag: 28})
	}()
	if frame, ok := readFrameWithTimeout(t, client, 200*time.Millisecond); ok {
		t.Fatalf("unexpected immediate frame before delayed spell: %x", frame)
	}
	<-secondDone
	readDelayedObserver := func(conn net.Conn) <-chan []byte {
		frames := make(chan []byte, 1)
		go func() {
			frame, ok := readFrameWithTimeout(t, conn, 2*time.Second)
			if ok {
				frames <- frame
				return
			}
			frames <- nil
		}()
		return frames
	}
	targetAFrame := readDelayedObserver(targetAClient)
	targetBFrame := readDelayedObserver(targetBClient)
	var casterFrames [][]byte
	wantCasterFrames := 1
	if cost > 0 {
		wantCasterFrames++
	}
	for len(casterFrames) < wantCasterFrames {
		frame, ok := readFrameWithTimeout(t, client, 2*time.Second)
		if !ok {
			t.Fatalf("missing delayed caster frame %d of %d", len(casterFrames)+1, wantCasterFrames)
		}
		casterFrames = append(casterFrames, frame)
	}
	for name, frame := range map[string][]byte{"target-a": <-targetAFrame, "target-b": <-targetBFrame} {
		if frame == nil {
			t.Fatalf("missing delayed %s SMSpell frame", name)
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil || cmd.Ident != mir176.SMSpell {
			t.Fatalf("delayed %s frame = %v, %v; want SMSpell", name, cmd, err)
		}
	}
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
			Characters:              []storage.Character{updated},
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
			Characters:             []storage.Character{updated},
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
	caster.EquippedItems = map[int]storage.UserItem{world.SlotBujuk: {ItemID: "护身符", Dura: 10000}}
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
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(caster.X) | uint32(caster.Y)<<16), Param: 0, Tag: 18})
	}()

	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh
	var sawAck, sawState, sawHealth bool
	spellIdx := -1
	magicIdx := -1
	stateIdx := -1
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
		case mir176.SMSpell:
			if spellIdx < 0 {
				spellIdx = i
			}
		case mir176.SMSpacemoveShow:
			sawState = true
			if stateIdx < 0 {
				stateIdx = i
			}
			assertCharDesc(t, body, s.world.HumanFeatureForCharacter(caster), 2)
		case mir176.SMCharStatusChanged:
			sawState = true
			if stateIdx < 0 {
				stateIdx = i
			}
		case mir176.SMHealthSpellChanged:
			sawHealth = true
		case mir176.SMMagicFire:
			if magicIdx < 0 {
				magicIdx = i
			}
		case mir176.SMMagicLvExp:
		case mir176.SMDelItems:
		case mir176.SMDuraChange:
		default:
			t.Fatalf("unexpected caster frame ident=%d body=%q", cmd.Ident, string(body))
		}
	}
	if !sawAck {
		t.Fatal("missing action ack frame")
	}
	if !sawState {
		t.Fatal("missing caster state change frame")
	}
	if cost > 0 && !sawHealth {
		t.Fatal("missing SMHealthSpellChanged frame")
	}
	if magicIdx >= 0 && stateIdx >= 0 && magicIdx < stateIdx {
		t.Fatalf("SMMagicFire arrived before character state: magic=%d state=%d", magicIdx, stateIdx)
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
		case mir176.SMCharStatusChanged:
			sawObserverState = true
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
	caster.EquippedItems = map[int]storage.UserItem{world.SlotBujuk: {ItemID: "护身符", Dura: 10000}}
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
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 0, Tag: 19})
	}()
	go func() {
		<-done
		result, err := s.world.Tick(s.PlayerSnapshots(), time.Now().Add(time.Second))
		if err == nil {
			s.applyWorldTick(result, time.Now())
		}
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
		case mir176.SMSpell:
		case mir176.SMTurn, mir176.SMWalk, mir176.SMRun, mir176.SMSitDown:
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
		case mir176.SMFeatureChanged:
			casterState[cmd.Recog] = true
		case mir176.SMUserName:
			casterState[cmd.Recog] = true
		case mir176.SMHealthSpellChanged:
			// expected: self and friend both receive health refreshes, caster gets one extra final refresh
		case mir176.SMCharStatusChanged:
			casterState[cmd.Recog] = true
		case mir176.SMMagicFire:
		case mir176.SMMagicLvExp:
		case mir176.SMDelItems:
		case mir176.SMDuraChange:
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
		case mir176.SMTurn, mir176.SMWalk, mir176.SMRun, mir176.SMSitDown:
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
		case mir176.SMFeatureChanged:
			friendState[cmd.Recog] = true
		case mir176.SMUserName:
			friendState[cmd.Recog] = true
		case mir176.SMCharStatusChanged:
			friendState[cmd.Recog] = true
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
	if casterClient, ok := s.ClientByCharacterID(caster.ID); !ok || casterClient.ch.TransparentUntil == 0 {
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
	caster.EquippedItems = map[int]storage.UserItem{world.SlotBujuk: {ItemID: "护身符(大)", Dura: 20000}}
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
	go func() { casterFramesCh <- collectFrames(client, 8) }()
	go func() { friendFramesCh <- collectFrames(friendClient, 4) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 0, Tag: 15})
	}()

	casterFrames := <-casterFramesCh
	friendFrames := <-friendFramesCh

	var casterHealthCount, casterAbilityCount, casterStatusCount int
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
		case mir176.SMHealthSpellChanged:
			casterHealthCount++
		case mir176.SMAbility:
			casterAbilityCount++
		case mir176.SMCharStatusChanged:
			casterStatusCount++
		case mir176.SMSystemMessage:
		case mir176.SMMagicFire:
		case mir176.SMDelItems:
		case mir176.SMDuraChange:
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	wantCasterHealthCount := 0
	if cost > 0 {
		wantCasterHealthCount = 1
	}
	if casterHealthCount != wantCasterHealthCount {
		t.Fatalf("caster SMHealthSpellChanged count = %d, want %d", casterHealthCount, wantCasterHealthCount)
	}
	if casterAbilityCount != 1 {
		t.Fatalf("caster SMAbility count = %d, want 1", casterAbilityCount)
	}
	if casterStatusCount != 0 {
		t.Fatalf("caster SMCharStatusChanged count = %d, want 0", casterStatusCount)
	}

	friendSpellCount, friendHealthCount, friendAbilityCount, friendStatusCount := 0, 0, 0, 0
	for _, frame := range friendFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			continue
		}
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode friend frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			friendSpellCount++
			if got := string(body); got != "15" {
				t.Fatalf("friend spell body = %q, want magic id 15", got)
			}
		case mir176.SMHealthSpellChanged:
			friendHealthCount++
		case mir176.SMAbility:
			friendAbilityCount++
		case mir176.SMCharStatusChanged:
			friendStatusCount++
		case mir176.SMSystemMessage:
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected friend frame ident=%d", cmd.Ident)
		}
	}
	if friendSpellCount != 1 {
		t.Fatalf("friend SMSpell count = %d, want 1", friendSpellCount)
	}
	if friendHealthCount != 0 {
		t.Fatalf("friend SMHealthSpellChanged count = %d, want 0", friendHealthCount)
	}
	if friendAbilityCount != 1 {
		t.Fatalf("friend SMAbility count = %d, want 1", friendAbilityCount)
	}
	if friendStatusCount != 0 {
		t.Fatalf("friend SMCharStatusChanged count = %d, want 0", friendStatusCount)
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

func TestHandleSpellProtectionStatusTargetsOnlyAffectedCharacter(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	target, err := s.world.CreateCharacterWithAppearance("test", "target", "wizard", 0, 0, mapID, x+1, y)
	if err != nil {
		t.Fatalf("CreateCharacter() target error = %v", err)
	}
	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, x+2, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	target.DefenceUpUntil = time.Now().Add(time.Minute).UnixNano()
	targetServer, targetClient := net.Pipe()
	defer targetServer.Close()
	defer targetClient.Close()
	observerServer, observerClient := net.Pipe()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(targetServer, target)
	defer s.unregisterClient(targetServer)
	s.registerClient(observerServer, observer)
	defer s.unregisterClient(observerServer)

	s.handleSpellEvent(targetServer, &caster, "神圣战甲术", data.StdSkill{}, world.SpellEvent{
		Kind:                    world.SpellEventAffectedCharacter,
		Character:               target,
		SendAbility:             true,
		SendStatus:              true,
		SuppressStatusBroadcast: true,
	})
	targetFrame, ok := readFrameWithTimeout(t, targetClient, time.Second)
	if !ok {
		t.Fatal("target did not receive status frame")
	}
	targetCommand, _, err := decodeMessageLikeClient(targetFrame)
	if err != nil {
		t.Fatalf("decode target frame error = %v", err)
	}
	if targetCommand.Ident != mir176.SMCharStatusChanged {
		t.Fatalf("target frame ident = %d, want SM_CHARSTATUSCHANGED (%d)", targetCommand.Ident, mir176.SMCharStatusChanged)
	}
	targetAbilityFrame, ok := readFrameWithTimeout(t, targetClient, time.Second)
	if !ok {
		t.Fatal("target did not receive ability frame")
	}
	targetAbilityCommand, _, err := decodeMessageLikeClient(targetAbilityFrame)
	if err != nil {
		t.Fatalf("decode target ability frame error = %v", err)
	}
	if targetAbilityCommand.Ident != mir176.SMAbility {
		t.Fatalf("target ability frame ident = %d, want SM_ABILITY (%d)", targetAbilityCommand.Ident, mir176.SMAbility)
	}
	if frame, ok := readFrameWithTimeout(t, observerClient, 100*time.Millisecond); ok {
		command, _, err := decodeMessageLikeClient(frame)
		if err == nil && command.Ident == mir176.SMCharStatusChanged {
			t.Fatal("observer received affected character status frame")
		}
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
	caster.EquippedItems = map[int]storage.UserItem{world.SlotBujuk: {ItemID: "护身符", Dura: 10000}}
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
	go func() { casterFramesCh <- collectFrames(client, 8) }()
	go func() { friendFramesCh <- collectFrames(friendClient, 4) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 0, Tag: 14})
	}()

	casterFrames := <-casterFramesCh
	friendFrames := <-friendFramesCh

	var casterHealthCount, casterAbilityCount, casterStatusCount int
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
		case mir176.SMHealthSpellChanged:
			casterHealthCount++
		case mir176.SMAbility:
			casterAbilityCount++
		case mir176.SMCharStatusChanged:
			casterStatusCount++
		case mir176.SMSystemMessage:
		case mir176.SMMagicFire:
		case mir176.SMDelItems:
		case mir176.SMDuraChange:
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	wantCasterHealthCount := 0
	if cost > 0 {
		wantCasterHealthCount = 1
	}
	if casterHealthCount != wantCasterHealthCount {
		t.Fatalf("caster SMHealthSpellChanged count = %d, want %d", casterHealthCount, wantCasterHealthCount)
	}
	if casterAbilityCount != 1 {
		t.Fatalf("caster SMAbility count = %d, want 1", casterAbilityCount)
	}
	if casterStatusCount != 0 {
		t.Fatalf("caster SMCharStatusChanged count = %d, want 0", casterStatusCount)
	}

	friendSpellCount, friendHealthCount, friendAbilityCount, friendStatusCount := 0, 0, 0, 0
	for _, frame := range friendFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			continue
		}
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode friend frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			friendSpellCount++
			if got := string(body); got != "14" {
				t.Fatalf("friend spell body = %q, want magic id 14", got)
			}
		case mir176.SMHealthSpellChanged:
			friendHealthCount++
		case mir176.SMAbility:
			friendAbilityCount++
		case mir176.SMCharStatusChanged:
			friendStatusCount++
		case mir176.SMSystemMessage:
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected friend frame ident=%d", cmd.Ident)
		}
	}
	if friendSpellCount != 1 {
		t.Fatalf("friend SMSpell count = %d, want 1", friendSpellCount)
	}
	if friendHealthCount != 0 {
		t.Fatalf("friend SMHealthSpellChanged count = %d, want 0", friendHealthCount)
	}
	if friendAbilityCount != 1 {
		t.Fatalf("friend SMAbility count = %d, want 1", friendAbilityCount)
	}
	if friendStatusCount != 0 {
		t.Fatalf("friend SMCharStatusChanged count = %d, want 0", friendStatusCount)
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
	casterFrames := make(chan [][]byte, 1)
	friendFrames := make(chan [][]byte, 1)
	go func() { casterFrames <- collectFrames(client) }()
	go func() { friendFrames <- collectFrames(friendClient) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 0, Tag: 29})
	}()

	casterCollected := <-casterFrames
	friendCollected := <-friendFrames
	tickResult, err := s.world.Tick([]world.PlayerSnapshot{{Character: caster}, {Character: friend}}, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	for _, updated := range tickResult.Characters {
		switch updated.ID {
		case caster.ID:
			caster = updated
		case friend.ID:
			friend = updated
			s.updateClientByCharacterID(updated)
		}
	}

	var casterAck, casterHealth, casterMagic bool
	for _, frame := range casterCollected {
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
		case mir176.SMMagicFire:
			casterMagic = true
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	if !casterAck {
		t.Fatal("missing caster action ack")
	}
	if cost > 0 && !casterHealth {
		t.Fatal("missing caster SMHealthSpellChanged frame")
	}
	if !casterMagic {
		t.Fatal("missing caster SMMagicFire frame")
	}

	friendSpellCount := 0
	for _, frame := range friendCollected {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode friend frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			friendSpellCount++
		case mir176.SMMagicFire:
			continue
		default:
			t.Fatalf("unexpected friend frame ident=%d", cmd.Ident)
		}
	}
	if friendSpellCount != 1 {
		t.Fatalf("friend SMSpell count = %d, want 1", friendSpellCount)
	}
	<-done
	if caster.HP <= 10 {
		t.Fatalf("caster HP = %d, want recovery after delayed healing is consumed", caster.HP)
	}
	if caster.IncHealing != 0 {
		t.Fatalf("caster IncHealing = %d, want recovery queue consumed", caster.IncHealing)
	}
	groupClient, ok := s.ClientByCharacterID(friend.ID)
	if !ok {
		t.Fatal("friend client missing")
	}
	if groupClient.ch.HP <= 12 {
		t.Fatalf("friend HP = %d, want recovery after delayed healing is consumed", groupClient.ch.HP)
	}
	if groupClient.ch.IncHealing != 0 {
		t.Fatalf("friend IncHealing = %d, want recovery queue consumed", groupClient.ch.IncHealing)
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
	go func() { casterFramesCh <- collectFrames(client, 8) }()
	go func() { observerFramesCh <- collectFrames(observerClient, 4) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x+2) | uint32(y)<<16), Param: 0, Tag: 22})
	}()

	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh

	// Ground events become visible during the world visibility scan, not in the
	// synchronous spell result.
	for _, frame := range casterFrames {
		if cmd, _, err := decodeMessageLikeClient(frame); err == nil && cmd.Ident == mir176.SMShowEvent {
			t.Fatal("caster received ground event during spell result")
		}
	}
	for _, frame := range observerFrames {
		if cmd, _, err := decodeMessageLikeClient(frame); err == nil && cmd.Ident == mir176.SMShowEvent {
			t.Fatal("observer received ground event during spell result")
		}
	}
	tickNow := time.Now()
	tickResult, err := s.world.Tick(s.PlayerSnapshots(), tickNow)
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	casterTickFramesCh := make(chan [][]byte, 1)
	observerTickFramesCh := make(chan [][]byte, 1)
	go func() { casterTickFramesCh <- collectFrames(client, 8) }()
	go func() { observerTickFramesCh <- collectFrames(observerClient, 8) }()
	s.applyWorldTick(tickResult, tickNow)
	casterSpellFrames := casterFrames
	observerSpellFrames := observerFrames
	casterTickFrames := <-casterTickFramesCh
	observerTickFrames := <-observerTickFramesCh

	var sawAck bool
	casterGroundEvents := 0
	var casterGroundCoords [][2]int
	for _, frame := range casterSpellFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			sawAck = true
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
		case mir176.SMShowEvent:
			t.Fatal("caster received ground event during spell result")
		case mir176.SMSendMyMagic:
		case mir176.SMHealthSpellChanged:
		case mir176.SMMagicFire:
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	if !sawAck {
		t.Fatal("missing action ack frame")
	}
	observerGroundEvents := 0
	observerSpellEvents := 0
	observerMagicFireEvents := 0
	for _, frame := range observerSpellFrames {
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			observerSpellEvents++
			if got := string(body); got != "22" {
				t.Fatalf("observer spell body = %q, want magic id 22", got)
			}
		case mir176.SMShowEvent:
			t.Fatal("observer received ground event during spell result")
		case mir176.SMMagicFire:
			observerMagicFireEvents++
		default:
			t.Fatalf("unexpected observer frame ident=%d", cmd.Ident)
		}
	}
	for _, frame := range casterTickFrames {
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster tick frame error = %v", err)
		}
		if cmd.Ident == mir176.SMShowEvent {
			decodedBody, decodeErr := mir176.DecodePlain6Payload(body)
			if decodeErr != nil || cmd.Recog == 0 || cmd.Param != 5 || len(decodedBody) < 2 || binary.LittleEndian.Uint16(decodedBody) != 0 {
				t.Fatalf("caster ground event fields = recog:%d param:%d body:%v, want nonzero ID, type 5, event param 0", cmd.Recog, cmd.Param, body)
			}
			casterGroundEvents++
			casterGroundCoords = append(casterGroundCoords, [2]int{int(cmd.Tag), int(cmd.Series)})
		}
	}
	if casterGroundEvents != 5 {
		t.Fatalf("caster ground event count = %d, want 5", casterGroundEvents)
	}
	for _, frame := range observerTickFrames {
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer tick frame error = %v", err)
		}
		if cmd.Ident == mir176.SMShowEvent {
			decodedBody, decodeErr := mir176.DecodePlain6Payload(body)
			if decodeErr != nil || cmd.Recog == 0 || cmd.Param != 5 || len(decodedBody) < 2 || binary.LittleEndian.Uint16(decodedBody) != 0 {
				t.Fatalf("observer ground event fields = recog:%d param:%d body:%v, want nonzero ID, type 5, event param 0", cmd.Recog, cmd.Param, body)
			}
			observerGroundEvents++
			want := [][2]int{{x + 1, y}, {x + 2, y - 1}, {x + 2, y}, {x + 2, y + 1}, {x + 3, y}}
			if observerGroundEvents <= len(want) && [2]int{int(cmd.Tag), int(cmd.Series)} != want[observerGroundEvents-1] {
				t.Fatalf("observer ground event %d = (%d,%d), want (%d,%d)", observerGroundEvents, cmd.Tag, cmd.Series, want[observerGroundEvents-1][0], want[observerGroundEvents-1][1])
			}
		}
	}
	wantGroundCoords := [][2]int{{x + 1, y}, {x + 2, y - 1}, {x + 2, y}, {x + 2, y + 1}, {x + 3, y}}
	if len(casterGroundCoords) != len(wantGroundCoords) {
		t.Fatalf("caster ground coordinates = %v, want %v", casterGroundCoords, wantGroundCoords)
	}
	for i := range wantGroundCoords {
		if casterGroundCoords[i] != wantGroundCoords[i] {
			t.Fatalf("caster ground event %d = %v, want %v", i+1, casterGroundCoords[i], wantGroundCoords[i])
		}
	}
	if casterGroundEvents != 5 || observerGroundEvents != 5 {
		t.Fatalf("ground event counts = caster:%d observer:%d, want 5 each", casterGroundEvents, observerGroundEvents)
	}
	if observerSpellEvents != 1 {
		t.Fatalf("observer SMSpell count = %d, want 1", observerSpellEvents)
	}
	if observerMagicFireEvents != 1 {
		t.Fatalf("observer SMMagicFire count = %d, want 1", observerMagicFireEvents)
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
	result, err := s.world.SpawnMonsterByNameAt(mapID, targetX, targetY, "黑色恶蛆1", 2)
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
	go func() { casterFramesCh <- collectFrames(client, 8) }()
	go func() { observerFramesCh <- collectFrames(observerClient, 8) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 0, Tag: 33})
		tickResult, err := s.world.Tick(s.PlayerSnapshots(), time.Now().Add(time.Second))
		if err != nil {
			t.Errorf("Tick() error = %v", err)
			return
		}
		s.applyWorldTick(tickResult, time.Now())
	}()

	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh

	var casterAck, casterStruck int
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			casterAck++
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster hit frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMStruck:
			casterStruck++
		case mir176.SMHealthSpellChanged:
		case mir176.SMMagicFire:
		case mir176.SMSpell:
		case mir176.SMMagicLvExp:
		case mir176.SMWalk, mir176.SMTurn, mir176.SMFeatureChanged:
		case mir176.SMWinExp, mir176.SMLevelUp:
		case mir176.SMUserName, mir176.SMChangeNameColor:
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	if casterAck != 1 {
		t.Fatalf("caster ack count = %d, want 1", casterAck)
	}
	if casterStruck != 2 {
		t.Fatalf("caster SMStruck count = %d, want 2", casterStruck)
	}

	var sawObserverSpell bool
	var observerStruck int
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			sawObserverSpell = true
		case mir176.SMStruck:
			observerStruck++
		case mir176.SMMagicFire:
		case mir176.SMWalk, mir176.SMTurn, mir176.SMFeatureChanged:
		case mir176.SMUserName, mir176.SMChangeNameColor:
		default:
			t.Fatalf("unexpected observer frame ident=%d", cmd.Ident)
		}
	}
	if !sawObserverSpell {
		t.Fatal("missing observer SMSpell frame")
	}
	if observerStruck != 2 {
		t.Fatalf("observer SMStruck count = %d, want 2", observerStruck)
	}

	<-done
	if caster.MP != cost+10-cost {
		t.Fatalf("MP = %d, want %d after ice storm", caster.MP, cost+10-cost)
	}
}

func TestHandleSpellElectricBlizzardReportsCharacterHit(t *testing.T) {
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

	if !mp.Walkable(x, y+1) {
		t.Fatal("could not find clear target tile for electric blizzard test")
	}
	target, err := s.world.CreateCharacterWithAppearance("test", "target", "warrior", 0, 0, mapID, x, y+1)
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
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 0, Tag: 24})
		tickResult, err := s.world.Tick(s.PlayerSnapshots(), time.Now().Add(time.Second))
		if err != nil {
			t.Errorf("Tick() error = %v", err)
			return
		}
		s.applyWorldTick(tickResult, time.Now())
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
	if cost > 0 && casterHealth < 1 {
		t.Fatalf("caster SMHealthSpellChanged count = %d, want at least 1", casterHealth)
	}
	if casterStruck != 1 {
		t.Fatalf("caster SMStruck count = %d, want 1 for character damage", casterStruck)
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
		t.Fatalf("target frames = %d, want SMStruck frame for character damage", len(targetFrames))
	}
	if !targetHealth {
		t.Fatalf("target frames = %d, want SMHealthSpellChanged frame for character damage", len(targetFrames))
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
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Param: 0, Tag: 8})
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
		case mir176.SMSpacemoveShow, mir176.SMSpacemoveShow2, mir176.SMDisappear:
			t.Fatalf("unexpected duplicate caster movement/state frame: %d", cmd.Ident)
		}
	}
	if !casterAck {
		t.Fatal("missing caster action ack")
	}
	if cost > 0 && !casterHealth {
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
		dir := world.Direction(x, y, targetX, targetY)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(dir) | uint32(targetY)<<16), Param: 0, Tag: 27})
	}()

	casterFrames := <-casterFramesCh
	targetFrames := <-targetFramesCh
	observerFrames := <-observerFramesCh

	var casterAck, casterHealth bool
	casterRush, casterFeature, casterStatus := 0, 0, 0
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
		case mir176.SMRush:
			casterRush++
		case mir176.SMFeatureChanged:
			casterFeature++
		case mir176.SMCharStatusChanged:
			casterStatus++
		case mir176.SMHealthSpellChanged:
			casterHealth = true
		}
	}
	if !casterAck {
		t.Fatal("missing caster action ack")
	}
	if cost > 0 && !casterHealth {
		t.Fatal("missing caster SMHealthSpellChanged frame")
	}
	if casterRush == 0 || casterFeature != 0 || casterStatus != 0 {
		t.Fatalf("caster rush/state frames = rush:%d feature:%d status:%d", casterRush, casterFeature, casterStatus)
	}

	targetRush, targetFeature, targetStatus := 0, 0, 0
	for _, frame := range targetFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode target frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMRush:
			targetRush++
		case mir176.SMFeatureChanged:
			targetFeature++
		case mir176.SMCharStatusChanged:
			targetStatus++
		}
		if cmd.Ident == mir176.SMSpacemoveShow || cmd.Ident == mir176.SMSpacemoveShow2 || cmd.Ident == mir176.SMDisappear {
			t.Fatalf("unexpected duplicate target movement/state frame: %d", cmd.Ident)
		}
	}
	if targetRush == 0 || targetFeature != 0 || targetStatus != 0 {
		t.Fatalf("target rush/state frames = rush:%d feature:%d status:%d", targetRush, targetFeature, targetStatus)
	}
	observerRush, observerFeature, observerStatus := 0, 0, 0
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMRush:
			observerRush++
		case mir176.SMFeatureChanged:
			observerFeature++
		case mir176.SMCharStatusChanged:
			observerStatus++
		}
		if cmd.Ident == mir176.SMSpacemoveShow || cmd.Ident == mir176.SMSpacemoveShow2 || cmd.Ident == mir176.SMDisappear {
			t.Fatalf("unexpected duplicate observer movement/state frame: %d", cmd.Ident)
		}
	}
	if observerRush == 0 || observerFeature != 0 || observerStatus != 0 {
		t.Fatalf("observer rush/state frames = rush:%d feature:%d status:%d", observerRush, observerFeature, observerStatus)
	}

	<-done
	if caster.MP != cost+10-cost {
		t.Fatalf("MP = %d, want %d after charge", caster.MP, cost+10-cost)
	}
	if caster.X == x && caster.Y == y {
		t.Fatal("caster position unchanged, want charge movement")
	}
	if got := s.clientForConn(server).character(); got.X != caster.X || got.Y != caster.Y {
		t.Fatalf("client caster position = (%d,%d), want (%d,%d)", got.X, got.Y, caster.X, caster.Y)
	}
}

func TestHandleSpellChargeDoesNotInferDirectionFromCoordinates(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	caster.Level = 100
	caster.Skills = storage.SkillStates{{ID: "野蛮冲撞", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("野蛮冲撞")
	if !ok {
		t.Fatal("skill 野蛮冲撞 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	caster.MP = cost + 1
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(8) | uint32(y)<<16), Tag: 27})
	}()
	body, err := mir176.UnwrapFrame(readFrame(t, client))
	if err != nil || !strings.HasPrefix(string(body), "+GOOD/") {
		t.Fatalf("charge acknowledgement = %q, %v; want +GOOD", body, err)
	}
	<-done
	if caster.X != x || caster.Y != y {
		t.Fatalf("caster moved to (%d,%d), want unchanged (%d,%d)", caster.X, caster.Y, x, y)
	}
}

func TestHandleSpellChargeInsufficientManaOnlyConfirmsAction(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	caster.Level = 100
	caster.Skills = storage.SkillStates{{ID: "野蛮冲撞", Level: 0, Train: 0}}
	skill, ok := s.world.Skill("野蛮冲撞")
	if !ok {
		t.Fatal("skill 野蛮冲撞 missing from config")
	}
	cost := s.world.SpellCost(skill, caster.Skills[0])
	if cost == 0 {
		t.Skip("configured charge skill has no resource cost")
	}
	caster.MP = cost - 1
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(2) | uint32(y)<<16), Tag: 27})
	}()

	body, err := mir176.UnwrapFrame(readFrame(t, client))
	if err != nil || !strings.HasPrefix(string(body), "+GOOD/") {
		t.Fatalf("charge acknowledgement = %q, %v; want +GOOD", body, err)
	}
	if _, ok := readFrameWithTimeout(t, client, 50*time.Millisecond); ok {
		t.Fatal("charge with insufficient mana sent an unexpected failure frame")
	}
	<-done
	if caster.X != x || caster.Y != y {
		t.Fatalf("caster moved to (%d,%d), want unchanged (%d,%d)", caster.X, caster.Y, x, y)
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
	caster.EquippedItems = map[int]storage.UserItem{world.SlotBujuk: {ItemID: "护身符", Dura: 10000}}
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

	targetID := world.CharacterActorID(target)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: uint16(targetID), Series: uint16(uint32(targetID) >> 16), Tag: 13})
	}()
	<-done
	s.hitImpactDelay = 0
	tickResult, err := s.world.Tick(s.PlayerSnapshots(), time.Now().Add(2*time.Second))
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	s.applyWorldTick(tickResult, time.Now())

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
	if cost > 0 && !casterHealth {
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
			if cmd.Recog == world.CharacterActorID(target) {
				targetObserverHealth = true
			}
		}
	}
	if !targetObserverSpell {
		t.Fatal("missing target observer SMStruck frame")
	}
	if targetObserverHealth {
		t.Fatal("unexpected target observer SMHealthSpellChanged frame while target HP is not public")
	}
}

func TestHandleSpellWarriorSkillPreservesMagicInterval(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "warrior", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	ch.Skills = storage.SkillStates{{ID: "基本剑术", Level: 0}}
	server, clientConn := net.Pipe()
	defer server.Close()
	defer clientConn.Close()
	client := s.registerClient(server, ch)
	defer s.unregisterClient(server)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &ch, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x) | uint32(y)<<16), Tag: 3})
	}()
	assertActionAck(t, readFrame(t, clientConn))
	<-done

	client.mu.Lock()
	interval := client.spellActionInterval
	client.mu.Unlock()
	if interval != 0 {
		t.Fatalf("spell action interval = %s, want unchanged zero interval", interval)
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
	caster.EquippedItems = map[int]storage.UserItem{world.SlotBujuk: {ItemID: "护身符", Dura: 10000}}
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
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 0, Tag: 13})
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
				t.Fatalf("SM_MAGICFIRE target = (%d,%d), want (%d,%d)", cmd.Param, cmd.Tag, targetX, targetY)
			}
			if cmd.Series != uint16(makeWord(8, 10)) {
				t.Fatalf("SM_MAGICFIRE series = %d, want effect type/effect 8/10", cmd.Series)
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
	caster.EquippedItems = map[int]storage.UserItem{world.SlotBujuk: {ItemID: "护身符", Dura: 10000}}
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
		observerFramesCh <- [][]byte{readFrame(t, observerClient), readFrame(t, observerClient)}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: 0, Tag: 13})
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
				t.Fatalf("SM_MAGICFIRE target = (%d,%d), want (%d,%d)", cmd.Param, cmd.Tag, targetX, targetY)
			}
			if cmd.Series != uint16(makeWord(8, 10)) {
				t.Fatalf("SM_MAGICFIRE series = %d, want effect type/effect 8/10", cmd.Series)
			}
			casterMagicFire = true
		}
	}
	if !casterAck {
		t.Fatal("missing caster action ack")
	}
	if !casterMagicFire {
		t.Fatal("missing caster SMMagicFire frame")
	}

	var observerSpell, observerMagicFire bool
	for _, frame := range observerFrames {
		cmd, body, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMSpell:
			if cmd.Param != uint16(targetX) || cmd.Tag != uint16(targetY) || string(body) != "13" {
				t.Fatalf("SM_SPELL position = (%d,%d), want (%d,%d)", cmd.Param, cmd.Tag, targetX, targetY)
			}
			if cmd.Series != 10 {
				t.Fatalf("SM_SPELL series = %d, want effect 10", cmd.Series)
			}
			observerSpell = true
		case mir176.SMMagicFire:
			observerMagicFire = true
		}
	}
	if !observerSpell {
		t.Fatal("missing observer SMSpell frame")
	}
	if !observerMagicFire {
		t.Fatal("missing observer SMMagicFire frame")
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
		targetID := world.CharacterActorID(target)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(targetX) | uint32(targetY)<<16), Param: uint16(targetID), Series: uint16(uint32(targetID) >> 16), Tag: 6})
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
	if cost > 0 && !casterHealth {
		t.Fatal("missing caster SMHealthSpellChanged frame")
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
	go func() { casterFramesCh <- collectFrames(client, 8) }()
	go func() { observerFramesCh <- collectFrames(observerClient, 8) }()

	done := make(chan struct{})
	go func() {
		defer close(done)
		targetID := world.MonsterActorID(target)
		s.handleSpell(server, &caster, mir176.Command{Ident: mir176.CMSpell, Recog: int32(uint32(x+2) | uint32(y)<<16), Param: uint16(targetID), Series: uint16(uint32(targetID) >> 16), Tag: 32})
	}()

	casterFrames := <-casterFramesCh
	observerFrames := <-observerFramesCh

	var sawAck, sawStruck, sawDeath, sawDrop, sawWinExp, sawHealth bool
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			sawAck = true
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode caster frame error = %v", err)
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
		case mir176.SMSpell:
		case mir176.SMMagicLvExp:
		default:
			t.Fatalf("unexpected caster frame ident=%d", cmd.Ident)
		}
	}
	if !sawAck {
		t.Fatal("missing action ack frame")
	}
	if sawStruck {
		t.Fatal("unexpected synthetic SMStruck frame during turn undead")
	}
	if cost > 0 && !sawHealth {
		t.Fatal("missing SMHealthSpellChanged frame")
	}
	if sawDeath || sawDrop || sawWinExp {
		t.Fatalf("death-side effects arrived during spell phase: death=%v drop=%v exp=%v", sawDeath, sawDrop, sawWinExp)
	}

	var sawSpell, sawObserverStruck, sawObserverDeath, sawObserverDrop bool
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode observer frame error = %v", err)
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
	if sawObserverStruck {
		t.Fatal("unexpected observer SMStruck frame during turn undead")
	}
	if sawObserverDeath || sawObserverDrop {
		t.Fatalf("observer death-side effects arrived during spell phase: death=%v drop=%v", sawObserverDeath, sawObserverDrop)
	}
	<-done
	tick, err := s.world.Tick(s.PlayerSnapshots(), time.Now())
	if err != nil {
		t.Fatalf("World.Tick() error = %v", err)
	}
	s.applyWorldTick(tick, time.Now())
	casterFrames = collectFrames(client, 8)
	observerFrames = collectFrames(observerClient, 8)
	for _, frame := range casterFrames {
		if body, err := mir176.UnwrapFrame(frame); err == nil && strings.HasPrefix(string(body), "+GOOD/") {
			continue
		}
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode delayed caster frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMNowDeath:
			sawDeath = true
		case mir176.SMItemShow:
			sawDrop = true
		case mir176.SMWinExp:
			sawWinExp = true
		case mir176.SMHealthSpellChanged:
		case mir176.SMMagicLvExp:
		case mir176.SMDisappear:
		default:
			continue
		}
	}
	if !sawDeath || !sawDrop || !sawWinExp {
		t.Fatalf("missing delayed caster death-side effects: death=%v drop=%v exp=%v", sawDeath, sawDrop, sawWinExp)
	}
	for _, frame := range observerFrames {
		cmd, _, err := decodeMessageLikeClient(frame)
		if err != nil {
			t.Fatalf("decode delayed observer frame error = %v", err)
		}
		switch cmd.Ident {
		case mir176.SMNowDeath:
			sawObserverDeath = true
		case mir176.SMItemShow:
			sawObserverDrop = true
		case mir176.SMDisappear:
		default:
			continue
		}
	}
	if !sawObserverDeath || !sawObserverDrop {
		t.Fatalf("missing delayed observer death-side effects: death=%v drop=%v", sawObserverDeath, sawObserverDrop)
	}
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

func TestSpaceMoveSpellEventsUseHide2AndShow2(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "space_move", "wizard", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, caster)
	defer s.unregisterClient(server)

	hideDone := make(chan struct{})
	go func() {
		s.dispatchSpellSpaceMoveFire(caster)
		close(hideDone)
	}()
	hideCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode hide2 frame error = %v", err)
	}
	if hideCmd.Ident != mir176.SMSpacemoveHide2 || hideCmd.Recog != world.CharacterActorID(caster) {
		t.Fatalf("hide2 command = %+v, want actor %d", hideCmd, world.CharacterActorID(caster))
	}
	<-hideDone

	show := caster
	show.X++
	show.Dir = 3
	mapDone := make(chan struct{})
	go func() {
		s.sendSpellSpaceMoveMapChange(server, show)
		close(mapDone)
	}()
	clearCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode clear objects frame error = %v", err)
	}
	if clearCmd.Ident != mir176.SMClearObjects || clearCmd.Recog != world.CharacterActorID(show) {
		t.Fatalf("clear objects command = %+v, want actor %d", clearCmd, world.CharacterActorID(show))
	}
	changeCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode change map frame error = %v", err)
	}
	if changeCmd.Ident != mir176.SMChangeMap || changeCmd.Recog != world.CharacterActorID(show) || int(changeCmd.Param) != show.X || int(changeCmd.Tag) != show.Y {
		t.Fatalf("change map command = %+v, want actor %d at (%d,%d)", changeCmd, world.CharacterActorID(show), show.X, show.Y)
	}
	if changeCmd.Series != uint16(s.world.MapLight(show.MapID)) {
		t.Fatalf("change map series = %d, want map day-bright %d", changeCmd.Series, s.world.MapLight(show.MapID))
	}
	areaCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode area state frame error = %v", err)
	}
	if areaCmd.Ident != mir176.SMAreaState {
		t.Fatalf("area state command = %+v, want SM_AREASTATE", areaCmd)
	}
	mapDescCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode map description frame error = %v", err)
	}
	if mapDescCmd.Ident != mir176.SMMapDescription {
		t.Fatalf("map description command = %+v, want SM_MAPDESCRIPTION", mapDescCmd)
	}
	<-mapDone
	if got := s.clientForConn(server).character(); got.MapID != show.MapID || got.X != show.X || got.Y != show.Y {
		t.Fatalf("client state after space move = %+v, want map %q at (%d,%d)", got, show.MapID, show.X, show.Y)
	}

	showDone := make(chan struct{})
	go func() {
		s.dispatchSpellSpaceMoveShow(show)
		close(showDone)
	}()
	showCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode show2 frame error = %v", err)
	}
	if showCmd.Ident != mir176.SMSpacemoveShow2 || showCmd.Recog != world.CharacterActorID(show) || int(showCmd.Param) != show.X || int(showCmd.Tag) != show.Y {
		t.Fatalf("show2 command = %+v, want actor %d at (%d,%d)", showCmd, world.CharacterActorID(show), show.X, show.Y)
	}
	<-showDone
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

func TestLoginNoticeClientTickUsesSeries(t *testing.T) {
	ch := storage.Character{SoftVersionDate: 0, ClientTick: 0}
	applyLoginNoticeClientTick(&ch, mir176.Command{Ident: mir176.CMLoginNoticeOK, Series: 7})
	if ch.ClientTick != 7 {
		t.Fatalf("ClientTick = %d, want 7", ch.ClientTick)
	}
	if ch.SoftVersionDate != 0 {
		t.Fatalf("SoftVersionDate = %d, want unchanged", ch.SoftVersionDate)
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
	ch.SoftVersionDate = 1
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
	wantSubAbility := SubAbilityCommand(s.world.SubAbilityStats(ch))
	if subAbilityCmd.Recog != wantSubAbility.Recog || subAbilityCmd.Param != wantSubAbility.Param || subAbilityCmd.Tag != wantSubAbility.Tag || subAbilityCmd.Series != wantSubAbility.Series {
		t.Fatalf("SM_SUBABILITY = %+v, want %+v", subAbilityCmd, wantSubAbility)
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
	if featureCmd.Series != uint16(s.world.CharacterFeatureEx(ch)) {
		t.Fatalf("SM_FEATURECHANGED Series = %d, want %d", featureCmd.Series, uint16(s.world.CharacterFeatureEx(ch)))
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
	if featureCmd.Series != uint16(s.world.CharacterFeatureEx(ch)) {
		t.Fatalf("SM_FEATURECHANGED Series = %d, want %d", featureCmd.Series, uint16(s.world.CharacterFeatureEx(ch)))
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
	wantSubAbility := SubAbilityCommand(s.world.SubAbilityStats(ch))
	if subAbilityCmd.Recog != wantSubAbility.Recog || subAbilityCmd.Param != wantSubAbility.Param || subAbilityCmd.Tag != wantSubAbility.Tag || subAbilityCmd.Series != wantSubAbility.Series {
		t.Fatalf("SM_SUBABILITY = %+v, want %+v", subAbilityCmd, wantSubAbility)
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
	wantSubAbility := SubAbilityCommand(s.world.SubAbilityStats(ch))
	if subAbilityCmd.Recog != wantSubAbility.Recog || subAbilityCmd.Param != wantSubAbility.Param || subAbilityCmd.Tag != wantSubAbility.Tag || subAbilityCmd.Series != wantSubAbility.Series {
		t.Fatalf("SM_SUBABILITY = %+v, want %+v", subAbilityCmd, wantSubAbility)
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
	if len(useMagicDecoded) != 84 {
		t.Fatalf("SM_SENDMYMAGIC decoded len = %d, want 84", len(useMagicDecoded))
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
	if useMagicDecoded[23] != 1 || useMagicDecoded[24] != 1 || useMagicDecoded[25] != 0 {
		t.Fatalf("SM_SENDMYMAGIC effect header = % x, want 01 01 00", useMagicDecoded[23:26])
	}
	if got := binary.LittleEndian.Uint16(useMagicDecoded[26:28]); got != 4 {
		t.Fatalf("SM_SENDMYMAGIC spell = %d, want 4", got)
	}
	if got := binary.LittleEndian.Uint16(useMagicDecoded[28:30]); got != 8 {
		t.Fatalf("SM_SENDMYMAGIC power = %d, want 8", got)
	}
	if got := useMagicDecoded[30]; got != 7 {
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
	if got := binary.LittleEndian.Uint32(useMagicDecoded[56:60]); got != 60 {
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
		s.handleMagicKeyChange(server, &ch, mir176.Command{Ident: mir176.CMMagicKeyChange, Recog: 4, Param: uint16('7')})
	}()

	if _, ok := readFrameWithTimeout(t, client, 100*time.Millisecond); ok {
		t.Fatal("unexpected frame after magic key change")
	}
	<-done
	if ch.Skills[0].Hotkey != '7' {
		t.Fatalf("character hotkey = %q, want '7'", ch.Skills[0].Hotkey)
	}
}

func TestInitializeSpellStateOnLoginMatchesReference(t *testing.T) {
	ch := storage.Character{ThrustingDisabled: true, HalfMoonDisabled: false}
	initializeSpellStateOnLogin(&ch)
	if ch.ThrustingDisabled {
		t.Fatal("ThrustingDisabled = true after login, want false")
	}
	if !ch.HalfMoonDisabled {
		t.Fatal("HalfMoonDisabled = false after login, want true")
	}
}

func TestNormalizeAttackIdentRespectsToggleState(t *testing.T) {
	tests := []struct {
		name string
		ch   storage.Character
		in   uint16
		want uint16
	}{
		{name: "long disabled", ch: storage.Character{Skills: storage.SkillStates{{ID: "刺杀剑术"}}, ThrustingDisabled: true}, in: mir176.CMLongHit, want: mir176.CMHit},
		{name: "long enabled", ch: storage.Character{Skills: storage.SkillStates{{ID: "刺杀剑术"}}}, in: mir176.CMLongHit, want: mir176.CMLongHit},
		{name: "wide disabled", ch: storage.Character{Skills: storage.SkillStates{{ID: "半月弯刀"}}, HalfMoonDisabled: true}, in: mir176.CMWideHit, want: mir176.CMHit},
		{name: "wide enabled", ch: storage.Character{Skills: storage.SkillStates{{ID: "半月弯刀"}}}, in: mir176.CMWideHit, want: mir176.CMWideHit},
		{name: "unlearned long", in: mir176.CMLongHit, want: mir176.CMHit},
		{name: "unlearned wide", in: mir176.CMWideHit, want: mir176.CMHit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeAttackIdent(tt.ch, tt.in); got != tt.want {
				t.Fatalf("normalizeAttackIdent() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSendUseMagicDoesNotAppendAttackSkillFlags(t *testing.T) {
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
	if extra, ok := readFrameWithTimeout(t, client, 50*time.Millisecond); ok {
		t.Fatalf("unexpected attack skill flag frame: %x", extra)
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
	cmd := SubAbilityCommand(world.SubAbilityStats{AntiMagic: 1, HitPoint: 5, SpeedPoint: 18})
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
	ch.PKFlag = true
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
	if got := binary.LittleEndian.Uint32(decoded[20:24]); got != 0x2F {
		t.Fatalf("name color = %d, want 0x2f", got)
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

func TestCharacterAppearUsesObserverNameColor(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() observer error = %v", err)
	}
	observer.GuildID = "guild"
	target := observer
	target.ID = "target"
	target.Name = "target"
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, observer)
	defer s.unregisterClient(server)
	viewer := s.clientForConn(server)
	if viewer == nil {
		t.Fatal("missing observer client")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		viewer.sendCharacterAppear(s, target)
	}()
	for i := 0; i < 2; i++ {
		if _, ok := readFrameWithTimeout(t, client, time.Second); !ok {
			t.Fatalf("timed out waiting for character appear frame %d", i)
		}
	}
	cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode character name frame error = %v", err)
	}
	if cmd.Ident != mir176.SMUserName || cmd.Param != 0xB4 {
		t.Fatalf("character name command = %+v, want observer guild color 0xb4", cmd)
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
		s.applyWorldTick(world.TickResult{
			Characters:        []storage.Character{updated},
			HealingCharacters: []string{updated.ID},
		}, now)
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

func TestApplyWorldTickSendsPoisonDeathAfterHealthRefresh(t *testing.T) {
	s := newTestServer(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "poison-dead", "warrior", 0, 0, "D12", 0, 0)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)

	updated := ch
	updated.HP = 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.applyWorldTick(world.TickResult{
			Characters:      []storage.Character{updated},
			CharacterDeaths: []storage.Character{updated},
		}, time.Now())
	}()

	health, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode health frame error = %v", err)
	}
	if health.Ident != mir176.SMHealthSpellChanged {
		t.Fatalf("first frame ident = %d, want SM_HEALTHSPELLCHANGED (%d)", health.Ident, mir176.SMHealthSpellChanged)
	}
	death, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode death frame error = %v", err)
	}
	if death.Ident != mir176.SMNowDeath || death.Recog != world.CharacterActorID(updated) || death.Param != uint16(updated.Dir) || death.Tag != uint16(updated.X) || death.Series != 1 {
		t.Fatalf("death command = %+v, want target actor, direction, x, and immediate marker", death)
	}
	if len(body) == 0 {
		t.Fatal("death body is empty")
	}
	<-done
}

func TestHandleSpellEventBroadcastsFeatureAndStatusChangesTogether(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	caster, err := s.world.CreateCharacterWithAppearance("test", "caster", "taoist", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter() caster error = %v", err)
	}
	observer, err := s.world.CreateCharacterWithAppearance("test", "observer", "wizard", 0, 0, mapID, x+1, y)
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

	previous := caster
	previous.TransparentUntil = time.Now().Add(time.Minute).UnixNano()
	updated := caster
	updated.X++
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleSpellEvent(server, &caster, "", data.StdSkill{}, world.SpellEvent{
			Kind:      world.SpellEventCharacter,
			Character: updated,
			Previous:  previous,
		})
	}()

	feature, _, err := decodeMessageLikeClient(readFrame(t, observerClient))
	if err != nil {
		t.Fatalf("decode feature refresh error = %v", err)
	}
	if feature.Ident != mir176.SMFeatureChanged {
		t.Fatalf("first observer frame ident = %d, want SM_FEATURECHANGED (%d)", feature.Ident, mir176.SMFeatureChanged)
	}
	status, _, err := decodeMessageLikeClient(readFrame(t, observerClient))
	if err != nil {
		t.Fatalf("decode status refresh error = %v", err)
	}
	if status.Ident != mir176.SMCharStatusChanged {
		t.Fatalf("second observer frame ident = %d, want SM_CHARSTATUSCHANGED (%d)", status.Ident, mir176.SMCharStatusChanged)
	}
	<-done
}

func TestApplyWorldTickSendsStatusRefreshForExpiredStealth(t *testing.T) {
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
			Characters:              []storage.Character{updated},
			StatusRefreshCharacters: []storage.Character{updated},
		}, now)
	}()

	refreshCmd, refreshBody, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode status refresh frame error = %v", err)
	}
	if refreshCmd.Ident != mir176.SMCharStatusChanged {
		t.Fatalf("frame ident = %d, want SM_CHARSTATUSCHANGED (%d)", refreshCmd.Ident, mir176.SMCharStatusChanged)
	}
	if refreshCmd.Recog != world.CharacterActorID(updated) {
		t.Fatalf("status refresh = %+v, want actor=%d", refreshCmd, world.CharacterActorID(updated))
	}
	if refreshCmd.Series != uint16(s.world.CharacterStatus(updated)) {
		t.Fatalf("status refresh series = %d, want %d", refreshCmd.Series, uint16(s.world.CharacterStatus(updated)))
	}
	if len(refreshBody) != 0 {
		t.Fatalf("status refresh body len = %d, want 0", len(refreshBody))
	}
	<-done
}

func TestApplyWorldTickSendsReleasedMonsterUsername(t *testing.T) {
	s := newTestServer(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "taoist", 0, 0, "D12", 0, 0)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	mon := world.Monster{ID: "mon-released", Name: "骷髅", MapID: ch.MapID, X: ch.X, Y: ch.Y}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.applyWorldTick(world.TickResult{NameMonsters: []world.Monster{mon}}, time.Now())
	}()

	cmd, body, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode released monster username error = %v", err)
	}
	if cmd.Ident != mir176.SMUserName {
		t.Fatalf("frame ident = %d, want SM_USERNAME (%d)", cmd.Ident, mir176.SMUserName)
	}
	if cmd.Recog != world.MonsterActorID(mon) || cmd.Param != 255 {
		t.Fatalf("released monster username command = %+v, want actor=%d color=255", cmd, world.MonsterActorID(mon))
	}
	decoded, err := mir176.DecodePlain6Payload(body)
	if err != nil {
		t.Fatalf("decode released monster username payload error = %v", err)
	}
	got, err := simplifiedchinese.GB18030.NewDecoder().String(string(decoded))
	if err != nil {
		t.Fatalf("decode released monster username GB18030 error = %v", err)
	}
	if got != mon.Name {
		t.Fatalf("released monster username = %q, want %q", got, mon.Name)
	}
	<-done
}

func TestApplyWorldTickSendsMonsterDeathForMasterLoss(t *testing.T) {
	s := newTestServer(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "taoist", 0, 0, "D12", 0, 0)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	death := world.AttackResult{MonsterID: "mon-dead", MonsterMapID: ch.MapID, MonsterX: ch.X, MonsterY: ch.Y, MonsterMaxHP: 100, Dead: true}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.applyWorldTick(world.TickResult{MonsterDeaths: []world.AttackResult{death}}, time.Now())
	}()

	cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode monster death error = %v", err)
	}
	if cmd.Ident != mir176.SMNowDeath {
		t.Fatalf("frame ident = %d, want SM_NOWDEATH (%d)", cmd.Ident, mir176.SMNowDeath)
	}
	if cmd.Recog != world.MonsterActorID(world.Monster{ID: death.MonsterID}) {
		t.Fatalf("monster death actor = %d, want %d", cmd.Recog, world.MonsterActorID(world.Monster{ID: death.MonsterID}))
	}
	<-done
}

func TestApplyWorldTickDoesNotDuplicateOrderedMonsterHit(t *testing.T) {
	s := newTestServer(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "taoist", 0, 0, "D12", 0, 0)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	hit := world.AttackResult{
		MonsterID: "mon-hit", MonsterMapID: ch.MapID, MonsterX: ch.X, MonsterY: ch.Y,
		MonsterHP: 90, MonsterMaxHP: 100, MonsterRaceImg: 50, Damage: 10,
		Magic: true, Character: ch,
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.applyWorldTick(world.TickResult{
			MonsterHits:        []world.AttackResult{hit},
			OrderedSpellEvents: []world.OrderedSpellEvent{{Kind: world.OrderedSpellEventMonsterHit, MonsterHit: hit}},
		}, time.Now())
	}()
	cmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode monster hit error = %v", err)
	}
	if cmd.Ident != mir176.SMStruck {
		t.Fatalf("monster hit ident = %d, want SM_STRUCK", cmd.Ident)
	}
	if err := client.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	buf := make([]byte, 256)
	if n, err := client.Read(buf); err == nil {
		t.Fatalf("duplicate monster hit frame = %q", buf[:n])
	}
	<-done
}

func TestApplyWorldTickSendsExperienceBeforeMonsterDeath(t *testing.T) {
	s := newTestServer(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "taoist", 0, 0, "D12", 0, 0)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	s.registerClient(server, ch)
	death := world.AttackResult{MonsterID: "mon-exp-dead", MonsterMapID: ch.MapID, MonsterX: ch.X, MonsterY: ch.Y, MonsterMaxHP: 100, Dead: true}
	experience := world.SpellExperience{CharacterID: ch.ID, Experience: 25, CurrentExp: 125, Character: ch}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.applyWorldTick(world.TickResult{MonsterDeaths: []world.AttackResult{death}, SpellExperience: []world.SpellExperience{experience}}, time.Now())
	}()

	expCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode experience error = %v", err)
	}
	if expCmd.Ident != mir176.SMWinExp || expCmd.Recog != int32(experience.CurrentExp) || expCmd.Param != uint16(experience.Experience) {
		t.Fatalf("experience command = %+v, want current=%d gained=%d", expCmd, experience.CurrentExp, experience.Experience)
	}
	deathCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode death error = %v", err)
	}
	if deathCmd.Ident != mir176.SMNowDeath {
		t.Fatalf("death command ident = %d, want SM_NOWDEATH (%d)", deathCmd.Ident, mir176.SMNowDeath)
	}
	<-done
}

func TestApplyWorldTickSendsOneStatusRefreshForExpiredParalysis(t *testing.T) {
	s := newTestServer(t)
	ch, err := s.world.CreateCharacterWithAppearance("test", "tester", "taoist", 0, 0, "D12", 0, 0)
	if err != nil {
		t.Fatalf("CreateCharacter() error = %v", err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ch.ParalyzedUntil = time.Now().Add(-time.Second).UnixNano()
	s.registerClient(server, ch)
	updated := ch
	updated.ParalyzedUntil = 0
	now := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.applyWorldTick(world.TickResult{
			Characters:              []storage.Character{updated},
			StatusRefreshCharacters: []storage.Character{updated},
		}, now)
	}()
	refreshCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode status refresh frame error = %v", err)
	}
	if refreshCmd.Ident != mir176.SMCharStatusChanged {
		t.Fatalf("frame ident = %d, want SM_CHARSTATUSCHANGED (%d)", refreshCmd.Ident, mir176.SMCharStatusChanged)
	}
	if frame, ok := readFrameWithTimeout(t, client, 100*time.Millisecond); ok {
		t.Fatalf("unexpected second status frame: %x", frame)
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

func TestApplyWorldTickSendsStatusThenAbilityRefreshTogether(t *testing.T) {
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
			StatusRefreshCharacters:  []storage.Character{updated},
			AbilityRefreshCharacters: []storage.Character{updated},
		}, now)
	}()

	statusCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode status refresh frame error = %v", err)
	}
	if statusCmd.Ident != mir176.SMCharStatusChanged {
		t.Fatalf("first frame ident = %d, want SM_CHARSTATUSCHANGED (%d)", statusCmd.Ident, mir176.SMCharStatusChanged)
	}
	abiliCmd, _, err := decodeMessageLikeClient(readFrame(t, client))
	if err != nil {
		t.Fatalf("decode ability refresh frame error = %v", err)
	}
	if abiliCmd.Ident != mir176.SMAbility {
		t.Fatalf("second frame ident = %d, want SM_ABILITY (%d)", abiliCmd.Ident, mir176.SMAbility)
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

func assertSystemMessage(t *testing.T, frame []byte, ch storage.Character, wantText string, wantColor uint16) {
	t.Helper()
	cmd, body, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decodeMessageLikeClient() error = %v", err)
	}
	if cmd.Ident != mir176.SMSystemMessage || cmd.Recog != world.CharacterActorID(ch) {
		t.Fatalf("ident/recog = %d/%d, want SM_SYSMESSAGE/%d", cmd.Ident, cmd.Recog, world.CharacterActorID(ch))
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
