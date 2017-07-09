package base

import (
	"bytes"
	"reflect"
	"strconv"
	"unsafe"
)

// String bring a no copy convert from byte slice to string
// consider the risk
func String(b []byte) (s string) {

	pbytes := (*reflect.SliceHeader)(unsafe.Pointer(&b))
	pstring := (*reflect.StringHeader)(unsafe.Pointer(&s))

	pstring.Data = pbytes.Data
	pstring.Len = pbytes.Len

	return
}

// Bytes bring a no copy convert from string to byte slice
// consider the risk
func Bytes(s string) (b []byte) {

	pbytes := (*reflect.SliceHeader)(unsafe.Pointer(&b))
	pstring := (*reflect.StringHeader)(unsafe.Pointer(&s))

	pbytes.Data = pstring.Data
	pbytes.Len = pstring.Len
	pbytes.Cap = pstring.Len

	return
}

// BytesCombine 拼接[]byte
func BytesCombine(pBytes ...[]byte) []byte {
	return bytes.Join(pBytes, []byte(""))
}

// CheckError print error message if e is not nil
func CheckError(e error, info string) {
	if e != nil {
		LogError(info + e.Error())
	}
}

// IsNumberString check s is a number string or not
func IsNumberString(s string) bool {
	if _, err := strconv.ParseFloat(s, 10); err == nil {
		return true
	}
	return false
}
