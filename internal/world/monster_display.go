package world

import (
	"strconv"
	"strings"
)

func MonsterFeature(mon Monster) int32 {
	return int32(uint32(mon.RaceImg&0xFF) | uint32(mon.MonsterWeapon&0xFF)<<8 | uint32(mon.Appr&0xFFFF)<<16)
}

func MonsterActorID(mon Monster) int32 {
	_, suffix, ok := strings.Cut(mon.ID, "-")
	if !ok {
		return 100000
	}
	n, err := strconv.Atoi(suffix)
	if err != nil {
		return 100000
	}
	return int32(100000 + n)
}

func DropActorID(drop GroundDrop) int32 {
	_, suffix, ok := strings.Cut(drop.ID, "-")
	if !ok {
		return 200000
	}
	n, err := strconv.Atoi(suffix)
	if err != nil {
		return 200000
	}
	return int32(200000 + n)
}

func DropMakeIndex(drop GroundDrop) int32 {
	if drop.MakeIndex > 0 {
		return drop.MakeIndex
	}
	_, suffix, ok := strings.Cut(drop.ID, "-")
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(suffix)
	if err != nil {
		return 0
	}
	return int32(n)
}
