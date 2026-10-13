// QClient 集成测试：Asio TCP 回显服务器。
#include <asio.hpp>
#include <cassert>
#include <chrono>
#include <cstdio>
#include <thread>

#include "qcontinuum/qcontinuum.hpp"
// ECHO 被系统头文件定义为宏（termios.h），protobuf 生成代码用到了 ECHO 枚举值
#ifdef ECHO
#undef ECHO
#endif
#include "test.pb.h" // generated

using namespace qcontinuum;

// 最小 TCP 回显服务器
void run_echo_server(asio::io_context& ioc, uint16_t port) {
    asio::ip::tcp::acceptor acceptor(ioc,
        asio::ip::tcp::endpoint(asio::ip::tcp::v4(), port));
    while (true) {
        asio::ip::tcp::socket sock(ioc);
        std::error_code ec;
        acceptor.accept(sock, ec);
        if (ec) break;
        std::thread([s = std::move(sock)]() mutable {
            std::vector<uint8_t> buf(65535);
            std::vector<uint8_t> acc;
            while (true) {
                std::error_code ec;
                std::size_t n = s.read_some(asio::buffer(buf), ec);
                if (ec) break;
                acc.insert(acc.end(), buf.begin(), buf.begin() + n);
                while (acc.size() >= 8) {
                    int32_t len;
                    std::memcpy(&len, acc.data(), 4);
                    std::size_t total = 8 + len;
                    if (acc.size() < total) break;
                    asio::write(s, asio::buffer(acc.data(), total), ec);
                    if (ec) break;
                    acc.erase(acc.begin(), acc.begin() + total);
                }
            }
        }).detach();
    }
}

void test_qclient_tcp_echo(uint16_t port) {
    QClientOptions opts;
    opts.transport = TransportKind::Tcp;
    opts.host = "127.0.0.1";
    opts.port = port;

    std::mutex m;
    std::condition_variable cv;
    bool got_msg = false;
    qcontinuum::test::Echo received;

    auto client = QClient::create(
        opts,
        [&](const QMessage& msg) {
            assert(msg.id == 2001);
            qcontinuum::test::Echo echo;
            assert(echo.ParseFromArray(msg.body.data(), msg.body.size()));
            {
                std::lock_guard<std::mutex> lock(m);
                received = echo;
                got_msg = true;
            }
            cv.notify_one();
        });

    auto err = client->connect();
    assert(!err.has_value());

    qcontinuum::test::Echo echo;
    echo.set_seq(42);
    echo.set_payload("hello-cpp");
    auto serr = client->send_message(2001, echo);
    assert(!serr.has_value());

    {
        std::unique_lock<std::mutex> lock(m);
        bool ok = cv.wait_for(lock, std::chrono::seconds(5),
                              [&] { return got_msg; });
        assert(ok);
        assert(received.seq() == 42);
        assert(received.payload() == "hello-cpp");
    }

    client->close();
    printf("test_qclient_tcp_echo ok\n");
}

int main() {
    asio::io_context ioc;
    // 找空闲端口
    asio::ip::tcp::acceptor tmp(ioc,
        asio::ip::tcp::endpoint(asio::ip::tcp::v4(), 0));
    uint16_t port = tmp.local_endpoint().port();
    tmp.close();

    std::thread server_thread([&] { run_echo_server(ioc, port); });
    std::this_thread::sleep_for(std::chrono::milliseconds(100));

    test_qclient_tcp_echo(port);

    ioc.stop();
    server_thread.detach();
    printf("All QClient tests passed!\n");
    return 0;
}
