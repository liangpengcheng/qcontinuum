#pragma once

#include <cstdint>
#include <vector>

#include "qcontinuum/protocol/error.hpp"

namespace qcontinuum {

// 一条解帧后的协议消息。
struct QMessage {
    int32_t id;                  // 消息号（如 2001 = ECHO）
    std::vector<uint8_t> body;   // protobuf body
};

// 消息号常量（与 proto/test.proto 的 MsgId 对齐）。
namespace msg_id {
inline constexpr int32_t ECHO = 2001;
inline constexpr int32_t HEARTBEAT = 2002;
} // namespace msg_id

} // namespace qcontinuum
