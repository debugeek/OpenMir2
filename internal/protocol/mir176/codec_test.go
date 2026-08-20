package mir176

import (
	"bytes"
	"testing"
)

func TestPayloadRoundTrip(t *testing.T) {
	cases := [][]byte{
		nil,
		{0},
		{1, 2},
		{1, 2, 3},
		[]byte("test/test"),
		[]byte("127.0.0.1/7100/12345"),
	}
	for _, tc := range cases {
		encoded := EncodePayload(tc)
		decoded, err := DecodePayload(encoded)
		if err != nil {
			t.Fatalf("DecodePayload(%q) error = %v", encoded, err)
		}
		if !bytes.Equal(decoded, tc) {
			t.Fatalf("roundtrip = %v, want %v", decoded, tc)
		}
	}
}

func TestCommandRoundTrip(t *testing.T) {
	want := Command{Recog: 12345, Ident: CMIDPassword, Param: 2, Tag: 3, Series: 4}
	encoded := EncodeCommand(want)
	if len(encoded) != 16 {
		t.Fatalf("encoded command length = %d", len(encoded))
	}
	got, err := DecodeCommand(encoded)
	if err != nil {
		t.Fatalf("DecodeCommand() error = %v", err)
	}
	if got != want {
		t.Fatalf("command = %#v, want %#v", got, want)
	}
}

func TestClientMessageRoundTrip(t *testing.T) {
	wantCmd := Command{Ident: CMSelectServer}
	wantText := []byte("OpenMir2")
	frame := EncodeClientMessage(wantCmd, wantText)
	if frame[0] != FrameStart || frame[len(frame)-1] != FrameEnd {
		t.Fatalf("frame delimiters missing: %q", frame)
	}
	gotCmd, gotText, err := DecodeClientMessage(frame)
	if err != nil {
		t.Fatalf("DecodeClientMessage() error = %v", err)
	}
	if gotCmd != wantCmd {
		t.Fatalf("command = %#v, want %#v", gotCmd, wantCmd)
	}
	if !bytes.Equal(gotText, wantText) {
		t.Fatalf("text = %q, want %q", gotText, wantText)
	}
}

func TestClientMessageRoundTripWithSequenceDigit(t *testing.T) {
	wantCmd := Command{Ident: CMIDPassword}
	wantText := []byte("test/test")
	frame := EncodeClientMessage(wantCmd, wantText)
	withSequence := append([]byte{FrameStart, '1'}, frame[1:]...)
	gotCmd, gotText, err := DecodeClientMessage(withSequence)
	if err != nil {
		t.Fatalf("DecodeClientMessage() error = %v", err)
	}
	if gotCmd != wantCmd {
		t.Fatalf("command = %#v, want %#v", gotCmd, wantCmd)
	}
	if !bytes.Equal(gotText, wantText) {
		t.Fatalf("text = %q, want %q", gotText, wantText)
	}
}

func TestPlain6ClientMessageRoundTripWithSequenceDigit(t *testing.T) {
	wantCmd := Command{Ident: 86, Recog: 1, Param: 2, Tag: 3, Series: 4}
	wantText := []byte("account/password")
	frame := EncodePlain6ClientMessage(wantCmd, wantText)
	withSequence := append([]byte{FrameStart, '2'}, frame[1:]...)
	gotCmd, gotText, err := DecodePlain6ClientMessage(withSequence)
	if err != nil {
		t.Fatalf("DecodePlain6ClientMessage() error = %v", err)
	}
	if gotCmd != wantCmd {
		t.Fatalf("command = %#v, want %#v", gotCmd, wantCmd)
	}
	if !bytes.Equal(gotText, wantText) {
		t.Fatalf("text = %q, want %q", gotText, wantText)
	}
}

func TestPlain6CapturedHandshakeFrameDecodesPlausibly(t *testing.T) {
	frame := []byte{0x23, 0x31, 0x3c, 0x3c, 0x3c, 0x3c, 0x3c, 0x48, 0x54, 0x47, 0x3c, 0x3c, 0x3c, 0x3c, 0x3c, 0x3c, 0x3c, 0x3c, 0x21}
	cmd, text, err := DecodePlain6ClientMessage(frame)
	if err != nil {
		t.Fatalf("DecodePlain6ClientMessage() error = %v", err)
	}
	if cmd.Ident != 3014 {
		t.Fatalf("plain6 ident = %d, want 3014", cmd.Ident)
	}
	if len(text) != 0 {
		t.Fatalf("text length = %d, want empty", len(text))
	}
}

func TestServerMessageRoundTrip(t *testing.T) {
	wantCmd := Command{Ident: SMCertificationOK}
	response := EncodeServerMessage(wantCmd, nil)
	if response[len(response)-1] != FrameTrailer {
		t.Fatalf("server response trailer = %q, want %q", response[len(response)-1], FrameTrailer)
	}
	frames, tail := SplitFrames(response)
	if len(frames) != 1 {
		t.Fatalf("frames = %d", len(frames))
	}
	if len(tail) != 0 {
		t.Fatalf("tail = %q, want empty", tail)
	}
	gotCmd, gotText, err := DecodeClientMessage(frames[0])
	if err != nil {
		t.Fatalf("DecodeClientMessage() error = %v", err)
	}
	if gotCmd != wantCmd {
		t.Fatalf("command = %#v, want %#v", gotCmd, wantCmd)
	}
	if len(gotText) != 0 {
		t.Fatalf("text = %q, want empty", gotText)
	}
}

func TestUnwrapFrameRejectsBadFrame(t *testing.T) {
	if _, err := UnwrapFrame([]byte("bad")); err == nil {
		t.Fatalf("expected bad frame error")
	}
}

func TestSplitFrames(t *testing.T) {
	first := EncodeClientMessage(Command{Ident: CMProtocol}, nil)
	second := EncodeClientMessage(Command{Ident: CMIDPassword}, []byte("test/test"))
	buffer := append([]byte("noise"), first...)
	buffer = append(buffer, second[:len(second)-2]...)
	frames, tail := SplitFrames(buffer)
	if len(frames) != 1 {
		t.Fatalf("frames = %d", len(frames))
	}
	if !bytes.Equal(frames[0], first) {
		t.Fatalf("first frame mismatch")
	}
	if !bytes.Equal(tail, second[:len(second)-2]) {
		t.Fatalf("tail = %q, want %q", tail, second[:len(second)-2])
	}
}
