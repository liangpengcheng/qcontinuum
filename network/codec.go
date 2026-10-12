package network

import (
	"fmt"
	"sync/atomic"

	"google.golang.org/protobuf/proto"
)

// Codec defines a generic encoder/decoder for messages.
// Implementations can provide JSON, MsgPack, FlatBuffers, etc.
type Codec interface {
	Marshal(v interface{}) ([]byte, error)
	Unmarshal(data []byte, v interface{}) error
}

// ProtobufCodec is the default codec using google.golang.org/protobuf/proto.
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

// 使用atomic.Value确保线程安全
var globalDefaultCodec atomic.Value

type codecHolder struct{ codec Codec }

func init() {
	globalDefaultCodec.Store(codecHolder{codec: ProtobufCodec{}})
}

// SetDefaultCodec sets the package-wide default codec used when none is specified.
// This function is thread-safe.
func SetDefaultCodec(c Codec) {
	if c != nil {
		globalDefaultCodec.Store(codecHolder{codec: c})
	}
}

// DefaultCodec returns the current package-wide default codec.
// This function is thread-safe.
func DefaultCodec() Codec {
	if c := globalDefaultCodec.Load(); c != nil {
		return c.(codecHolder).codec
	}
	return ProtobufCodec{}
}
