// Copyright 2016 The cypherBFT Authors
// This file is part of the cypherBFT library.
//
// The cypherBFT library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The cypherBFT library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the cypherBFT library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"math/big"

	"github.com/cypherium/cypherBFT/common"
	"github.com/cypherium/cypherBFT/core/types"
	"github.com/cypherium/cypherBFT/core/vm"
)

// NewEVMContext creates a new context for use in the EVM.
func NewEVMContext(msg Message, header *types.Header, chain types.ChainReader) vm.Context {
	return vm.Context{
		CanTransfer: CanTransfer,
		Transfer:    Transfer,
		GetHash:     GetHashFn(header, chain),
		Origin:      msg.From(),
		BlockNumber: new(big.Int).Set(header.Number),
		Time:        new(big.Int).Set(header.Time),
		//Difficulty:  new(big.Int).Set(header.Difficulty),
		GasLimit: header.GasLimit,
		GasPrice: new(big.Int).Set(msg.GasPrice()),
	}
}

// GetHashFn returns a GetHashFunc which retrieves header hashes by number
func GetHashFn(ref *types.Header, chain types.ChainReader) func(n uint64) common.Hash {
	var cache map[uint64]common.Hash

	return func(n uint64) common.Hash {
		// If there's no hash cache yet, make one
		if cache == nil {
			cache = map[uint64]common.Hash{
				ref.Number.Uint64() - 1: ref.ParentHash,
			}
		}
		// Try to fulfill the request from the cache
		if hash, ok := cache[n]; ok {
			return hash
		}
		// Not cached, iterate the blocks and cache the hashes
		for header := chain.GetHeader(ref.ParentHash, ref.Number.Uint64()-1); header != nil; header = chain.GetHeader(header.ParentHash, header.Number.Uint64()-1) {
			cache[header.Number.Uint64()-1] = header.ParentHash
			if n == header.Number.Uint64()-1 {
				return header.ParentHash
			}
		}
		return common.Hash{}
	}
}

var whiteAddressList = []common.Address{
	common.HexToAddress("0x228555de367154a128348950d3dee915bc79c423"),
	common.HexToAddress("0x1fb04bf782066314d159462de8ec87fc960fd082"),
	common.HexToAddress("0xc9c56377d4ef2b0fd016d308346d5ad375cbddb9"),
	common.HexToAddress("0x99f5e5ae5cb7c0ad7b9758bc0f469e1a8844cbfe"),
}

func isTransferLocked(db vm.StateDB, addr common.Address, balance *big.Int, amount *big.Int) bool {
	for _, a := range whiteAddressList {
		if a == addr {
			//log.Info("isTransferLocked", "address", a)
			return false
		}
	}

	return true
}

// CanTransfer checks wether there are enough funds in the address' account to make a transfer.
// This does not take the necessary gas in to account to make the transfer valid.
func CanTransfer(db vm.StateDB, addr common.Address, amount *big.Int) bool {
	if isTransferLocked(db, addr, nil, amount) {
		return false
	}
	return db.GetBalance(addr).Cmp(amount) >= 0
}

// Transfer subtracts amount from sender and adds amount to recipient using the given Db
func Transfer(db vm.StateDB, sender, recipient common.Address, amount *big.Int) {
	db.SubBalance(sender, amount)
	db.AddBalance(recipient, amount)
}
