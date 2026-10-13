#include "qcontinuum/protocol/framing.hpp"

#include <cstring>

namespace qcontinuum {
namespace framing {

namespace {

int32_t read_i32_le(const uint8_t* p) {
    int32_t v;
    std::memcpy(&v, p, 4);
    return v; // x86/ARM 均为小端，与 Go 一致
}

void write_i32_le(uint8_t* p, int32_t v) {
    std::memcpy(p, &v, 4);
}

} // namespace

EncodeResult encode(int32_t id, const uint8_t* body, std::size_t len) {
    if (len > static_cast<std::size_t>(MAX_MESSAGE_SIZE)) {
        return {{},
                QError(QErrorKind::MessageTooLarge,
                       "body exceeds " + std::to_string(MAX_MESSAGE_SIZE) + " bytes")};
    }
    std::vector<uint8_t> pkt(HEADER_LEN + len);
    write_i32_le(pkt.data(), static_cast<int32_t>(len));
    write_i32_le(pkt.data() + 4, id);
    if (len > 0) {
        std::memcpy(pkt.data() + HEADER_LEN, body, len);
    }
    return {std::move(pkt), std::nullopt};
}

HeaderResult try_read_header(const uint8_t* data, std::size_t len) {
    if (len < HEADER_LEN) {
        return {std::nullopt, std::nullopt}; // 数据不足
    }
    int32_t length = read_i32_le(data);
    int32_t id = read_i32_le(data + 4);
    if (length < 0) {
        return {std::nullopt,
                QError(QErrorKind::ProtocolViolation,
                       "negative frame length: " + std::to_string(length))};
    }
    if (length > MAX_MESSAGE_SIZE) {
        return {std::nullopt,
                QError(QErrorKind::MessageTooLarge,
                       "frame length " + std::to_string(length) + " exceeds limit")};
    }
    return {std::make_pair(length, id), std::nullopt};
}

void PacketReader::feed(const uint8_t* data, std::size_t len) {
    buf_.insert(buf_.end(), data, data + len);
}

TakeResult PacketReader::try_take() {
    HeaderResult hr = try_read_header(buf_.data(), buf_.size());
    if (hr.error.has_value()) {
        return {std::nullopt, hr.error};
    }
    if (!hr.header.has_value()) {
        return {std::nullopt, std::nullopt}; // 数据不足
    }
    auto [length, id] = hr.header.value();
    std::size_t total = HEADER_LEN + static_cast<std::size_t>(length);
    if (buf_.size() < total) {
        return {std::nullopt, std::nullopt}; // body 不完整
    }
    std::vector<uint8_t> body(buf_.begin() + HEADER_LEN, buf_.begin() + total);
    buf_.erase(buf_.begin(), buf_.begin() + total);
    return {std::make_pair(id, std::move(body)), std::nullopt};
}

void PacketReader::reset() {
    buf_.clear();
}

} // namespace framing
} // namespace qcontinuum
