#pragma once

#include "qcontinuum/transport/transport.hpp"

namespace qcontinuum {

// WebSocket 传输：基于 Asio TCP 的最小客户端实现。
// 固定路径 /ws，二进制消息，处理分片重组。
class WebSocketTransport : public Transport {
public:
    explicit WebSocketTransport(asio::io_context& ioc);
    ~WebSocketTransport() override = default;

    asio::awaitable<std::optional<QError>> connect(
        const std::string& host, uint16_t port,
        std::chrono::milliseconds timeout) override;

    asio::awaitable<std::optional<QError>> send(
        const std::vector<uint8_t>& packet) override;

    asio::awaitable<RecvResult> recv() override;

    asio::awaitable<void> close() override;

private:
    asio::ip::tcp::socket socket_;

    // WebSocket 握手
    asio::awaitable<std::optional<QError>> do_handshake(
        const std::string& host, uint16_t port);

    // 发送一个 WS 帧（FIN=1, opcode=binary, masked）
    asio::awaitable<std::optional<QError>> send_frame(
        const std::vector<uint8_t>& payload);

    // 接收一个完整 WS 消息（处理分片、ping/pong、close）
    asio::awaitable<RecvResult> recv_message();
};

} // namespace qcontinuum
