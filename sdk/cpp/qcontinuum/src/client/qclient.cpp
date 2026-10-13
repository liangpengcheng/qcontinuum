#include "qcontinuum/client/qclient.hpp"

#include <asio/co_spawn.hpp>
#include <asio/detached.hpp>
#include <asio/use_awaitable.hpp>
#include <random>

#include "qcontinuum/protocol/framing.hpp"
#include "qcontinuum/transport/kcp_transport.hpp"
#include "qcontinuum/transport/tcp_transport.hpp"
#include "qcontinuum/transport/websocket_transport.hpp"

namespace qcontinuum {

namespace {

std::unique_ptr<Transport> make_transport(TransportKind kind,
                                          asio::io_context& ioc) {
    switch (kind) {
        case TransportKind::Tcp:
            return std::make_unique<TcpTransport>(ioc);
        case TransportKind::Kcp:
            return std::make_unique<KcpTransport>(ioc);
        case TransportKind::WebSocket:
            return std::make_unique<WebSocketTransport>(ioc);
    }
    return nullptr;
}

} // namespace

std::shared_ptr<QClient> QClient::create(QClientOptions options,
                                         MessageCallback on_message,
                                         StateCallback on_state) {
    return std::shared_ptr<QClient>(
        new QClient(std::move(options), std::move(on_message),
                    std::move(on_state)));
}

QClient::QClient(QClientOptions options, MessageCallback on_message,
                 StateCallback on_state)
    : options_(std::move(options)),
      on_message_(std::move(on_message)),
      on_state_(std::move(on_state)),
      work_guard_(asio::make_work_guard(ioc_)) {
    // 启动后台 io 线程
    io_thread_ = std::thread([this] { ioc_.run(); });
}

QClient::~QClient() {
    close();
    work_guard_.reset();
    ioc_.stop();
    if (io_thread_.joinable()) {
        io_thread_.join();
    }
}

void QClient::set_state(ConnectionState s) {
    // 仅在 io 线程中调用，无需加锁（state() 读取时加锁）
    {
        std::lock_guard<std::mutex> lock(mutex_);
        state_ = s;
    }
    if (on_state_) {
        on_state_(s);
    }
}

std::optional<QError> QClient::connect() {
    // 在调用线程同步执行连接（用 promise 等待协程完成）
    std::promise<std::optional<QError>> prom;
    auto fut = prom.get_future();

    asio::co_spawn(
        ioc_,
        [self = shared_from_this(), &prom]() -> asio::awaitable<void> {
            self->set_state(ConnectionState::Connecting);
            self->transport_ =
                make_transport(self->options_.transport, self->ioc_);
            auto err = co_await self->transport_->connect(
                self->options_.host, self->options_.port,
                self->options_.connect_timeout);
            if (err.has_value()) {
                self->set_state(ConnectionState::Disconnected);
                prom.set_value(err);
                co_return;
            }
            self->set_state(ConnectionState::Connected);
            prom.set_value(std::nullopt);
            // 启动收包循环
            co_await self->recv_loop();
        },
        asio::detached);

    // 等待连接完成（带超时）
    auto status = fut.wait_for(options_.connect_timeout +
                               std::chrono::seconds(2));
    if (status != std::future_status::ready) {
        return QError(QErrorKind::ConnectTimeout, "connect timed out");
    }
    return fut.get();
}

asio::awaitable<void> QClient::recv_loop() {
    framing::PacketReader reader;

    while (!closing_) {
        RecvResult r = co_await transport_->recv();

        if (r.closed || r.error.has_value()) {
            if (!closing_) {
                // 进入重连
                set_state(ConnectionState::Reconnecting);
                bool ok = co_await do_reconnect();
                if (!ok) {
                    set_state(ConnectionState::Disconnected);
                    co_return;
                }
                set_state(ConnectionState::Connected);
                reader.reset();
            }
            continue;
        }

        reader.feed(r.data.data(), r.data.size());
        while (true) {
            auto t = reader.try_take();
            if (t.error.has_value()) {
                // 协议违规：重连
                set_state(ConnectionState::Reconnecting);
                bool ok = co_await do_reconnect();
                if (!ok) {
                    set_state(ConnectionState::Disconnected);
                    co_return;
                }
                set_state(ConnectionState::Connected);
                reader.reset();
                break;
            }
            if (!t.packet.has_value()) {
                break; // 数据不足
            }
            QMessage msg;
            msg.id = t.packet->first;
            msg.body = std::move(t.packet->second);
            if (on_message_) {
                on_message_(msg);
            }
        }
    }
    co_return;
}

asio::awaitable<bool> QClient::do_reconnect() {
    uint32_t attempts = 0;
    while (!closing_) {
        uint32_t max = options_.max_reconnect_attempts;
        if (max > 0 && attempts >= max) {
            co_return false;
        }
        auto delay = backoff_delay(options_, attempts);
        ++attempts;

        asio::steady_timer timer(ioc_, delay);
        try {
            co_await timer.async_wait(asio::use_awaitable);
        } catch (...) {
            co_return false; // 被取消
        }
        if (closing_) {
            co_return false;
        }

        auto new_transport = make_transport(options_.transport, ioc_);
        auto err = co_await new_transport->connect(
            options_.host, options_.port, options_.connect_timeout);
        if (!err.has_value()) {
            transport_ = std::move(new_transport);
            co_return true;
        }
        // 失败，继续退避
    }
    co_return false;
}

std::chrono::milliseconds QClient::backoff_delay(
    const QClientOptions& opts, uint32_t attempt) {
    double base = static_cast<double>(opts.reconnect_base_delay.count());
    double max = static_cast<double>(opts.reconnect_max_delay.count());
    double exp = base * std::pow(2.0, std::min(attempt, 10u));
    double capped = std::min(exp, max);
    thread_local std::mt19937 gen(std::random_device{}());
    std::uniform_real_distribution<double> dist(
        1.0 - opts.reconnect_jitter, 1.0 + opts.reconnect_jitter);
    double jittered = capped * dist(gen);
    return std::chrono::milliseconds(
        static_cast<int64_t>(std::max(1.0, jittered)));
}

std::optional<QError> QClient::send_raw(int32_t id, const uint8_t* body,
                                        std::size_t len) {
    auto enc = framing::encode(id, body, len);
    if (enc.error.has_value()) {
        return enc.error;
    }
    auto packet = std::make_shared<std::vector<uint8_t>>(std::move(enc.data));

    // 线程安全：post 到 io 线程发送
    asio::post(ioc_, [self = shared_from_this(), packet]() {
        if (self->closing_ || !self->transport_) {
            return;
        }
        asio::co_spawn(
            self->ioc_,
            [self, packet]() -> asio::awaitable<void> {
                auto err = co_await self->transport_->send(*packet);
                if (err.has_value()) {
                    // 发送失败由 recv_loop 的错误处理触发重连；
                    // 这里仅记录（实际应通过回调通知）
                }
            },
            asio::detached);
    });
    return std::nullopt;
}

ConnectionState QClient::state() const {
    std::lock_guard<std::mutex> lock(mutex_);
    return state_;
}

void QClient::close() {
    bool expected = false;
    // closing_ 用 mutex 保护（简化）
    {
        std::lock_guard<std::mutex> lock(mutex_);
        if (closing_) {
            return;
        }
        closing_ = true;
    }
    // 在 io 线程关闭传输
    asio::post(ioc_, [self = shared_from_this()]() {
        asio::co_spawn(
            self->ioc_,
            [self]() -> asio::awaitable<void> {
                if (self->transport_) {
                    co_await self->transport_->close();
                }
            },
            asio::detached);
    });
    {
        std::lock_guard<std::mutex> lock(mutex_);
        state_ = ConnectionState::Disconnected;
    }
}

} // namespace qcontinuum
