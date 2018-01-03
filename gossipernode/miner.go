package gossipernode

import (
	"bytes"
	"time"
)

func (gossiper *Gossiper) Mine(target []byte, previousBlock *Block, txs []*Transaction) {
	// Hash the previous block
	prevHash := previousBlock.hash()

	// Compute the fees
	fees := gossiper.computeFees(txs)
	fees++

	// Init block
	nonce := 0
	block := Block{
		Timestamp: time.Now().Unix(),
		Height:    previousBlock.Height + 1,
		Nonce:     nonce,
		PrevHash:  prevHash,
		Txs:       txs,
	}

	blockHash := block.hash()
	for bytes.Compare(blockHash[:], target) >= 0 {
		nonce++
		block := Block{
			Timestamp: time.Now().Unix(),
			Height:    previousBlock.Height + 1,
			Nonce:     nonce,
			PrevHash:  prevHash,
			Txs:       txs,
		}
		blockHash = block.hash()
	}
}

func (gossiper *Gossiper) computeFees(txs []*Transaction) int {
	totalFees := 0
	for _, tx := range txs {
		fees, err := gossiper.computeTxFee(tx)
		if err != nil {
			// TODO
		}
		totalFees += fees
	}

	return totalFees
}
