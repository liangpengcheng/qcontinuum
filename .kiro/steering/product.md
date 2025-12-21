# QContinuum

QContinuum is a high-performance Go networking library designed for building scalable server applications. The project has undergone a major architectural refactor from traditional blocking I/O to a modern async, zero-copy, lock-free design while maintaining complete backward compatibility.

## Key Features

- **Async I/O Architecture**: Event-driven networking based on epoll (Linux) and kqueue (macOS)
- **Zero-Copy Design**: Memory pool management with reference counting to minimize allocations
- **Lock-Free Concurrency**: Atomic operations and lock-free ring buffers for high-performance concurrent processing
- **Protocol Support**: TCP, WebSocket, and KCP protocols
- **Backward Compatibility**: All existing APIs remain unchanged, allowing gradual migration

## Target Use Cases

- High-concurrency game servers
- Real-time messaging systems
- Network proxy and gateway applications
- Any Go application requiring high-performance networking with minimal latency