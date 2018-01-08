package gossipernode

import (
	"bytes"
	"math/rand"
	"time"
)

const BaseReward = 1000

func (gossiper *Gossiper) Mine() {
	gossiper.errLogger.Println("Started mining node.")

	// Mine until we find a block or we're told to start mining again
	var nonce uint32 = rand.Uint32()
	var block *Block

	for {
		// Stop if not mining anymore
		miner := gossiper.isMinerNode()
		if !miner {
			break
		}

		gossiper.resetBlockMutex.Lock()
		resetBlock := gossiper.resetBlock
		gossiper.resetBlockMutex.Unlock()

		if resetBlock {
			gossiper.resetBlockMutex.Lock()
			gossiper.resetBlock = false
			gossiper.resetBlockMutex.Unlock()

			// Randomize nonce when resetting
			nonce = rand.Uint32()

			// Get all necessary information to mine new block

			gossiper.topBlockMutex.Lock()
			prevHash := gossiper.topBlock
			gossiper.topBlockMutex.Unlock()

			gossiper.blocksMutex.Lock()
			previousBlock, _ := gossiper.blocks[prevHash]
			gossiper.blocksMutex.Unlock()

			txs := gossiper.getMaxTxsFromPool()

			// Compute the fees
			fees, feesErr := gossiper.computeFees(txs)
			if feesErr != nil {
				gossiper.errLogger.Printf("Error when computing fees of txs (prevHash = %x, height = %d); sleeping for 60 sec.\n", prevHash[:], previousBlock.Height+1)
				time.Sleep(60 * time.Second)
			}

			gossiper.errLogger.Printf("Fees computed with %d txs: %d\n", len(txs), fees)

			// Create Coinbase transaction
			coinbaseTx, coinbaseErr := gossiper.createCoinbaseTx(fees)
			if coinbaseErr != nil {
				gossiper.errLogger.Printf("Error when signing coinbase tx (prevHash = %x, height = %d); sleeping for 60 sec.\n", prevHash[:], previousBlock.Height+1)
				time.Sleep(60 * time.Second)
			}

			// Prepend the Coinbase tx to all txs
			newTxs := make([]*Tx, 1)
			newTxs[0] = coinbaseTx
			newTxs = append(newTxs, txs...)

			// Create new block + hash
			block = &Block{
				Timestamp:        time.Now().Unix(),
				Height:           previousBlock.Height + 1,
				Nonce:            nonce,
				PrevHash:         prevHash,
				TransactionsHash: hashTxs(newTxs),
				Txs:              newTxs,
				Target:           BytesToHash(InitialTarget),
			}
		}

		block.Nonce++
		blockHash := block.hash()

		// See if found new valid block
		if bytes.Compare(blockHash[:], InitialTarget) < 0 {
			// Verify block
			gossiper.errLogger.Printf("Found new block worth %d tibcoins: %x (height = %d).\n", block.Txs[0].Outputs[0].Value, blockHash[:], block.Height)
			if gossiper.VerifyBlock(block, blockHash) {
				// Found block!
				gossiper.errLogger.Printf("Valid new block")
			} else {
				gossiper.errLogger.Println("Invalid mined block")
			}

			gossiper.resetBlockMutex.Lock()
			gossiper.resetBlock = true
			gossiper.resetBlockMutex.Unlock()
		}
	}
}

func (gossiper *Gossiper) computeFees(txs []*Tx) (int, error) {
	totalFees := 0
	for _, tx := range txs {
		// Get current transaction fee
		fees, feeErr := gossiper.computeTxFee(tx)
		if feeErr != nil {
			return 0, feeErr
		}

		totalFees += fees
	}

	return totalFees, nil
}

func (gossiper *Gossiper) createCoinbaseTx(fees int) (*Tx, error) {
	// 1 special input (hash = nil, idx = -1)
	inputs := make([]*TxInput, 1)
	inputs[0] = &TxInput{
		OutputTxHash: NilHash,
		OutputIdx:    -1,
	}

	// 1 single output being ourselves
	outputs := make([]*TxOutput, 1)
	outputs[0] = &TxOutput{
		Value: fees + BaseReward,
		To:    PublicKeyToAddress(gossiper.publicKey),
	}

	// Create tx (unsigned)
	tx := &Tx{
		Inputs:    inputs,
		Outputs:   outputs,
		PublicKey: gossiper.publicKey,
	}

	// Sign it
	signedTx, signErr := gossiper.signTx(tx)
	if signErr != nil {
		return nil, signErr
	}

	return signedTx, nil
}

// Go in the pool and take as much tx as we can (don't remove anything yet)
func (gossiper *Gossiper) getMaxTxsFromPool() []*Tx {
	gossiper.txPoolMutex.Lock()

	// Take txs until max size or pool empty
	var txs []*Tx
	currentSize := 0
	idx := 0
	for currentSize <= MaxBlockSize && idx < len(gossiper.txPool) {
		currentTx := gossiper.txPool[idx]
		txs = append(txs, currentTx)
		currentSize += currentTx.size()
		idx++
	}

	// If we put one too much, remove last tx
	if currentSize > MaxBlockSize {
		txs = txs[:len(txs)-1]
	}

	gossiper.txPoolMutex.Unlock()

	gossiper.errLogger.Printf("Got %d txs from pool to create new block.\n", len(txs))

	return txs
}
