package gossipernode

import (
	"bytes"
	"crypto/sha256"
	"strconv"

	"github.com/paulnicolet/tibcoin/common"
)

func (tx *Transaction) hash() [32]byte {
	h := sha256.New()

	// Hash inputs
	for _, input := range tx.inputs {
		h.Write(input.hash())
	}

	// Hash outputs
	for _, output := range tx.outputs {
		h.Write(output.hash())
	}

	// Hash signature
	h.Write()
}

func (tx *Transaction) getSignable() []byte {
	h := sha256.New()

	// Hash inputs
	for _, input := range tx.inputs {
		h.Write(input.hash())
	}

	// Hash outputs
	for _, output := range tx.outputs {
		h.Write(output.hash())
	}

	return h.Sum(nil)
}

func (tx *Transaction) equals(other *Transaction) bool {
	if !tx.sig.Equal(other.sig) {
		return false
	}

	if !common.PublicKeyEqual(tx.publicKey, other.publicKey) {
		return false
	}

	for i, input := range tx.inputs {
		if !input.equals(&other.inputs[i]) {
			return false
		}
	}

	for i, output := range tx.outputs {
		if !output.equals(&other.outputs[i]) {
			return false
		}
	}

	return true
}

// Transaction inputs internals
func (in *TxInput) hash() []byte {
	h := sha256.New()
	h.Write(in.outputTxHash)
	h.Write([]byte(strconv.Itoa(int(in.outputIdx))))
	return h.Sum(nil)
}

func (in *TxInput) equals(other *TxInput) bool {
	return bytes.Equal(in.outputTxHash, other.outputTxHash) && in.outputIdx == other.outputIdx
}

// We are looking only in the main branch for a correpsonding transaction
func (gossiper *Gossiper) getOutput(in *TxInput) (*TxOutput, error) {
	currentBlock := gossiper.topBlock
	blockExists := true
	for blockExists {
		// Check if block contains transaction we are looking for
		for _, tx := currentBlock.Txs {
			if tx.hash()
		}

		tx, foundTx := currentBlock.Txs[in.outputTxHash]
		if foundTx {

			return (tx, nil)
		}

		// Get the previous block
		currentBlock, blockExists = gossiper.blocks[currentBlock.PrevHash]
	}

	return nil, nil
}

// Transaction outputs internals

func (out *TxOutput) hash() []byte {
	h := sha256.New()
	h.Write(out.to.PubKeyHash[:])
	h.Write([]byte(strconv.Itoa(int(out.value))))
	return h.Sum(nil)
}

func (out *TxOutput) equals(other *TxOutput) bool {
	return bytes.Equal(out.to.PubKeyHash[:], other.to.PubKeyHash[:]) && out.value == other.value
}
