package gossipernode

func (tx *Transaction) getSignable() []byte {
	// Return signable version of transaction
	// This should exclude tx signature and hash the result
	return nil
}

func (tx *Transaction) equals(other *Transaction) bool {
	return false
}

func (in *TxInput) getOutput() (*TxOutput, error) {
	return nil, nil
}
