package randomX

/*
#cgo CFLAGS: -I./
#cgo LDFLAGS: -Llib -lrandomx -lstdc++
#include <randomx.h>
*/
import "C"
import "fmt"
import "unsafe"

// import "encoding/hex"
import "encoding/binary"

// build commnd:
//		- go build -ldflags "-w" randomx.go
//      - go verions : 1.11
var (
	ErrInvalidMode = fmt.Errorf("invalid randomx mode")
	ErrFailCache   = fmt.Errorf("failed to create randomx cache")
	ErrFailVM      = fmt.Errorf("failed to create a virtual machine")
	ErrFailDataset = fmt.Errorf("failed to create dataset")
)

const (
	ModeVerify = iota
	ModeMining
)

type PowRx struct {
	mode    uint64
	cache   *C.struct_randomx_cache
	dataset *C.struct_randomx_dataset
	VM      *C.struct_randomx_vm
}

func RxInit(mode uint64) (error, *PowRx, *C.struct_randomx_vm) {
	rx := &PowRx{
		mode: mode,
	}

	if err := rx.reinit(mode); err != nil {
		return err, nil, nil
	}

	return nil, rx, rx.VM
}

func (rx *PowRx) reinit(mode uint64) error {
	key := []byte("0")

	if mode == ModeVerify {
		flags := C.randomx_flags(C.RANDOMX_FLAG_DEFAULT | C.RANDOMX_FLAG_ARGON2_AVX2 | C.RANDOMX_FLAG_JIT | C.RANDOMX_FLAG_HARD_AES)
		rx.cache = C.randomx_alloc_cache(flags)
		if rx.cache == nil {
			// fmt.Println("failed to create randomx cache")
			return ErrFailCache
		}

		C.randomx_init_cache(rx.cache, unsafe.Pointer(&key[0]), (C.ulong)(len(key)))

		rx.VM = C.randomx_create_vm(flags, rx.cache, nil)
		rx.dataset = nil
	} else if mode == ModeMining {
		//| C.RANDOMX_FLAG_ARGON2_AVX2
		flags := C.randomx_flags(C.RANDOMX_FLAG_DEFAULT | C.RANDOMX_FLAG_ARGON2_AVX2 | C.RANDOMX_FLAG_JIT | C.RANDOMX_FLAG_HARD_AES | C.RANDOMX_FLAG_FULL_MEM)
		rx.cache = C.randomx_alloc_cache(flags)
		if rx.cache == nil {
			// fmt.Println("failed to create randomx cache")
			return ErrFailCache
		}
		C.randomx_init_cache(rx.cache, unsafe.Pointer(&key[0]), (C.ulong)(len(key)))

		rx.dataset = C.randomx_alloc_dataset(flags)
		if rx.dataset == nil {
			// fmt.Println("failed to create dataset ")
			return ErrFailDataset
		}

		datasetItemCount := C.randomx_dataset_item_count()
		C.randomx_init_dataset(rx.dataset, rx.cache, 0, datasetItemCount)

		C.randomx_release_cache(rx.cache)

		rx.VM = C.randomx_create_vm(flags, nil, rx.dataset)
		if rx.VM == nil {
			// fmt.Println("failed to create a virtual machine")
			return ErrFailVM
		}

		rx.mode = ModeMining
	} else {
		return ErrInvalidMode
	}

	return nil
}

func (rx *PowRx) Destroy() {
	if rx.cache != nil {
		C.randomx_release_cache(rx.cache)
	}

	if rx.dataset != nil {
		C.randomx_release_dataset(rx.dataset)
	}

	if rx.VM != nil {
		C.randomx_destroy_vm(rx.VM)
	}
}

func (rx *PowRx) Hash(data []byte, nonce uint64, mode uint64) []byte {
	var result [C.RANDOMX_HASH_SIZE]byte

	if mode != rx.mode {
		fmt.Println("warning: mode not match!")
		rx.Destroy()
		rx.reinit(mode)
	}

	input := make([]byte, len(data)+8)
	if input == nil {
		fmt.Println("fail to allocate")
		return nil
	}
	copy(input[0:], data)

	binary.LittleEndian.PutUint64(input[len(data):], uint64(nonce))

	C.randomx_calculate_hash(rx.VM, unsafe.Pointer(&input[0]), (C.ulong)(len(input)), unsafe.Pointer(&result[0]))
	// C.randomx_calculate_hash(rx.VM, unsafe.Pointer(&data[0]), (C.ulong)(len(data)), unsafe.Pointer(&result[0]))

	return result[:]
}
