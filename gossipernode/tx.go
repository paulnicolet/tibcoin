package gossipernode

import (
	"crypto/ecdsa"
	"crypto/rand"
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

type Transaction struct {
	inputs  []TxInput
	outputs []TxOutput
	// Transaction signature
	sig Sig
	// Public key needed to check signature against UTXOs
	publicKey ecdsa.PublicKey
}

type TxInput struct {
	outputTxHash []byte
	outputIdx    uint
}

type TxOutput struct {
	value uint
	to    string
}

func (gossiper *Gossiper) NewTransaction(to string, value uint) (*Transaction, error) {
	// Scan blockchain to gather enough UTXO to pay for "value" coins
	// Gather UTXOs in inputs

	// Create output for "to"
	// Create output for itself if change needed
	// Add transaction fees

	// Sign transaction

	return nil, nil
}

func (gossiper *Gossiper) VerifyTransaction(tx *Transaction) bool {
	// Simplified version of https://en.bitcoin.it/wiki/Protocol_rules#.22tx.22_messages
	// Check neither in or out lists are empty
	// Check size < block size
	// Each output must be in legal money range
	// Reject if tx already present in the pool
	// Check that UTXO of each input are not already used by tx in the pool
	// Look for UTXOs in the main chain, or if in fork or orphan block, put in orphan txs
	// Check that input values are in legal money range
	// Check that sum of input > sum of outputs
	// Check that tx fee is enough to get into an empty block
	// Check sig against each output
	return false
}

func (gossiper *Gossiper) signTx(tx *Transaction) (*Transaction, error) {
	r, s, err := ecdsa.Sign(rand.Reader, gossiper.privateKey, tx.getSignable())
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
		output, err := input.getOutput()
		if err != nil {
			return false
		}

		// Check receiver address
		if !(output.to == address) {
			return false
		}

		// Check signature
		if !ecdsa.Verify(&tx.publicKey, signable, tx.sig.R, tx.sig.S) {
			return false
		}
	}

	return true
}
