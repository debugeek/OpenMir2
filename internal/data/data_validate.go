package data

import "fmt"

func (b StdBundle) Validate() error {
	for id, mon := range b.Monsters {
		if mon.HP <= 0 {
			return fmt.Errorf("monster %s hp must be positive", id)
		}
	}
	for id, dt := range b.Drops {
		for _, e := range dt.Entries {
			if _, ok := b.Items[e.ItemID]; !ok {
				return fmt.Errorf("drop table %s references missing item %s", id, e.ItemID)
			}
			if e.Chance < 0 || e.Chance > 1 {
				return fmt.Errorf("drop table %s has invalid chance", id)
			}
			if e.MinCount <= 0 || e.MaxCount < e.MinCount {
				return fmt.Errorf("drop table %s has invalid count range", id)
			}
		}
	}
	for id, skill := range b.Skills {
		if skill.ID == "" || skill.Name == "" {
			return fmt.Errorf("skill %s must have id and name", id)
		}
	}
	for id, mp := range b.Maps {
		if mp.Width <= 0 || mp.Height <= 0 {
			return fmt.Errorf("map %s dimensions must be positive", id)
		}
		for _, conn := range mp.Connections {
			if conn.ToMap == "" {
				return fmt.Errorf("map %s has a connection with missing destination map", id)
			}
		}
	}
	totalStartPoints := 0
	for _, mp := range b.Maps {
		totalStartPoints += len(mp.StartPoints)
	}
	if totalStartPoints == 0 {
		return fmt.Errorf("start points are required")
	}
	for id, mp := range b.Maps {
		for _, sp := range mp.StartPoints {
			if !mp.Walkable(sp.X, sp.Y) {
				return fmt.Errorf("start point is not walkable on map %s", id)
			}
		}
	}
	for _, sp := range b.Spawns {
		if _, ok := b.Maps[sp.MapID]; !ok {
			return fmt.Errorf("spawn %s references missing map %s", sp.ID, sp.MapID)
		}
		if _, ok := b.Monsters[sp.MonsterID]; !ok {
			return fmt.Errorf("spawn %s references missing monster %s", sp.ID, sp.MonsterID)
		}
		if sp.Count <= 0 {
			return fmt.Errorf("spawn %s count must be positive", sp.ID)
		}
		if sp.RespawnSeconds <= 0 {
			return fmt.Errorf("spawn %s respawn_seconds must be positive", sp.ID)
		}
		if sp.MissionGenRate < 0 || sp.MissionGenRate > 100 {
			return fmt.Errorf("spawn %s mission_gen_rate must be between 0 and 100", sp.ID)
		}
	}
	for itemName, recipe := range b.MakeItems {
		if _, ok := b.Items[itemName]; !ok {
			return fmt.Errorf("make item %s references missing output item", itemName)
		}
		if len(recipe) == 0 {
			return fmt.Errorf("make item %s requires at least one ingredient", itemName)
		}
		for _, ing := range recipe {
			if _, ok := b.Items[ing.ItemID]; !ok {
				return fmt.Errorf("make item %s references missing ingredient %s", itemName, ing.ItemID)
			}
			if ing.Count <= 0 {
				return fmt.Errorf("make item %s has invalid ingredient count for %s", itemName, ing.ItemID)
			}
		}
	}
	for id, entity := range b.NPCs.Entities {
		if _, ok := b.Maps[entity.MapID]; !ok {
			return fmt.Errorf("npc %s references missing map %s", id, entity.MapID)
		}
		if entity.ScriptID != "" {
			if _, ok := b.NPCs.Scripts[entity.ScriptID]; !ok {
				return fmt.Errorf("npc %s references missing script %s", id, entity.ScriptID)
			}
		}
	}
	return nil
}

func (m StdMap) Walkable(x, y int) bool {
	if x < 0 || y < 0 || x >= m.Width || y >= m.Height {
		return false
	}
	for _, p := range m.Blocked {
		if p.X == x && p.Y == y {
			return false
		}
	}
	return true
}
