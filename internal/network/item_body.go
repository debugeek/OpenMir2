package network

import (
	"bytes"
	"strconv"

	"openmir2/internal/data"
	"openmir2/internal/world"
)

func ClientItemBody(item data.StdItem, desc [14]byte, makeIndex int32, dura, duraMax uint16) []byte {
	return clientItemBody(item, desc, makeIndex, dura, duraMax, true)
}

func ClientBagItemBody(item data.StdItem, desc [14]byte, makeIndex int32, dura, duraMax uint16) []byte {
	return clientItemBody(item, desc, makeIndex, dura, duraMax, false)
}

func clientItemBody(item data.StdItem, desc [14]byte, makeIndex int32, dura, duraMax uint16, appendTimedDurability bool) []byte {
	if appendTimedDurability && item.StdMode == 50 {
		item.Name += " #" + strconv.Itoa(int(dura))
	}
	body := bytes.NewBuffer(make([]byte, 0, 192))
	writeGBKAsciiString(body, item.Name, itemNameLen)
	writeByte(body, byte(item.StdMode))
	writeByte(body, byte(item.Shape))
	writeByte(body, byte(item.Weight))
	writeByte(body, byte(item.AniCount))
	writeByte(body, byte(item.SpecialPwr))
	writeByte(body, byte(item.ItemDesc))
	writeByte(body, byte(item.NeedIdentify))
	writeU16(body, uint16(item.Looks))
	writeU16(body, uint16(item.DuraMax))
	writeU16(body, uint16(world.PackStatWord(item.Stats.AcMin, item.Stats.AcMax)))
	writeU16(body, uint16(world.PackStatWord(item.Stats.MacMin, item.Stats.MacMax)))
	writeU16(body, uint16(world.PackStatWord(item.Stats.DcMin, item.Stats.DcMax)))
	writeU16(body, uint16(world.PackStatWord(item.Stats.McMin, item.Stats.McMax)))
	writeU16(body, uint16(world.PackStatWord(item.Stats.ScMin, item.Stats.ScMax)))
	writeByte(body, byte(item.Need))
	writeByte(body, byte(item.NeedLevel))
	writeU16(body, 0)
	writeI32(body, int32(item.Price))
	writeI32(body, makeIndex)
	writeU16(body, dura)
	writeU16(body, duraMax)
	writeI32(body, 0)
	return body.Bytes()
}

func LegacyClientItemBody(item data.StdItem, makeIndex int32, dura, duraMax uint16) []byte {
	return legacyClientItemBody(item, makeIndex, dura, duraMax, true)
}

func LegacyBagItemBody(item data.StdItem, makeIndex int32, dura, duraMax uint16) []byte {
	return legacyClientItemBody(item, makeIndex, dura, duraMax, true)
}

func ClientEquippedItemBody(item data.StdItem, desc [14]byte, makeIndex int32, dura, duraMax uint16) []byte {
	return ClientNoTimedDurabilityItemBody(item, desc, makeIndex, dura, duraMax)
}

func LegacyEquippedItemBody(item data.StdItem, makeIndex int32, dura, duraMax uint16) []byte {
	return LegacyNoTimedDurabilityItemBody(item, makeIndex, dura, duraMax)
}

func ClientNoTimedDurabilityItemBody(item data.StdItem, desc [14]byte, makeIndex int32, dura, duraMax uint16) []byte {
	return clientItemBody(item, desc, makeIndex, dura, duraMax, false)
}

func LegacyNoTimedDurabilityItemBody(item data.StdItem, makeIndex int32, dura, duraMax uint16) []byte {
	return legacyClientItemBody(item, makeIndex, dura, duraMax, false)
}

func legacyClientItemBody(item data.StdItem, makeIndex int32, dura, duraMax uint16, appendTimedDurability bool) []byte {
	if appendTimedDurability && item.StdMode == 50 {
		item.Name += " #" + strconv.Itoa(int(dura))
	}
	body := bytes.NewBuffer(make([]byte, 0, 64))
	writeGBKAsciiString(body, item.Name, itemNameLen)
	writeByte(body, byte(item.StdMode))
	writeByte(body, byte(item.Shape))
	writeByte(body, byte(item.Weight))
	writeByte(body, byte(item.AniCount))
	writeByte(body, byte(item.SpecialPwr))
	writeByte(body, byte(item.ItemDesc))
	writeByte(body, byte(item.NeedIdentify))
	writeU16(body, uint16(item.Looks))
	writeU16(body, uint16(item.DuraMax))
	writeU16(body, uint16(world.PackStatWord(item.Stats.AcMin, item.Stats.AcMax)))
	writeU16(body, uint16(world.PackStatWord(item.Stats.MacMin, item.Stats.MacMax)))
	writeU16(body, uint16(world.PackStatWord(item.Stats.DcMin, item.Stats.DcMax)))
	writeU16(body, uint16(world.PackStatWord(item.Stats.McMin, item.Stats.McMax)))
	writeU16(body, uint16(world.PackStatWord(item.Stats.ScMin, item.Stats.ScMax)))
	writeByte(body, byte(item.Need))
	writeByte(body, byte(item.NeedLevel))
	writeU32(body, uint32(item.Price))
	writeI32(body, makeIndex)
	writeU16(body, dura)
	writeU16(body, duraMax)
	return body.Bytes()
}
