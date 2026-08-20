package world

import (
	"openmir2/internal/data"
	"openmir2/internal/storage"
)

func PackClientItemStats(item data.StdItem) data.StdItem {
	item.Stats.AcMin = PackStatWord(item.Stats.AcMin, item.Stats.AcMax)
	item.Stats.MacMin = PackStatWord(item.Stats.MacMin, item.Stats.MacMax)
	item.Stats.DcMin = PackStatWord(item.Stats.DcMin, item.Stats.DcMax)
	item.Stats.McMin = PackStatWord(item.Stats.McMin, item.Stats.McMax)
	item.Stats.ScMin = PackStatWord(item.Stats.ScMin, item.Stats.ScMax)
	return item
}

func UpgradeClientItemForDisplay(item data.StdItem, userItem storage.UserItem, applyShape bool) data.StdItem {
	item = PackClientItemStats(item)
	switch item.StdMode {
	case 5, 6:
		item.Stats.DcMin = packWordLowHigh(item.Stats.DcMin, userItem.Desc[0], 255)
		item.Stats.McMin = packWordLowHigh(item.Stats.McMin, userItem.Desc[1], 255)
		item.Stats.ScMin = packWordLowHigh(item.Stats.ScMin, userItem.Desc[2], 255)
		item.Stats.AcMin = packWordLowHigh(item.Stats.AcMin, userItem.Desc[3], 255)
		item.Stats.MacMin = packWordLowHigh(item.Stats.MacMin, userItem.Desc[4], 255)
		item.Stats.MacMin = setHighByteWithSpeed(item.Stats.MacMin, userItem.Desc[6])
		if userItem.Desc[7] >= 1 && userItem.Desc[7] <= 10 && item.SpecialPwr >= 0 {
			item.SpecialPwr = int(userItem.Desc[7])
		}
		if userItem.Desc[10] != 0 {
			item.ItemDesc |= 0x01
		}
		item.Slowdown += int(userItem.Desc[12])
		item.Tox += int(userItem.Desc[13])
	case 10, 11:
		item.Stats.AcMin = packWordLowHigh(item.Stats.AcMin, userItem.Desc[0], 255)
		item.Stats.MacMin = packWordLowHigh(item.Stats.MacMin, userItem.Desc[1], 255)
		item.Stats.DcMin = packWordLowHigh(item.Stats.DcMin, userItem.Desc[2], 255)
		item.Stats.McMin = packWordLowHigh(item.Stats.McMin, userItem.Desc[3], 255)
		item.Stats.ScMin = packWordLowHigh(item.Stats.ScMin, userItem.Desc[4], 255)
		item.Agility += int(userItem.Desc[11])
		item.MgAvoid += int(userItem.Desc[12])
		item.ToxAvoid += int(userItem.Desc[13])
	case 15:
		item.Stats.AcMin = packWordLowHigh(item.Stats.AcMin, userItem.Desc[0], 255)
		item.Stats.MacMin = packWordLowHigh(item.Stats.MacMin, userItem.Desc[1], 255)
		item.Stats.DcMin = packWordLowHigh(item.Stats.DcMin, userItem.Desc[2], 255)
		item.Stats.McMin = packWordLowHigh(item.Stats.McMin, userItem.Desc[3], 255)
		item.Stats.ScMin = packWordLowHigh(item.Stats.ScMin, userItem.Desc[4], 255)
		item.Accurate += int(userItem.Desc[11])
		item.MgAvoid += int(userItem.Desc[12])
		item.ToxAvoid += int(userItem.Desc[13])
		if userItem.Desc[5] > 0 {
			item.Need = int(userItem.Desc[5])
		}
		if userItem.Desc[6] > 0 {
			item.NeedLevel = int(userItem.Desc[6])
		}
	case 19, 20, 21:
		item.Stats.AcMin = packWordLowHigh(item.Stats.AcMin, userItem.Desc[0], 255)
		item.Stats.MacMin = packWordLowHigh(item.Stats.MacMin, userItem.Desc[1], 255)
		item.Stats.DcMin = packWordLowHigh(item.Stats.DcMin, userItem.Desc[2], 255)
		item.Stats.McMin = packWordLowHigh(item.Stats.McMin, userItem.Desc[3], 255)
		item.Stats.ScMin = packWordLowHigh(item.Stats.ScMin, userItem.Desc[4], 255)
		item.AtkSpd += int(userItem.Desc[9])
		item.Slowdown += int(userItem.Desc[12])
		item.Tox += int(userItem.Desc[13])
		switch item.StdMode {
		case 19:
			item.Accurate += int(userItem.Desc[11])
		case 20:
			item.MgAvoid += int(userItem.Desc[11])
		case 21:
			item.Accurate += int(userItem.Desc[11])
			item.MgAvoid += int(userItem.Desc[7])
		}
		if userItem.Desc[5] > 0 {
			item.Need = int(userItem.Desc[5])
		}
		if userItem.Desc[6] > 0 {
			item.NeedLevel = int(userItem.Desc[6])
		}
	case 22, 23:
		item.Stats.AcMin = packWordLowHigh(item.Stats.AcMin, userItem.Desc[0], 255)
		item.Stats.MacMin = packWordLowHigh(item.Stats.MacMin, userItem.Desc[1], 255)
		item.Stats.DcMin = packWordLowHigh(item.Stats.DcMin, userItem.Desc[2], 255)
		item.Stats.McMin = packWordLowHigh(item.Stats.McMin, userItem.Desc[3], 255)
		item.Stats.ScMin = packWordLowHigh(item.Stats.ScMin, userItem.Desc[4], 255)
		item.AtkSpd += int(userItem.Desc[9])
		item.Slowdown += int(userItem.Desc[12])
		item.Tox += int(userItem.Desc[13])
		if userItem.Desc[5] > 0 {
			item.Need = int(userItem.Desc[5])
		}
		if userItem.Desc[6] > 0 {
			item.NeedLevel = int(userItem.Desc[6])
		}
	case 24:
		item.Stats.AcMin = packWordLowHigh(item.Stats.AcMin, userItem.Desc[0], 255)
		item.Stats.MacMin = packWordLowHigh(item.Stats.MacMin, userItem.Desc[1], 255)
		item.Stats.DcMin = packWordLowHigh(item.Stats.DcMin, userItem.Desc[2], 255)
		item.Stats.McMin = packWordLowHigh(item.Stats.McMin, userItem.Desc[3], 255)
		item.Stats.ScMin = packWordLowHigh(item.Stats.ScMin, userItem.Desc[4], 255)
		if userItem.Desc[5] > 0 {
			item.Need = int(userItem.Desc[5])
		}
		if userItem.Desc[6] > 0 {
			item.NeedLevel = int(userItem.Desc[6])
		}
	case 26:
		item.Stats.AcMin = packWordLowHigh(item.Stats.AcMin, userItem.Desc[0], 255)
		item.Stats.MacMin = packWordLowHigh(item.Stats.MacMin, userItem.Desc[1], 255)
		item.Stats.DcMin = packWordLowHigh(item.Stats.DcMin, userItem.Desc[2], 255)
		item.Stats.McMin = packWordLowHigh(item.Stats.McMin, userItem.Desc[3], 255)
		item.Stats.ScMin = packWordLowHigh(item.Stats.ScMin, userItem.Desc[4], 255)
		item.Accurate += int(userItem.Desc[11])
		item.Agility += int(userItem.Desc[12])
		if userItem.Desc[5] > 0 {
			item.Need = int(userItem.Desc[5])
		}
		if userItem.Desc[6] > 0 {
			item.NeedLevel = int(userItem.Desc[6])
		}
	case 52:
		item.Stats.AcMin = packWordLowHigh(item.Stats.AcMin, userItem.Desc[0], 255)
		item.Stats.MacMin = packWordLowHigh(item.Stats.MacMin, userItem.Desc[1], 255)
		item.Agility += int(userItem.Desc[3])
	case 54:
		item.Stats.AcMin = packWordLowHigh(item.Stats.AcMin, userItem.Desc[0], 255)
		item.Stats.MacMin = packWordLowHigh(item.Stats.MacMin, userItem.Desc[1], 255)
		item.Accurate += int(userItem.Desc[2])
		item.Agility += int(userItem.Desc[3])
		item.ToxAvoid += int(userItem.Desc[13])
	}
	if applyShape && isStdModeShapeMode(item.StdMode) {
		if userItem.Desc[8] == 0 {
			item.Shape = 0
		} else {
			item.Shape = 130
		}
	}
	return item
}

func PackStatWord(low, high int) int {
	return int(MakeWord(low, high))
}

func MakeWord(low, high int) uint16 {
	return uint16(byte(low)) | uint16(byte(high))<<8
}

func packWordLowHigh(packed int, add byte, max int) int {
	low := int(byte(packed))
	high := int(byte(packed >> 8))
	high = minInt(max, high+int(add))
	return int(MakeWord(low, high))
}

func setHighByteWithSpeed(packed int, add byte) int {
	low := int(byte(packed))
	high := int(byte(packed >> 8))
	high = int(getAttackSpeed(byte(high), add))
	return int(MakeWord(low, high))
}

func realAttackSpeed(wAtkSpd byte) int {
	if wAtkSpd <= 10 {
		return -int(wAtkSpd)
	}
	return int(wAtkSpd) - 10
}

func naturalAttackSpeed(iAtkSpd int) byte {
	if iAtkSpd <= 0 {
		return byte(-iAtkSpd)
	}
	return byte(iAtkSpd + 10)
}

func getAttackSpeed(base, user byte) byte {
	return naturalAttackSpeed(realAttackSpeed(base) + realAttackSpeed(user))
}

func isStdModeShapeMode(stdMode int) bool {
	switch stdMode {
	case 15, 19, 20, 21, 22, 23, 24, 26:
		return true
	default:
		return false
	}
}

func ItemDuraForDrop(item data.StdItem, drop GroundDrop) uint16 {
	dura := drop.DuraMax
	if dura <= 0 {
		dura = uint16(item.DuraMax)
	}
	if dura <= 0 {
		dura = 1000
	}
	if dura > 0xFFFF {
		dura = 0xFFFF
	}
	return uint16(dura)
}

func ItemDuraForEquip(item data.StdItem) uint16 {
	dura := item.DuraMax
	if dura <= 0 {
		dura = 1000
	}
	if dura > 0xFFFF {
		dura = 0xFFFF
	}
	return uint16(dura)
}
