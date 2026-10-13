#pragma once

#include <cstdint>
#include <optional>
#include <vector>

#include "qcontinuum/protocol/error.hpp"

namespace qcontinuum {
namespace framing {

// 帧头长度（字节）。
inline constexpr std::size_t HEADER_LEN = 8;
// 单包 body 上限：100 MiB。
inline constexpr int32_t MAX_MESSAGE_SIZE = 100 * 1024 * 1024;

// 编码结果：成功时 data 有效，失败时 error 有效。
struct EncodeResult {
    std::vector<uint8_t> data;
    std::optional<QError> error;
};

// 把 (id, body) 编码为完整协议包。
EncodeResult encode(int32_t id, const uint8_t* body, std::size_t len);
inline EncodeResult encode(int32_t id, const std::vector<uint8_t>& body) {
    return encode(id, body.data(), body.size());
}

// 帧头解析结果：
// - header 有效 → 成功解析
// - header 为 nullopt 且 error 为 nullopt → 数据不足 8 字节
// - error 有效 → 协议违规
struct HeaderResult {
    std::optional<std::pair<int32_t, int32_t>> header; // (length, id)
    std::optional<QError> error;
};

HeaderResult try_read_header(const uint8_t* data, std::size_t len);

// 取包结果：
// - packet 有效 → 取到完整包
// - packet 为 nullopt 且 error 为 nullopt → 数据不足
// - error 有效 → 协议违规（调用方应关闭连接）
struct TakeResult {
    std::optional<std::pair<int32_t, std::vector<uint8_t>>> packet; // (id, body)
    std::optional<QError> error;
};

// 流式组包器：处理粘包 / 半包 / 跨包。
class PacketReader {
public:
    PacketReader() = default;

    // 喂入新收到的数据。
    void feed(const uint8_t* data, std::size_t len);

    // 尝试取出一个完整包。
    TakeResult try_take();

    // 清空内部缓冲（重连时调用，丢弃半包）。
    void reset();

    std::size_t buffered_len() const { return buf_.size(); }

private:
    std::vector<uint8_t> buf_;
};

} // namespace framing
} // namespace qcontinuum
