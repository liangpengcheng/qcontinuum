// C++ <-> Go 联调：protobuf Echo 回显（消息号 2001）。
// 用法：interop_echo <tcp|kcp|ws> [host] [port] [count]
// 对标 C# 的 QContinuum.InteropTest。
#include <algorithm>
#include <asio.hpp>
#include <chrono>
#include <condition_variable>
#include <cstdio>
#include <cstring>
#include <mutex>
#include <string>
#include <vector>

#include "qcontinuum/qcontinuum.hpp"
// ECHO 被系统头文件定义为宏（termios.h），protobuf 生成代码用到了 ECHO 枚举值
#ifdef ECHO
#undef ECHO
#endif
#include "test.pb.h" // generated

using namespace qcontinuum;

static int64_t now_ns() {
    return std::chrono::duration_cast<std::chrono::nanoseconds>(
               std::chrono::system_clock::now().time_since_epoch())
        .count();
}

int main(int argc, char** argv) {
    std::string transport = argc > 1 ? argv[1] : "tcp";
    std::string host = argc > 2 ? argv[2] : "127.0.0.1";
    uint16_t port = 0;
    if (argc > 3) {
        port = static_cast<uint16_t>(std::stoi(argv[3]));
    } else {
        if (transport == "tcp")
            port = 9001;
        else if (transport == "ws")
            port = 9002;
        else if (transport == "kcp")
            port = 9003;
        else {
            std::fprintf(stderr, "transport must be tcp|kcp|ws\n");
            return 2;
        }
    }
    long count = argc > 4 ? std::stol(argv[4]) : 100;

    QClientOptions opts;
    if (transport == "tcp")
        opts.transport = TransportKind::Tcp;
    else if (transport == "kcp")
        opts.transport = TransportKind::Kcp;
    else if (transport == "ws")
        opts.transport = TransportKind::WebSocket;
    else {
        std::fprintf(stderr, "transport must be tcp|kcp|ws\n");
        return 2;
    }
    opts.host = host;
    opts.port = port;

    std::mutex m;
    std::condition_variable cv;
    int64_t got_seq = -1;
    qcontinuum::test::Echo got_echo;
    bool got = false;

    auto client = QClient::create(
        opts, [&](const QMessage& msg) {
            if (msg.id != msg_id::kEcho) return;
            qcontinuum::test::Echo echo;
            if (!echo.ParseFromArray(msg.body.data(),
                                     static_cast<int>(msg.body.size())))
                return;
            {
                std::lock_guard<std::mutex> lock(m);
                got_echo = echo;
                got_seq = echo.seq();
                got = true;
            }
            cv.notify_one();
        });

    std::printf("[%s] connecting to %s:%u ...\n", transport.c_str(),
                host.c_str(), port);
    if (auto err = client->connect()) {
        std::fprintf(stderr, "[%s] connect failed: %s\n", transport.c_str(),
                     err->to_string().c_str());
        return 1;
    }
    std::printf("[%s] connected, sending %ld protobuf echoes ...\n",
                transport.c_str(), count);

    std::string payload(256, 0);
    for (std::size_t i = 0; i < payload.size(); ++i)
        payload[i] = static_cast<char>((i * 37 + 11) & 0xFF);

    std::vector<double> rtts;
    auto t0 = std::chrono::steady_clock::now();
    for (long seq = 1; seq <= count; ++seq) {
        qcontinuum::test::Echo echo;
        echo.set_seq(seq);
        echo.set_send_time_unix_nano(now_ns());
        echo.set_payload(payload);
        {
            std::lock_guard<std::mutex> lock(m);
            got = false;
            got_seq = -1;
        }
        if (auto serr = client->send_message(msg_id::kEcho, echo)) {
            std::fprintf(stderr, "[%s] send failed seq=%ld: %s\n",
                         transport.c_str(), seq, serr->to_string().c_str());
            return 1;
        }
        {
            std::unique_lock<std::mutex> lock(m);
            if (!cv.wait_for(lock, std::chrono::seconds(10),
                             [&] { return got && got_seq == seq; })) {
                std::fprintf(stderr, "[%s] TIMEOUT waiting echo seq=%ld\n",
                             transport.c_str(), seq);
                return 1;
            }
        }
        if (got_echo.seq() != seq || got_echo.payload() != payload) {
            std::fprintf(stderr, "[%s] MISMATCH seq=%ld\n", transport.c_str(),
                         seq);
            return 2;
        }
        rtts.push_back((now_ns() - got_echo.send_time_unix_nano()) / 1e6);
    }
    auto elapsed =
        std::chrono::duration<double>(std::chrono::steady_clock::now() - t0)
            .count();

    std::sort(rtts.begin(), rtts.end());
    std::printf("[%s] OK: %ld/%ld echoes matched, %.1fs, rtt p50=%.2fms "
                "p99=%.2fms\n",
                transport.c_str(), count, count, elapsed,
                rtts[rtts.size() / 2],
                rtts[static_cast<std::size_t>(rtts.size() * 0.99)]);
    client->close();
    return 0;
}
