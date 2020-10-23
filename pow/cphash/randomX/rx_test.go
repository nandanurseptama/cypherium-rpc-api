package randomX

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"
	"time"
)

func TestBasic(t *testing.T) {
	data := []byte("1")

	err, rx := RxInit(ModeVerify)
	if err != nil {
		t.Fatal(err)
	}

	verifyResult := rx.Hash(data, 1, ModeVerify)
	rx.Destroy()
	fmt.Println("result = ", hex.EncodeToString(verifyResult))

	err, rx2 := RxInit(ModeMining)
	if err != nil {
		t.Fatal(err)
	}

	miningResult := rx2.Hash(data, 1, ModeMining)
	rx2.Destroy()

	if !bytes.Equal(verifyResult, miningResult) {
		t.Fatal("not equal")
	}
}

func TestDifficulty(t *testing.T) {
	maxUint256 := new(big.Int).Exp(big.NewInt(2), big.NewInt(256), big.NewInt(0))

	target := new(big.Int).Div(maxUint256, big.NewInt(0x4400CC)) // ff0000 error

	fmt.Println("target = ", target.Uint64())

	data := []byte("1")

	err, rx := RxInit(ModeMining)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()

	nonce := 300000
	seconds := 0.

	// re := rx.Hash(data, (uint64)(1), ModeMining)
	// fmt.Println("data = ", hex.EncodeToString(data))

	// fmt.Println("length = ", len(data))

	// fmt.Println("result = ", hex.EncodeToString(re))
	// for _, b := range re {
	// 	fmt.Printf("%02x", b & 0xff)
	// }
	// fmt.Print("\n\r")

	for {
		result := rx.Hash(data, (uint64)(1), ModeMining)
		if new(big.Int).SetBytes(result).Cmp(target) < 0 {
			// if new(big.Int).SetBytes(result).Uint64() < target.Uint64() {
			elapsed := time.Now().Sub(start)
			fmt.Printf("time elapsed: \t%f seconds\n", elapsed.Seconds())
			fmt.Println("result = ", new(big.Int).SetBytes(result).Uint64(), "nonce = ", nonce)
			break
		}
		elapsed := time.Now().Sub(start)
		sec := elapsed.Seconds()
		if sec-seconds > 1. {
			seconds = sec
			// fmt.Printf(".")
			fmt.Println("result = ", new(big.Int).SetBytes(result).Uint64(), "nonce = ", nonce)
		}

		nonce--

		if nonce == 0 {
			break
		}
	}

	rx.Destroy()

}
