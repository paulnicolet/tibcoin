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

func (sig Sig) Equal(other Sig) bool {
	return sig.R.Cmp(other.R) == 0 && sig.S.Cmp(other.S) == 0
}
