cmake_minimum_required(VERSION 3.16)
project(qcontinuum-cpp VERSION 0.1.0 LANGUAGES C CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

# Standalone Asio (header-only)
add_library(asio INTERFACE)
target_include_directories(asio INTERFACE
    ${CMAKE_CURRENT_SOURCE_DIR}/../../third_party/asio/asio/include)
target_compile_definitions(asio INTERFACE ASIO_STANDALONE)
if(WIN32)
    # standalone Asio 要求调用方定义 Windows 目标版本
    target_compile_definitions(asio INTERFACE _WIN32_WINNT=0x0A00)
endif()
if(UNIX)
    target_link_libraries(asio INTERFACE pthread)
endif()

# ikcp (C)
add_library(ikcp STATIC
    ${CMAKE_CURRENT_SOURCE_DIR}/../../third_party/ikcp.c)
target_include_directories(ikcp PUBLIC
    ${CMAKE_CURRENT_SOURCE_DIR}/../../third_party)

# protobuf
find_package(Protobuf REQUIRED)

add_subdirectory(qcontinuum)
