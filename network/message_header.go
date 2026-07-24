package network

import (
	"encoding/binary"
	"errors"
)

const messageHeaderSize = 8

func ReadHeadFromBuffer(data []byte) (MessageHead, error) {
	if len(data) < messageHeaderSize {
		return MessageHead{}, errors.New("insufficient data for message head")
	}
	return MessageHead{
		Length: int32(binary.LittleEndian.Uint32(data[0:4])),
		ID:     int32(binary.LittleEndian.Uint32(data[4:8])),
	}, nil
}

func WriteHeadToBuffer(buffer *Buffer, head MessageHead) error {
	if err := buffer.EnsureSpace(messageHeaderSize); err != nil {
		return err
	}
	if buffer.Cap()-buffer.Len() < messageHeaderSize {
		return ErrInsufficientSize
	}

	offset := buffer.Len()
	binary.LittleEndian.PutUint32(buffer.Data()[offset:offset+4], uint32(head.Length))
	binary.LittleEndian.PutUint32(buffer.Data()[offset+4:offset+8], uint32(head.ID))
	buffer.SetLen(offset + messageHeaderSize)
	return nil
}
