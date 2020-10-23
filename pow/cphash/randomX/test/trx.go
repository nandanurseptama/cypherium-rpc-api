package main

/*
#cgo CFLAGS: -I../
#cgo LDFLAGS: -L../lib/Linux -lrandomx -lstdc++
#include <randomx.h>
*/
import "C"

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"unsafe"
)

// build commnd:
//		- go build -ldflags "-w" trx.go
//      - go verions : 1.11

func main() {
	key := []byte("0")

	//data := []byte("1565a45821432f9d41ae4847efd34fb8a04a1f3fff2fa97e998e86f7f7a27ae4")
	data := []byte("156")

	//flags := C.randomx_flags(C.RANDOMX_FLAG_DEFAULT | C.RANDOMX_FLAG_ARGON2_AVX2 | C.RANDOMX_FLAG_JIT | C.RANDOMX_FLAG_HARD_AES | C.RANDOMX_FLAG_FULL_MEM)
	flags := C.randomx_get_flags()
	flags |= C.RANDOMX_FLAG_FULL_MEM
	cache := C.randomx_alloc_cache(flags)
	if cache == nil {
		fmt.Println("failed to create randomx cache")
		return
	}
	C.randomx_init_cache(cache, unsafe.Pointer(&key[0]), (C.ulong)(len(key)))

	dataset := C.randomx_alloc_dataset(flags)
	if dataset == nil {
		fmt.Println("failed to create dataset ")
		return
	}

	datasetItemCount := C.randomx_dataset_item_count()
	C.randomx_init_dataset(dataset, cache, 0, datasetItemCount)

	C.randomx_release_cache(cache)

	//////////// single thread mode
	vm := C.randomx_create_vm(flags, nil, dataset)
	if vm == nil {
		fmt.Println("failed to create a virtual machine")
		return
	}

	nonce := (uint64)(0)

	input := make([]byte, len(data)+8)
	copy(input[0:], data)
	var result [C.RANDOMX_HASH_SIZE]byte

	binary.LittleEndian.PutUint64(input[len(data):], uint64(nonce))
	C.randomx_calculate_hash(vm, unsafe.Pointer(&input[0]), (C.ulong)(len(input)), unsafe.Pointer(&result[0]))

	fmt.Println("mresult = ", hex.EncodeToString(result[:]))

	/// verify the found nonce
	//vflags := C.randomx_flags(C.RANDOMX_FLAG_DEFAULT | C.RANDOMX_FLAG_ARGON2_AVX2 | C.RANDOMX_FLAG_JIT | C.RANDOMX_FLAG_HARD_AES )
	vflags := C.randomx_get_flags()
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

	binary.LittleEndian.PutUint64(inputWithNoce[len(data):], uint64(nonce))

	C.randomx_calculate_hash(vvm, unsafe.Pointer(&inputWithNoce[0]), (C.ulong)(len(inputWithNoce)), unsafe.Pointer(&vResult[0]))

	fmt.Println("vresult = ", hex.EncodeToString(vResult[:]))

	C.randomx_release_cache(vcache)
	C.randomx_destroy_vm(vvm)
}
