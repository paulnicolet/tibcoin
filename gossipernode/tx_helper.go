package gossipernode

import (
	"bytes"
	"crypto/sha256"
	"strconv"
)

func (tx *Transaction) hash() []byte {
	hash := sha256.New()
	return hash.Sum(nil)
}

func (tx *Transaction) getSignable() []byte {
	h := sha256.New()

	// Hash inputs
	for _, input := range tx.inputs {
		h.Write(input.hash())
	}

	// Hash ouputs
	for _, output := range tx.outputs {
		h.Write(output.hash())
	}

	return h.Sum(nil)
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
func (in *TxInput) hash() []byte {
	h := sha256.New()
	h.Write(in.outputTxHash)
	h.Write([]byte(strconv.Itoa(int(in.outputIdx))))
	return h.Sum(nil)
}

func (in *TxInput) equals(other *TxInput) bool {
	return bytes.Equal(in.outputTxHash, other.outputTxHash) && in.outputIdx == other.outputIdx
}

func (gossiper *Gossiper) getOutput(in *TxInput) (*TxOutput, error) {
	return nil, nil
}

// Transaction outputs internals

func (out *TxOutput) hash() []byte {
	h := sha256.New()
	h.Write([]byte(out.to))
	h.Write([]byte(strconv.Itoa(int(out.value))))
	return h.Sum(nil)
}

func (out *TxOutput) equals(other *TxOutput) bool {
	return (out.to == other.to) && out.value == other.value
}
