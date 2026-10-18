package data

import "testing"

func TestLoadConfigsReadsRuntimeConfigs(t *testing.T) {
	b, _, err := LoadConfigsWithReport("../../configs")
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	if _, ok := b.Items["木剑"]; !ok {
		t.Fatalf("expected 木剑 item in runtime configs")
	}
	if item := b.Items["回城卷"]; item.StdMode != 3 {
		t.Fatalf("expected 回城卷 metadata from reference, got %+v", item)
	}
	if item := b.Items["金创药(小量)"]; item.Looks != 398 {
		t.Fatalf("expected 金创药(小量) metadata from reference, got %+v", item)
	}
	if item := b.Items["金手镯"]; item.StdMode != 26 || item.Looks != 207 {
		t.Fatalf("expected 金手镯 metadata from reference, got %+v", item)
	}
	if item := b.Items["黑铁矿石"]; item.StdMode != 43 {
		t.Fatalf("expected 黑铁矿石 metadata from reference, got %+v", item)
	}
	if _, ok := b.Monsters["赤月恶魔"]; !ok {
		t.Fatalf("expected 赤月恶魔 in runtime configs")
	}
	if _, ok := b.Drops["赤月恶魔"]; !ok {
		t.Fatalf("expected 赤月恶魔 drop table in runtime configs")
	}
	if _, ok := b.Skills["火球术"]; !ok {
		t.Fatalf("expected 火球术 skill in runtime configs")
	}
	if len(b.Maps["0"].Connections) == 0 {
		t.Fatalf("expected map 0 connections in runtime configs")
	}
	if len(b.Castle.WarMaps) != 6 || b.Castle.WarMaps[0] != "0151" || b.Castle.WarMaps[5] != "0156" {
		t.Fatalf("castle war maps = %v, want 0151-0156", b.Castle.WarMaps)
	}
	if len(b.Maps["0"].Spawns) == 0 {
		t.Fatalf("expected map 0 spawns in runtime configs")
	}
	for _, sp := range b.Maps["0"].Spawns {
		if sp.ReferenceOrder <= 0 {
			t.Fatalf("map 0 spawn lacks reference order: %+v", sp)
		}
	}
	for _, id := range []string{"R001", "Q004", "H007", "H106"} {
		if !b.Maps[id].NeedHole {
			t.Fatalf("expected %s to require an active dig-out event", id)
		}
	}
	for _, id := range []string{"G001", "G002", "0162", "G004", "G006", "G008", "G010", "G014", "B101", "B102"} {
		if !b.Maps[id].Safe {
			t.Fatalf("expected %s to be a safe map", id)
		}
	}
	if !b.Maps["D2079"].NoPositionMove {
		t.Fatalf("expected D2079 to reject position movement")
	}
	if b.Maps["D001"].Light != 1 || b.Maps["G001"].Light != 0 || b.Maps["T118"].Light != 0 {
		t.Fatalf("expected reference map light values, got D001=%d G001=%d T118=%d", b.Maps["D001"].Light, b.Maps["G001"].Light, b.Maps["T118"].Light)
	}
	if b.MiniMaps["D001"] != 1 || b.MiniMaps["G004"] != 116 || len(b.MiniMaps) != 130 {
		t.Fatalf("minimap mappings = D001:%d G004:%d count:%d, want 1/116/130", b.MiniMaps["D001"], b.MiniMaps["G004"], len(b.MiniMaps))
	}
	orders := make(map[int]string)
	orderedNPCs := 0
	for id, entity := range b.NPCs.Entities {
		if entity.ReferenceOrder <= 0 {
			continue
		}
		orderedNPCs++
		if previous, exists := orders[entity.ReferenceOrder]; exists {
			t.Fatalf("duplicate NPC reference order %d: %s and %s", entity.ReferenceOrder, previous, id)
		}
		orders[entity.ReferenceOrder] = id
	}
	if orderedNPCs != 95 || b.NPCs.Entities["比奇国王-0122"].ReferenceOrder != 142 || b.NPCs.Entities["沙城总管-0150"].ReferenceOrder != 143 {
		t.Fatalf("ordered NPCs = %d, king order = %d, governor order = %d; want 95/142/143", orderedNPCs, b.NPCs.Entities["比奇国王-0122"].ReferenceOrder, b.NPCs.Entities["沙城总管-0150"].ReferenceOrder)
	}
	if !b.Maps["D001"].LightFixed || !b.Maps["G001"].LightFixed || b.Maps["0"].LightFixed {
		t.Fatalf("map light fixed markers = D001:%t G001:%t 0:%t, want fixed/fixed/dynamic", b.Maps["D001"].LightFixed, b.Maps["G001"].LightFixed, b.Maps["0"].LightFixed)
	}
	if !b.Maps["0100"].FightZone || !b.Maps["F001"].Fight3Zone {
		t.Fatalf("expected reference fight map flags, got 0100=%+v F001=%+v", b.Maps["0100"], b.Maps["F001"])
	}
	for _, id := range []string{"D10011", "D10012", "D10013", "D1002", "D405", "D412", "D415", "D416"} {
		if !b.Maps[id].Mine {
			t.Fatalf("expected %s to enable mining", id)
		}
	}
	foundStartPoint := false
	for _, mp := range b.Maps {
		for _, sp := range mp.StartPoints {
			foundStartPoint = true
			if !mp.Walkable(sp.X, sp.Y) {
				t.Fatalf("start point should be walkable: %+v", sp)
			}
		}
	}
	if !foundStartPoint {
		t.Fatalf("expected at least one start point in runtime configs")
	}
	mon := b.Monsters["半兽人"]
	if mon.Level != 15 || mon.HP != 30 || mon.MinAttack != 4 || mon.MaxAttack != 9 || mon.Defense != 1 || mon.Experience != 25 || mon.WalkSpeedMS != 1500 || mon.AttackIntervalMS != 2500 {
		t.Fatalf("monster attributes were not loaded completely: %+v", mon)
	}
}

func TestLoadConfigsWithReportRecordsExpectedSkippedEntries(t *testing.T) {
	_, report, err := LoadConfigsWithReport("../../configs")
	if err != nil {
		t.Fatalf("LoadConfigsWithReport() error = %v", err)
	}
	foundMissingMonster := false
	for _, skipped := range report.Skipped {
		if skipped.Kind == "spawn" && skipped.Reason == "missing monster attributes" {
			foundMissingMonster = true
		}
	}
	if foundMissingMonster {
		t.Fatalf("did not expect missing monster spawn skips in current configs")
	}
}
