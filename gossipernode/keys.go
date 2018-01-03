package gossipernode

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"math/big"

	"github.com/btcsuite/btcutil/base58"
	"golang.org/x/crypto/ripemd160"
)

// Public keys
func PublicKeyToAddress(public ecdsa.PublicKey) string {
	prefix, _ := hex.DecodeString("04")
	key := append(public.X.Bytes(), public.Y.Bytes()...)
	payload := append(prefix, key...)

	ripemHash := ripemd160.New()
	sha := sha256.Sum256(payload)
	ripemHash.Write(sha[:])
	pubKeyHash := append([]byte{0}, ripemHash.Sum(nil)...)

	shaSum1 := sha256.Sum256(pubKeyHash)
	checkSum := sha256.Sum256(shaSum1[:])

	address := base58.CheckEncode(append(pubKeyHash, checkSum[:4]...), byte(1))
	return address
}

func PublicKeyEqual(k1 ecdsa.PublicKey, k2 ecdsa.PublicKey) bool {
	return k1.X.Cmp(k1.X) == 0 && k2.Y.Cmp(k2.Y) == 0
}

// Signatures
type Sig struct {
	R *big.Int
	S *big.Int
}

func (sig Sig) Equal(other Sig) bool {
	return sig.R.Cmp(other.R) == 0 && sig.S.Cmp(other.S) == 0
}
