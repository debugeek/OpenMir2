package world

import (
	"fmt"

	"openmir2/internal/data"
)

func unpackBundleItem(item data.StdItem) (string, error) {
	switch item.Shape {
	case 100:
		return "超级金创药", nil
	case 101:
		return "超级魔法药", nil
	case 102:
		return "金创药(小量)", nil
	case 103:
		return "魔法药(小量)", nil
	case 104:
		return "金创药(中量)", nil
	case 105:
		return "魔法药(中量)", nil
	case 106:
		return "地牢逃脱卷", nil
	case 107:
		return "随机传送卷", nil
	case 108:
		return "回城卷", nil
	case 109:
		return "行会回城卷", nil
	case 117:
		return "强效太阳水", nil
	case 118:
		return "万年雪霜", nil
	case 119:
		return "疗伤药", nil
	default:
		return "", fmt.Errorf("item %s cannot be used", item.ID)
	}
}
