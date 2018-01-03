package common

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"math/big"
	"time"
)

const MaxBlockSize = 1000000

type Sig struct {
	R *big.Int
	S *big.Int
}

type Block struct {
	TimeStamp int64
	Height    int
	Nonce     int
	PrevHash  []byte
	Txs       map[[]byte]Transaction
}

const CoinBase = 50
const GenesisBlock = Block{
	TimeStamp: new time.Date(2018, 1, 3, 11, 00, 00, 00, time.UTC).Unix(),
	Height: 0,
	Nonce: 0,
	PrevHash: make([]byte, 0),
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
