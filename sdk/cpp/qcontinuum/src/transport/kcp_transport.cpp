#include "qcontinuum/transport/kcp_transport.hpp"

#include <asio/use_awaitable.hpp>
#include <chrono>
#include <random>

namespace qcontinuum {

KcpTransport::KcpTransport(asio::io_context& ioc)
    : socket_(ioc), update_timer_(ioc) {}

KcpTransport::~KcpTransport() {
    update_running_ = false;
    update_timer_.cancel();
    if (kcp_) {
        ikcp_release(kcp_);
    }
}

uint32_t KcpTransport::now_ms() {
    using namespace std::chrono;
    return static_cast<uint32_t>(
        duration_cast<milliseconds>(steady_clock::now().time_since_epoch())
            .count());
}

int KcpTransport::kcp_output(const char* buf, int len, ikcpcb* /*kcp*/,
                             void* user) {
    auto* self = static_cast<KcpTransport*>(user);
    // 同步发送（ikcp 回调中不能用协程，用阻塞 send_to）
    std::error_code ec;
    self->socket_.send_to(asio::buffer(buf, len), self->remote_ep_, 0, ec);
    return 0;
}

asio::awaitable<std::optional<QError>> KcpTransport::connect(
    const std::string& host, uint16_t port,
    std::chrono::milliseconds /*timeout*/) {
    try {
        // 绑定本地 UDP（系统分配端口）
        socket_.open(asio::ip::udp::v4());
        socket_.bind(asio::ip::udp::endpoint(asio::ip::udp::v4(), 0));

        asio::ip::udp::resolver resolver(socket_.get_executor());
        auto endpoints = co_await resolver.async_resolve(
            asio::ip::udp::v4(), host, std::to_string(port),
            asio::use_awaitable);
        remote_ep_ = *endpoints.begin();

        // 随机 conv（对标 kcp-go 客户端）
        std::random_device rd;
        conv_ = rd();

        kcp_ = ikcp_create(conv_, this);
        kcp_->output = KcpTransport::kcp_output;
        ikcp_nodelay(kcp_, 1, 10, 2, 1);
        ikcp_wndsize(kcp_, 4096, 4096);
        ikcp_setmtu(kcp_, 1400);

        start_update_loop();
        co_return std::nullopt;
    } catch (const std::exception& e) {
        co_return QError(QErrorKind::ConnectTimeout,
                         std::string("kcp connect failed: ") + e.what());
    }
}

void KcpTransport::start_update_loop() {
    update_running_ = true;
    // 用协程循环驱动 update（每 10ms）
    asio::co_spawn(
        socket_.get_executor(),
        [this]() -> asio::awaitable<void> {
            while (update_running_) {
                update_timer_.expires_after(std::chrono::milliseconds(10));
                try {
                    co_await update_timer_.async_wait(asio::use_awaitable);
                } catch (...) {
                    break;
                }
                if (kcp_) {
                    ikcp_update(kcp_, now_ms());
                }
            }
            co_return;
        },
        asio::detached);
}

asio::awaitable<std::optional<QError>> KcpTransport::send(
    const std::vector<uint8_t>& packet) {
    if (!kcp_) {
        co_return QError(QErrorKind::NotConnected, "kcp not connected");
    }
    int ret = ikcp_send(kcp_, reinterpret_cast<const char*>(packet.data()),
                        static_cast<int>(packet.size()));
    if (ret < 0) {
        co_return QError(QErrorKind::SendFailed, "ikcp_send failed");
    }
    // 立即 flush 降低延迟
    ikcp_update(kcp_, now_ms());
    co_return std::nullopt;
}

asio::awaitable<RecvResult> KcpTransport::recv() {
    RecvResult result;
    if (!kcp_) {
        result.error = QError(QErrorKind::NotConnected, "kcp not connected");
        co_return result;
    }

    std::vector<uint8_t> udp_buf(64 * 1024);
    std::vector<char> msg_buf(64 * 1024);

    try {
        while (true) {
            // 先看 kcp 里有没有已重组好的消息
            int n = ikcp_recv(kcp_, msg_buf.data(),
                              static_cast<int>(msg_buf.size()));
            if (n > 0) {
                result.data.assign(msg_buf.begin(), msg_buf.begin() + n);
                co_return result;
            }
            // 收 UDP 数据报，喂给 kcp
            asio::ip::udp::endpoint sender;
            std::size_t len = co_await socket_.async_receive_from(
                asio::buffer(udp_buf), sender, asio::use_awaitable);
            if (len == 0) {
                result.closed = true;
                co_return result;
            }
            ikcp_input(kcp_, reinterpret_cast<const char*>(udp_buf.data()),
                       static_cast<long>(len));
        }
    } catch (const std::system_error& e) {
        auto kind = map_asio_error(e.code());
        if (kind == QErrorKind::RemoteClosed) {
            result.closed = true;
        } else {
            result.error =
                QError(kind, "kcp recv failed: " + std::string(e.what()));
        }
    }
    co_return result;
}

asio::awaitable<void> KcpTransport::close() {
    update_running_ = false;
    update_timer_.cancel();
    std::error_code ec;
    socket_.close(ec);
    co_return;
}

} // namespace qcontinuum
