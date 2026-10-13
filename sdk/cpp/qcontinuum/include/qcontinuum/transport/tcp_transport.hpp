#pragma once

#include "qcontinuum/transport/transport.hpp"

namespace qcontinuum {

// TCP 传输：asio::ip::tcp::socket，开启 nodelay。
class TcpTransport : public Transport {
public:
    explicit TcpTransport(asio::io_context& ioc);
    ~TcpTransport() override = default;

    asio::awaitable<std::optional<QError>> connect(
        const std::string& host, uint16_t port,
        std::chrono::milliseconds timeout) override;

    asio::awaitable<std::optional<QError>> send(
        const std::vector<uint8_t>& packet) override;

    asio::awaitable<RecvResult> recv() override;

    asio::awaitable<void> close() override;

private:
    asio::ip::tcp::socket socket_;
};

} // namespace qcontinuum
