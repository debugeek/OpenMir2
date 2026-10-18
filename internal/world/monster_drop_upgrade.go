package world

import (
	"math/rand"

	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func referenceRandomRange(r *rand.Rand, count, rate int) int {
	value := 0
	for i := 0; i < count; i++ {
		if r.Intn(rate) == 0 {
			value++
		}
	}
	return value
}

func addDropDurability(item *storage.UserItem, amount int) {
	max := int(item.DuraMax) + amount
	if max > 65000 {
		max = 65000
	}
	item.DuraMax = uint16(max)
	current := int(item.Dura) + amount
	if current > 65000 {
		current = 65000
	}
	item.Dura = uint16(current)
}

func applyReferenceMonsterDropUpgrade(r *rand.Rand, std data.StdItem, item *storage.UserItem) {
	switch std.StdMode {
	case 5, 6:
		n := referenceRandomRange(r, 12, 15)
		if r.Intn(15) == 0 {
			item.Desc[0] = byte(n + 1)
		}
		n = referenceRandomRange(r, 12, 15)
		if r.Intn(20) == 0 {
			inc := (n + 1) / 3
			if inc > 0 {
				if r.Intn(3) != 0 {
					item.Desc[6] = byte(inc)
				} else {
					item.Desc[6] = byte(inc + 10)
				}
			}
		}
		n = referenceRandomRange(r, 12, 15)
		if r.Intn(15) == 0 {
			item.Desc[1] = byte(n + 1)
		}
		n = referenceRandomRange(r, 12, 15)
		if r.Intn(15) == 0 {
			item.Desc[2] = byte(n + 1)
		}
		n = referenceRandomRange(r, 12, 15)
		if r.Intn(24) == 0 {
			item.Desc[5] = byte(n/2 + 1)
		}
		n = referenceRandomRange(r, 12, 12)
		if r.Intn(3) < 2 {
			addDropDurability(item, (n+1)*2000)
		}
		n = referenceRandomRange(r, 12, 15)
		if r.Intn(10) == 0 {
			item.Desc[7] = byte(n/2 + 1)
		}
	case 10, 11:
		n := referenceRandomRange(r, 6, 15)
		if r.Intn(30) == 0 {
			item.Desc[0] = byte(n + 1)
		}
		n = referenceRandomRange(r, 6, 15)
		if r.Intn(30) == 0 {
			item.Desc[1] = byte(n + 1)
		}
		n = referenceRandomRange(r, 6, 20)
		if r.Intn(40) == 0 {
			item.Desc[2] = byte(n + 1)
		}
		n = referenceRandomRange(r, 6, 20)
		if r.Intn(40) == 0 {
			item.Desc[3] = byte(n + 1)
		}
		n = referenceRandomRange(r, 6, 20)
		if r.Intn(40) == 0 {
			item.Desc[4] = byte(n + 1)
		}
		n = referenceRandomRange(r, 6, 10)
		if r.Intn(8) < 6 {
			addDropDurability(item, (n+1)*2000)
		}
	case 20, 21, 24:
		applyReferenceAccessoryMode(r, item, 60, 60, 30, 12, 20, 15, 30, 20)
	case 26:
		applyReferenceAccessoryMode(r, item, 20, 20, 20, 12, 20, 15, 30, 20)
	case 19:
		applyReferenceAccessoryMode(r, item, 40, 40, 20, 10, 4, 3, 30, 20)
	case 22:
		applyReferenceAccessoryMode(r, item, 0, 0, 20, 12, 4, 3, 30, 20)
	case 23:
		applyReferenceAccessoryMode(r, item, 40, 40, 20, 12, 4, 3, 30, 20)
	case 15:
		applyReferenceAccessoryMode(r, item, 40, 30, 20, 12, 4, 3, 30, 20)
	}
}

func applyReferenceAccessoryMode(r *rand.Rand, item *storage.UserItem, acRate, macRate, firstRandomRate, duraRandomRate, duraRate, duraThreshold, combatRate, combatRandomRate int) {
	if acRate > 0 {
		n := referenceRandomRange(r, 6, firstRandomRate)
		if r.Intn(acRate) == 0 {
			item.Desc[0] = byte(n + 1)
		}
		n = referenceRandomRange(r, 6, firstRandomRate)
		if r.Intn(macRate) == 0 {
			item.Desc[1] = byte(n + 1)
		}
	}
	for i := 2; i <= 4; i++ {
		n := referenceRandomRange(r, 6, combatRandomRate)
		if r.Intn(combatRate) == 0 {
			item.Desc[i] = byte(n + 1)
		}
	}
	if r.Intn(duraRate) < duraThreshold {
		addDropDurability(item, (referenceRandomRange(r, 6, duraRandomRate)+1)*1000)
	}
}

func applyReferenceAccessoryDropUpgrade(r *rand.Rand, mode int, item *storage.UserItem) {
	switch mode {
	case 20, 21, 24:
		applyReferenceAccessoryMode(r, item, 60, 60, 30, 12, 20, 15, 30, 20)
	case 26:
		applyReferenceAccessoryMode(r, item, 20, 20, 20, 12, 20, 15, 30, 20)
	case 19:
		applyReferenceAccessoryMode(r, item, 40, 40, 20, 10, 4, 3, 30, 20)
	case 22:
		applyReferenceAccessoryMode(r, item, 0, 0, 20, 12, 4, 3, 30, 20)
	case 23:
		applyReferenceAccessoryMode(r, item, 40, 40, 20, 12, 4, 3, 30, 20)
	case 15:
		applyReferenceAccessoryMode(r, item, 40, 30, 20, 12, 4, 3, 30, 20)
	}
}

func applyReferenceUnknownMonsterDropUpgrade(r *rand.Rand, mode int, item *storage.UserItem) {
	firstCount, firstRate, combatCount, combatRate, threshold := 0, 0, 0, 0, 0
	switch mode {
	case 15:
		firstCount, firstRate, combatCount, combatRate, threshold = 4, 20, 3, 30, 3
	case 22, 23:
		firstCount, firstRate, combatCount, combatRate, threshold = 6, 20, 6, 20, 3
	case 24, 26:
		firstCount, firstRate, combatCount, combatRate, threshold = 5, 20, 5, 30, 2
	default:
		return
	}
	points := 0
	for i := 0; i <= 1; i++ {
		n := referenceRandomRange(r, firstCount, firstRate)
		item.Desc[i] = byte(n)
		points += n
	}
	for i := 2; i <= 4; i++ {
		n := referenceRandomRange(r, combatCount, combatRate)
		item.Desc[i] = byte(n)
		points += n
	}
	n := referenceRandomRange(r, 6, 30)
	if n > 0 {
		addDropDurability(item, (n+1)*1000)
	}
	if r.Intn(30) == 0 {
		item.Desc[7] = 1
	}
	item.Desc[8] = 1
	if points < threshold {
		return
	}
	if mode == 15 {
		switch {
		case item.Desc[0] >= 5:
			item.Desc[5], item.Desc[6] = 1, item.Desc[0]*3+25
		case item.Desc[2] >= 2:
			item.Desc[5], item.Desc[6] = 1, item.Desc[2]*4+35
		case item.Desc[3] >= 2:
			item.Desc[5], item.Desc[6] = 2, item.Desc[3]*2+18
		case item.Desc[4] >= 2:
			item.Desc[5], item.Desc[6] = 3, item.Desc[4]*2+18
		default:
			item.Desc[6] = byte(points*2 + 18)
		}
		return
	}
	if mode == 24 || mode == 26 {
		switch {
		case item.Desc[0] >= 3:
			item.Desc[5], item.Desc[6] = 1, item.Desc[0]*3+25
		case item.Desc[2] >= 2:
			item.Desc[5], item.Desc[6] = 1, item.Desc[2]*3+30
		case item.Desc[3] >= 2:
			item.Desc[5], item.Desc[6] = 2, item.Desc[3]*2+20
		case item.Desc[4] >= 2:
			item.Desc[5], item.Desc[6] = 3, item.Desc[4]*2+20
		default:
			item.Desc[6] = byte(points*2 + 18)
		}
		return
	}
	switch {
	case item.Desc[2] >= 3:
		item.Desc[5], item.Desc[6] = 1, item.Desc[2]*3+25
	case item.Desc[3] >= 3:
		item.Desc[5], item.Desc[6] = 2, item.Desc[3]*2+18
	case item.Desc[4] >= 3:
		item.Desc[5], item.Desc[6] = 3, item.Desc[4]*2+18
	default:
		item.Desc[6] = byte(points*2 + 18)
	}
}
