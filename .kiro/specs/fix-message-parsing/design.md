# Design Document

## Overview

The current message parsing implementation in `network/message.go` has critical issues with byte order interpretation when using unsafe pointer casting. This leads to "invalid message length" errors for valid messages. The design addresses these issues by implementing safe byte order conversion, updating deprecated protobuf dependencies, and adding comprehensive testing.

## Architecture

### Current Issues Analysis

1. **Unsafe Pointer Casting**: `ReadHeadFromBuffer` uses `unsafe.Pointer` to directly cast bytes to `MessageHead` struct, which assumes native byte order
2. **Endianness Problems**: Different systems (little-endian vs big-endian) interpret the same bytes differently
3. **Deprecated Dependencies**: Using old `github.com/golang/protobuf/proto` instead of modern `google.golang.org/protobuf/proto`
4. **Insufficient Validation**: Edge cases in message length validation are not properly handled

### Solution Architecture

```
Message Parsing Flow (Fixed):
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│ Raw Bytes       │───▶│ Safe Byte Order  │───▶│ Validated       │
│ from Network    │    │ Conversion       │    │ MessageHead     │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                              │
                              ▼
                       ┌──────────────────┐
                       │ Binary Encoding  │
                       │ (Little Endian)  │
                       └──────────────────┘
```

## Components and Interfaces

### 1. Safe Message Header Parsing

**Current Implementation (Problematic):**
```go
func ReadHeadFromBuffer(data []byte) (MessageHead, error) {
    head := (*MessageHead)(unsafe.Pointer(&data[0]))
    return *head, nil
}
```

**New Implementation (Safe):**
```go
func ReadHeadFromBuffer(data []byte) (MessageHead, error) {
    if len(data) < 8 {
        return MessageHead{}, errors.New("insufficient data for message head")
    }
    
    // Use binary.LittleEndian for consistent byte order
    length := int32(binary.LittleEndian.Uint32(data[0:4]))
    id := int32(binary.LittleEndian.Uint32(data[4:8]))
    
    return MessageHead{
        Length: length,
        ID:     id,
    }, nil
}
```

### 2. Safe Message Header Writing

**New Implementation:**
```go
func WriteHeadToBuffer(buffer *Buffer, head MessageHead) {
    if buffer.Cap()-buffer.Len() < 8 {
        buffer.Grow(8)
    }
    
    data := buffer.Data()[buffer.Len():]
    binary.LittleEndian.PutUint32(data[0:4], uint32(head.Length))
    binary.LittleEndian.PutUint32(data[4:8], uint32(head.ID))
    buffer.SetLen(buffer.Len() + 8)
}
```

### 3. Enhanced Message Length Validation

**Improved Validation Logic:**
```go
func validateMessageLength(length int32) error {
    if length < 0 {
        return errors.New("negative message length")
    }
    if length > int32(maxMessageLength) {
        return fmt.Errorf("message length %d exceeds maximum %d", length, maxMessageLength)
    }
    return nil
}
```

### 4. Updated Protobuf Integration

**Dependency Update:**
- Replace `github.com/golang/protobuf/proto` with `google.golang.org/protobuf/proto`
- Update all import statements
- Ensure compatibility with existing protobuf usage

## Data Models

### MessageHead Structure
```go
type MessageHead struct {
    Length int32  // Message body length in bytes
    ID     int32  // Message type identifier
}
```

**Wire Format (Little Endian):**
```
Bytes 0-3: Length (int32, little endian)
Bytes 4-7: ID (int32, little endian)
```

### ZeroCopyMessage Enhancement
```go
type ZeroCopyMessage struct {
    Head   MessageHead
    Buffer *Buffer
    Offset int
    // Add validation state
    validated bool
}
```

## Error Handling

### 1. Specific Error Types
```go
var (
    ErrInsufficientData = errors.New("insufficient data for message head")
    ErrNegativeLength   = errors.New("negative message length")
    ErrMessageTooLarge  = errors.New("message length exceeds maximum")
    ErrInvalidBuffer    = errors.New("invalid buffer state")
)
```

### 2. Error Context Enhancement
- Add detailed error messages with actual vs expected values
- Include buffer state information in error context
- Provide debugging information for troubleshooting

### 3. Graceful Degradation
- Handle partial message scenarios without errors
- Maintain backward compatibility with existing error handling
- Ensure resource cleanup on error conditions

## Testing Strategy

### 1. Unit Tests

**Message Header Parsing Tests:**
```go
func TestReadHeadFromBuffer(t *testing.T) {
    tests := []struct {
        name     string
        input    []byte
        expected MessageHead
        wantErr  bool
    }{
        {
            name:     "valid header",
            input:    []byte{0x10, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00},
            expected: MessageHead{Length: 16, ID: 1},
            wantErr:  false,
        },
        {
            name:     "zero length message",
            input:    []byte{0x00, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00},
            expected: MessageHead{Length: 0, ID: 2},
            wantErr:  false,
        },
        // Add more test cases...
    }
}
```

**Endianness Tests:**
```go
func TestEndianness(t *testing.T) {
    // Test that parsing is consistent regardless of system endianness
    head := MessageHead{Length: 1000, ID: 42}
    
    buffer := GetBuffer()
    defer buffer.Release()
    
    WriteHeadToBuffer(buffer, head)
    parsed, err := ReadHeadFromBuffer(buffer.Bytes())
    
    assert.NoError(t, err)
    assert.Equal(t, head, parsed)
}
```

### 2. Integration Tests

**AsyncMessageReader Tests:**
```go
func TestAsyncMessageReaderWithVariousLengths(t *testing.T) {
    reader := NewAsyncMessageReader()
    defer reader.Release()
    
    // Test with messages of different sizes
    testSizes := []int{0, 1, 100, 1024, 65536, maxMessageLength}
    
    for _, size := range testSizes {
        // Create test message and verify parsing
    }
}
```

### 3. Performance Tests

**Benchmark Comparisons:**
```go
func BenchmarkReadHeadFromBuffer_Old(b *testing.B) {
    // Benchmark old unsafe implementation
}

func BenchmarkReadHeadFromBuffer_New(b *testing.B) {
    // Benchmark new safe implementation
}
```

### 4. Compatibility Tests

**Backward Compatibility:**
- Verify existing message formats still work
- Test with real network data
- Validate protobuf integration

## Implementation Phases

### Phase 1: Core Message Header Fixes
1. Replace unsafe pointer casting with safe binary encoding
2. Update `ReadHeadFromBuffer` and `WriteHeadToBuffer` functions
3. Add comprehensive error handling

### Phase 2: Protobuf Dependency Update
1. Update go.mod with new protobuf dependency
2. Replace all import statements
3. Verify compatibility with existing protobuf usage

### Phase 3: Enhanced Validation and Testing
1. Implement robust message length validation
2. Add comprehensive unit tests
3. Create integration tests for AsyncMessageReader

### Phase 4: Performance Validation
1. Benchmark new implementation vs old
2. Ensure performance regression is minimal
3. Optimize if necessary

## Backward Compatibility

### API Compatibility
- All existing function signatures remain unchanged
- Error types and messages may be enhanced but remain compatible
- Zero-copy semantics are preserved

### Wire Protocol Compatibility
- Message format on wire remains identical
- Little-endian byte order is enforced consistently
- Existing clients and servers continue to work

### Migration Path
- Changes are drop-in replacements
- No configuration changes required
- Existing code continues to work without modification