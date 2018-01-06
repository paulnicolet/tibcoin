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

const (
	ValidTx = iota
	InvalidTx
	OrphanTx
	DuplicateTx
)

// From Mastering Bitcoin book
// Returns (validated, orphan, already in pool)
func (gossiper *Gossiper) VerifyTx(tx *Tx) int {
	gossiper.errLogger.Printf("Verifying tx %x", tx.hash())

	if !tx.checkInOutListsNotEmpty() {
		return InvalidTx
	}

	gossiper.errLogger.Println(1)

	// Check size < block size
	if !tx.checkSize() {
		return InvalidTx
	}

	gossiper.errLogger.Println(2)

	// Each output and total must be in legal money range
	if !tx.checkOutputMoneyRange() {
		return InvalidTx
	}

	gossiper.errLogger.Println(3)

	if !tx.checkNotCoinbaseTx() {
		return InvalidTx
	}

	gossiper.errLogger.Println(4)

	// Make sure it is not already in the tx pool or main branch
	if !tx.checkNotExistsInPoolOrMainBranch(gossiper, true) {
		return DuplicateTx
	}

	gossiper.errLogger.Println(5)

	if !tx.checkOutputsNotRefInPool(gossiper) {
		return InvalidTx
	}

	gossiper.errLogger.Println(6)

	// Get all the referenced outputs
	outputs, anyMissing := tx.getOutputs(gossiper, true)

	// If any missing, it is an orphan
	if len(outputs) != 0 && anyMissing {
		return OrphanTx
	}

	gossiper.errLogger.Println(7)

	// If all missing, reject
	if len(outputs) == 0 {
		return InvalidTx
	}

	gossiper.errLogger.Println(8)

	// If already spent, reject
	if !gossiper.checkNotSpentOutputs(outputs, true) {
		return InvalidTx
	}

	gossiper.errLogger.Println(9)

	// Check input sum
	if !tx.checkInputsMoneyRange(outputs) {
		return InvalidTx
	}

	gossiper.errLogger.Println(10)

	// Check inputs less than outputs
	if !tx.checkInputLessThanOutputs(outputs) {
		return InvalidTx
	}

	gossiper.errLogger.Println(11)

	// Check sig against each output
	if !gossiper.checkSig(tx) {
		return InvalidTx
	}

	return ValidTx
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

	if !tx.isCoinbaseTx() {
		for _, input := range tx.Inputs {
			output, err := gossiper.getOutput(input)
			if err != nil {
				return false
			}

			// Check receiver address
			if !(output.To == address) {
				return false
			}
		}
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
				validationResult := gossiper.VerifyTx(orphan)

				if validationResult == ValidTx {
					gossiper.errLogger.Printf("Tx not orphan anymore: %x", orphan.hash())

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

func (gossiper *Gossiper) addToPool(tx *Tx) {
	gossiper.errLogger.Printf("Adding tx to txPool: %x", tx.hash())
	gossiper.txPoolMutex.Lock()
	gossiper.txPool = append(gossiper.txPool, tx)
	gossiper.txPoolMutex.Unlock()
}

func (gossiper *Gossiper) addToOrphanPool(tx *Tx) {
	gossiper.errLogger.Printf("Adding tx to orphanTxPool: %x", tx.hash())
	gossiper.orphanTxPoolMutex.Lock()
	gossiper.orphanTxPool = append(gossiper.orphanTxPool, tx)
	gossiper.orphanTxPoolMutex.Unlock()
}

func (tx *Tx) isCoinbaseTx() bool {
	return len(tx.Inputs) == 1 && bytes.Equal(tx.Inputs[0].OutputTxHash[:], NilHash[:]) && tx.Inputs[0].OutputIdx == -1
}
