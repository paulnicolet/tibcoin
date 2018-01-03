package gossipernode

import (
	"crypto/sha256"
	"time"
)

const MaxBlockSize = 1000000

type Block struct {
	TimeStamp int64
	Height    int
	Nonce     int
	PrevHash  [32]byte
	Txs       []*Transaction
}

func (block *Block) Hash() [32]byte {
	// TODO
	return sha256.Sum256(nil)
}

const CoinBase = 50

var GenesisBlock = Block{
	TimeStamp: time.Date(2018, 1, 3, 11, 00, 00, 00, time.UTC).Unix(),
	Height:    0,
	Nonce:     0,
}

func GenesisBlockHash() [32]byte {
	// TODO
	return sha256.Sum256(nil)
}
