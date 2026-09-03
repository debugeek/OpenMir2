package mir176

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	FrameStart   = byte('#')
	FrameEnd     = byte('!')
	FrameTrailer = byte('$')
	CommandLen   = 12
)

const (
	CMQueryCharacter  = 100
	CMQueryUserName   = 80
	CMQueryUserState  = 82
	CMNewCharacter    = 101
	CMDeleteCharacter = 102
	CMSelectCharacter = 103
	CMSelectServer    = 104
	CMQueryBagItems   = 81
	CMLoginNoticeOK   = 1018

	CMProtocol                = 2000
	CMIDPassword              = 2001
	CMAddNewUser              = 2002
	CMDropItem                = 1000
	CMPickup                  = 1001
	CMMerchantQuerySellPrice  = 1012
	CMEat                     = 1006
	CMThrow                   = 3005
	CMTurn                    = 3010
	CMWalk                    = 3011
	CMSitDown                 = 3012
	CMRun                     = 3013
	CMHit                     = 3014
	CMHeavyHit                = 3015
	CMBigHit                  = 3016
	CMSpell                   = 3017
	CMPowerHit                = 3018
	CMLongHit                 = 3019
	CMWideHit                 = 3024
	CMFireHit                 = 3025
	CMSay                     = 3030
	CMUserCommand             = 3033
	CMClickNPC                = 1010
	CMMerchantDlgSelect       = 1011
	CMUserSellItem            = 1013
	CMUserBuyItem             = 1014
	CMUserGetDetailItem       = 1015
	CMUserRepairItem          = 1023
	CMMerchantQueryRepairCost = 1024
	CMUserStorageItem         = 1031
	CMUserTakeBackStorageItem = 1032
	CMUserMakeDrugItem        = 1034
	CMMagicKeyChange          = 1008

	CMTakeOnItem     = 1003
	CMTakeOffItem    = 1004
	CMGroupMode      = 1019
	CMCreateGroup    = 1020
	CMAddGroupMember = 1021
	CMDelGroupMember = 1022

	SMTurn               = 10
	SMWalk               = 11
	SMRush               = 6
	SMRushKung           = 7
	SMBackStep           = 9
	SMSitDown            = 12
	SMRun                = 13
	SMHit                = 14
	SMHeavyHit           = 15
	SMBigHit             = 16
	SMSpell              = 17
	SMPowerHit           = 18
	SMLongHit            = 19
	SMDigUp              = 20
	SMDigDown            = 21
	SMDisappear          = 30
	SMWideHit            = 24
	SMFireHit            = 8
	SMMoveFail           = 28
	SMStruck             = 31
	SMDeath              = 32
	SMSkeleton           = 33
	SMNowDeath           = 34
	SMCertificationOK    = 500
	SMCertificationFail  = 501
	SMAppleM2CertOK      = 502
	SMPasswordFail       = 503
	SMPassOKSelectServer = 529
	SMSelectServerOK     = 530
	SMQueryCharacter     = 520
	SMNewCharacterOK     = 521
	SMNewCharacterFail   = 522
	SMStartPlay          = 525
	SMStartFail          = 526

	SMHear                       = 40
	SMGhost                      = 803
	SMMapDescription             = 54
	SMLogon                      = 50
	SMNewMap                     = 51
	SMAbility                    = 52
	SMWinExp                     = 44
	SMLevelUp                    = 45
	SMDayChanging                = 46
	SMFeatureChanged             = 41
	SMShowEvent                  = 804
	SMHideEvent                  = 805
	SMChangeLight                = 654
	SMChangeNameColor            = 656
	SMUserName                   = 42
	SMClearObjects               = 633
	SMChangeMap                  = 634
	SMMagicFire                  = 638
	SMMagicFireFail              = 639
	SMMagicLvExp                 = 640
	SMMerchantSay                = 643
	SMSendGoodsList              = 645
	SMSendUserSell               = 646
	SMSendBuyPrice               = 647
	SMUserSellItemOK             = 648
	SMUserSellItemFail           = 649
	SMBuyItemSuccess             = 650
	SMBuyItemFail                = 651
	SMMerchantDlgClose           = 644
	SMSendDetailGoodsList        = 652
	SMGoldChanged                = 653
	SMSendUserRepair             = 668
	SMUserRepairItemOK           = 669
	SMUserRepairItemFail         = 670
	SMSendRepairCost             = 671
	SMStorageInfo                = 5287
	SMDelItems                   = 709
	SMSendUserStorageItem        = 700
	SMStorageOK                  = 701
	SMStorageFull                = 702
	SMStorageFail                = 703
	SMSaveItemList               = 704
	SMTakeBackStorageItemOK      = 705
	SMTakeBackStorageItemFail    = 706
	SMTakeBackStorageItemFullBag = 707
	SMSendUserMakeDrugItemList   = 712
	SMMakeDrugSuccess            = 713
	SMMakeDrugFail               = 714
	SMSpacemoveHide              = 800
	SMSpacemoveShow              = 801
	SMSpacemoveHide2             = 806
	SMSpacemoveShow2             = 807
	SMSendUserState              = 751
	SMBagItems                   = 201
	SMSystemMessage              = 100
	SMHealthSpellChanged         = 53
	SMDuraChange                 = 642
	SMOpenHealth                 = 1100
	SMCloseHealth                = 1101
	SMInstanceHealGauge          = 1103
	SMCharStatusChanged          = 657
	SMWeightChanged              = 622
	SMSendMyMagic                = 211
	SMAttackMode                 = 213
	SMServerConfig               = 11029
	SMServerUnbind               = 20019
	SMSendUseItems               = 621
	SMSendNotice                 = 658
	SMGroupModeChanged           = 659
	SMCreateGroupOK              = 660
	SMCreateGroupFail            = 661
	SMGroupAddMemOK              = 662
	SMGroupDelMemOK              = 663
	SMGroupAddMemFail            = 664
	SMGroupDelMemFail            = 665
	SMGroupCancel                = 666
	SMGroupMembers               = 667
	SMAreaState                  = 766
	SMMyStatus                   = 708
	SMSubAbility                 = 752
	SMGameGoldName               = 5008

	SMTakeOnOK        = 615
	SMTakeOnFail      = 616
	SMTakeOffOK       = 619
	SMTakeOffFail     = 620
	SMAddItem         = 200
	SMDelItem         = 202
	SMUpdateItem      = 203
	SMItemShow        = 610
	SMItemHide        = 611
	SMDropItemSuccess = 600
	SMDropItemFail    = 601
	SMEatOK           = 635
	SMEatFail         = 636
)

type Command struct {
	Recog  int32
	Ident  uint16
	Param  uint16
	Tag    uint16
	Series uint16
}

func EncodePlain6Payload(src []byte) []byte {
	out := make([]byte, 0, encodedLen(len(src)))
	restCount := byte(0)
	rest := byte(0)
	for _, ch := range src {
		out, restCount, rest = appendPlain6Byte(out, ch, restCount, rest)
	}
	if restCount > 0 {
		out = append(out, rest+0x3C)
	}
	return out
}

func appendPlain6Byte(out []byte, ch byte, restCount byte, rest byte) ([]byte, byte, byte) {
	made := (rest | (ch >> (2 + restCount))) & 0x3F
	rest = ((ch << (8 - (2 + restCount))) >> 2) & 0x3F
	restCount += 2
	if restCount < 6 {
		return append(out, made+0x3C), restCount, rest
	}
	out = append(out, made+0x3C, rest+0x3C)
	return out, 0, 0
}

func DecodePlain6Payload(src []byte) ([]byte, error) {
	return decodePlain6Payload(src, false)
}

func decodePlain6Payload(src []byte, xorByPosition bool) ([]byte, error) {
	masks := map[byte]byte{2: 0xFC, 4: 0xF0, 6: 0xC0}
	out := make([]byte, 0, decodedLen(len(src)))
	bitPos := byte(2)
	madeBit := byte(0)
	tmp := byte(0)
	for _, raw := range src {
		if raw < 0x3C {
			return nil, fmt.Errorf("invalid applem2 payload byte %x", raw)
		}
		ch := raw - 0x3C
		if madeBit+6 >= 8 {
			decoded := tmp | ((ch & 0x3F) >> (6 - bitPos))
			if xorByPosition {
				decoded ^= byte(0xAA + len(out))
			}
			out = append(out, decoded)
			madeBit = 0
			if bitPos < 6 {
				bitPos += 2
			} else {
				bitPos = 2
				continue
			}
		}
		tmp = (ch << bitPos) & masks[bitPos]
		madeBit += 8 - bitPos
	}
	return out, nil
}

func MarshalCommand(cmd Command) []byte {
	out := make([]byte, CommandLen)
	binary.LittleEndian.PutUint32(out[0:4], uint32(cmd.Recog))
	binary.LittleEndian.PutUint16(out[4:6], cmd.Ident)
	binary.LittleEndian.PutUint16(out[6:8], cmd.Param)
	binary.LittleEndian.PutUint16(out[8:10], cmd.Tag)
	binary.LittleEndian.PutUint16(out[10:12], cmd.Series)
	return out
}

func UnmarshalCommand(data []byte) (Command, error) {
	if len(data) < CommandLen {
		return Command{}, fmt.Errorf("command payload too short: %d", len(data))
	}
	return Command{
		Recog:  int32(binary.LittleEndian.Uint32(data[0:4])),
		Ident:  binary.LittleEndian.Uint16(data[4:6]),
		Param:  binary.LittleEndian.Uint16(data[6:8]),
		Tag:    binary.LittleEndian.Uint16(data[8:10]),
		Series: binary.LittleEndian.Uint16(data[10:12]),
	}, nil
}

func EncodePlain6Command(cmd Command) []byte {
	return EncodePlain6Payload(MarshalCommand(cmd))
}

func DecodePlain6Command(encoded []byte) (Command, error) {
	payload, err := DecodePlain6Payload(encoded)
	if err != nil {
		return Command{}, err
	}
	return UnmarshalCommand(payload)
}

func WrapFrame(encoded []byte) []byte {
	out := make([]byte, 0, len(encoded)+2)
	out = append(out, FrameStart)
	out = append(out, encoded...)
	out = append(out, FrameEnd)
	return out
}

func UnwrapFrame(frame []byte) ([]byte, error) {
	if len(frame) < 2 {
		return nil, errors.New("frame too short")
	}
	if frame[0] != FrameStart || frame[len(frame)-1] != FrameEnd {
		return nil, errors.New("frame delimiters missing")
	}
	return frame[1 : len(frame)-1], nil
}

func SplitFrames(buffer []byte) ([][]byte, []byte) {
	frames := [][]byte{}
	search := 0
	for {
		start := -1
		for i := search; i < len(buffer); i++ {
			if buffer[i] == FrameStart {
				start = i
				break
			}
		}
		if start < 0 {
			return frames, nil
		}
		end := -1
		for i := start + 1; i < len(buffer); i++ {
			if buffer[i] == FrameEnd {
				end = i
				break
			}
		}
		if end < 0 {
			tail := make([]byte, len(buffer[start:]))
			copy(tail, buffer[start:])
			return frames, tail
		}
		frame := make([]byte, end-start+1)
		copy(frame, buffer[start:end+1])
		frames = append(frames, frame)
		search = end + 1
	}
}

func EncodePlain6ClientMessage(cmd Command, text []byte) []byte {
	payload := append(EncodePlain6Command(cmd), EncodePlain6Payload(text)...)
	return WrapFrame(payload)
}

func DecodePlain6ClientMessage(frame []byte) (Command, []byte, error) {
	encoded, err := UnwrapFrame(frame)
	if err != nil {
		return Command{}, nil, err
	}
	if len(encoded) > 0 && encoded[0] >= '1' && encoded[0] <= '9' {
		encoded = encoded[1:]
	}
	if len(encoded) < encodedLen(CommandLen) {
		return Command{}, nil, fmt.Errorf("encoded plain6 message too short: %d", len(encoded))
	}
	cmd, err := DecodePlain6Command(encoded[:encodedLen(CommandLen)])
	if err != nil {
		return Command{}, nil, err
	}
	text, err := DecodePlain6Payload(encoded[encodedLen(CommandLen):])
	if err != nil {
		return Command{}, nil, err
	}
	return cmd, text, nil
}

func encodedLen(n int) int {
	full := n / 3 * 4
	switch n % 3 {
	case 0:
		return full
	case 1:
		return full + 2
	default:
		return full + 3
	}
}

func decodedLen(n int) int {
	full := n / 4 * 3
	switch n % 4 {
	case 2:
		return full + 1
	case 3:
		return full + 2
	default:
		return full
	}
}
