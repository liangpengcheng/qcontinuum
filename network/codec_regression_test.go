package network

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestProtobufCodecRoundTrip(t *testing.T) {
	codec := ProtobufCodec{}
	var message proto.Message = wrapperspb.String("hello")
	wire, err := codec.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	wantWire := []byte{0x0a, 0x05, 'h', 'e', 'l', 'l', 'o'}
	if !bytes.Equal(wire, wantWire) {
		t.Fatalf("protobuf wire format changed: got %x, want %x", wire, wantWire)
	}

	decoded := new(wrapperspb.StringValue)
	if err := codec.Unmarshal(wire, decoded); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(message, decoded) {
		t.Fatalf("protobuf round trip changed the message: got %v, want %v", decoded, message)
	}
}

func TestProtobufCodecRejectsNonMessages(t *testing.T) {
	codec := ProtobufCodec{}
	for _, value := range []interface{}{nil, struct{}{}} {
		if _, err := codec.Marshal(value); err == nil {
			t.Errorf("Marshal accepted non-message %T", value)
		}
		if err := codec.Unmarshal(nil, value); err == nil {
			t.Errorf("Unmarshal accepted non-message %T", value)
		}
	}
}

func TestProtobufCodecRejectsMalformedData(t *testing.T) {
	// The length prefix promises five bytes, but only one follows.
	wire := []byte{0x0a, 0x05, 'x'}
	if err := (ProtobufCodec{}).Unmarshal(wire, new(wrapperspb.StringValue)); err == nil {
		t.Fatal("Unmarshal accepted a truncated protobuf message")
	}
}

type alternateCodec struct{}

func (alternateCodec) Marshal(v interface{}) ([]byte, error) {
	return []byte("alternate"), nil
}

func (alternateCodec) Unmarshal(data []byte, v interface{}) error {
	return nil
}

func TestDefaultCodecAcceptsDifferentImplementations(t *testing.T) {
	defer SetDefaultCodec(ProtobufCodec{})
	SetDefaultCodec(alternateCodec{})
	if _, ok := DefaultCodec().(alternateCodec); !ok {
		t.Fatalf("default codec was not replaced")
	}
}
