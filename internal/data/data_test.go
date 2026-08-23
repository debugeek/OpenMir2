package data

import "testing"

func TestLoadConfigsReadsRuntimeConfigs(t *testing.T) {
	b, err := Load("../../configs")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := b.Items["木剑"]; !ok {
		t.Fatalf("expected 木剑 item in runtime configs")
	}
	if item := b.Items["回城卷"]; item.Kind != "consumable" || item.StdMode != 3 {
		t.Fatalf("expected 回城卷 metadata from reference, got %+v", item)
	}
	if item := b.Items["金创药(小量)"]; item.Kind != "consumable" {
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
	if len(b.Maps["0"].Spawns) == 0 {
		t.Fatalf("expected map 0 spawns in runtime configs")
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
	if mon.Level != 15 || mon.HP != 30 || mon.MinAttack != 4 || mon.MaxAttack != 9 || mon.Defense != 1 || mon.Experience != 20 || mon.WalkSpeedMS != 1500 || mon.AttackIntervalMS != 2500 {
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
