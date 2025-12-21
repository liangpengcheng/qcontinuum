# Technology Stack

## Language & Runtime
- **Go 1.12+**: Primary language with focus on performance and concurrency
- **Platform Support**: Linux (epoll), macOS (kqueue), Windows (compatibility layer)

## Core Dependencies
- **Networking**: `valyala/fasthttp`, `gorilla/websocket`, `fasthttp/websocket`
- **Logging**: `go.uber.org/zap` for structured, high-performance logging
- **Protocols**: `xtaci/kcp-go` for KCP protocol support
- **Web Framework**: `gin-gonic/gin` for HTTP/WebSocket endpoints
- **Database**: `gopkg.in/mgo.v2` (MongoDB), `gopkg.in/couchbase/gocb.v1` (Couchbase)
- **Utilities**: `google/uuid`, `golang/protobuf`

## Build System
- **Module**: Uses Go modules (`go.mod`) with replace directives for specific versions
- **Testing**: Standard Go testing with `go test`
- **Performance Testing**: Custom performance benchmarks in `test_performance.go`

## Common Commands

### Development
```bash
# Run tests
go test ./...

# Run specific package tests
go test ./network
go test ./base

# Run performance tests
go run test_performance.go

# Build the project
go build

# Get dependencies
go mod tidy
go mod download
```

### Testing
```bash
# Run network tests (includes TCP server and WebSocket tests)
go test ./network -v

# Run base utility tests
go test ./base -v

# Run processor tests specifically
go test ./network -run TestProcessor
```

## Architecture Patterns
- **Event-Driven**: Reactor pattern with epoll/kqueue
- **Zero-Copy**: Buffer pooling and reference counting
- **Lock-Free**: Atomic operations and CAS for concurrency
- **Backward Compatible**: Wrapper interfaces maintain old API