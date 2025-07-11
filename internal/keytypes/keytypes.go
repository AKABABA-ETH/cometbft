package keytypes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cometbft/cometbft/v2/crypto"
	"github.com/cometbft/cometbft/v2/crypto/bls12381"
	"github.com/cometbft/cometbft/v2/crypto/ed25519"
	"github.com/cometbft/cometbft/v2/crypto/secp256k1"
	"github.com/cometbft/cometbft/v2/crypto/secp256k1eth"
)

var keyTypes map[string]func() (crypto.PrivKey, error)

func init() {
	keyTypes = map[string]func() (crypto.PrivKey, error){
		ed25519.KeyType: func() (crypto.PrivKey, error) { //nolint: unparam
			return ed25519.GenPrivKey(), nil
		},
		secp256k1.KeyType: func() (crypto.PrivKey, error) { //nolint: unparam
			return secp256k1.GenPrivKey(), nil
		},
	}

	if secp256k1eth.Enabled {
		keyTypes[secp256k1eth.KeyType] = func() (crypto.PrivKey, error) { //nolint: unparam
			return secp256k1eth.GenPrivKey(), nil
		}
	}

	if bls12381.Enabled {
		keyTypes[bls12381.KeyType] = func() (crypto.PrivKey, error) {
			pk, err := bls12381.GenPrivKey()
			if err != nil {
				return nil, fmt.Errorf("failed to generate BLS key: %w", err)
			}
			return pk, nil
		}
	}
}

// GenPrivKey generates a private key of the given type.
func GenPrivKey(keyType string) (crypto.PrivKey, error) {
	genF, ok := keyTypes[keyType]
	if !ok {
		return nil, fmt.Errorf("unsupported key type: %q", keyType)
	}
	return genF()
}

// SupportedKeyTypesStr returns a string of supported key types.
func SupportedKeyTypesStr() string {
	keyTypesSlice := make([]string, 0, len(keyTypes))

	for k := range keyTypes {
		keyTypesSlice = append(keyTypesSlice, fmt.Sprintf("%q", k))
	}
	sort.Slice(keyTypesSlice, func(i, j int) bool {
		return keyTypesSlice[i] < keyTypesSlice[j]
	})
	return strings.Join(keyTypesSlice, ", ")
}

// ListSupportedKeyTypes returns a list of supported key types.
func ListSupportedKeyTypes() []string {
	keyTypesSlice := make([]string, 0, len(keyTypes))
	for k := range keyTypes {
		keyTypesSlice = append(keyTypesSlice, k)
	}
	sort.Slice(keyTypesSlice, func(i, j int) bool {
		return keyTypesSlice[i] < keyTypesSlice[j]
	})
	return keyTypesSlice
}

// IsSupported returns true if the key type is supported.
func IsSupported(keyType string) bool {
	_, ok := keyTypes[keyType]
	return ok
}
