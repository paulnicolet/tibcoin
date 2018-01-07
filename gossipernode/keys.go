package gossipernode

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/btcsuite/btcutil/base58"
	"golang.org/x/crypto/ripemd160"
)

const MaxLengthPrefixAddress = 5

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

func GeneratePrivateAndPublicKeys(addrPrefix string) (*ecdsa.PrivateKey, *PublicKey, error) {
	if len(addrPrefix) > MaxLengthPrefixAddress {
		return nil, nil, errors.New(fmt.Sprintf("The prefix requested is too long, it will take too much time to find an address. Max. size is: %d", MaxLengthPrefixAddress))
	}

	r, _ := regexp.Compile("^[a-zA-Z0-9]+$")

	if len(addrPrefix) > 0 && (!r.MatchString(addrPrefix) || strings.Index(addrPrefix, "0") != -1 || strings.Index(addrPrefix, "O") != -1 || strings.Index(addrPrefix, "I") != -1 || strings.Index(addrPrefix, "l") != -1) {
		return nil, nil, errors.New(fmt.Sprintf("The prefix should contain only alpha-numeric characters and cannot contain any '0', 'O', 'I' nor 'l'; prefix = %s", addrPrefix))
	}

	fmt.Println("Looking for an address...")

	// TODO: Add a timer to stop after some time?
	for true {
		privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, nil, err
		}

		publicKey := &PublicKey{
			X: privateKey.PublicKey.X,
			Y: privateKey.PublicKey.Y,
		}

		address := PublicKeyToAddress(publicKey)
		if strings.Index(address, "1" + addrPrefix) == 0 {
			fmt.Printf("Address found: %s\n", address)
			return privateKey, publicKey, nil
		}
	}

	return nil, nil, errors.New("Exited loop to find address matching prefix; should never happend")
}

// Public keys
func PublicKeyToAddress(public *PublicKey) string {
	key := append(public.X.Bytes(), public.Y.Bytes()...)
	payload := append([]byte{0x04}, key...)

	ripemHash := ripemd160.New()
	sha := sha256.Sum256(payload)
	ripemHash.Write(sha[:])

	address := base58.CheckEncode(ripemHash.Sum(nil), 0x00)
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
