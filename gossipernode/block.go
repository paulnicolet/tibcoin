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
		hash.Write(tx.hash())
	}

	return BytesToHash(hash.Sum(nil))
}

const CoinBase = 50

var GenesisBlock = Block{
	Timestamp: time.Date(2018, 1, 3, 11, 00, 00, 00, time.UTC).Unix(),
	Height:    0,
	Nonce:     0,
}

func GenesisBlockHash() [32]byte {
	// TODO
	return sha256.Sum256(nil)
}
