package network

import (
	"bytes"
	"net"
	"testing"

	"openmir2/internal/storage"
	"openmir2/internal/world"
)

func TestMonsterPacketTraceRecordsObserverAndFrame(t *testing.T) {
	s := newTestServer(t)
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	client := &Client{
		conn: serverConn,
		ch:   storage.Character{ID: "trace-observer", MapID: testMapID, X: 10, Y: 10, HP: 100, MaxHP: 100},
		visibleMonsters: map[string]world.Monster{
			"trace-monster": {ID: "trace-monster", MapID: testMapID, X: 10, Y: 10},
		},
	}
	action := world.MonsterAction{MonsterID: "trace-monster", MapID: testMapID, X: 11, Y: 10, Dir: 2, Status: 7, Kind: world.MonsterActionWalk}
	s.EnableMonsterPacketTrace(true)
	done := make(chan struct{})
	go func() {
		s.broadcastMonsterWalk([]*Client{client}, action)
		close(done)
	}()
	frame := readFrame(t, clientConn)
	<-done

	traces := s.MonsterPacketTraces()
	if len(traces) != 1 {
		t.Fatalf("packet traces = %d, want 1", len(traces))
	}
	trace := traces[0]
	if trace.Sequence != 0 || trace.Observer != "trace-observer" || trace.MonsterID != action.MonsterID || trace.Phase != "action" || trace.Command != MonsterWalkCommand(action, s.world.MapLight(action.MapID)).Ident {
		t.Fatalf("packet trace = %+v", trace)
	}
	if !bytes.Equal(trace.Frame, frame) {
		t.Fatalf("traced frame differs from emitted frame: trace=%x emitted=%x", trace.Frame, frame)
	}
}

func TestMonsterPacketTraceRecordsSpaceMoveObserverPhases(t *testing.T) {
	s := newTestServer(t)
	oldServer, oldClient := net.Pipe()
	newServer, newClient := net.Pipe()
	defer oldServer.Close()
	defer oldClient.Close()
	defer newServer.Close()
	defer newClient.Close()
	oldMap := testMapID
	newMap := "1"
	s.registerClient(oldServer, storage.Character{ID: "trace-old", MapID: oldMap, X: 10, Y: 10, HP: 100, MaxHP: 100})
	s.registerClient(newServer, storage.Character{ID: "trace-new", MapID: newMap, X: 10, Y: 10, HP: 100, MaxHP: 100})
	defer s.unregisterClient(oldServer)
	defer s.unregisterClient(newServer)
	action := world.MonsterAction{MonsterID: "trace-space", PreviousMapID: oldMap, PreviousX: 10, PreviousY: 10, MapID: newMap, X: 10, Y: 10, Dir: 4, Status: 3, Kind: world.MonsterActionSpaceMove}
	s.EnableMonsterPacketTrace(true)
	done := make(chan struct{})
	go func() {
		s.broadcastMonsterSpaceMove(action)
		close(done)
	}()
	oldFrame := readFrame(t, oldClient)
	newFrame := readFrame(t, newClient)
	<-done
	if len(oldFrame) == 0 || len(newFrame) == 0 {
		t.Fatal("space move emitted an empty frame")
	}
	traces := s.MonsterPacketTraces()
	if len(traces) != 2 {
		t.Fatalf("packet traces = %+v, want old hide and new show", traces)
	}
	if traces[0].Observer != "trace-old" || traces[0].Phase != "old.hide" || traces[1].Observer != "trace-new" || traces[1].Phase != "new.show" {
		t.Fatalf("space move packet traces = %+v", traces)
	}
	if !bytes.Equal(traces[0].Frame, oldFrame) || !bytes.Equal(traces[1].Frame, newFrame) {
		t.Fatal("space move trace frames differ from emitted frames")
	}
}
