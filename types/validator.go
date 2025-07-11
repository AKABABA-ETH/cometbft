package types

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

	cmtproto "github.com/cometbft/cometbft/api/cometbft/types/v2"
	"github.com/cometbft/cometbft/v2/crypto"
	ce "github.com/cometbft/cometbft/v2/crypto/encoding"
	"github.com/cometbft/cometbft/v2/internal/keytypes"
	cmtrand "github.com/cometbft/cometbft/v2/internal/rand"
)

// ErrUnsupportedPubKeyType is returned when a public key type is not supported.
type ErrUnsupportedPubKeyType struct {
	KeyType string
}

func (e ErrUnsupportedPubKeyType) Error() string {
	return fmt.Sprintf(
		"unsupported pubkey type %q, must be one of: %s",
		e.KeyType,
		keytypes.SupportedKeyTypesStr(),
	)
}

// Volatile state for each Validator
// NOTE: The ProposerPriority is not included in Validator.Hash();
// make sure to update that method if changes are made here.
type Validator struct {
	Address     Address       `json:"address"`
	PubKey      crypto.PubKey `json:"pub_key"`
	VotingPower int64         `json:"voting_power"`

	ProposerPriority int64 `json:"proposer_priority"`
}

// NewValidator returns a new validator with the given pubkey and voting power.
func NewValidator(pubKey crypto.PubKey, votingPower int64) *Validator {
	return &Validator{
		Address:          pubKey.Address(),
		PubKey:           pubKey,
		VotingPower:      votingPower,
		ProposerPriority: 0,
	}
}

// ValidateBasic performs basic validation.
func (v *Validator) ValidateBasic() error {
	if v == nil {
		return errors.New("nil validator")
	}
	if v.PubKey == nil {
		return errors.New("validator does not have a public key")
	}

	if v.VotingPower < 0 {
		return errors.New("validator has negative voting power")
	}

	addr := v.PubKey.Address()
	if !bytes.Equal(v.Address, addr) {
		return fmt.Errorf("validator address is incorrectly derived from pubkey. Exp: %v, got %v", addr, v.Address)
	}

	keyType := v.PubKey.Type()
	if !keytypes.IsSupported(keyType) {
		return ErrUnsupportedPubKeyType{KeyType: keyType}
	}

	return nil
}

// Copy creates a new copy of the validator so we can mutate ProposerPriority.
// Panics if the validator is nil.
func (v *Validator) Copy() *Validator {
	vCopy := *v
	return &vCopy
}

// CompareProposerPriority returns the one with higher ProposerPriority.
func (v *Validator) CompareProposerPriority(other *Validator) *Validator {
	if v == nil {
		return other
	}
	switch {
	case v.ProposerPriority > other.ProposerPriority:
		return v
	case v.ProposerPriority < other.ProposerPriority:
		return other
	default:
		result := bytes.Compare(v.Address, other.Address)
		switch {
		case result < 0:
			return v
		case result > 0:
			return other
		default:
			panic("Cannot compare identical validators")
		}
	}
}

// String returns a string representation of String.
//
// 1. address
// 2. public key
// 3. voting power
// 4. proposer priority.
func (v *Validator) String() string {
	if v == nil {
		return "nil-Validator"
	}
	return fmt.Sprintf("Validator{%v %v VP:%v A:%v}",
		v.Address,
		v.PubKey,
		v.VotingPower,
		v.ProposerPriority)
}

// ValidatorListString returns a prettified validator list for logging purposes.
func ValidatorListString(vals []*Validator) string {
	var sb strings.Builder
	for i, val := range vals {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(val.Address.String())
		sb.WriteString(":")
		sb.WriteString(strconv.FormatInt(val.VotingPower, 10))
	}
	return sb.String()
}

// Bytes computes the unique encoding of a validator with a given voting power.
// These are the bytes that gets hashed in consensus. It excludes address
// as its redundant with the pubkey. This also excludes ProposerPriority
// which changes every round.
func (v *Validator) Bytes() []byte {
	pk, err := ce.PubKeyToProto(v.PubKey)
	if err != nil {
		panic(err)
	}

	pbv := cmtproto.SimpleValidator{
		PubKey:      &pk,
		VotingPower: v.VotingPower,
	}

	bz, err := pbv.Marshal()
	if err != nil {
		panic(err)
	}
	return bz
}

// ToProto converts Validator to protobuf.
func (v *Validator) ToProto() (*cmtproto.Validator, error) {
	if v == nil {
		return nil, errors.New("nil validator")
	}

	if v.PubKey == nil {
		return nil, errors.New("nil pubkey")
	}

	vp := cmtproto.Validator{
		Address:          v.Address,
		PubKeyType:       v.PubKey.Type(),
		PubKeyBytes:      v.PubKey.Bytes(),
		VotingPower:      v.VotingPower,
		ProposerPriority: v.ProposerPriority,
	}

	return &vp, nil
}

// ValidatorFromProto sets a protobuf Validator to the given pointer.
// It returns an error if the public key is invalid.
func ValidatorFromProto(vp *cmtproto.Validator) (*Validator, error) {
	if vp == nil {
		return nil, errors.New("nil validator")
	}

	pk, err := ce.PubKeyFromTypeAndBytes(vp.PubKeyType, vp.PubKeyBytes)
	if err != nil {
		pk, err = ce.PubKeyFromProto(*vp.PubKey)
		if err != nil {
			return nil, err
		}
	}
	v := new(Validator)
	v.Address = vp.GetAddress()
	v.PubKey = pk
	v.VotingPower = vp.GetVotingPower()
	v.ProposerPriority = vp.GetProposerPriority()

	return v, nil
}

// ----------------------------------------
// RandValidator

// RandValidator returns a randomized validator, useful for testing.
// UNSTABLE.
func RandValidator(randPower bool, minPower int64) (*Validator, PrivValidator) {
	privVal := NewMockPV()
	votePower := minPower
	if randPower {
		votePower += int64(cmtrand.Uint32())
	}
	pubKey, err := privVal.GetPubKey()
	if err != nil {
		panic(fmt.Errorf("could not retrieve pubkey %w", err))
	}
	val := NewValidator(pubKey, votePower)
	return val, privVal
}
