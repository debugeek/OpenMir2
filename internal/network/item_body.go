package network

import (
	"bytes"

	"openmir2/internal/data"
	"openmir2/internal/world"
)

func ClientItemBody(item data.StdItem, desc [14]byte, makeIndex int32, dura, duraMax uint16) []byte {
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
