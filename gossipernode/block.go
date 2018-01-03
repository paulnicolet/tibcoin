package gossipernode

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"time"
)

const MaxBlockSize = 1000000

type Block struct {
	Timestamp int64
	Height    int
	Nonce     int
	PrevHash  [32]byte
	Txs       []*Transaction
}

func (block *Block) hash() [32]byte {
	hash := sha256.New()
	hash.Write([]byte(fmt.Sprintf("%v", block.Timestamp)))
	hash.Write([]byte(strconv.Itoa(block.Height)))
	hash.Write([]byte(strconv.Itoa(block.Nonce)))
	hash.Write(block.PrevHash[:])

	for _, tx := range block.Txs {
		txHash := tx.hash()
		hash.Write(txHash[:])
	}

	return BytesToHash(hash.Sum(nil))
}

var GenesisBlock = &Block{
	Timestamp: time.Date(2018, 1, 3, 11, 00, 00, 00, time.UTC).Unix(),
	Height:    0,
	Nonce:     0,
	PrevHash:  BytesToHash(make([]byte, 32)),
	Txs:	   make([]*Transaction, 0),
}
