package main

/*
#cgo CFLAGS: -I../
#cgo LDFLAGS: -L../lib/Linux -lrandomx -lstdc++
#include <randomx.h>
*/
import "C"

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"unsafe"
)

// build commnd:
//		- go build -ldflags "-w" vrx.go
//      - go verions : 1.11

func main() {
	key := []byte("0")
	maxUint256 := new(big.Int).Exp(big.NewInt(2), big.NewInt(256), big.NewInt(0))

	// 0x4400C: 1632.498561 seconds
	// 0x4400: 65.662391 seconds
	// 0xff0000: 32186.692118 seconds
	// 0xff000 : 9633.293546 seconds nonce = 3666918
	// 0xff00  ：88.315587 seconds nonce = 32923
	// 0xa0000 : 4074.040426 seconds nonce = 1521405
	target := new(big.Int).Div(maxUint256, big.NewInt(0xff000))
	var targetNonce uint64
	// 8 threads:
	// 0x22000: nonce = 89626, 63.627208 seconds
	// 0x44000: nonce = 593441, 406.310473 seconds
	// 0x55000: nonce = 593442, 393.656196 seconds
	// 0xa0000: nonce = 1521405, 1045.136057 seconds
	// 0x70000: nonce = 1082060, 762.471943 seconds
	// 0x60000: nonce = 1082060, 739.948816 seconds ,,432.057884 seconds

	fmt.Println("target = ", target.Uint64())

	//data := []byte("1565a45821432f9d41ae4847efd34fb8a04a1f3fff2fa97e998e86f7f7a27ae4")
	data := []byte("156")

	/// verify the found nonce
	vflags := C.randomx_flags(C.RANDOMX_FLAG_DEFAULT | C.RANDOMX_FLAG_ARGON2_AVX2 | C.RANDOMX_FLAG_JIT | C.RANDOMX_FLAG_HARD_AES)
	vcache := C.randomx_alloc_cache(vflags)
	if vcache == nil {
		fmt.Println("failed to create verify cache")
		return
	}

	C.randomx_init_cache(vcache, unsafe.Pointer(&key[0]), (C.ulong)(len(key)))
	vvm := C.randomx_create_vm(vflags, vcache, nil)

	var vResult [C.RANDOMX_HASH_SIZE]byte
	inputWithNoce := make([]byte, len(data)+8)
	copy(inputWithNoce[0:], data)

	targetNonce = 979783

	binary.LittleEndian.PutUint64(inputWithNoce[len(data):], uint64(targetNonce))

	C.randomx_calculate_hash(vvm, unsafe.Pointer(&inputWithNoce[0]), (C.ulong)(len(inputWithNoce)), unsafe.Pointer(&vResult[0]))

	if new(big.Int).SetBytes(vResult[:]).Cmp(target) < 0 {
		fmt.Println("verify ok")
	} else {
		fmt.Println("verify fail")
	}

	C.randomx_release_cache(vcache)
	C.randomx_destroy_vm(vvm)
}
