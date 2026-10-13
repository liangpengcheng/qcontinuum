#pragma once

#include "qcontinuum/transport/transport.hpp"

extern "C" {
#include "ikcp.h"
}

namespace qcontinuum {

// KCP 传输：ikcp + Asio UDP。
// 参数对齐 Go DefaultKCPConfig：nodelay(1,10,2,1)，窗口 4096/4096，MTU 1400。
class KcpTransport : public Transport {
public:
    explicit KcpTransport(asio::io_context& ioc);
    ~KcpTransport() override;

    asio::awaitable<std::optional<QError>> connect(
        const std::string& host, uint16_t port,
        std::chrono::milliseconds timeout) override;

    asio::awaitable<std::optional<QError>> send(
        const std::vector<uint8_t>& packet) override;

    asio::awaitable<RecvResult> recv() override;

    asio::awaitable<void> close() override;

private:
    asio::ip::udp::socket socket_;
    asio::ip::udp::endpoint remote_ep_;
    ikcpcb* kcp_ = nullptr;
    uint32_t conv_ = 0;

    // ikcp output 回调：把数据经 UDP 发出
    static int kcp_output(const char* buf, int len, ikcpcb* kcp, void* user);

    // 后台 update 循环
    void start_update_loop();
    asio::steady_timer update_timer_;
    bool update_running_ = false;

    static uint32_t now_ms();
};

} // namespace qcontinuum
