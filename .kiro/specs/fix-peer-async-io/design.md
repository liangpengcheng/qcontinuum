# Design Document

## Overview

This design addresses the async I/O implementation issues in network/peer.go and creates comprehensive test programs to validate the functionality across TCP, WebSocket, and KCP protocols.

## Architecture

### Core Components

1. **Fixed AsyncClientPeer**: Properly integrated with async I/O system
2. **Platform Abstraction**: Consistent async I/O interface across Linux/macOS
3. **Resource Management**: Proper buffer lifecycle and connection cleanup
4. **Test Suite**: Comprehensive validation programs

### Component Interactions

```
Test Programs
    ↓
AsyncClientPeer ←→ AsyncIOHandler Interface
    ↓                    ↓
ZeroCopyWriter    AsyncMessageReader
    ↓                    ↓
Platform-Specific I/O (epoll/kqueue)
    ↓
Buffer Pool Management
```

## Components and Interfaces

### 1. Platform Abstraction Layer

**Purpose**: Provide consistent async I/O interface across platforms

**Key Functions**:
- `setNonblock(fd int) error`: Set file descriptor non-blocking
- `setConnForFd(fd int, conn net.Conn)`: Map fd to connection (macOS)
- `getConnFromFd(fd int) net.Conn`: Retrieve connection by fd (macOS)

**Implementation**:
- Linux: Direct syscall operations
- macOS: Connection mapping with kqueue integration
- Windows: Compatibility layer (future)

### 2. Enhanced AsyncClientPeer

**Purpose**: Manage client connections with proper async I/O integration

**Key Methods**:
- `NewAsyncClientPeer()`: Proper initialization with error handling
- `StartAsyncIO()`: Register with reactor and start I/O processing
- `SendMessage()`: Zero-copy message sending with proper queuing
- `Close()`: Complete resource cleanup

**State Management**:
- Atomic state transitions (Connected → Closing → Closed)
- Reference counting for shared resources
- Proper reactor registration/deregistration

### 3. Resource Management

**Buffer Lifecycle**:
1. Allocation from pool via `getBuffer()`
2. Reference counting for shared access
3. Automatic release when reference count reaches zero
4. Return to pool for reuse

**Connection Cleanup**:
1. Remove from reactor
2. Close network connection
3. Release all buffers
4. Clear connection mappings (macOS)

### 4. Test Programs

**TCP Test Program**:
- Create async TCP server
- Connect multiple clients
- Send/receive various message sizes
- Measure throughput and latency
- Verify proper cleanup

**WebSocket Test Program**:
- HTTP server with WebSocket upgrade
- WebSocket client connections
- Bidirectional message exchange
- Connection lifecycle testing

**KCP Test Program**:
- KCP server/client setup
- Reliable UDP message testing
- Packet loss simulation
- Performance comparison with TCP

## Data Models

### AsyncClientPeer Structure
```go
type AsyncClientPeer struct {
    Connection   net.Conn
    fd           int
    state        int32          // atomic state
    processor    *Processor
    reader       *AsyncMessageReader
    writer       *ZeroCopyMessageWriter
    reactor      *EpollReactor
    stats        ConnectionStats
}
```

### ConnectionStats Structure
```go
type ConnectionStats struct {
    bytesRead    uint64
    bytesWritten uint64
    messagesIn   uint64
    messagesOut  uint64
    lastActive   int64
    errors       uint64
}
```

## Error Handling

### Error Categories
1. **Network Errors**: Connection failures, timeouts, resets
2. **Protocol Errors**: Invalid message format, size limits
3. **Resource Errors**: Buffer allocation failures, queue full
4. **System Errors**: File descriptor limits, permission issues

### Error Handling Strategy
1. **Graceful Degradation**: Continue operation when possible
2. **Resource Cleanup**: Always clean up on errors
3. **Event Notification**: Send appropriate events to processor
4. **Logging**: Structured logging with context

### Recovery Mechanisms
1. **Connection Retry**: Automatic reconnection for clients
2. **Buffer Recovery**: Return buffers to pool on errors
3. **State Reset**: Clean state transitions on failures

## Testing Strategy

### Unit Tests
- Individual component testing
- Mock reactor for isolated testing
- Buffer lifecycle validation
- Error condition simulation

### Integration Tests
- End-to-end protocol testing
- Multi-client scenarios
- Resource leak detection
- Performance benchmarking

### Load Tests
- High connection count testing
- Message throughput testing
- Memory usage monitoring
- CPU utilization tracking

## Performance Considerations

### Zero-Copy Optimization
- Direct buffer sharing between components
- Minimal memory allocations
- Reference counting for safety

### Async I/O Efficiency
- Edge-triggered events (Linux)
- Batch processing of events
- Non-blocking operations throughout

### Memory Management
- Buffer pool reuse
- Atomic reference counting
- Automatic garbage collection integration