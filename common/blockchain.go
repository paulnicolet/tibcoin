package common

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"math/big"
)

const MaxBlockSize = 1000000

type Sig struct {
	R *big.Int
	S *big.Int
}

type Address struct {
	PubKeyHash [32]byte
}

func (sig Sig) Equal(other Sig) bool {
	return sig.R.Cmp(other.R) == 0 && sig.S.Cmp(other.S) == 0
}

// ECDSA public key internals

func PublicKeyToAddress(public ecdsa.PublicKey) Address {
	key := append(public.X.Bytes(), public.Y.Bytes()...)
	return Address{PubKeyHash: sha256.Sum256(key)}
}

func PublicKeyEqual(k1 ecdsa.PublicKey, k2 ecdsa.PublicKey) bool {
	return k1.X.Cmp(k1.X) == 0 && k2.Y.Cmp(k2.Y) == 0
}
