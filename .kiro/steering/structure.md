# Project Structure

## Root Level
- `go.mod` / `go.sum`: Go module definition and dependencies
- `test_performance.go`: Performance benchmarks and validation tests
- `REFACTOR_SUMMARY.md`: Detailed documentation of the async I/O refactor

## Core Packages

### `/base` - Foundation Utilities
Common utilities and base functionality used across the project:
- `log.go` / `zap.go`: Logging infrastructure with file rotation
- `ccolor*.go`: Cross-platform console color support (darwin/unix/windows)
- `util.go` / `time.go` / `math.go`: General utilities
- `uuid.go` / `md5.go`: ID generation and hashing
- `table_reader.go`: Data parsing utilities
- Build tags control debug vs release logging (`log_debug.go`, `log_release.go`)

### `/network` - Core Networking
Main networking layer with async I/O implementation:
- `tcpserver.go`: TCP server with both async and compatibility interfaces
- `processor.go`: Message processing and event handling system
- `peer.go`: Client connection management
- `message.go`: Message protocol definitions
- `websocket.go`: WebSocket protocol support
- `async_*.go`: Platform-specific async I/O (epoll/kqueue)
- `buffer_pool.go`: Zero-copy buffer management
- `*_test.go`: Comprehensive test suite

### `/fasthttp` & `/ginhttp` - Web Integration
WebSocket implementations for different HTTP frameworks:
- Integration with FastHTTP and Gin frameworks
- WebSocket protocol adapters

### `/kcp` - KCP Protocol
- `kcpserver.go`: KCP (reliable UDP) protocol implementation

### `/tools` - Development Tools
- `/protobuf/win/`: Windows protobuf compiler binaries

## File Naming Conventions
- `*_test.go`: Test files following Go conventions
- `*_darwin.go` / `*_linux.go` / `*_windows.go`: Platform-specific implementations
- `*_debug.go` / `*_release.go`: Build tag conditional compilation
- `*_on.go` / `*_off.go`: Feature toggle implementations

## Architecture Layers
```
Application Layer
    ↓
Processor (Message/Event handling)
    ↓
Network Layer (TCP/WebSocket/KCP)
    ↓
Async I/O (epoll/kqueue)
    ↓
Base Utilities (Logging/Utils)
```

## Key Design Patterns
- **Platform Abstraction**: Separate files for OS-specific code
- **Interface Compatibility**: Old and new APIs coexist
- **Event-Driven**: Central processor handles all events and messages
- **Zero-Copy**: Buffer pooling minimizes allocations