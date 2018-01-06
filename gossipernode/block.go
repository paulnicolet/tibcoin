package gossipernode

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"
)

const MaxBlockSize = 500000
const MaxCoins = 1000000000
const MaxSecondsBlockInFuture = 2 * 3600 // 2 hours
const NbBlocksToCheckForTime = 11        // must be odd

type Block struct {
	Timestamp int64
	Height    uint32
	Nonce     uint32
	Target    [32]byte // TODO (maybe): Change into 4 bytes and use difficulty + change it over time
	PrevHash  [32]byte
	Txs       []*Tx
}

type SerializableBlock struct {
	Timestamp int64
	Height    uint32
	Nonce     uint32
	Target    [32]byte
	PrevHash  [32]byte
	Txs       []*SerializableTx
}

// Used for sorting []int64
type int64arr []int64

func (a int64arr) Len() int           { return len(a) }
func (a int64arr) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a int64arr) Less(i, j int) bool { return a[i] < a[j] }

var NilHash = BytesToHash(make([]byte, 32))

var InitialTarget, _ = hex.DecodeString("00000F0000000000000000000000000000000000000000000000000000000000")

// Nonce found in order to have a genesis block respecting initial target; should be recomputed
// if anything about the genesis block is changed
var GenesisNonce uint32 = 538367

var GenesisBlock = &Block{
	Timestamp: time.Date(2018, 1, 3, 11, 00, 00, 00, time.UTC).Unix(),
	Height:    0,
	Nonce:     GenesisNonce,
	Target:    BytesToHash(InitialTarget),
	PrevHash:  NilHash,
	Txs:       make([]*Tx, 0),
}

// Inspired by: https://en.bitcoin.it/wiki/Protocol_rules#.22block.22_messages
func (gossiper *Gossiper) VerifyBlock(block *Block, hash [32]byte) bool {

	gossiper.errLogger.Printf("Verifying block %x\n", hash)

	// Need to take all necessary locks here if VerifyBlock can be called by
	// multiple threads
	gossiper.blocksMutex.Lock()
	gossiper.forksMutex.Lock()
	gossiper.topBlockMutex.Lock()

	valid := true

	if valid && gossiper.isCorrupted(block, hash) {
		valid = false
	}

	if valid && gossiper.isBlockDuplicate(block, hash) {
		valid = false
	}

	if valid && gossiper.isTxListEmpty(block, hash) {
		valid = false
	}

	if valid && !gossiper.satisfyTarget(block, hash) {
		valid = false
	}

	if valid && gossiper.tooMuchInFuture(block, hash) {
		valid = false
	}

	if valid && !gossiper.onlyFirstTxIsCoinbase(block, hash) {
		valid = false
	}

	// Apply tx checks 2-4
	for _, tx := range block.Txs {
		if !tx.checkInOutListsNotEmpty() {
			valid = false
		}

		if !tx.checkSize() {
			valid = false
		}

		if !tx.checkOutputMoneyRange() {
			valid = false
		}
	}

	if valid && gossiper.isOrphan(block, hash) {
		gossiper.blockOrphanPoolMutex.Lock()
		gossiper.blockOrphanPool[hash] = block.PrevHash
		gossiper.blockOrphanPoolMutex.Unlock()

		gossiper.blocks[hash] = block

		gossiper.topBlockMutex.Unlock()
		gossiper.forksMutex.Unlock()
		gossiper.blocksMutex.Unlock()

		return true
	}

	if valid && !gossiper.containsExpectedTarget(block, hash) {
		valid = false
	}

	if valid && gossiper.isTooLateComparedToMedian(block, hash) {
		valid = false
	}

	if gossiper.extendsMainChain(block, hash) {
		// Case 1
		valid = gossiper.addToMainBranch(block, hash)
	} else if !gossiper.isNewChainBiggerThanMain(block, hash) {
		// Case 2
		valid = gossiper.addToSideBranch(block, hash)
	} else {
		// Case 3
		valid = gossiper.replaceMainBranch(block, hash)
	}

	gossiper.topBlockMutex.Unlock()
	gossiper.forksMutex.Unlock()
	gossiper.blocksMutex.Unlock()

	// Check 19
	if valid {
		// Recursively check orphans which are our children
		gossiper.blockOrphanPoolMutex.Lock()

		var orphanHashesToCheck [][32]byte
		for oprhanHash, prevOrphanHash := range gossiper.blockOrphanPool {
			if bytes.Equal(hash[:], prevOrphanHash[:]) {
				orphanHashesToCheck = append(orphanHashesToCheck, oprhanHash)
			}
		}

		gossiper.blockOrphanPoolMutex.Unlock()

		for _, orphanHashToCheck := range orphanHashesToCheck {
			gossiper.blocksMutex.Lock()
			gossiper.blockOrphanPoolMutex.Lock()
			orphan, foundOrphan := gossiper.blocks[orphanHashToCheck]
			delete(gossiper.blocks, orphanHashToCheck)
			delete(gossiper.blockOrphanPool, orphanHashToCheck)
			gossiper.blockOrphanPoolMutex.Unlock()
			gossiper.blocksMutex.Unlock()

			if !foundOrphan {
				panic(errors.New(fmt.Sprintf("Cannot find orphan block (hash = %x).", orphanHashToCheck)))
			}

			if !gossiper.VerifyBlock(orphan, orphanHashToCheck) {
				gossiper.errLogger.Printf("Orphan of %x failed verification; most likely ok (hash = %x).\n", hash[:], orphanHashToCheck[:])
			}
		}
	}

	return valid
}

/* Verify helpers */

// Check 1
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) isCorrupted(block *Block, hash [32]byte) bool {
	tmp := block.hash()
	return !bytes.Equal(tmp[:], hash[:])
}

// Check 2
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) isBlockDuplicate(block *Block, hash [32]byte) bool {
	_, isDuplicate := gossiper.blocks[hash]

	return isDuplicate
}

// Check 3
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) isTxListEmpty(block *Block, hash [32]byte) bool {
	// Tx list must be non-empty (except for genesisBlock in our case)
	return !bytes.Equal(NilHash[:], block.PrevHash[:]) && len(block.Txs) == 0
}

// Check 4
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) satisfyTarget(block *Block, hash [32]byte) bool {
	return bytes.Compare(hash[:], block.Target[:]) < 0
}

// Check 5
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) tooMuchInFuture(block *Block, hash [32]byte) bool {
	return block.Timestamp > time.Now().Unix()+MaxSecondsBlockInFuture
}

// Check 6
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) onlyFirstTxIsCoinbase(block *Block, hash [32]byte) bool {
	for i := 1; i < len(block.Txs); i++ {
		tx := block.Txs[i]
		if tx.isCoinbaseTx() {
			return false
		}
	}

	coinbaseTx := block.Txs[0]
	return coinbaseTx.isCoinbaseTx()
}

// Check 11
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) isOrphan(block *Block, hash [32]byte) bool {
	_, containsOurPrev := gossiper.blocks[block.PrevHash]

	if containsOurPrev {
		gossiper.blockOrphanPoolMutex.Lock()
		_, isOrphan := gossiper.blockOrphanPool[block.PrevHash]
		gossiper.blockOrphanPoolMutex.Unlock()

		if !isOrphan {
			return false
		}
	}

	return true
}

// Check 12
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) containsExpectedTarget(block *Block, hash [32]byte) bool {
	prevBlock, foundPrevBlock := gossiper.blocks[block.PrevHash]
	if !foundPrevBlock {
		panic(errors.New(fmt.Sprintf("Cannot find prev block (hash = %x).", block.PrevHash[:])))
	}

	return bytes.Equal(prevBlock.Target[:], block.Target[:])
}

// Check 13
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) isTooLateComparedToMedian(block *Block, hash [32]byte) bool {
	prevBlock, foundPrevBlock := gossiper.blocks[block.PrevHash]
	if !foundPrevBlock {
		panic(errors.New(fmt.Sprintf("Cannot find prev block (hash = %x).", block.PrevHash[:])))
	}

	var lastTimestamps int64arr
	currentBlock := prevBlock
	blockExists := true
	for i := 0; i < NbBlocksToCheckForTime && blockExists; i++ {
		lastTimestamps = append(lastTimestamps, currentBlock.Timestamp)

		// Get the previous block
		currentBlock, blockExists = gossiper.blocks[currentBlock.PrevHash]
	}

	if len(lastTimestamps) == NbBlocksToCheckForTime {
		sort.Sort(lastTimestamps)
		medianTimestamp := lastTimestamps[NbBlocksToCheckForTime/2]

		if block.Timestamp <= medianTimestamp {
			return true
		}
	}

	return false
}

// Check 15-1
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) extendsMainChain(block *Block, hash [32]byte) bool {
	return bytes.Equal(block.PrevHash[:], gossiper.topBlock[:])
}

// Check 15-2
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) isNewChainBiggerThanMain(block *Block, hash [32]byte) bool {
	prevBlock, foundPrevBlock := gossiper.blocks[block.PrevHash]
	if !foundPrevBlock {
		panic(errors.New(fmt.Sprintf("Cannot find prev block (hash = %x).", block.PrevHash[:])))
	}

	topBlock, foundTopBlock := gossiper.blocks[gossiper.topBlock]
	if !foundTopBlock {
		panic(errors.New(fmt.Sprintf("Cannot find top block (hash = %x).", gossiper.topBlock[:])))
	}

	return prevBlock.Height+1 > topBlock.Height
}

// Check 16
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) addToMainBranch(block *Block, hash [32]byte) bool {
	// TODO: Apply 16.1.1-7 to all txs but coinbase

	if !gossiper.correctCoinbaseValue(block, hash) {
		return false
	}

	// Make it extend the main branch and add it to blocks and forks
	gossiper.forks[hash] = true
	delete(gossiper.forks, gossiper.topBlock)
	gossiper.blocks[hash] = block
	gossiper.topBlock = hash

	// Filter txPool
	gossiper.removeBlockTxsFromPool(block)

	// Broadcast
	gossiper.broadcastBlockToPeers(block)

	return true
}

// Check 16.2
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) correctCoinbaseValue(block *Block, hash [32]byte) bool {
	coinbaseValue := block.Txs[0].Outputs[0].Value
	fees, feesError := gossiper.computeFees(block.Txs[1:])
	if feesError == nil && fees+BaseReward == coinbaseValue {
		return true
	}

	return false
}

// Check 16.5
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) removeBlockTxsFromPool(block *Block) {
	gossiper.txPoolMutex.Lock()

	// Check which tx are in pool but not in given block
	var filteredPool []*Tx
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
		} else {
			txHash := txPool.hash()
			gossiper.errLogger.Printf("Removed tx: %x\n", txHash[:])
		}
	}

	// Update pool
	gossiper.txPool = filteredPool

	gossiper.txPoolMutex.Unlock()
}

// Check 16.6
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) broadcastBlockToPeers(block *Block) {
	gossiper.peersMutex.Lock()
	for _, peer := range gossiper.peers {
		sendErr := gossiper.sendBlockTo(block, peer.addr)
		if sendErr != nil {
			blockHash := block.hash()
			gossiper.errLogger.Printf("Error sending mined block (hash = %x): %v\n", blockHash[:], sendErr)
		}
	}
	gossiper.peersMutex.Unlock()
}

// Check 17
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) addToSideBranch(block *Block, hash [32]byte) bool {
	gossiper.blocks[hash] = block
	gossiper.forks[hash] = true
	delete(gossiper.forks, block.PrevHash)

	return true
}

// Check 18
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) replaceMainBranch(block *Block, hash [32]byte) bool {
	forkHash, forkErr := gossiper.findForkBlockHash(block)

	// If couldnt find fork, we are in a bad state, panic
	if forkErr != nil {
		panic(forkErr)
	}

	if gossiper.verifyNewMainBranch(block, hash, forkHash) {
		gossiper.blocks[hash] = block
		gossiper.forks[hash] = true
		delete(gossiper.forks, block.PrevHash)
		gossiper.topBlock = hash
	} else {
		return false
	}

	return true
}

// Check 18.1
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) findForkBlockHash(topBlockFork *Block) ([32]byte, error) {

	// First we should go down the entire main chain and store all hashes
	var mainHashes map[[32]byte]bool = make(map[[32]byte]bool)
	mainHashes[gossiper.topBlock] = true
	currentBlock, blockExists := gossiper.blocks[gossiper.topBlock]
	for blockExists {
		// Don't add prev of genesis block
		if !bytes.Equal(currentBlock.PrevHash[:], NilHash[:]) {
			mainHashes[currentBlock.PrevHash] = true
		}

		currentBlock, blockExists = gossiper.blocks[currentBlock.PrevHash]
	}

	// Then we want to go down the fork branch (from the given block) and the first time we see
	// a hash we've already seen, it will be the block from where we forked
	topBlockForkHash := topBlockFork.hash()
	currentBlock = topBlockFork
	currentHash := topBlockForkHash
	blockExists = true
	for blockExists {
		if _, found := mainHashes[currentHash]; found {
			return currentHash, nil
		}

		currentHash = currentBlock.PrevHash
		currentBlock, blockExists = gossiper.blocks[currentHash]
	}

	return NilHash, errors.New(fmt.Sprintf("Couldn't find the block from where we forked on the main branch; block in fork: %x.", topBlockForkHash[:]))
}

// Check 18.3
// Assume locks: topBlock, forks, blocks
func (gossiper *Gossiper) verifyNewMainBranch(topBlockFork *Block, topBlockForkHash [32]byte, forkHash [32]byte) bool {
	var blocksToAdd []*Block
	var hashesToAdd [][32]byte

	blocksToAdd = append(blocksToAdd, topBlockFork)
	hashesToAdd = append(hashesToAdd, topBlockForkHash)

	currentHash := topBlockForkHash
	currentBlock := topBlockFork
	foundBlock := true
	for !bytes.Equal(currentHash[:], forkHash[:]) {
		currentHash = currentBlock.PrevHash
		currentBlock, foundBlock = gossiper.blocks[currentHash]
		if !foundBlock {
			panic(errors.New(fmt.Sprintf("Cannot find block when verifying new main branch (hash = %x).", currentHash[:])))
		}

		blocksToAdd = append(blocksToAdd, currentBlock)
		hashesToAdd = append(hashesToAdd, currentHash)
	}

	for i := len(blocksToAdd) - 1; i >= 0; i-- {
		currentBlock = blocksToAdd[i]
		currentHash = hashesToAdd[i]

		// We maybe should do 3-11, but why? Doesn't seem useful...

		// TODO: Apply 16.1.1-7 to all txs but coinbase

		if !gossiper.correctCoinbaseValue(currentBlock, currentHash) {
			return false
		}
	}

	currentHash = gossiper.topBlock
	currentBlock, foundBlock = gossiper.blocks[currentHash]
	if !foundBlock {

	}

	// Add txs back to the txPool from the old main branch
	for !bytes.Equal(currentHash[:], forkHash[:]) {
		// Iterate over txs and put valid ones in txPool
		for _, tx := range currentBlock.Txs[1:] {
			// TODO: tx checks 2-9 (8 is weird)

			valid := true

			if valid {
				gossiper.txPoolMutex.Lock()
				gossiper.txPool = append(gossiper.txPool, tx)
				gossiper.txPoolMutex.Unlock()
			}
		}

		currentHash = currentBlock.PrevHash
		currentBlock, foundBlock = gossiper.blocks[currentHash]
	}

	// Remove block's txs from the txPool (from the new main branch)
	for i := len(blocksToAdd) - 1; i >= 0; i-- {
		currentBlock = blocksToAdd[i]
		currentHash = hashesToAdd[i]

		gossiper.removeBlockTxsFromPool(currentBlock)
	}

	gossiper.broadcastBlockToPeers(topBlockFork)

	return true
}

func (block *Block) hash() [32]byte {
	hash := sha256.New()
	hash.Write([]byte(fmt.Sprintf("%v", block.Timestamp)))
	hash.Write([]byte(strconv.Itoa(int(block.Height))))
	hash.Write([]byte(strconv.Itoa(int(block.Nonce))))
	hash.Write(block.PrevHash[:])

	for _, tx := range block.Txs {
		txHash := tx.hash()
		hash.Write(txHash[:])
	}

	return BytesToHash(hash.Sum(nil))
}

func (block *Block) toSerializable() (*SerializableBlock, error) {
	var serTxs []*SerializableTx

	for _, tx := range block.Txs {
		serTx, err := tx.toSerializable()
		if err != nil {
			return nil, err
		}

		serTxs = append(serTxs, serTx)
	}

	return &SerializableBlock{
		Timestamp: block.Timestamp,
		Height:    block.Height,
		Nonce:     block.Nonce,
		Target:    block.Target,
		PrevHash:  block.PrevHash,
		Txs:       serTxs,
	}, nil
}

func (block *SerializableBlock) toNormal() (*Block, error) {
	var txs []*Tx

	for _, serTx := range block.Txs {
		tx, err := serTx.toNormal()
		if err != nil {
			return nil, err
		}

		txs = append(txs, tx)
	}

	return &Block{
		Timestamp: block.Timestamp,
		Height:    block.Height,
		Nonce:     block.Nonce,
		Target:    block.Target,
		PrevHash:  block.PrevHash,
		Txs:       txs,
	}, nil
}
