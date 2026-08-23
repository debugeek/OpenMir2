package npc

import (
	"sort"
	"strconv"
	"strings"
)

const (
	KindNormal   = "normal"
	KindMerchant = "merchant"
	KindQuest    = "quest"
	KindGuard    = "guard"
	KindSpecial  = "special"
)

type Library struct {
	Entities map[string]Entity `json:"entities"`
	Scripts  map[string]Script `json:"scripts"`
}

type Entity struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	MapID         string          `json:"map_id"`
	X             int             `json:"x"`
	Y             int             `json:"y"`
	Dir           int             `json:"dir"`
	Appr          int             `json:"appr,omitempty"`
	RaceImg       int             `json:"race_img,omitempty"`
	MonsterWeapon int             `json:"monster_weapon,omitempty"`
	Hidden        bool            `json:"hidden,omitempty"`
	ScriptID      string          `json:"script_id,omitempty"`
	Merchant      MerchantProfile `json:"merchant,omitempty"`
	Dialogue      string          `json:"dialogue,omitempty"`
	Metadata      map[string]any  `json:"metadata,omitempty"`
}

type MerchantProfile struct {
	PriceRate    int                  `json:"price_rate,omitempty"`
	Capabilities MerchantCapabilities `json:"capabilities,omitempty"`
	ItemTypes    []string             `json:"item_types,omitempty"`
	Stock        []MerchantStockItem  `json:"stock,omitempty"`
}

type MerchantCapabilities struct {
	Buy         bool `json:"buy,omitempty"`
	Sell        bool `json:"sell,omitempty"`
	Storage     bool `json:"storage,omitempty"`
	GetBack     bool `json:"get_back,omitempty"`
	Repair      bool `json:"repair,omitempty"`
	SendMsg     bool `json:"send_msg,omitempty"`
	UseItemName bool `json:"use_item_name,omitempty"`
	OffLineMsg  bool `json:"offline_msg,omitempty"`
	YBDeal      bool `json:"yb_deal,omitempty"`
}

type MerchantStockItem struct {
	ItemID        string `json:"item_id"`
	Count         int    `json:"count"`
	RefillMinutes int    `json:"refill_minutes,omitempty"`
}

type Script struct {
	ID     string           `json:"id"`
	Labels map[string]Label `json:"labels"`
}

type Label struct {
	Name       string      `json:"name"`
	ExtJump    bool        `json:"ext_jump,omitempty"`
	Procedures []Procedure `json:"procedures"`
}

type Procedure struct {
	Conditions  []Condition `json:"conditions,omitempty"`
	Say         string      `json:"say,omitempty"`
	ElseSay     string      `json:"else_say,omitempty"`
	Actions     []Action    `json:"actions,omitempty"`
	ElseActions []Action    `json:"else_actions,omitempty"`
}

type Condition struct {
	Op   string   `json:"op"`
	Args []string `json:"args,omitempty"`
}

type Action struct {
	Op   string   `json:"op"`
	Args []string `json:"args,omitempty"`
}

type Context struct {
	OwnerGuild       string
	Lord             string
	CastleGold       int
	TodayIncome      int
	CastleDoorState  string
	RepairDoorGold   int
	RepairWallGold   int
	GuardFee         int
	ArcherFee        int
	UpgradeWeaponFee int
	UserWeapon       string
}

type Conversation struct {
	NPC       Entity
	Label     string
	Text      string
	Close     bool
	NextLabel string
}

func NormalizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", KindNormal, "norm", "normalnpc", "management", "manager":
		return KindNormal
	case KindMerchant, "shop", "trade":
		return KindMerchant
	case KindQuest, "questnpc":
		return KindQuest
	case KindGuard, "castle_guard":
		return KindGuard
	default:
		return KindSpecial
	}
}

func (l Library) Conversation(entityID, label string, ctx Context) (Conversation, bool) {
	entity, ok := l.Entities[entityID]
	if !ok {
		return Conversation{}, false
	}
	if label == "" {
		label = "@main"
	}
	if entity.ScriptID == "" {
		return Conversation{
			NPC:   entity,
			Label: label,
			Text:  entity.Dialogue,
		}, true
	}
	script, ok := l.Scripts[entity.ScriptID]
	if !ok {
		return Conversation{}, false
	}
	if _, ok := lookupLabel(script.Labels, label); !ok && strings.EqualFold(label, "@main") {
		if fallback, ok := firstLabel(script.Labels); ok {
			label = fallback.Name
		}
	}
	return script.Conversation(entity, label, ctx), true
}

func (s Script) Conversation(entity Entity, label string, ctx Context) Conversation {
	if label == "" {
		label = "@main"
	}
	conversation := Conversation{NPC: entity, Label: label}
	lbl, ok := lookupLabel(s.Labels, label)
	if !ok {
		if entity.Dialogue != "" {
			conversation.Text = entity.Dialogue
		}
		return conversation
	}
	for _, proc := range lbl.Procedures {
		matched := true
		for _, condition := range proc.Conditions {
			if !condition.Match(ctx, entity) {
				matched = false
				break
			}
		}
		text := proc.Say
		actions := proc.Actions
		if !matched {
			text = proc.ElseSay
			actions = proc.ElseActions
		}
		if text == "" && len(actions) == 0 {
			continue
		}
		conversation.Text = renderDialogueText(text, ctx, entity)
		for _, action := range actions {
			switch strings.ToLower(strings.TrimSpace(action.Op)) {
			case "close":
				conversation.Close = true
			case "goto":
				if len(action.Args) > 0 {
					conversation.NextLabel = action.Args[0]
				}
			}
		}
		return conversation
	}
	if entity.Dialogue != "" {
		conversation.Text = renderDialogueText(entity.Dialogue, ctx, entity)
	}
	return conversation
}

func lookupLabel(labels map[string]Label, want string) (Label, bool) {
	if lbl, ok := labels[want]; ok {
		return lbl, true
	}
	for key, lbl := range labels {
		if strings.EqualFold(key, want) {
			return lbl, true
		}
	}
	if base, ok := stripLabelSuffix(want); ok {
		if lbl, ok := lookupLabel(labels, base); ok {
			return lbl, true
		}
	}
	return Label{}, false
}

func firstLabel(labels map[string]Label) (Label, bool) {
	if len(labels) == 0 {
		return Label{}, false
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return labels[keys[0]], true
}

func stripLabelSuffix(label string) (string, bool) {
	label = strings.TrimSpace(label)
	if label == "" {
		return "", false
	}
	for i := len(label) - 1; i >= 0; i-- {
		if label[i] != '-' {
			continue
		}
		if i == 0 || i == len(label)-1 {
			return "", false
		}
		for _, ch := range label[i+1:] {
			if ch < '0' || ch > '9' {
				return "", false
			}
		}
		return label[:i], true
	}
	return "", false
}

func (c Condition) Match(ctx Context, entity Entity) bool {
	switch strings.ToLower(strings.TrimSpace(c.Op)) {
	case "", "always", "true":
		return true
	default:
		return false
	}
}

func renderDialogueText(text string, ctx Context, entity Entity) string {
	if text == "" {
		return ""
	}
	text = strings.NewReplacer(
		"\\n", "\\",
		"\\r", "\\",
		"\r\n", "\\",
		"\n", "\\",
		"\r", "\\",
	).Replace(text)
	replacer := strings.NewReplacer(
		"<$OWNERGUILD>", ctx.OwnerGuild,
		"<$LORD>", ctx.Lord,
		"<$CASTLEGOLD>", strconv.Itoa(ctx.CastleGold),
		"<$TODAYINCOME>", strconv.Itoa(ctx.TodayIncome),
		"<$CASTLEDOORSTATE>", ctx.CastleDoorState,
		"<$REPAIRDOORGOLD>", strconv.Itoa(ctx.RepairDoorGold),
		"<$REPAIRWALLGOLD>", strconv.Itoa(ctx.RepairWallGold),
		"<$GUARDFEE>", strconv.Itoa(ctx.GuardFee),
		"<$ARCHERFEE>", strconv.Itoa(ctx.ArcherFee),
		"<$UPGRADEWEAPONFEE>", strconv.Itoa(ctx.UpgradeWeaponFee),
		"<$USERWEAPON>", ctx.UserWeapon,
	)
	return replacer.Replace(text)
}
