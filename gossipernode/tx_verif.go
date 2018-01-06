package gossipernode

// See https://en.bitcoin.it/wiki/Protocol_rules#.22block.22_messages
// 2
func (tx *Tx) checkInOutListsNotEmpty() bool {
	return len(tx.Inputs) != 0 && len(tx.Outputs) != 0
}

// 3
func (tx *Tx) checkSize() bool {
	return tx.size() < MaxBlockSize
}

// 4
func (tx *Tx) checkOutputMoneyRange() bool {
	outputSum := 0
	for _, output := range tx.Outputs {
		outputSum += output.Value
		if output.Value <= 0 || output.Value > MaxCoins {
			return false
		}
	}

	return outputSum > 0 || outputSum <= MaxCoins
}

// 5
func (tx *Tx) checkNotCoinbaseTx() bool {
	return !tx.isCoinbaseTx()
}

// 8
func (tx *Tx) checkNotExistsInPoolOrMainBranch(gossiper *Gossiper, lookInMainBranch bool) bool {
	// Look in the pool
	gossiper.txPoolMutex.Lock()
	for _, other := range gossiper.txPool {
		if tx.equals(other) {
			return false
		}
	}
	gossiper.txPoolMutex.Unlock()

	if lookInMainBranch {
		// Look in main branch
		gossiper.topBlockMutex.Lock()
		topBlockHash := gossiper.topBlock
		gossiper.topBlockMutex.Unlock()
		gossiper.blocksMutex.Lock()
		currentBlock, blockExists := gossiper.blocks[topBlockHash]
		gossiper.blocksMutex.Unlock()
		for blockExists {
			for _, other := range currentBlock.Txs {
				if tx.equals(other) {
					return false
				}
			}

			gossiper.blocksMutex.Lock()
			currentBlock, blockExists = gossiper.blocks[currentBlock.PrevHash]
			gossiper.blocksMutex.Unlock()
		}
	}

	return true
}

// 9
func (tx *Tx) checkOutputsNotRefInPool(gossiper *Gossiper) bool {
	gossiper.txPoolMutex.Lock()
	defer gossiper.txPoolMutex.Unlock()

	for _, other := range gossiper.txPool {
		for _, otherIn := range other.Inputs {
			for _, in := range tx.Inputs {
				if in.sameOutput(otherIn) {
					return false
				}
			}
		}
	}

	return true
}

// 12
func (gossiper *Gossiper) checkNotSpentOutputs(outputs []*TxOutputLocation, lookInPool bool) bool {
	// Get first hash
	gossiper.topBlockMutex.Lock()
	topBlockHash := gossiper.topBlock
	gossiper.topBlockMutex.Unlock()

	// Get block
	gossiper.blocksMutex.Lock()
	currentBlock, blockExists := gossiper.blocks[topBlockHash]
	gossiper.blocksMutex.Unlock()

	// Look for spent outputs
	for blockExists {
		for _, tx := range currentBlock.Txs {
			for _, input := range tx.Inputs {
				for _, outputLocation := range outputs {
					if input.sameOutputLocation(outputLocation) {
						return false
					}
				}
			}
		}

		gossiper.blocksMutex.Lock()
		currentBlock, blockExists = gossiper.blocks[currentBlock.PrevHash]
		gossiper.blocksMutex.Unlock()
	}

	if lookInPool {
		// Look in the pool
		gossiper.txPoolMutex.Lock()
		for _, tx := range gossiper.txPool {
			for _, input := range tx.Inputs {
				for _, outputLocation := range outputs {
					if input.sameOutputLocation(outputLocation) {
						return false
					}
				}
			}
		}
		gossiper.txPoolMutex.Unlock()
	}

	return true
}

// 13
func (tx *Tx) checkInputsMoneyRange(outputs []*TxOutputLocation) bool {
	outputSum := 0
	for _, output := range outputs {
		outputSum += output.Output.Value
		if output.Output.Value <= 0 || output.Output.Value > MaxCoins {
			return false
		}
	}

	return outputSum > 0 || outputSum <= MaxCoins
}

// 14
func (tx *Tx) checkInputLessThanOutputs(outputs []*TxOutputLocation) bool {
	inputSum := 0
	for _, output := range outputs {
		inputSum += output.Output.Value
	}

	outputSum := 0
	for _, output := range tx.Outputs {
		outputSum += output.Value
	}

	return inputSum > outputSum
}

// 18.5.1.1
func (tx *Tx) VerifyOldMainBranchTx(gossiper *Gossiper) bool {
	if !tx.checkInOutListsNotEmpty() {
		return false
	}

	if !tx.checkSize() {
		return false
	}

	if !tx.checkOutputMoneyRange() {
		return false
	}

	if !tx.checkNotCoinbaseTx() {
		return false
	}

	if !tx.checkNotExistsInPoolOrMainBranch(gossiper, false) {
		return false
	}

	if !tx.checkOutputsNotRefInPool(gossiper) {
		return false
	}

	return true
}

// VerifyBlockTxs is called to verify txs in a Block
// Checks 16.1.[1-7] and 18.3.2.[1-7]
func (gossiper *Gossiper) VerifyBlockTxs(block *Block) bool {
	for _, tx := range block.Txs {
		if tx.isCoinbaseTx() {
			continue
		}
		// Get referenced outputs in the main branch only
		outputs, anyMissing := tx.getOutputs(gossiper, false)
		if anyMissing {
			return false
		}

		// Check signature
		if !gossiper.checkSig(tx) {
			return false
		}

		// Check if outputs are already spent in the main branch only
		if !gossiper.checkNotSpentOutputs(outputs, false) {
			return false
		}

		// Check input values
		if !tx.checkInputsMoneyRange(outputs) {
			return false
		}

		// Check input more than outputs
		if !tx.checkInputLessThanOutputs(outputs) {
			return false
		}
	}

	return true
}
