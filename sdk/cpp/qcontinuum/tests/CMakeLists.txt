# Tests
add_executable(test_framing test_framing.cpp)
target_link_libraries(test_framing PRIVATE qcontinuum)
add_test(NAME framing COMMAND test_framing)

add_executable(test_qclient test_qclient.cpp)
target_link_libraries(test_qclient PRIVATE qcontinuum)
# test.pb.h is in the generated dir (already on qcontinuum's interface)
add_test(NAME qclient COMMAND test_qclient)
