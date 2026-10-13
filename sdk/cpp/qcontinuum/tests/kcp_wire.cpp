// KCP wire compat: C++ (ikcp) <-> kcp-go via file shuttling.
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <fstream>
#include <vector>

extern "C" {
#include "ikcp.h"
}

static const uint32_t CONV = 0x12345678;
static std::vector<uint8_t> g_output;

static int kcp_output(const char* buf, int len, ikcpcb* /*kcp*/, void* /*user*/) {
    g_output.insert(g_output.end(), buf, buf + len);
    return 0;
}

int main(int argc, char** argv) {
    std::string mode = argc > 1 ? argv[1] : "send";

    if (mode == "send") {
        ikcpcb* kcp = ikcp_create(CONV, nullptr);
        kcp->output = kcp_output;
        ikcp_nodelay(kcp, 1, 10, 2, 1);
        ikcp_wndsize(kcp, 4096, 4096);
        ikcp_setmtu(kcp, 1400);

        const char* msg = "HELLO-FROM-CPP";
        ikcp_send(kcp, msg, strlen(msg));
        ikcp_update(kcp, 0);
        ikcp_flush(kcp);

        std::ofstream f("/tmp/kcp_cpp_to_go.bin", std::ios::binary);
        uint32_t len = g_output.size();
        f.write(reinterpret_cast<char*>(&len), 4);
        f.write(reinterpret_cast<char*>(g_output.data()), len);
        printf("C++ wrote %u bytes (conv=0x%x)\n", len, CONV);
        ikcp_release(kcp);
    } else if (mode == "verify") {
        std::ifstream f("/tmp/kcp_go_to_cpp.bin", std::ios::binary);
        std::vector<uint8_t> data((std::istreambuf_iterator<char>(f)),
                                   std::istreambuf_iterator<char>());

        ikcpcb* kcp = ikcp_create(CONV, nullptr);
        kcp->output = kcp_output;
        ikcp_nodelay(kcp, 1, 10, 2, 1);
        ikcp_wndsize(kcp, 4096, 4096);
        ikcp_setmtu(kcp, 1400);

        ikcp_input(kcp, reinterpret_cast<char*>(data.data()), data.size());
        ikcp_update(kcp, 100);

        char buf[65535];
        int n = ikcp_recv(kcp, buf, sizeof(buf));
        if (n > 0) {
            std::string msg(buf, n);
            printf("C++ received: \"%s\"\n", msg.c_str());
            if (msg == "REPLY-FROM-KCPGO") {
                printf("KCP-WIRE-COMPAT OK\n");
            } else {
                printf("MISMATCH!\n");
                return 1;
            }
        } else {
            printf("ikcp_recv failed: %d\n", n);
            return 1;
        }
        ikcp_release(kcp);
    }
    return 0;
}
