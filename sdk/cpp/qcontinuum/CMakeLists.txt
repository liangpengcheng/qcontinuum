# Generate C++ protobuf from proto/test.proto
# Use system protoc (matches system libprotobuf 3.21).
# The bundled 36.x protoc generates code incompatible with libprotobuf 3.21.
set(PROTO_DIR ${CMAKE_CURRENT_SOURCE_DIR}/../../../proto)
# Generate C++ protobuf from proto/test.proto.
# Use the protoc that matches the linked libprotobuf: prefer the one found
# by find_package(Protobuf) (set in the parent scope), fall back to PATH.
# Note: the bundled 36.x protoc generates code incompatible with
# system libprotobuf 3.21, so PATH lookup only makes sense when they match.
if(Protobuf_PROTOC_EXECUTABLE)
    set(PROTOC_BIN ${Protobuf_PROTOC_EXECUTABLE})
else()
    set(PROTOC_BIN protoc)
endif()

set(GENERATED_DIR ${CMAKE_CURRENT_BINARY_DIR}/generated)
file(MAKE_DIRECTORY ${GENERATED_DIR})

add_custom_command(
    OUTPUT ${GENERATED_DIR}/test.pb.h ${GENERATED_DIR}/test.pb.cc
    COMMAND ${PROTOC_BIN}
        --cpp_out=${GENERATED_DIR}
        --proto_path=${PROTO_DIR}
        ${PROTO_DIR}/test.proto
    DEPENDS ${PROTO_DIR}/test.proto
    COMMENT "Generating C++ protobuf from test.proto"
)

add_library(qcontinuum STATIC
    src/protocol/error.cpp
    src/protocol/framing.cpp
    src/transport/tcp_transport.cpp
    src/transport/websocket_transport.cpp
    src/transport/kcp_transport.cpp
    src/client/qclient.cpp
    ${GENERATED_DIR}/test.pb.cc
    # ikcp compiled separately as C library
)

target_include_directories(qcontinuum PUBLIC
    ${CMAKE_CURRENT_SOURCE_DIR}/include
    ${GENERATED_DIR}
)

target_link_libraries(qcontinuum PUBLIC
    asio
    ikcp
    protobuf::libprotobuf
)

target_compile_options(qcontinuum PRIVATE -Wall -Wextra)

# Tests
enable_testing()
add_subdirectory(tests)
