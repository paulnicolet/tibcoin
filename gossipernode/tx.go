package gossipernode

import (
	"bytes"
	"crypto/ecdsa"
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

type Transaction struct {
	inputs  []*TxInput
	outputs []*TxOutput
	// Transaction signature
	sig Sig
	// Public key needed to check signature against UTXOs
	publicKey ecdsa.PublicKey
}

type TxInput struct {
	outputTxHash [32]byte
	outputIdx    int
}

type TxOutput struct {
	value int
	to    string
}

type TxOutputLocation struct {
	output       *TxOutput
	outputTxHash [32]byte
	outputIdx    int
}

func (gossiper *Gossiper) NewTransaction(to string, value int) (*Transaction, error) {
	// Create new transaction
	tx := &Transaction{
		publicKey: gossiper.privateKey.PublicKey,
	}

	// Get outputs
	outputs := gossiper.CollectOutputs()
	unspent := gossiper.FilterSpentOutputs(outputs)

	// Add inputs
	sum := 0
	fees := int(math.Ceil(FeeRatio * float64(value)))
	for _, output := range unspent {
		input := &TxInput{
			outputTxHash: output.outputTxHash,
			outputIdx:    output.outputIdx,
		}

		tx.inputs = append(tx.inputs, input)

		sum += output.output.value
		if sum >= value+fees {
			break
		}
	}

	if sum < value+fees {
		return nil, errors.New("Not enough UTXO to create new transaction")
	}

	// Add main output
	mainOutput := &TxOutput{
		to:    to,
		value: value,
	}
	tx.outputs = append(tx.outputs, mainOutput)

	// Add change
	change := sum - (value + fees)
	changeOutput := &TxOutput{
		to:    PublicKeyToAddress(gossiper.privateKey.PublicKey),
		value: change,
	}
	tx.outputs = append(tx.outputs, changeOutput)

	// Sign transaction
	tx, err := gossiper.signTx(tx)
	if err != nil {
		return nil, err
	}

	return tx, nil
}

func (gossiper *Gossiper) VerifyTransaction(tx *Transaction) bool {
	// TODO orphan transactions ?
	// Simplified version of https://en.bitcoin.it/wiki/Protocol_rules#.22tx.22_messages
	// Check neither in or out lists are empty
	if len(tx.inputs) == 0 || len(tx.outputs) == 0 {
		return false
	}

	// Check size < block size
	if tx.size() >= MaxBlockSize {
		return false
	}

	// Each output must be in legal money range
	outputSum := 0
	for _, output := range tx.outputs {
		outputSum += output.value
		if output.value <= 0 || output.value > MaxCoins {
			return false
		}
	}

	// Reject if tx already present in the pool
	// Check that UTXO of each input are not already used by tx in the pool
	gossiper.txPoolMutex.Lock()
	for _, other := range gossiper.txPool {
		if tx.equals(other) {
			gossiper.txPoolMutex.Unlock()
			return false
		}

		for _, otherInput := range other.inputs {
			for _, input := range tx.inputs {
				if bytes.Equal(input.outputTxHash[:], otherInput.outputTxHash[:]) && input.outputIdx == otherInput.outputIdx {
					gossiper.txPoolMutex.Unlock()
					return false
				}
			}
		}
	}
	gossiper.txPoolMutex.Unlock()

	// Look for UTXOs in the main chain or tx pool
	inputSum := 0
	var outputs []*TxOutputLocation
	for _, input := range tx.inputs {
		found := false
		output, err := gossiper.getOutput(input)

		// Check that input values are in legal money range BTW
		inputSum += output.value
		if output.value <= 0 || output.value > MaxCoins {
			return false
		}

		if err != nil {
			// Look in the pool if not found
			gossiper.txPoolMutex.Lock()
			for _, other := range gossiper.txPool {
				otherTxHash := other.hash()
				if bytes.Equal(input.outputTxHash[:], otherTxHash[:]) && input.outputIdx < len(other.outputs) {
					found = true
					break
				}
			}
			gossiper.txPoolMutex.Unlock()

			if !found {
				return false
			}
		} else {
			// If found, collect output, to check if spent later on (to go only once through the chain)
			location := &TxOutputLocation{
				output:       output,
				outputTxHash: input.outputTxHash,
				outputIdx:    input.outputIdx,
			}
			outputs = append(outputs, location)
		}
	}

	// Check if outputs have been spent already
	if gossiper.alreadySpent(outputs) {
		return false
	}

	// Check that sum of input > sum of outputs
	if inputSum <= outputSum {
		return false
	}

	// Check that tx fee is enough to get into an empty block (WHAT ?)

	// Check sig against each output
	return gossiper.checkSig(tx)
}

// Return an error if an input didn't correspond to any known output in the main branch
func (gossiper *Gossiper) computeTxFee(tx *Transaction) (int, error) {
	// Look for input values
	inputsCash := 0
	for _, input := range tx.inputs {
		corrTxOutput, outputErr := gossiper.getOutput(input)
		if outputErr != nil {
			return 0, outputErr
		}

		inputsCash += corrTxOutput.value
	}

	// Look for output values
	outputsCash := 0
	for _, output := range tx.outputs {
		inputsCash += output.value
	}

	// Fee is the difference
	return inputsCash - outputsCash, nil
}

func (gossiper *Gossiper) signTx(tx *Transaction) (*Transaction, error) {
	signable := tx.getSignable()
	r, s, err := ecdsa.Sign(rand.Reader, gossiper.privateKey, signable[:])
	if err != nil {
		return nil, err
	}

	tx.sig = Sig{R: r, S: s}
	return tx, nil
}

func (gossiper *Gossiper) checkSig(tx *Transaction) bool {
	// Check tx signature against each UTXO
	address := PublicKeyToAddress(tx.publicKey)
	signable := tx.getSignable()

	for _, input := range tx.inputs {
		output, err := gossiper.getOutput(input)
		if err != nil {
			return false
		}

		// Check receiver address
		if !(output.to == address) {
			return false
		}

		// Check signature
		if !ecdsa.Verify(&tx.publicKey, signable[:], tx.sig.R, tx.sig.S) {
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
			for _, input := range tx.inputs {
				for idx, outputLocation := range outputs {
					if bytes.Equal(outputLocation.outputTxHash[:], input.outputTxHash[:]) && outputLocation.outputIdx == input.outputIdx {
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
		for _, j := range toRemove {
			if i != j {
				unspent = append(unspent, output)
			}
		}
	}

	return unspent
}

func (gossiper *Gossiper) CollectOutputs() []*TxOutputLocation {
	address := PublicKeyToAddress(gossiper.privateKey.PublicKey)
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
			for idx, output := range tx.outputs {
				if output.to == address {
					location := &TxOutputLocation{
						output:       output,
						outputTxHash: tx.hash(),
						outputIdx:    idx,
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
