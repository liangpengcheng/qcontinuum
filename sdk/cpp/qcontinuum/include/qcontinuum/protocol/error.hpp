#pragma once

#include <string>
#include <system_error>

namespace qcontinuum {

// 错误种类，对标 C#/Rust SDK 的 QErrorKind。
enum class QErrorKind {
    ConnectionRefused,  // 连接被拒绝（对端无监听）
    HostUnreachable,    // 主机不可达
    ConnectTimeout,     // 建连超时
    IoTimeout,          // IO 超时（读写）
    ProtocolViolation,  // 协议违规（坏帧头、非法 protobuf 等）
    MessageTooLarge,    // 消息超过上限（100 MiB）
    RemoteClosed,       // 对端关闭连接
    SendFailed,         // 发送失败
    ReconnectExhausted, // 重连次数耗尽
    Cancelled,          // 操作被取消
    NotConnected,       // 未连接时调用需要连接的方法
};

// qcontinuum SDK 错误。
struct QError {
    QErrorKind kind;
    std::string message;

    QError(QErrorKind k, std::string msg)
        : kind(k), message(std::move(msg)) {}

    std::string to_string() const;
};

// 把 asio::error_code 映射到 QErrorKind。
QErrorKind map_asio_error(const std::error_code& ec);

} // namespace qcontinuum
