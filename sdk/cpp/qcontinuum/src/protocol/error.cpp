#include "qcontinuum/protocol/error.hpp"

#include <asio.hpp>

namespace qcontinuum {

std::string QError::to_string() const {
    return "[" + std::to_string(static_cast<int>(kind)) + "] " + message;
}

QErrorKind map_asio_error(const std::error_code& ec) {
    using asio::error::basic_errors;
    // asio 错误码映射
    if (ec == asio::error::connection_refused) {
        return QErrorKind::ConnectionRefused;
    }
    if (ec == asio::error::host_unreachable ||
        ec == asio::error::network_unreachable) {
        return QErrorKind::HostUnreachable;
    }
    if (ec == asio::error::timed_out) {
        return QErrorKind::IoTimeout;
    }
    if (ec == asio::error::eof ||
        ec == asio::error::connection_reset ||
        ec == asio::error::connection_aborted ||
        ec == asio::error::broken_pipe) {
        return QErrorKind::RemoteClosed;
    }
    if (ec == asio::error::operation_aborted) {
        return QErrorKind::Cancelled;
    }
    return QErrorKind::SendFailed;
}

} // namespace qcontinuum
