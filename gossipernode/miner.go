package gossipernode

import (
	"bytes"
	"fmt"
	"time"
)

const BaseReward = 1000

func (gossiper *Gossiper) Mine(channel <-chan bool) (*Block, error) {
	// Wait the first time for the channel
	<-channel

	fmt.Println("Started mining node.")

	// Get all necessary information to mine new block
	gossiper.targetMutex.Lock()
	target := gossiper.target
	gossiper.targetMutex.Unlock()

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

	// Create Coinbase transaction
	coinbaseTx, coinbaseErr := gossiper.createCoinbaseTx(fees)
	if coinbaseErr != nil {
		gossiper.errLogger.Printf("Error when signing coinbase tx (prevHash = %x, height = %d); sleeping for 60 sec.\n", prevHash[:], previousBlock.Height+1)
		time.Sleep(60 * time.Second)
	}

	// Prepend the Coinbase tx to all txs
	newTxs := make([]*Transaction, 1)
	newTxs[0] = coinbaseTx
	newTxs = append(newTxs, txs...)

	// Mine until we find a block or we're told to start mining again 
	var nonce uint32 = 0
	resetBlock := false
	
	for {
		select {
		case <-channel:
			resetBlock = true
		default:
			// Do nothing
		}

		if resetBlock {
			// Get all necessary information to mine new block
			gossiper.targetMutex.Lock()
			target = gossiper.target
			gossiper.targetMutex.Unlock()

			gossiper.topBlockMutex.Lock()
			prevHash = gossiper.topBlock
			gossiper.topBlockMutex.Unlock()

			gossiper.blocksMutex.Lock()
			previousBlock, _ = gossiper.blocks[prevHash]
			gossiper.blocksMutex.Unlock()

			txs = gossiper.getMaxTxsFromPool()

			// Compute the fees
			fees, feesErr = gossiper.computeFees(txs)
			if feesErr != nil {
				gossiper.errLogger.Printf("Error when computing fees of txs (prevHash = %x, height = %d); sleeping for 60 sec.\n", prevHash[:], previousBlock.Height+1)
				time.Sleep(60 * time.Second)
			}

			// Create Coinbase transaction
			coinbaseTx, coinbaseErr = gossiper.createCoinbaseTx(fees)
			if coinbaseErr != nil {
				gossiper.errLogger.Printf("Error when signing coinbase tx (prevHash = %x, height = %d); sleeping for 60 sec.\n", prevHash[:], previousBlock.Height+1)
				time.Sleep(60 * time.Second)
			}

			// Prepend the Coinbase tx to all txs
			newTxs = make([]*Transaction, 1)
			newTxs[0] = coinbaseTx
			newTxs = append(newTxs, txs...)
		}

		// Create new block + hash
		block := &Block{
			Timestamp: time.Now().Unix(),
			Height:    previousBlock.Height + 1,
			Nonce:     nonce,
			PrevHash:  prevHash,
			Txs:       newTxs,
		}

		blockHash := block.hash()

		// See if found new valid block
		if bytes.Compare(blockHash[:], target[:]) < 0 {
			// Found block!
			fmt.Printf("Found new block: %x (height = %d).\n", blockHash[:], previousBlock.Height+1)
			gossiper.blocksMutex.Lock()
			gossiper.blocks[blockHash] = block
			gossiper.blocksMutex.Unlock()

			gossiper.topBlockMutex.Lock()
			gossiper.topBlock = blockHash
			gossiper.topBlockMutex.Unlock()

			// Filter txPool
			gossiper.removeBlockTxsFromPool(block)

			resetBlock = true
		} else {
			resetBlock = false
		}

		nonce++
	}
}

func (gossiper *Gossiper) computeFees(txs []*Transaction) (int, error) {
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

func (gossiper *Gossiper) createCoinbaseTx(fees int) (*Transaction, error) {
	// 1 special input (hash = nil, idx = -1)
	inputs := make([]*TxInput, 1)
	outputs[0] = &TxInput{
		outputTxHash: NilHash,
		outputIdx: -1,
	}

	// 1 single output being ourselves
	outputs := make([]*TxOutput, 1)
	outputs[0] = &TxOutput{
		Value: fees + BaseReward,
		To:    PublicKeyToAddress(gossiper.publicKey),
	}

	// Create tx (unsigned)
	tx := &Transaction{
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
func (gossiper *Gossiper) getMaxTxsFromPool() []*Transaction {
	gossiper.txPoolMutex.Lock()

	// Take txs until max size or pool empty
	var txs []*Transaction
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

	return txs
}
