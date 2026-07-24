package network

import "testing"

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
