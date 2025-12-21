# Implementation Plan

- [ ] 1. Update protobuf dependency and imports
  - Update go.mod to use google.golang.org/protobuf/proto instead of deprecated github.com/golang/protobuf
  - Replace all protobuf import statements in network/message.go
  - Verify protobuf marshaling/unmarshaling still works correctly
  - _Requirements: 4.1, 4.2, 4.3, 4.4_

- [ ] 2. Implement safe message header parsing
  - Replace unsafe pointer casting in ReadHeadFromBuffer with binary.LittleEndian
  - Add encoding/binary import to network/message.go
  - Implement safe byte order conversion for both Length and ID fields
  - Maintain the same function signature for backward compatibility
  - _Requirements: 1.1, 1.2, 1.4_

- [ ] 3. Implement safe message header writing
  - Replace unsafe pointer operations in WriteHeadToBuffer with binary.LittleEndian
  - Use binary.LittleEndian.PutUint32 for consistent byte order writing
  - Ensure buffer growth logic remains intact
  - Verify zero-copy semantics are preserved
  - _Requirements: 1.1, 1.4_

- [ ] 4. Enhance message length validation
  - Create specific error types for different validation failures
  - Improve error messages in AsyncMessageReader.FeedData to include actual values
  - Add validateMessageLength helper function with detailed error reporting
  - Handle zero-length messages as valid empty messages
  - _Requirements: 2.1, 2.2, 2.3_

- [ ] 5. Create comprehensive unit tests for message header parsing
  - Write tests for ReadHeadFromBuffer with various valid message lengths including edge cases
  - Create tests for WriteHeadToBuffer to verify correct byte order writing
  - Add tests for zero-length messages and maximum-length messages
  - Test insufficient data scenarios and verify proper error handling
  - _Requirements: 3.1, 3.2_

- [ ] 6. Create endianness compatibility tests
  - Write tests that verify consistent parsing regardless of system byte order
  - Create round-trip tests (write then read) to ensure data integrity
  - Test with known byte sequences to verify little-endian encoding
  - Add tests for cross-platform compatibility scenarios
  - _Requirements: 3.2_

- [ ] 7. Create integration tests for AsyncMessageReader
  - Write tests for FeedData method with various message sizes and buffer states
  - Test partial message scenarios where complete messages arrive in multiple chunks
  - Create tests for multiple messages in a single buffer
  - Test buffer boundary conditions and memory management
  - _Requirements: 2.3, 2.4, 3.3_

- [ ] 8. Add performance benchmarks and validation
  - Create benchmarks comparing old unsafe implementation vs new safe implementation
  - Measure memory allocation patterns to ensure zero-copy semantics are maintained
  - Benchmark AsyncMessageReader.FeedData with various message patterns
  - Verify performance regression is within acceptable limits (< 10% overhead)
  - _Requirements: 3.4_

- [ ] 9. Update error handling throughout message processing
  - Replace generic "invalid message length" errors with specific error types
  - Add context information to error messages for better debugging
  - Ensure all error paths properly clean up resources (buffer releases)
  - Test error scenarios to verify graceful failure handling
  - _Requirements: 2.1, 2.2_

- [x] 10. Validate backward compatibility and integration
  - Run existing tests to ensure no regressions in current functionality
  - Test with real protobuf messages to verify marshaling/unmarshaling works
  - Verify ZeroCopyMessage functionality remains intact
  - Test compatibility with existing TCP server and WebSocket implementations
  - _Requirements: 1.3, 4.3_