#pragma once

#include <asio.hpp>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <functional>
#include <future>
#include <memory>
#include <mutex>
#include <string>
#include <thread>

#include "qcontinuum/protocol/error.hpp"
#include "qcontinuum/protocol/message.hpp"
#include "qcontinuum/transport/transport.hpp"

namespace qcontinuum {

// 传输类型。
enum class TransportKind {
    Tcp,
    Kcp,
    WebSocket,
};

// 连接状态。
enum class ConnectionState {
    Disconnected,
    Connecting,
    Connected,
    Reconnecting,
};

// QClient 配置。
struct QClientOptions {
    TransportKind transport = TransportKind::Tcp;
    std::string host = "127.0.0.1";
    uint16_t port = 9001;
    std::chrono::milliseconds connect_timeout{10000};
    // 最大重连次数。0 = 无限重连。
    uint32_t max_reconnect_attempts = 0;
    std::chrono::milliseconds reconnect_base_delay{500};
    std::chrono::milliseconds reconnect_max_delay{30000};
    double reconnect_jitter = 0.2;
};

// qcontinuum 高层客户端。
//
// 后台 io_context 线程驱动网络；消息通过回调投递。
class QClient : public std::enable_shared_from_this<QClient> {
public:
    using MessageCallback =
        std::function<void(const QMessage&)>;
    using StateCallback =
        std::function<void(ConnectionState)>;

    static std::shared_ptr<QClient> create(
        QClientOptions options,
        MessageCallback on_message,
        StateCallback on_state = nullptr);

    ~QClient();

    // 连接服务器（阻塞直到成功或超时）。后台启动收包与重连协程。
    std::optional<QError> connect();

    // 发送 protobuf 消息（线程安全，非阻塞）。
    // 模板：T 需有 SerializeToString 方法（protobuf Message）。
    template <typename T>
    std::optional<QError> send_message(int32_t id, const T& msg) {
        std::string body;
        if (!msg.SerializeToString(&body)) {
            return QError(QErrorKind::ProtocolViolation,
                          "protobuf serialize failed");
        }
        return send_raw(id,
                        reinterpret_cast<const uint8_t*>(body.data()),
                        body.size());
    }

    // 发送原始 (id, body)。线程安全，非阻塞。
    std::optional<QError> send_raw(int32_t id, const uint8_t* body,
                                   std::size_t len);

    // 当前连接状态（线程安全）。
    ConnectionState state() const;

    // 主动关闭（不再重连）。
    void close();

private:
    QClient(QClientOptions options, MessageCallback on_message,
            StateCallback on_state);

    void set_state(ConnectionState s);

    // 后台协程：收包循环
    asio::awaitable<void> recv_loop();
    // 后台协程：重连
    asio::awaitable<bool> do_reconnect();

    static std::chrono::milliseconds backoff_delay(
        const QClientOptions& opts, uint32_t attempt);

    QClientOptions options_;
    MessageCallback on_message_;
    StateCallback on_state_;

    asio::io_context ioc_;
    asio::executor_work_guard<asio::io_context::executor_type> work_guard_;
    std::thread io_thread_;

    std::unique_ptr<Transport> transport_;
    mutable std::mutex mutex_; // 保护 state_ 与 closing_（跨线程）
    ConnectionState state_ = ConnectionState::Disconnected;
    bool closing_ = false;
};

} // namespace qcontinuum
