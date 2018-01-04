package gossipernode

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"time"
)

const MaxBlockSize = 500000
const MaxCoins = 1000000000

type Block struct {
	Timestamp int64
	Height    int
	Nonce     int
	PrevHash  [32]byte
	Txs       []*Transaction
}

var GenesisBlock = &Block{
	Timestamp: time.Date(2018, 1, 3, 11, 00, 00, 00, time.UTC).Unix(),
	Height:    0,
	Nonce:     0,
	PrevHash:  BytesToHash(make([]byte, 32)),
	Txs:	   make([]*Transaction, 0),
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

func (gossiper *Gossiper) removeBlockTxsFromPool(block *Block) {
	gossiper.txPoolMutex.Lock()

	// Check which tx are in pool but not in given block
	var filteredPool []*Transaction
	for _, txPool := range gossiper.txPool {
		inBlock := false
		for _, txBlock := range block.Txs {
			if txPool.equals(txBlock) {
				inBlock = true
				break
			}
		}

		// Only keep if not in block
		if !inBlock {
			filteredPool = append(filteredPool, txPool)
		}
	}

	// Update pool
	gossiper.txPool = filteredPool

	gossiper.txPoolMutex.Unlock()
}
