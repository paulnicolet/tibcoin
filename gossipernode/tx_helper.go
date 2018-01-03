package gossipernode

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
)

func (tx *Transaction) hash() [32]byte {
	h := sha256.New()

	// Hash inputs
	for _, input := range tx.inputs {
		inputHash := input.hash()
		h.Write(inputHash[:])
	}

	// Hash outputs
	for _, output := range tx.outputs {
		outputHash := output.hash()
		h.Write(outputHash[:])
	}

	// Hash signature
	h.Write(tx.sig.R.Bytes())
	h.Write(tx.sig.S.Bytes())

	// Hash Public Key
	h.Write(tx.publicKey.X.Bytes())
	h.Write(tx.publicKey.Y.Bytes())

	return BytesToHash(h.Sum(nil))
}

func (tx *Transaction) getSignable() [32]byte {
	h := sha256.New()

	// Hash inputs
	for _, input := range tx.inputs {
		inputHash := input.hash()
		h.Write(inputHash[:])
	}

	// Hash outputs
	for _, output := range tx.outputs {
		outputHash := output.hash()
		h.Write(outputHash[:])
	}

	return BytesToHash(h.Sum(nil))
}

func (tx *Transaction) equals(other *Transaction) bool {
	if !tx.sig.Equal(other.sig) {
		return false
	}

	if !PublicKeyEqual(tx.publicKey, other.publicKey) {
		return false
	}

	for i, input := range tx.inputs {
		if !input.equals(other.inputs[i]) {
			return false
		}
	}

	for i, output := range tx.outputs {
		if !output.equals(other.outputs[i]) {
			return false
		}
	}

	return true
}

// Transaction inputs internals
func (in *TxInput) hash() [32]byte {
	h := sha256.New()
	h.Write(in.outputTxHash[:])
	h.Write([]byte(strconv.Itoa(int(in.outputIdx))))
	return BytesToHash(h.Sum(nil))
}

func (in *TxInput) equals(other *TxInput) bool {
	return bytes.Equal(in.outputTxHash[:], other.outputTxHash[:]) && in.outputIdx == other.outputIdx
}

// We are looking only in the main branch for a correpsonding transaction
func (gossiper *Gossiper) getOutput(in *TxInput) (*TxOutput, error) {
	currentBlock, blockExists := gossiper.blocks[gossiper.topBlock]

	if !blockExists {
		return nil, errors.New(fmt.Sprintf("Top block (hash = %x) not found in 'gossiper.blocks'.", gossiper.topBlock[:]))
	}

	for blockExists {
		// Check if block contains transaction we are looking for
		for _, tx := range currentBlock.Txs {
			txHash := tx.hash()
			if bytes.Equal(txHash[:], in.outputTxHash[:]) {
				if int(in.outputIdx) < len(tx.outputs) {
					return tx.outputs[in.outputIdx], nil
				} else {
					return nil, errors.New(fmt.Sprintf("Transaction found (hash = %x) but not enough output: expected at least %d, got %d.", txHash[:], in.outputIdx + 1, len(tx.outputs)))
				}
			}
		}

		// Get the previous block
		currentBlock, blockExists = gossiper.blocks[currentBlock.PrevHash]
	}

	return nil, errors.New(fmt.Sprintf("Transaction not found in main branch (hash = %x).", in.outputTxHash[:]))
}

// Transaction outputs internals

func (out *TxOutput) hash() [32]byte {
	h := sha256.New()
	h.Write([]byte(out.to))
	h.Write([]byte(strconv.Itoa(int(out.value))))
	return BytesToHash(h.Sum(nil))
}

func (out *TxOutput) equals(other *TxOutput) bool {
	return (out.to == other.to) && out.value == other.value
}
