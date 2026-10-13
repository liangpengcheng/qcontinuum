#pragma once

#include <asio.hpp>
#include <asio/awaitable.hpp>
#include <chrono>
#include <cstdint>
#include <optional>
#include <string>
#include <vector>

#include "qcontinuum/protocol/error.hpp"

namespace qcontinuum {

// recv() 结果：data 有效，或 closed=true，或 error 有效。
struct RecvResult {
    std::vector<uint8_t> data;
    bool closed = false;
    std::optional<QError> error;
};

// 传输抽象。
class Transport {
public:
    virtual ~Transport() = default;

    // 建立连接。成功返回 nullopt。
    virtual asio::awaitable<std::optional<QError>> connect(
        const std::string& host, uint16_t port,
        std::chrono::milliseconds timeout) = 0;

    // 发送一个完整协议包（已组帧）。
    virtual asio::awaitable<std::optional<QError>> send(
        const std::vector<uint8_t>& packet) = 0;

    // 接收下一批字节。closed=true 表示对端已关闭。
    virtual asio::awaitable<RecvResult> recv() = 0;

    // 关闭连接。
    virtual asio::awaitable<void> close() = 0;
};

} // namespace qcontinuum
