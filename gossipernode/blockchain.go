package gossipernode

import (
	"time"
)

const MaxBlockSize = 1000000

type Block struct {
	TimeStamp int64
	Height    int
	Nonce     int
	PrevHash  []byte
	Txs       map[[32]byte]*Transaction
}

const CoinBase = 50

var GenesisBlock = Block{
	TimeStamp: time.Date(2018, 1, 3, 11, 00, 00, 00, time.UTC).Unix(),
	Height:    0,
	Nonce:     0,
	PrevHash:  make([]byte, 0),
}
