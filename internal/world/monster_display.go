package world

import (
	"strconv"
	"strings"
	"time"
)

func MonsterDisplayName(mon Monster) string {
	if mon.MasterName == "" {
		return mon.Name
	}
	return mon.Name + "(" + mon.MasterName + ")"
}

func MonsterFeature(mon Monster) int32 {
	return int32(uint32(mon.RaceImg&0xFF) | uint32(mon.MonsterWeapon&0xFF)<<8 | uint32(mon.Appr&0xFFFF)<<16)
}

func MonsterNameColor(mon Monster) uint16 {
	if !mon.HolySeizeUntil.IsZero() {
		return 0x7D
	}
	if !mon.CrazyUntil.IsZero() {
		return 0xF9
	}
	return 255
}

func MonsterStatus(mon Monster, now time.Time) int32 {
	status := int32(0)
	active := func(until time.Time) bool {
		return !until.IsZero()
	}
	if active(mon.PoisonHealthUntil) {
		status |= -2147483648
	}
	if mon.PoisonArmorLevel > 0 && active(mon.PoisonArmorUntil) {
		status |= int32(uint32(0x40000000))
	}
	if mon.DefenceUpUntil > 0 {
		status |= 0x00400000
	}
	if mon.MagDefenceUpUntil > 0 {
		status |= 0x00200000
	}
	if mon.ShowHPUntil > 0 {
		status |= 0x20000000
	}
	if !mon.TransparentUntil.IsZero() {
		status |= 0x00800000
	}
	if !mon.ParalyzedUntil.IsZero() {
		status |= 0x04000000
	}
	return status
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
