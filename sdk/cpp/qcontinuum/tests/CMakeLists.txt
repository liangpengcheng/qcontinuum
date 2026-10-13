# Tests
add_executable(test_framing test_framing.cpp)
target_link_libraries(test_framing PRIVATE qcontinuum)
add_test(NAME framing COMMAND test_framing)
