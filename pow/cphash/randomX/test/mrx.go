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
	"reflect"
	"runtime"
	"sync/atomic"
	"time"
	"unsafe"
)

// build commnd:
//		- go build -ldflags "-w" mrx.go
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
	target := new(big.Int).Div(maxUint256, big.NewInt(0x4f000))
	var targetNonce uint64
	// 8 threads:
	// 0x22000: nonce = 89626, 63.627208 seconds
	// 0x44000: nonce = 593441, 406.310473 seconds
	// 0x55000: nonce = 593442, 393.656196 seconds
	// 0xa0000: nonce = 1521405, 1045.136057 seconds
	// 0x70000: nonce = 1082060, 762.471943 seconds
	// 0x60000: nonce = 1082060, 739.948816 seconds ,,432.057884 seconds

	fmt.Println("target = ", target.Uint64())

	data := []byte("1565a45821432f9d41ae4847efd34fb8a04a1f3fff2fa97e998e86f7f7a27ae4")
	//data := []byte("156")

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
	// vm := C.randomx_create_vm(flags, nil, dataset)
	// if vm == nil {
	// 	fmt.Println("failed to create a virtual machine")
	// 	return
	// }

	// start := time.Now()
	// seconds := 0.
	// nonce := (uint64)(0)

	// input := make([]byte, len(data) + 8)
	// copy(input[0:], data)
	// var result [C.RANDOMX_HASH_SIZE]byte

	// for {
	// 	binary.LittleEndian.PutUint64(input[len(data):], uint64(nonce))
	// 	C.randomx_calculate_hash(vm, unsafe.Pointer(&input[0]), (C.ulong)(len(input)), unsafe.Pointer(&result[0]))

	// 	if new(big.Int).SetBytes(result[:]).Cmp(target) < 0 {
	// 	// if new(big.Int).SetBytes(result).Uint64() < target.Uint64() {
	// 		elapsed := time.Now().Sub(start)
	// 		fmt.Printf("time elapsed: \t%f seconds\n", elapsed.Seconds())
	// 		fmt.Println("result = ", new(big.Int).SetBytes(result[:]).Uint64(), "nonce = ", nonce)
	// 		break
	// 	}
	// 	elapsed := time.Now().Sub(start)
	// 	sec := elapsed.Seconds()
	// 	if sec - seconds > 1.{
	// 		seconds = sec
	// 		// fmt.Printf(".")
	// 		fmt.Println("result = ", new(big.Int).SetBytes(result[:]).Uint64(), "nonce = ", nonce)
	// 	}

	// 	nonce ++
	// }

	/////////// multithread mode

	totalThread := runtime.NumCPU()
	found := false
	exitCh := make([]chan bool, 0)

	for i := 0; i < totalThread; i++ {
		ex := make(chan bool)
		exitCh = append(exitCh, ex)
	}

	vms := make([]*C.struct_randomx_vm, totalThread)
	for i := 0; i < totalThread; i++ {
		vm := C.randomx_create_vm(flags, nil, dataset)
		if vm == nil {
			fmt.Println("failed to create a virtual machine")
			return
		}

		fmt.Println("vm", i, "is ready")
		vms[i] = vm
	}

	nonce := (uint64)(0)

	doHash := func(in []byte, index int) {
		var localResult [C.RANDOMX_HASH_SIZE]byte
		inputWithNoce := make([]byte, len(in)+8)
		copy(inputWithNoce[0:], in)

		for !found {
			localNonce := atomic.AddUint64(&nonce, 1)
			binary.LittleEndian.PutUint64(inputWithNoce[len(in):], uint64(localNonce))
			C.randomx_calculate_hash(vms[index], unsafe.Pointer(&inputWithNoce[0]), (C.ulong)(len(inputWithNoce)), unsafe.Pointer(&localResult[0]))

			if new(big.Int).SetBytes(localResult[:]).Cmp(target) < 0 {
				found = true
				fmt.Println("found nonce = ", localNonce)
				targetNonce = localNonce
				break
			}
		}

		exitCh[index] <- true
	}

	start := time.Now()

	for i := 0; i < totalThread; i++ {
		go doHash(data, i)
	}

	for !found {
		time.Sleep(time.Second)
		fmt.Println("nonce = ", atomic.LoadUint64(&nonce))

	}

	elapsed := time.Now().Sub(start)
	fmt.Printf("time elapsed: \t%f seconds\n", elapsed.Seconds())

	//join threads
	// Use reflect to wait on dynamic channels
	cases := make([]reflect.SelectCase, totalThread)
	for i, ch := range exitCh {
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)}
	}

	fmt.Println("wait..")
	exit := 0
	for {
		chosen, _, _ := reflect.Select(cases)
		fmt.Println("thread exit", chosen)
		exit += 1
		if exit == totalThread {
			break
		}
	}

	C.randomx_release_dataset(dataset)
	for i := 0; i < totalThread; i++ {
		C.randomx_destroy_vm(vms[i])
	}
	///////////////////////////////////////////

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
	// C.randomx_release_cache(vcache)

	var vResult [C.RANDOMX_HASH_SIZE]byte
	inputWithNoce := make([]byte, len(data)+8)
	copy(inputWithNoce[0:], data)

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
