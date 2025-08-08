package network

import (
	"fmt"

	"github.com/golang/protobuf/proto"
)

// Codec defines a generic encoder/decoder for messages.
// Implementations can provide JSON, MsgPack, FlatBuffers, etc.
type Codec interface {
	Marshal(v interface{}) ([]byte, error)
	Unmarshal(data []byte, v interface{}) error
}

// ProtobufCodec is the default codec using github.com/golang/protobuf/proto
type ProtobufCodec struct{}

func (ProtobufCodec) Marshal(v interface{}) ([]byte, error) {
	m, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("protobuf codec expects proto.Message, got %T", v)
	}
	return proto.Marshal(m)
}

func (ProtobufCodec) Unmarshal(data []byte, v interface{}) error {
	m, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("protobuf codec expects proto.Message, got %T", v)
	}
	return proto.Unmarshal(data, m)
}

var globalDefaultCodec Codec = ProtobufCodec{}

// SetDefaultCodec sets the package-wide default codec used when none is specified.
func SetDefaultCodec(c Codec) {
	if c != nil {
		globalDefaultCodec = c
	}
}

// DefaultCodec returns the current package-wide default codec.
func DefaultCodec() Codec {
	return globalDefaultCodec
}
