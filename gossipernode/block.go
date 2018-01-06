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
const NbBlocksToCheckForTime = 11 // must be odd

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
func (gossiper *Gossiper) VerifyBlock(block *Block) bool {
	// Get the hash of the given block
	blockHash := block.hash()

	gossiper.errLogger.Printf("Verifying block: %x\n", blockHash)

	// Get current top hash
	gossiper.topBlockMutex.Lock()
	prevHash := gossiper.topBlock
	gossiper.topBlockMutex.Unlock()

	// Get current top block
	gossiper.blocksMutex.Lock()
	prevBlock, blockExists := gossiper.blocks[prevHash]
	gossiper.blocksMutex.Unlock()

	// If we couldn't find the top block, we are in a wrong state, we should panic
	if !blockExists {
		panic(errors.New(fmt.Sprintf("Cannot find top block (hash = %x).", prevHash[:])))
	}

	// Check syntactic correctness?

	// TODO: really needed? it seems already done before calling that by checking in 'blocks'
	// Reject if duplicate of block in main branch
	/*
		currentBlock := prevBlock
		for blockExists {
			currentHash := currentBlock.hash()
			if bytes.Equal(currentHash[:], blockHash[:]) {
				return false
			}

			// Get the previous block
			gossiper.blocksMutex.Lock()
			currentBlock, blockExists = gossiper.blocks[currentBlock.PrevHash]
			gossiper.blocksMutex.Unlock()
		}

		// Reject if duplicate of block in one of the forks
		gossiper.forksMutex.Lock()
		for topForkHash, _ := range gossiper.forks {
			// Get current top fork block
			gossiper.blocksMutex.Lock()
			currentBlock, blockExists = gossiper.blocks[topForkHash]
			gossiper.blocksMutex.Unlock()

			// If we couldn't find the top fork block, we are in a wrong state, we should panic
			if !blockExists {
				panic(errors.New(fmt.Sprintf("Cannot find top fork block (hash = %x).", topForkHash[:])))
			}

			for blockExists {
				currentHash := currentBlock.hash()
				if bytes.Equal(currentHash[:], blockHash[:]) {
					return false
				}

				// Get the previous block
				gossiper.blocksMutex.Lock()
				currentBlock, blockExists = gossiper.blocks[currentBlock.PrevHash]
				gossiper.blocksMutex.Unlock()
			}
		}
		gossiper.forksMutex.Unlock()

		// Reject if duplicate of block in orpans
		gossiper.blockOrphanPoolMutex.Lock()
		for orphanHash, _ := range gossiper.blockOrphanPool {
			if bytes.Equal(orphanHash[:], blockHash[:]) {
				return false
			}
		}
		gossiper.blockOrphanPoolMutex.Unlock()
	*/

	// Tx list must be non-empty (except for genesisBlock in our case)
	if !bytes.Equal(NilHash[:], block.PrevHash[:]) && len(block.Txs) == 0 {
		return false
	}

	// Block hash must satisfy its target
	if bytes.Compare(blockHash[:], block.Target[:]) >= 0 {
		return false
	}

	// Block timestamp must not be more than X seconds in the future (currently 2 hours)
	// TODO (maybe): might need to change the 2 hours in something more meaningful for us
	if block.Timestamp > time.Now().Unix()+MaxSecondsBlockInFuture {
		return false
	}

	// First transaction must be coinbase (i.e. only 1 input, with hash=0, idx=-1)
	coinbaseTx := block.Txs[0]
	if !coinbaseTx.isCoinbaseTx() {
		return false
	}

	// and the rest must not be coinbase
	for i := 1; i < len(block.Txs); i++ {
		tx := block.Txs[i]
		if tx.isCoinbaseTx() {
			return false
		}
	}

	// Re-verify all transactions (in Bitcoin, only doing 2-4 + verifying MerkleTree)
	for _, tx := range block.Txs {
		validTx, _ := gossiper.VerifyTx(tx)
		if !validTx {
			return false
		}
	}

	// Check that target is indeed the one it should be (checking previous block to see that)
	// TODO: change this if target change over time, should compute the expected target from the previous
	// block and check that the current target is indeed what was expected.
	if !bytes.Equal(prevBlock.Target[:], block.Target[:]) {
		return false
	}

	// Reject if timestamp is the median time of the last x blocks or before
	// TODO (maybe): might change the number of blocks to check for that (currently 11)
	var lastTimestamps int64arr
	currentBlock := prevBlock
	for i := 0; i < NbBlocksToCheckForTime && blockExists; i++ {
		lastTimestamps = append(lastTimestamps, currentBlock.Timestamp)

		// Get the previous block
		gossiper.blocksMutex.Lock()
		currentBlock, blockExists = gossiper.blocks[currentBlock.PrevHash]
		gossiper.blocksMutex.Unlock()
	}

	if len(lastTimestamps) == NbBlocksToCheckForTime {
		sort.Sort(lastTimestamps)
		medianTimestamp := lastTimestamps[NbBlocksToCheckForTime / 2]

	    if block.Timestamp <= medianTimestamp {
	    	return false
	    }
	}

	// Reject if coinbase value > sum of block creation fee and transaction fees
	coinbaseValue := block.Txs[0].Outputs[0].Value
	fees, feesError := gossiper.computeFees(block.Txs[1:])
	if feesError != nil || fees + BaseReward != coinbaseValue {
		return false
	}

	// TODO rest

	return true
}

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

// Already have 'gossiper.blocksMutex' when calling this function
func (gossiper *Gossiper) findForkBlockHash(topBlockFork *Block) ([32]byte, error) {
	gossiper.topBlockMutex.Lock()

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

	gossiper.topBlockMutex.Unlock()

	// Then we want to go down the fork branch (from the given block) and the first time we see
	// a hash we've already seen, it will be the block from where we forked
	topBlockForkHash := currentBlock.hash()
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
