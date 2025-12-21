# Requirements Document

## Introduction

Fix the message parsing logic in network/message.go to resolve "invalid message length" errors that occur when parsing message headers. The current implementation uses unsafe pointer casting which can lead to incorrect byte interpretation due to endianness issues.

## Requirements

### Requirement 1

**User Story:** As a developer, I want message headers to be parsed correctly regardless of byte order, so that valid messages are not rejected with "invalid message length" errors.

#### Acceptance Criteria

1. WHEN a valid message header is received THEN it SHALL be parsed correctly without endianness issues
2. WHEN ReadHeadFromBuffer is called THEN it SHALL use safe byte order conversion instead of unsafe pointer casting
3. WHEN message length is within valid range THEN it SHALL not trigger "invalid message length" error
4. WHEN WriteHeadToBuffer is called THEN it SHALL write headers in a consistent byte order

### Requirement 2

**User Story:** As a developer, I want the message parsing to be robust and handle edge cases properly, so that the system doesn't fail on valid but edge-case messages.

#### Acceptance Criteria

1. WHEN message length is zero THEN it SHALL be handled as a valid empty message
2. WHEN message length is at the maximum allowed size THEN it SHALL be processed correctly
3. WHEN buffer contains partial message data THEN it SHALL wait for complete data without errors
4. WHEN multiple messages are in the buffer THEN they SHALL all be parsed correctly

### Requirement 3

**User Story:** As a developer, I want comprehensive tests for message parsing, so that I can verify the fix works correctly across different scenarios.

#### Acceptance Criteria

1. WHEN message parsing tests are run THEN they SHALL cover various message sizes including edge cases
2. WHEN endianness tests are run THEN they SHALL verify correct parsing on both little-endian and big-endian systems
3. WHEN buffer boundary tests are run THEN they SHALL verify correct handling of partial messages
4. WHEN performance tests are run THEN they SHALL verify the fix doesn't significantly impact performance

### Requirement 4

**User Story:** As a developer, I want the protobuf dependency to be updated to the modern version, so that the code uses current best practices and avoids deprecated warnings.

#### Acceptance Criteria

1. WHEN protobuf imports are updated THEN they SHALL use "google.golang.org/protobuf/proto" instead of deprecated version
2. WHEN code is compiled THEN it SHALL not show deprecation warnings for protobuf usage
3. WHEN protobuf marshaling/unmarshaling is used THEN it SHALL work correctly with the new package
4. WHEN go.mod is updated THEN it SHALL include the correct protobuf dependency version