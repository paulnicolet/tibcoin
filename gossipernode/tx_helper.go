package gossipernode

import (
	"bytes"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
)

func (tx *Tx) hash() [32]byte {
	h := sha256.New()

	// Hash inputs
	for _, input := range tx.Inputs {
		inputHash := input.hash()
		h.Write(inputHash[:])
	}

	// Hash outputs
	for _, output := range tx.Outputs {
		outputHash := output.hash()
		h.Write(outputHash[:])
	}

	// Hash signature
	h.Write(tx.Sig.R.Bytes())
	h.Write(tx.Sig.S.Bytes())

	// Hash Public Key
	h.Write(tx.PublicKey.X.Bytes())
	h.Write(tx.PublicKey.Y.Bytes())

	return BytesToHash(h.Sum(nil))
}

func (tx *Tx) getSignable() [32]byte {
	h := sha256.New()

	// Hash inputs
	for _, input := range tx.Inputs {
		inputHash := input.hash()
		h.Write(inputHash[:])
	}

	// Hash outputs
	for _, output := range tx.Outputs {
		outputHash := output.hash()
		h.Write(outputHash[:])
	}

	return BytesToHash(h.Sum(nil))
}

func (tx *Tx) equals(other *Tx) bool {
	if !tx.Sig.Equal(other.Sig) {
		return false
	}

	if !PublicKeyEqual(tx.PublicKey, other.PublicKey) {
		return false
	}

	for i, input := range tx.Inputs {
		if !input.equals(other.Inputs[i]) {
			return false
		}
	}

	for i, output := range tx.Outputs {
		if !output.equals(other.Outputs[i]) {
			return false
		}
	}

	return true
}

func (tx *Tx) size() int {
	size := 0

	// Inputs
	for _, input := range tx.Inputs {
		size += input.size()
	}

	// Outputs
	for _, output := range tx.Outputs {
		size += output.size()
	}

	// Suppose a big int sizes 8 bytes
	size += (8 * 4)

	return size
}

func (tx *Tx) toSerializable() (*SerializableTx, error) {

	R, _ := tx.Sig.R.MarshalJSON()
	S, _ := tx.Sig.S.MarshalJSON()
	/*
		X, err3 := tx.PublicKey.X.MarshalJSON()
		Y, err4 := tx.PublicKey.Y.MarshalJSON()
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			return nil, errors.New("Could not serialize R, S, X or Y")
		}*/

	serTx := &SerializableTx{
		Inputs:    tx.Inputs,
		Outputs:   tx.Outputs,
		PublicKey: elliptic.Marshal(elliptic.P256(), tx.PublicKey.X, tx.PublicKey.Y), //&SerializedSig{R: R, S: S},
		Sig:       &SerializedSig{R: R, S: S},                                        //&SerializedPublicKey{X: X, Y: Y},
	}

	fmt.Printf("ideoidide %v", tx.PublicKey.X)
	fmt.Printf("OYYOYOYO %v", serTx.PublicKey)

	return serTx, nil
}

func (tx *SerializableTx) toNormal() (*Tx, error) {
	R, S := &big.Int{}, &big.Int{} //, &big.Int{}, &big.Int{}
	R.UnmarshalJSON(tx.Sig.R)
	S.UnmarshalJSON(tx.Sig.S)

	/*
		err3 := X.UnmarshalJSON(tx.PublicKey.X)
		err4 := Y.UnmarshalJSON(tx.PublicKey.Y)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			return nil, errors.New("Could not deserialize R, S, X or Y")
		}*/

	X, Y := elliptic.Unmarshal(elliptic.P256(), tx.PublicKey)
	fmt.Printf("HEHEHEHE %v", tx.PublicKey)
	fmt.Printf("HEHEHEHE %v", X)
	//R, S := elliptic.Unmarshal(elliptic.P256(), tx.Sig)

	fmt.Printf("HEHEHEHE %v", R)

	newTx := &Tx{
		Inputs:    tx.Inputs,
		Outputs:   tx.Outputs,
		Sig:       &Sig{R: R, S: S},
		PublicKey: &PublicKey{X: X, Y: Y},
	}

	return newTx, nil
}

func (tx *Tx) String() string {
	res := fmt.Sprintf("\nTx generated by %s\n", PublicKeyToAddress(tx.PublicKey))

	// Inputs
	res += "- Inputs:\n"
	for _, input := range tx.Inputs {
		res += fmt.Sprintf("\t• Tx: %s, Idx: %d\n", hex.EncodeToString(input.OutputTxHash[:]), input.OutputIdx)
	}

	// Ouputs
	res += "- Outputs:\n"
	for _, output := range tx.Outputs {
		res += fmt.Sprintf("\t• %d tibcoins to %s\n", output.Value, output.To)
	}

	return res
}

// Tx inputs internals
func (in *TxInput) hash() [32]byte {
	h := sha256.New()
	h.Write(in.OutputTxHash[:])
	h.Write([]byte(strconv.Itoa(int(in.OutputIdx))))
	return BytesToHash(h.Sum(nil))
}

func (in *TxInput) equals(other *TxInput) bool {
	return bytes.Equal(in.OutputTxHash[:], other.OutputTxHash[:]) && in.OutputIdx == other.OutputIdx
}

func (in *TxInput) references(tx *Tx) bool {
	txHash := tx.hash()
	return bytes.Equal(txHash[:], in.OutputTxHash[:]) && int(in.OutputIdx) < len(tx.Outputs)
}

func (in *TxInput) sameOutput(other *TxInput) bool {
	return bytes.Equal(in.OutputTxHash[:], other.OutputTxHash[:]) && in.OutputIdx == other.OutputIdx
}

func (in *TxInput) sameOutputLocation(other *TxOutputLocation) bool {
	return bytes.Equal(in.OutputTxHash[:], other.OutputTxHash[:]) && in.OutputIdx == other.OutputIdx
}

// We are looking only in the main branch for a correpsonding transaction
func (gossiper *Gossiper) getOutput(in *TxInput) (*TxOutput, error) {
	// Get top block hash
	gossiper.topBlockMutex.Lock()
	topBlock := gossiper.topBlock
	gossiper.topBlockMutex.Unlock()

	// Get top block
	gossiper.blocksMutex.Lock()
	currentBlock, blockExists := gossiper.blocks[topBlock]
	gossiper.blocksMutex.Unlock()

	if !blockExists {
		return nil, errors.New(fmt.Sprintf("Top block (hash = %x) not found in 'gossiper.blocks'.", topBlock[:]))
	}

	for blockExists {
		// Check if block contains transaction we are looking for
		for _, tx := range currentBlock.Txs {
			txHash := tx.hash()
			if bytes.Equal(txHash[:], in.OutputTxHash[:]) {
				if in.OutputIdx < len(tx.Outputs) {
					return tx.Outputs[in.OutputIdx], nil
				} else {
					return nil, errors.New(fmt.Sprintf("Tx found (hash = %x) but not enough output: expected at least %d, got %d.", txHash[:], in.OutputIdx+1, len(tx.Outputs)))
				}
			}
		}

		// Get the previous block
		gossiper.blocksMutex.Lock()
		currentBlock, blockExists = gossiper.blocks[currentBlock.PrevHash]
		gossiper.blocksMutex.Unlock()
	}

	return nil, errors.New(fmt.Sprintf("Tx not found in main branch (hash = %x).", in.OutputTxHash[:]))
}

func (gossiper *Gossiper) alreadySpent(outputs []*TxOutputLocation) bool {
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
						return true
					}
				}
			}
		}

		gossiper.blocksMutex.Lock()
		currentBlock, blockExists = gossiper.blocks[currentBlock.PrevHash]
		gossiper.blocksMutex.Unlock()
	}

	// Look in the pool
	gossiper.txPoolMutex.Lock()
	for _, tx := range gossiper.txPool {
		for _, input := range tx.Inputs {
			for _, outputLocation := range outputs {
				if input.sameOutputLocation(outputLocation) {
					return true
				}
			}
		}
	}
	gossiper.txPoolMutex.Unlock()

	return false
}

func (in *TxInput) size() int {
	// Hardcode input size
	return 4 + len(in.OutputTxHash)
}

// Tx outputs internals

func (out *TxOutput) hash() [32]byte {
	h := sha256.New()
	h.Write([]byte(out.To))
	h.Write([]byte(strconv.Itoa(int(out.Value))))
	return BytesToHash(h.Sum(nil))
}

func (out *TxOutput) equals(other *TxOutput) bool {
	return (out.To == other.To) && out.Value == other.Value
}

func (out *TxOutput) size() int {
	// Hardcode output size
	return 4 + len([]byte(out.To))
}
