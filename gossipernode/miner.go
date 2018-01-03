package gossipernode

import (
	"crypto/sha256"
	"time"
)

func (gossiper *Gossiper) Mine(target []byte, previousBlock *Block, txs []*Transaction)  {
	// Hash the previous block
	prevHash := sha256.Sum256(previousBlock)

	// Compute the fees
	fees := gossiper.computeFees(txs)

	// Init block
	nonce := 0
	block := Block{
		TimeStamp: time.Now().Unix(),
		Height: previousBlock.Height + 1,
		Nonce: nonce,
		PrevHash: prevHash,
		Txs: txs
	}

	for sha256.Sum256(block) >= target {
		nonce++
		block := Block{
			TimeStamp: time.Now().Unix(),
			Height: previousBlock.Height + 1,
			Nonce: nonce,
			PrevHash: prevHash,
			Txs: txs
		}
	}
}

func (gossiper *Gossiper) computeFees(txs []*Transaction) int {
	totalFees := 0
	for _, tx := txs {
		totalFees += gossiper.computeTxFee(tx)
	}

	return totalFees
}

