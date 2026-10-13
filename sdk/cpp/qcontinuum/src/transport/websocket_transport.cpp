#include "qcontinuum/transport/websocket_transport.hpp"

#include <asio/use_awaitable.hpp>
#include <random>
#include <sstream>

namespace qcontinuum {

namespace {

// Base64 编码（用于 Sec-WebSocket-Key）
std::string base64_encode(const uint8_t* data, std::size_t len) {
    static const char* chars =
        "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    std::string out;
    for (std::size_t i = 0; i < len; i += 3) {
        uint32_t n = data[i] << 16;
        if (i + 1 < len) n |= data[i + 1] << 8;
        if (i + 2 < len) n |= data[i + 2];
        out.push_back(chars[(n >> 18) & 63]);
        out.push_back(chars[(n >> 12) & 63]);
        out.push_back(i + 1 < len ? chars[(n >> 6) & 63] : '=');
        out.push_back(i + 2 < len ? chars[n & 63] : '=');
    }
    return out;
}

} // namespace

WebSocketTransport::WebSocketTransport(asio::io_context& ioc) : socket_(ioc) {}

asio::awaitable<std::optional<QError>> WebSocketTransport::connect(
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
                             "ws tcp connect failed: " + std::string(e.what()));
        }
        socket_.set_option(asio::ip::tcp::no_delay(true));

        // WebSocket 握手
        if (auto err = co_await do_handshake(host, port); err.has_value()) {
            co_return err;
        }
        co_return std::nullopt;
    } catch (const std::exception& e) {
        co_return QError(QErrorKind::ConnectTimeout,
                         std::string("ws connect exception: ") + e.what());
    }
}

asio::awaitable<std::optional<QError>> WebSocketTransport::do_handshake(
    const std::string& host, uint16_t port) {
    // 生成 Sec-WebSocket-Key（16 随机字节的 base64）
    std::random_device rd;
    std::mt19937 gen(rd());
    uint8_t key_bytes[16];
    for (auto& b : key_bytes) b = static_cast<uint8_t>(gen());
    std::string key = base64_encode(key_bytes, 16);

    std::ostringstream req;
    req << "GET /ws HTTP/1.1\r\n"
        << "Host: " << host << ":" << port << "\r\n"
        << "Upgrade: websocket\r\n"
        << "Connection: Upgrade\r\n"
        << "Sec-WebSocket-Key: " << key << "\r\n"
        << "Sec-WebSocket-Version: 13\r\n"
        << "\r\n";
    std::string req_str = req.str();

    try {
        co_await asio::async_write(socket_, asio::buffer(req_str),
                                   asio::use_awaitable);

        // 读响应头（直到 \r\n\r\n）
        asio::streambuf resp_buf;
        co_await asio::async_read_until(socket_, resp_buf, "\r\n\r\n",
                                        asio::use_awaitable);
        std::istream resp(&resp_buf);
        std::string status_line;
        std::getline(resp, status_line);
        // 期望 "HTTP/1.1 101 ..."
        if (status_line.find("101") == std::string::npos) {
            co_return QError(QErrorKind::ProtocolViolation,
                             "ws handshake failed: " + status_line);
        }
        co_return std::nullopt;
    } catch (const std::system_error& e) {
        co_return QError(map_asio_error(e.code()),
                         "ws handshake failed: " + std::string(e.what()));
    }
}

asio::awaitable<std::optional<QError>> WebSocketTransport::send(
    const std::vector<uint8_t>& packet) {
    return send_frame(packet);
}

asio::awaitable<std::optional<QError>> WebSocketTransport::send_frame(
    const std::vector<uint8_t>& payload) {
    try {
        std::vector<uint8_t> frame;
        frame.push_back(0x82); // FIN=1, opcode=2 (binary)

        std::size_t len = payload.size();
        // 客户端必须 mask，所以 MASK 位为 1
        if (len < 126) {
            frame.push_back(0x80 | static_cast<uint8_t>(len));
        } else if (len < 65536) {
            frame.push_back(0x80 | 126);
            frame.push_back((len >> 8) & 0xFF);
            frame.push_back(len & 0xFF);
        } else {
            frame.push_back(0x80 | 127);
            for (int i = 7; i >= 0; --i) {
                frame.push_back((len >> (i * 8)) & 0xFF);
            }
        }

        // mask key（4 随机字节）
        std::random_device rd;
        std::mt19937 gen(rd());
        uint8_t mask[4];
        for (auto& b : mask) b = static_cast<uint8_t>(gen());
        frame.insert(frame.end(), mask, mask + 4);

        // masked payload
        for (std::size_t i = 0; i < len; ++i) {
            frame.push_back(payload[i] ^ mask[i % 4]);
        }

        co_await asio::async_write(socket_, asio::buffer(frame),
                                   asio::use_awaitable);
        co_return std::nullopt;
    } catch (const std::system_error& e) {
        co_return QError(map_asio_error(e.code()),
                         "ws send failed: " + std::string(e.what()));
    }
}

asio::awaitable<RecvResult> WebSocketTransport::recv() {
    return recv_message();
}

asio::awaitable<RecvResult> WebSocketTransport::recv_message() {
    RecvResult result;
    std::vector<uint8_t> assembled;

    try {
        while (true) {
            // 读帧头（至少 2 字节）
            uint8_t hdr[2];
            co_await asio::async_read(socket_, asio::buffer(hdr, 2),
                                      asio::use_awaitable);

            bool fin = (hdr[0] & 0x80) != 0;
            uint8_t opcode = hdr[0] & 0x0F;
            bool masked = (hdr[1] & 0x80) != 0;
            uint64_t payload_len = hdr[1] & 0x7F;

            if (payload_len == 126) {
                uint8_t ext[2];
                co_await asio::async_read(socket_, asio::buffer(ext, 2),
                                          asio::use_awaitable);
                payload_len = (static_cast<uint64_t>(ext[0]) << 8) | ext[1];
            } else if (payload_len == 127) {
                uint8_t ext[8];
                co_await asio::async_read(socket_, asio::buffer(ext, 8),
                                          asio::use_awaitable);
                payload_len = 0;
                for (int i = 0; i < 8; ++i) {
                    payload_len = (payload_len << 8) | ext[i];
                }
            }

            uint8_t mask[4] = {};
            if (masked) {
                co_await asio::async_read(socket_, asio::buffer(mask, 4),
                                          asio::use_awaitable);
            }

            // 读 payload
            std::vector<uint8_t> payload(payload_len);
            if (payload_len > 0) {
                co_await asio::async_read(socket_,
                                          asio::buffer(payload),
                                          asio::use_awaitable);
                if (masked) {
                    for (std::size_t i = 0; i < payload_len; ++i) {
                        payload[i] ^= mask[i % 4];
                    }
                }
            }

            // 处理 opcode
            if (opcode == 0x8) { // close
                result.closed = true;
                co_return result;
            } else if (opcode == 0x9) { // ping → 回 pong
                std::vector<uint8_t> pong_frame;
                pong_frame.push_back(0x8A); // FIN=1, opcode=0xA
                pong_frame.push_back(static_cast<uint8_t>(payload.size()));
                pong_frame.insert(pong_frame.end(), payload.begin(), payload.end());
                co_await asio::async_write(socket_, asio::buffer(pong_frame),
                                           asio::use_awaitable);
                continue;
            } else if (opcode == 0xA) { // pong，忽略
                continue;
            } else if (opcode == 0x2 || opcode == 0x0) { // binary / continuation
                assembled.insert(assembled.end(), payload.begin(), payload.end());
                if (fin) {
                    result.data = std::move(assembled);
                    co_return result;
                }
                // 否则继续收分片
            } else {
                // 未知 opcode，忽略
                continue;
            }
        }
    } catch (const std::system_error& e) {
        auto kind = map_asio_error(e.code());
        if (kind == QErrorKind::RemoteClosed) {
            result.closed = true;
        } else {
            result.error = QError(kind, "ws recv failed: " + std::string(e.what()));
        }
    }
    co_return result;
}

asio::awaitable<void> WebSocketTransport::close() {
    try {
        // 发送 close 帧
        std::vector<uint8_t> close_frame = {0x88, 0x00};
        co_await asio::async_write(socket_, asio::buffer(close_frame),
                                   asio::use_awaitable);
    } catch (...) {
    }
    std::error_code ec;
    socket_.shutdown(asio::ip::tcp::socket::shutdown_both, ec);
    socket_.close(ec);
    co_return;
}

} // namespace qcontinuum
