#include "qcontinuum/transport/tcp_transport.hpp"

#include <asio/use_awaitable.hpp>

namespace qcontinuum {

TcpTransport::TcpTransport(asio::io_context& ioc) : socket_(ioc) {}

asio::awaitable<std::optional<QError>> TcpTransport::connect(
    const std::string& host, uint16_t port,
    std::chrono::milliseconds timeout) {
    try {
        asio::ip::tcp::resolver resolver(socket_.get_executor());
        auto endpoints = co_await resolver.async_resolve(
            host, std::to_string(port), asio::use_awaitable);

        try {
            co_await asio::async_connect(socket_, endpoints,
                                         asio::use_awaitable);
        } catch (const std::system_error& e) {
            co_return QError(map_asio_error(e.code()),
                             "tcp connect failed: " + std::string(e.what()));
        }

        socket_.set_option(asio::ip::tcp::no_delay(true));
        co_return std::nullopt;
    } catch (const std::exception& e) {
        co_return QError(QErrorKind::ConnectTimeout,
                         std::string("tcp connect exception: ") + e.what());
    }
}

asio::awaitable<std::optional<QError>> TcpTransport::send(
    const std::vector<uint8_t>& packet) {
    try {
        co_await asio::async_write(socket_, asio::buffer(packet),
                                   asio::use_awaitable);
        co_return std::nullopt;
    } catch (const std::system_error& e) {
        co_return QError(map_asio_error(e.code()),
                         "tcp send failed: " + std::string(e.what()));
    }
}

asio::awaitable<RecvResult> TcpTransport::recv() {
    RecvResult result;
    try {
        std::vector<uint8_t> buf(64 * 1024);
        std::size_t n = co_await socket_.async_read_some(
            asio::buffer(buf), asio::use_awaitable);
        if (n == 0) {
            result.closed = true;
        } else {
            buf.resize(n);
            result.data = std::move(buf);
        }
    } catch (const std::system_error& e) {
        auto kind = map_asio_error(e.code());
        if (kind == QErrorKind::RemoteClosed) {
            result.closed = true;
        } else {
            result.error = QError(kind, "tcp recv failed: " + std::string(e.what()));
        }
    }
    co_return result;
}

asio::awaitable<void> TcpTransport::close() {
    std::error_code ec;
    socket_.shutdown(asio::ip::tcp::socket::shutdown_both, ec);
    socket_.close(ec);
    co_return;
}

} // namespace qcontinuum
