package mir176

import (
	"bytes"
	"fmt"
	"testing"
)

func encodeClientMessage(cmd Command, text []byte) []byte {
	payload := append(MarshalCommand(cmd), text...)
	return WrapFrame(payload)
}

func decodeClientMessage(frame []byte) (Command, []byte, error) {
	encoded, err := UnwrapFrame(frame)
	if err != nil {
		return Command{}, nil, err
	}
	if len(encoded) > 0 && encoded[0] >= '1' && encoded[0] <= '9' {
		encoded = encoded[1:]
	}
	if len(encoded) < CommandLen {
		return Command{}, nil, fmt.Errorf("encoded message too short: %d", len(encoded))
	}
	cmd, err := UnmarshalCommand(encoded[:CommandLen])
	if err != nil {
		return Command{}, nil, err
	}
	text := encoded[CommandLen:]
	return cmd, text, nil
}

func TestCommandRoundTrip(t *testing.T) {
	want := Command{Recog: 12345, Ident: CMIDPassword, Param: 2, Tag: 3, Series: 4}
	encoded := MarshalCommand(want)
	got, err := UnmarshalCommand(encoded)
	if err != nil {
		t.Fatalf("UnmarshalCommand() error = %v", err)
	}
	if got != want {
		t.Fatalf("command = %#v, want %#v", got, want)
	}
}

func TestClientMessageRoundTrip(t *testing.T) {
	wantCmd := Command{Ident: CMSelectServer}
	wantText := []byte("OpenMir2")
	frame := encodeClientMessage(wantCmd, wantText)
	if frame[0] != FrameStart || frame[len(frame)-1] != FrameEnd {
		t.Fatalf("frame delimiters missing: %q", frame)
	}
	gotCmd, gotText, err := decodeClientMessage(frame)
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
	frame := encodeClientMessage(wantCmd, wantText)
	withSequence := append([]byte{FrameStart, '1'}, frame[1:]...)
	gotCmd, gotText, err := decodeClientMessage(withSequence)
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
	response := append(encodeClientMessage(wantCmd, nil), FrameTrailer)
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
	gotCmd, gotText, err := decodeClientMessage(frames[0])
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
	first := encodeClientMessage(Command{Ident: CMProtocol}, nil)
	second := encodeClientMessage(Command{Ident: CMIDPassword}, []byte("test/test"))
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
