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
func CheckError(e error, info string) bool {
	if e != nil {
		LogError("%s:%v", info, e)
		return false
	}
	return true
}

// PanicError panic error if e is not nil
func PanicError(e error, info string) {
	if e != nil {
		LogPanic("%s:%v", info, e)
	}
}

// IsNumberString check s is a number string or not
func IsNumberString(s string) bool {
	if _, err := strconv.ParseFloat(s, 10); err == nil {
		return true
	}
	return false
}

// Ato64 string -> int64
func Ato64(s string) int64 {
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return i
}

// Ato32 string -> int32
func Ato32(s string) int32 {
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return int32(i)
}

// Realerr 返回一个非nil的错误(如果有的话)
func Realerr(err1 error, err2 error) error {
	if err1 != nil {
		return err1
	} else if err2 != nil {
		return err2
	} else {
		return nil
	}
}

// NotNegative 返回一个非负数
// 主要处理数值的下限问题
func NotNegative(i int) int {
	if i >= 0 {
		return i
	}
	return 0
}
