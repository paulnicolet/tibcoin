package gossipernode

import (
	"bytes"
	"time"
)

func (gossiper *Gossiper) Mine(target []byte, previousBlock *Block, txs []*Transaction) {
	// Hash the previous block
	prevHash := previousBlock.Hash()

	// Compute the fees
	fees := gossiper.computeFees(txs)
	fees++

	// Init block
	nonce := 0
	block := Block{
		TimeStamp: time.Now().Unix(),
		Height:    previousBlock.Height + 1,
		Nonce:     nonce,
		PrevHash:  prevHash,
		Txs:       txs,
	}

	blockHash := block.Hash()
	for bytes.Compare(blockHash[:], target) >= 0 {
		nonce++
		block := Block{
			TimeStamp: time.Now().Unix(),
			Height:    previousBlock.Height + 1,
			Nonce:     nonce,
			PrevHash:  prevHash,
			Txs:       txs,
		}
		blockHash = block.Hash()
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
