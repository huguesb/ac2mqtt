package gateway

import "golang.org/x/exp/constraints"

func Ptr[T any](v T) *T {
	return &v
}

func getBit(b byte, i int) bool {
	return ((b >> (7 - i)) & 1) == 0
}

func getBits(b byte, i, j int) byte {
	return (b >> ((8 - i) - j)) & (0xff >> (8 - j))
}

func getShort(d []byte, i int) uint16 {
	return uint16(d[i+1]&0xff) | ((uint16(d[i]) << 8) & 0xff00)
}

func aboveThreshold[T constraints.Integer](x T, y T, thresh T) bool {
	if x > y {
		return x-y > thresh
	}
	return y-x > thresh
}

func crc16(d []byte, i int, n int) uint16 {
	var b uint16 = 0xffff
	for k := i; k < i+n; k++ {
		b2 := (((b << 8) | (b >> 8)) & 0xffff) ^ (uint16(d[k]) & 0xff)
		b3 := b2 ^ ((b2 & 0xff) >> 4)
		b4 := b3 ^ ((b3 << 12) & 0xffff)
		b = b4 ^ (((b4 & 0xff) << 5) & 0xffff)
	}
	return b
}
