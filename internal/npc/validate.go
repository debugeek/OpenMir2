package npc

import "fmt"

func (l Library) Validate() error {
	for id, entity := range l.Entities {
		if entity.ID == "" {
			return fmt.Errorf("npc %s must have id", id)
		}
		if entity.Name == "" {
			return fmt.Errorf("npc %s must have name", id)
		}
		if entity.MapID == "" {
			return fmt.Errorf("npc %s must have map_id", id)
		}
		if entity.X < 0 || entity.Y < 0 {
			return fmt.Errorf("npc %s coordinates must be non-negative", id)
		}
		if entity.Dir < 0 || entity.Dir > 7 {
			return fmt.Errorf("npc %s dir must be between 0 and 7", id)
		}
		switch entity.Kind {
		case KindNormal, KindMerchant, KindQuest, KindGuard, KindSpecial:
		default:
			return fmt.Errorf("npc %s has unsupported kind %q", id, entity.Kind)
		}
		if entity.ScriptID != "" {
			if _, ok := l.Scripts[entity.ScriptID]; !ok {
				return fmt.Errorf("npc %s references missing script %s", id, entity.ScriptID)
			}
		}
		if err := entity.Merchant.Validate(id); err != nil {
			return err
		}
	}
	for id, script := range l.Scripts {
		if script.ID == "" {
			return fmt.Errorf("script %s must have id", id)
		}
		for labelName, label := range script.Labels {
			if label.Name == "" {
				return fmt.Errorf("script %s label %s must have name", id, labelName)
			}
			for _, proc := range label.Procedures {
				if err := proc.Validate(id, labelName); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (m MerchantProfile) Validate(id string) error {
	if m.PriceRate < 0 {
		return fmt.Errorf("npc %s merchant price_rate must be >= 0", id)
	}
	for _, stock := range m.Stock {
		if stock.ItemID == "" {
			return fmt.Errorf("npc %s merchant stock item_id is required", id)
		}
		if stock.Count <= 0 {
			return fmt.Errorf("npc %s merchant stock count must be positive", id)
		}
		if stock.RefillMinutes < 0 {
			return fmt.Errorf("npc %s merchant refill_minutes must be >= 0", id)
		}
	}
	return nil
}

func (p Procedure) Validate(scriptID, labelName string) error {
	for _, c := range p.Conditions {
		if err := c.Validate(scriptID, labelName); err != nil {
			return err
		}
	}
	for _, a := range p.Actions {
		if err := a.Validate(scriptID, labelName); err != nil {
			return err
		}
	}
	for _, a := range p.ElseActions {
		if err := a.Validate(scriptID, labelName); err != nil {
			return err
		}
	}
	return nil
}

func (c Condition) Validate(scriptID, labelName string) error {
	if c.Op == "" {
		return fmt.Errorf("script %s label %s has condition with missing op", scriptID, labelName)
	}
	return nil
}

func (a Action) Validate(scriptID, labelName string) error {
	if a.Op == "" {
		return fmt.Errorf("script %s label %s has action with missing op", scriptID, labelName)
	}
	return nil
}
