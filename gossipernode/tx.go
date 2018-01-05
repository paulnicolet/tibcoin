package gossipernode

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"math"
)

/*
Simplified transaction scheme:

Bitcoin uses scripting to lock and unlock transactions to provide a generic
way to verifiy transactions, since it provides multiple transaction types.
In our case only P2PKH is used, and we remove scripting:

1. Each transaction output links receiver with its address (PKH)
2. Each transaction input links UTXO with its tx hash and index
3. Each transaction is signed using the private key and provides the public key
4. To verify a transaction signature
	1. Derive address from transaction public key and make sure it is the same
		as in each UTXO
	2. If it is the case, check the signature with the public key
	3. If it succeeds, the transaction has been signed by the receiver of each
		UTXO, it can then spend the UTXOs
*/

const FeeRatio = float64(0.02)

type Tx struct {
	Inputs    []*TxInput
	Outputs   []*TxOutput
	Sig       *Sig
	PublicKey *PublicKey
}

type SerializableTx struct {
	Inputs    []*TxInput
	Outputs   []*TxOutput
	Sig       *SerializedSig
	PublicKey *SerializedPublicKey
}

type TxInput struct {
	OutputTxHash [32]byte
	OutputIdx    int
}

type TxOutput struct {
	Value int
	To    string
}

type TxOutputLocation struct {
	Output       *TxOutput
	OutputTxHash [32]byte
	OutputIdx    int
}

func (gossiper *Gossiper) NewTx(to string, value int) (*Tx, error) {
	// Create new transaction
	tx := &Tx{
		PublicKey: gossiper.publicKey,
	}

	// Get outputs
	outputs := gossiper.CollectOutputs()
	unspent := gossiper.FilterSpentOutputs(outputs)

	// Add inputs
	sum := 0
	fees := int(math.Ceil(FeeRatio * float64(value)))
	for _, output := range unspent {
		input := &TxInput{
			OutputTxHash: output.OutputTxHash,
			OutputIdx:    output.OutputIdx,
		}

		tx.Inputs = append(tx.Inputs, input)

		sum += output.Output.Value
		if sum >= value+fees {
			break
		}
	}

	gossiper.errLogger.Println("Collecting UTXOs for new tx...")
	gossiper.errLogger.Printf("Collected %d UTXOs and %d are unspent", len(outputs), len(unspent))
	gossiper.errLogger.Printf("Using a sum of %d for value %d and fees %d", sum, value, fees)

	if sum < value+fees {
		gossiper.errLogger.Println("Not enough UTXO to create new tx")
		return nil, errors.New("Not enough UTXO to create new transaction")
	}

	// Add main output
	mainOutput := &TxOutput{
		To:    to,
		Value: value,
	}
	tx.Outputs = append(tx.Outputs, mainOutput)

	// Add change
	change := sum - (value + fees)
	changeOutput := &TxOutput{
		To:    PublicKeyToAddress(gossiper.publicKey),
		Value: change,
	}
	tx.Outputs = append(tx.Outputs, changeOutput)

	// Sign transaction
	tx, err := gossiper.signTx(tx)
	if err != nil {
		return nil, err
	}

	return tx, nil
}

// From Mastering Bitcoin book
// Returns (validated, orphan)
func (gossiper *Gossiper) VerifyTx(tx *Tx) (bool, bool) {
	gossiper.errLogger.Printf("\nVerifying tx %x", tx.hash())

	// Check neither in or out lists are empty
	if len(tx.Inputs) == 0 || len(tx.Outputs) == 0 {
		return false, false
	}

	// Check size < block size
	if tx.size() >= MaxBlockSize {
		return false, false
	}

	// Each output and total must be in legal money range
	outputSum := 0
	for _, output := range tx.Outputs {
		outputSum += output.Value
		if output.Value <= 0 || output.Value > MaxCoins {
			return false, false
		}
	}

	if outputSum > MaxCoins {
		return false, false
	}

	// Make sure it is not already in the tx pool
	gossiper.txPoolMutex.Lock()
	for _, other := range gossiper.txPool {
		if tx.equals(other) {
			return false, false
		}
	}
	gossiper.txPoolMutex.Unlock()

	// Cut short if coinbase tx
	if tx.isCoinbaseTx() {
		return gossiper.checkSig(tx), false
	}

	// Look for corresponding UTXOs in main branch and tx pool
	inputSum := 0
	var outputs []*TxOutputLocation
	anyMissing := false
	allMissing := true
	for _, input := range tx.Inputs {
		found := false
		output, err := gossiper.getOutput(input)

		// Look in the pool if not found
		if err != nil {
			gossiper.txPoolMutex.Lock()
			for _, other := range gossiper.txPool {
				if input.references(other) {
					found = true
					break
				}
			}
			gossiper.txPoolMutex.Unlock()
		} else {
			found = true

			// Check that input values are in legal money range BTW
			inputSum += output.Value
			if output.Value <= 0 || output.Value > MaxCoins {
				return false, false
			}
		}

		if found {
			allMissing = false

			// If found, collect output, to check if spent later on (to go only once through the chain)
			location := &TxOutputLocation{
				Output:       output,
				OutputTxHash: input.OutputTxHash,
				OutputIdx:    input.OutputIdx,
			}
			outputs = append(outputs, location)
		} else {
			anyMissing = true
		}
	}

	// If none of the UTXO's is found, reject
	if allMissing {
		return false, false
	}

	// Check if found outputs have been spent already
	if gossiper.alreadySpent(outputs) {
		return false, false
	}

	// If any is not found, it is an orphan
	if anyMissing {
		return false, true
	}

	// Check that sum of input > sum of outputs
	if inputSum <= outputSum {
		return false, false
	}

	// Check sig against each output
	return gossiper.checkSig(tx), false
}

// Return an error if an input didn't correspond to any known output in the main branch
func (gossiper *Gossiper) computeTxFee(tx *Tx) (int, error) {
	// Look for input values
	inputsCash := 0
	for _, input := range tx.Inputs {
		corrTxOutput, outputErr := gossiper.getOutput(input)
		if outputErr != nil {
			return 0, outputErr
		}

		inputsCash += corrTxOutput.Value
	}

	// Look for output values
	outputsCash := 0
	for _, output := range tx.Outputs {
		outputsCash += output.Value
	}

	// Fee is the difference
	fee := inputsCash - outputsCash
	txHash := tx.hash()
	gossiper.errLogger.Printf("Fee for tx (hash = %x): %d\n", txHash[:], fee)
	return fee, nil
}

func (gossiper *Gossiper) signTx(tx *Tx) (*Tx, error) {
	signable := tx.getSignable()
	r, s, err := ecdsa.Sign(rand.Reader, gossiper.privateKey, signable[:])
	if err != nil {
		return nil, err
	}

	tx.Sig = &Sig{R: r, S: s}
	return tx, nil
}

func (gossiper *Gossiper) checkSig(tx *Tx) bool {
	// Check tx signature against each UTXO
	address := PublicKeyToAddress(tx.PublicKey)
	signable := tx.getSignable()

	for _, input := range tx.Inputs {
		output, err := gossiper.getOutput(input)
		if err != nil {
			return false
		}

		// Check receiver address
		if !(output.To == address) {
			return false
		}

		// Check signature
		ecdsaPublicKey := &ecdsa.PublicKey{
			X:     tx.PublicKey.X,
			Y:     tx.PublicKey.Y,
			Curve: elliptic.P256(),
		}
		if !ecdsa.Verify(ecdsaPublicKey, signable[:], tx.Sig.R, tx.Sig.S) {
			return false
		}
	}

	return true
}

func (gossiper *Gossiper) FilterSpentOutputs(outputs []*TxOutputLocation) []*TxOutputLocation {
	var toRemove []int

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
				for idx, outputLocation := range outputs {
					if input.sameOutputLocation(outputLocation) {
						toRemove = append(toRemove, idx)
					}
				}
			}
		}

		gossiper.blocksMutex.Lock()
		currentBlock, blockExists = gossiper.blocks[currentBlock.PrevHash]
		gossiper.blocksMutex.Unlock()
	}

	// Filter spent outputs
	var unspent []*TxOutputLocation
	for i, output := range outputs {
		include := true
		for _, j := range toRemove {
			if i == j {
				include = false
				break
			}
		}

		if include {
			unspent = append(unspent, output)
		}
	}

	return unspent
}

func (gossiper *Gossiper) CollectOutputs() []*TxOutputLocation {
	address := PublicKeyToAddress(gossiper.publicKey)
	var outputs []*TxOutputLocation

	// Get first hash
	gossiper.topBlockMutex.Lock()
	topBlockHash := gossiper.topBlock
	gossiper.topBlockMutex.Unlock()

	// Get block
	gossiper.blocksMutex.Lock()
	currentBlock, blockExists := gossiper.blocks[topBlockHash]
	gossiper.blocksMutex.Unlock()

	for blockExists {
		for _, tx := range currentBlock.Txs {
			for idx, output := range tx.Outputs {
				if output.To == address {
					location := &TxOutputLocation{
						Output:       output,
						OutputTxHash: tx.hash(),
						OutputIdx:    idx,
					}
					outputs = append(outputs, location)
				}
			}
		}

		gossiper.blocksMutex.Lock()
		currentBlock, blockExists = gossiper.blocks[currentBlock.PrevHash]
		gossiper.blocksMutex.Unlock()
	}

	return outputs
}

func (gossiper *Gossiper) updateOrphansTx(tx *Tx) {
	gossiper.errLogger.Println("Looking for orphan tx to bring up...")

	// Get a copy of the orphans for validation
	gossiper.orphanTxPoolMutex.Lock()
	orphans := make([]*Tx, len(gossiper.orphanTxPool))
	copy(orphans, gossiper.orphanTxPool)
	gossiper.orphanTxPoolMutex.Unlock()

	for _, orphan := range orphans {
		for _, input := range orphan.Inputs {
			// If orphan references tx, try to validate it
			if input.references(tx) {
				valid, _ := gossiper.VerifyTx(orphan)

				if valid {
					gossiper.errLogger.Printf("\nTx not orphan anymore: %x", orphan.hash())

					// Remove from orphans
					gossiper.orphanTxPoolMutex.Lock()
					for i, rem := range gossiper.orphanTxPool {
						if rem == orphan {
							gossiper.orphanTxPool = append(gossiper.orphanTxPool[:i], gossiper.orphanTxPool[i+1:]...)
						}
					}
					gossiper.orphanTxPoolMutex.Unlock()

					// Add to transaction pool
					gossiper.addToPool(orphan)

					// Broadcast tx
					gossiper.broadcastTx(orphan)
				}
			}
		}
	}
}

func (tx *Tx) isCoinbaseTx() bool {
	return len(tx.Inputs) == 1 && bytes.Equal(tx.Inputs[0].OutputTxHash[:], NilHash[:]) && tx.Inputs[0].OutputIdx == -1
}

func (gossiper *Gossiper) addToPool(tx *Tx) {
	gossiper.errLogger.Printf("\nAdding tx to txPool: %x", tx.hash())
	gossiper.txPoolMutex.Lock()
	gossiper.txPool = append(gossiper.txPool, tx)
	gossiper.txPoolMutex.Unlock()
}

func (gossiper *Gossiper) addToOrphanPool(tx *Tx) {
	gossiper.errLogger.Printf("\nAdding tx to orphanTxPool: %x", tx.hash())
	gossiper.orphanTxPoolMutex.Lock()
	gossiper.orphanTxPool = append(gossiper.orphanTxPool, tx)
	gossiper.orphanTxPoolMutex.Unlock()
}
