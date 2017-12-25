package common

import "math/big"

const MaxBlockSize = 1000000

type Sig struct {
	R *big.Int
	S *big.Int
}

type Address struct {
	PubKeyHash []byte
}
