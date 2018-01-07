package gossipernode

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"

	"github.com/btcsuite/btcutil/base58"
	"golang.org/x/crypto/ripemd160"
)

type PublicKey struct {
	// Curve is P256
	X *big.Int
	Y *big.Int
}

type SerializedPublicKey struct {
	X []byte
	Y []byte
}

type Sig struct {
	R *big.Int
	S *big.Int
}

type SerializedSig struct {
	R []byte
	S []byte
}

// Public keys
func PublicKeyToAddress(public *PublicKey) string {
	prefix, _ := hex.DecodeString("04")
	key := append(public.X.Bytes(), public.Y.Bytes()...)
	payload := append(prefix, key...)

	ripemHash := ripemd160.New()
	sha := sha256.Sum256(payload)
	ripemHash.Write(sha[:])
	pubKeyHash := append([]byte{0}, ripemHash.Sum(nil)...)

	shaSum1 := sha256.Sum256(pubKeyHash)
	checkSum := sha256.Sum256(shaSum1[:])

	address := base58.CheckEncode(append(pubKeyHash, checkSum[:4]...), byte(0))
	return address
}

func PublicKeyEqual(k1 *PublicKey, k2 *PublicKey) bool {
	return k1.X.Cmp(k1.X) == 0 && k2.Y.Cmp(k2.Y) == 0
}

// Signatures
func (sig *Sig) Equal(other *Sig) bool {
	return sig.R.Cmp(other.R) == 0 && sig.S.Cmp(other.S) == 0
}

func BytesToHash(bytes []byte) [32]byte {
	var hash [32]byte
	copy(hash[:], bytes)
	return hash
}
