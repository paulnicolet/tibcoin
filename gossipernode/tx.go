package gossipernode

import (
	"crypto/ecdsa"
	"crypto/rand"

	"github.com/paulnicolet/tibcoin/common"
)

type Transaction struct {
	inputs  []TxInput
	outputs []TxOutput
	sig     common.Sig
}

type TxInput struct {
	outputTxHash []byte
	outputIdx    uint
}

type TxOutput struct {
	value uint
	to    common.Address
}

func (gossiper *Gossiper) NewTransaction(to common.Address, value uint) (*Transaction, error) {
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
	// Look for UTXOs in the chain
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

	tx.sig = common.Sig{R: r, S: s}
	return tx, nil
}

func (gossiper *Gossiper) checkSig(tx *Transaction) bool {
	// Check tx signature against each UTXO
	return false
}
