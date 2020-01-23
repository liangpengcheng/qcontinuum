package base

import (
	"math/rand"
	"time"
)

// Krand random
func Krand(size int, kind int) []byte {
	ikind, kinds, result := kind, [][]int{[]int{10, 48}, []int{26, 97}, []int{26, 65}}, make([]byte, size)
	isall := kind > 2 || kind < 0
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < size; i++ {
		if isall { // random ikind
			ikind = rand.Intn(3)
		}
		scope, base := kinds[ikind][0], kinds[ikind][1]
		result[i] = uint8(base + rand.Intn(scope))
	}
	return result
}

// MaxInt return max int
func MaxInt(x int, y int) int {
	if x > y {
		return x
	}
	return y
}

// MaxInt32 reuturn max int32
func MaxInt32(x int32, y int32) int32 {
	if x > y {
		return x
	}
	return y
}

// MinInt return min int
func MinInt(x int, y int) int {
	if x < y {
		return x
	}
	return y
}

// MinInt32 return min int32
func MinInt32(x int32, y int32) int32 {
	if x < y {
		return x
	}
	return y
}

// MinUInt return min int
func MinUInt(x uint32, y uint32) uint32 {
	if x < y {
		return x
	}
	return y
}

func ABS(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
func ABSInt32(x int32) int32 {
	if x < 0 {
		return -x
	}
	return x
}
