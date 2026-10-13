#[test]
fn prost_echo_roundtrip() {
    use prost::Message;
    use qcontinuum::test_proto::Echo;
    let echo = Echo { seq: 42, send_time_unix_nano: 123, payload: b"hi".to_vec() };
    let bytes = echo.encode_to_vec();
    let back = Echo::decode(bytes.as_slice()).unwrap();
    assert_eq!(back.seq, 42);
    assert_eq!(back.payload, b"hi");
}
