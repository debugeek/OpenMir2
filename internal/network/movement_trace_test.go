package network

import (
	"net"
	"testing"

	"openmir2/internal/protocol/mir176"
	"openmir2/internal/world"
)

type movementTraceEntry struct {
	Kind    string
	Ident   uint16
	Recog   int32
	Param   uint16
	Tag     uint16
	Dir     byte
	BodyLen int
	X       int
	Y       int
}

func TestMovementWalkPacketStateTrace(t *testing.T) {
	s := newTestServer(t)
	mapID, x, y := testDefaultSpawn(t)
	actor, err := s.world.CreateCharacterWithAppearance("test", "trace-actor", "warrior", 0, 0, mapID, x, y)
	if err != nil {
		t.Fatalf("CreateCharacter(actor) error = %v", err)
	}
	observer, err := s.world.CreateCharacterWithAppearance("test", "trace-observer", "warrior", 0, 0, mapID, x+2, y)
	if err != nil {
		t.Fatalf("CreateCharacter(observer) error = %v", err)
	}
	actorServer, actorClient := net.Pipe()
	observerServer, observerClient := net.Pipe()
	defer actorServer.Close()
	defer actorClient.Close()
	defer observerServer.Close()
	defer observerClient.Close()
	s.registerClient(actorServer, actor)
	s.registerClient(observerServer, observer)

	trace := []movementTraceEntry{{Kind: "state.before", X: actor.X, Y: actor.Y, Dir: byte(actor.Dir)}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleMove(actorServer, &actor, mir176.Command{
			Ident: mir176.CMWalk,
			Recog: int32(uint32(x+1) | uint32(y)<<16),
			Tag:   2,
		}, false)
	}()

	ack := readFrame(t, actorClient)
	assertActionAck(t, ack)
	trace = append(trace, movementTraceEntry{Kind: "packet.action_ok"})
	frame := readFrame(t, observerClient)
	command, body, err := decodeMessageLikeClient(frame)
	if err != nil {
		t.Fatalf("decode movement trace frame error = %v", err)
	}
	trace = append(trace, movementTraceEntry{
		Kind:    "packet.observer",
		Ident:   command.Ident,
		Recog:   command.Recog,
		Param:   command.Param,
		Tag:     command.Tag,
		Dir:     byte(command.Series),
		BodyLen: len(body),
		X:       int(command.Param),
		Y:       int(command.Tag),
	})
	<-done
	trace = append(trace, movementTraceEntry{Kind: "state.after", X: actor.X, Y: actor.Y, Dir: byte(actor.Dir)})

	if len(trace) != 4 {
		t.Fatalf("movement trace length = %d, want 4: %+v", len(trace), trace)
	}
	if trace[1].Kind != "packet.action_ok" || trace[2].Kind != "packet.observer" || trace[2].Ident != mir176.SMWalk || trace[2].Recog != world.CharacterActorID(actor) || trace[2].X != x+1 || trace[2].Y != y || trace[2].Dir != 2 || trace[2].BodyLen == 0 {
		t.Fatalf("movement packet trace = %+v", trace)
	}
	if trace[3].X != x+1 || trace[3].Y != y || trace[3].Dir != 2 {
		t.Fatalf("movement state trace = %+v", trace)
	}
	t.Logf("movement trace: %+v", trace)
}
