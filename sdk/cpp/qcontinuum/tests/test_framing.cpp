// Phase 1 单元测试：帧编解码。
#include <cassert>
#include <cstdio>
#include <cstring>

#include "qcontinuum/protocol/framing.hpp"

using namespace qcontinuum;
using namespace qcontinuum::framing;

void test_header_layout() {
    auto r = encode(2001, nullptr, 0);
    assert(!r.error.has_value());
    assert(r.data.size() == 8);
    // length = 0 (LE)
    assert(r.data[0] == 0x00 && r.data[1] == 0x00 && r.data[2] == 0x00 && r.data[3] == 0x00);
    // id = 2001 = 0x7D1 (LE)
    assert(r.data[4] == 0xD1 && r.data[5] == 0x07 && r.data[6] == 0x00 && r.data[7] == 0x00);
    printf("test_header_layout ok\n");
}

void test_roundtrip() {
    std::vector<uint8_t> body = {'h', 'e', 'l', 'l', 'o'};
    auto r = encode(2001, body);
    assert(!r.error.has_value());

    PacketReader reader;
    reader.feed(r.data.data(), r.data.size());
    auto t = reader.try_take();
    assert(!t.error.has_value());
    assert(t.packet.has_value());
    assert(t.packet->first == 2001);
    assert(t.packet->second == body);
    printf("test_roundtrip ok\n");
}

void test_split_feed() {
    auto r = encode(2001, std::vector<uint8_t>{'w', 'o', 'r', 'l', 'd'});
    PacketReader reader;
    reader.feed(r.data.data(), 3);
    auto t1 = reader.try_take();
    assert(!t1.packet.has_value() && !t1.error.has_value());
    reader.feed(r.data.data() + 3, r.data.size() - 3);
    auto t2 = reader.try_take();
    assert(t2.packet.has_value());
    assert(t2.packet->second == std::vector<uint8_t>({'w', 'o', 'r', 'l', 'd'}));
    printf("test_split_feed ok\n");
}

void test_coalesced() {
    auto p1 = encode(2001, std::vector<uint8_t>{'a'});
    auto p2 = encode(2002, std::vector<uint8_t>{'b', 'b'});
    PacketReader reader;
    reader.feed(p1.data.data(), p1.data.size());
    reader.feed(p2.data.data(), p2.data.size());
    auto t1 = reader.try_take();
    auto t2 = reader.try_take();
    assert(t1.packet->first == 2001);
    assert(t2.packet->first == 2002);
    printf("test_coalesced ok\n");
}

void test_negative_length() {
    uint8_t bad[8] = {0xFF, 0xFF, 0xFF, 0xFF, 0x01, 0x00, 0x00, 0x00};
    PacketReader reader;
    reader.feed(bad, 8);
    auto t = reader.try_take();
    assert(t.error.has_value());
    assert(t.error->kind == QErrorKind::ProtocolViolation);
    printf("test_negative_length ok\n");
}

void test_oversize() {
    uint8_t hdr[8] = {};
    int32_t big = MAX_MESSAGE_SIZE + 1;
    std::memcpy(hdr, &big, 4);
    PacketReader reader;
    reader.feed(hdr, 8);
    auto t = reader.try_take();
    assert(t.error.has_value());
    assert(t.error->kind == QErrorKind::MessageTooLarge);
    printf("test_oversize ok\n");
}

void test_encode_oversize() {
    std::vector<uint8_t> big(MAX_MESSAGE_SIZE + 1, 0);
    auto r = encode(1, big);
    assert(r.error.has_value());
    assert(r.error->kind == QErrorKind::MessageTooLarge);
    printf("test_encode_oversize ok\n");
}

int main() {
    test_header_layout();
    test_roundtrip();
    test_split_feed();
    test_coalesced();
    test_negative_length();
    test_oversize();
    test_encode_oversize();
    printf("All framing tests passed!\n");
    return 0;
}
