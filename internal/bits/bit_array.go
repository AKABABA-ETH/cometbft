package bits

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"math/rand"
	"regexp"
	"strings"
	"sync"

	cmtprotobits "github.com/cometbft/cometbft/api/cometbft/libs/bits/v1"
	cmtmath "github.com/cometbft/cometbft/v2/libs/math"
)

// BitArray is a thread-safe implementation of a bit array.
type BitArray struct {
	mtx          sync.Mutex
	TrueBitCount int      `json:"true_bits_count"` // Number of bits set to true
	Bits         int      `json:"bits"`            // NOTE: persisted via reflect, must be exported
	Elems        []uint64 `json:"elems"`           // NOTE: persisted via reflect, must be exported
}

// NewBitArray returns a new bit array.
// It returns nil if the number of bits is zero.
func NewBitArray(bits int) *BitArray {
	if bits <= 0 {
		return nil
	}
	return &BitArray{
		Bits:         bits,
		Elems:        make([]uint64, (bits+63)/64),
		TrueBitCount: 0,
	}
}

// NewBitArrayFromFn returns a new bit array.
// It returns nil if the number of bits is zero.
// It initializes the `i`th bit to the value of `fn(i)`.
func NewBitArrayFromFn(bits int, fn func(int) bool) *BitArray {
	if bits <= 0 {
		return nil
	}
	bA := &BitArray{
		Bits:         bits,
		Elems:        make([]uint64, (bits+63)/64),
		TrueBitCount: 0,
	}
	for i := 0; i < bits; i++ {
		v := fn(i)
		if v {
			bA.Elems[i/64] |= (uint64(1) << uint(i%64))
			bA.TrueBitCount++
		}
	}
	return bA
}

// Size returns the number of bits in the bitarray.
func (bA *BitArray) Size() int {
	if bA == nil {
		return 0
	}
	return bA.Bits
}

// GetIndex returns the bit at index i within the bit array.
// The behavior is undefined if i >= bA.Bits.
func (bA *BitArray) GetIndex(i int) bool {
	if bA == nil {
		return false
	}
	bA.mtx.Lock()
	defer bA.mtx.Unlock()
	return bA.getIndex(i)
}

func (bA *BitArray) getIndex(i int) bool {
	if i >= bA.Bits {
		return false
	}
	return bA.Elems[i/64]&(uint64(1)<<uint(i%64)) > 0
}

// SetIndex sets the bit at index i within the bit array.
// The behavior is undefined if i >= bA.Bits.
func (bA *BitArray) SetIndex(i int, v bool) bool {
	if bA == nil {
		return false
	}
	bA.mtx.Lock()
	defer bA.mtx.Unlock()
	return bA.setIndex(i, v)
}

func (bA *BitArray) setIndex(i int, v bool) bool {
	if i >= bA.Bits {
		return false
	}
	// Check current bit value
	oldValue := bA.getIndex(i)

	if v {
		if !oldValue {
			bA.TrueBitCount++
		}
		bA.Elems[i/64] |= (uint64(1) << uint(i%64))
	} else {
		if oldValue {
			bA.TrueBitCount--
		}
		bA.Elems[i/64] &= ^(uint64(1) << uint(i%64))
	}
	return true
}

// Copy returns a copy of the provided bit array.
func (bA *BitArray) Copy() *BitArray {
	if bA == nil {
		return nil
	}
	bA.mtx.Lock()
	defer bA.mtx.Unlock()
	return bA.copy()
}

func (bA *BitArray) copy() *BitArray {
	c := make([]uint64, len(bA.Elems))
	copy(c, bA.Elems)
	return &BitArray{
		Bits:         bA.Bits,
		Elems:        c,
		TrueBitCount: bA.TrueBitCount,
	}
}

func (bA *BitArray) copyBits(bits int) *BitArray {
	c := make([]uint64, (bits+63)/64)
	copy(c, bA.Elems)

	// Calculate true bit count for the new size
	newTrueBitCount := 0
	for i := 0; i < bits; i++ {
		if c[i/64]&(uint64(1)<<uint(i%64)) > 0 {
			newTrueBitCount++
		}
	}
	return &BitArray{
		Bits:         bits,
		Elems:        c,
		TrueBitCount: newTrueBitCount,
	}
}

// Or returns a bit array resulting from a bitwise OR of the two bit arrays.
// If the two bit-arrys have different lengths, Or right-pads the smaller of the two bit-arrays with zeroes.
// Thus the size of the return value is the maximum of the two provided bit arrays.
func (bA *BitArray) Or(o *BitArray) *BitArray {
	if bA == nil && o == nil {
		return nil
	}
	if bA == nil && o != nil {
		return o.Copy()
	}
	if o == nil {
		return bA.Copy()
	}
	bA.mtx.Lock()
	o.mtx.Lock()
	c := bA.copyBits(cmtmath.MaxInt(bA.Bits, o.Bits))
	smaller := cmtmath.MinInt(len(bA.Elems), len(o.Elems))
	for i := 0; i < smaller; i++ {
		c.Elems[i] |= o.Elems[i]
	}
	bA.mtx.Unlock()
	o.mtx.Unlock()
	return c
}

// And returns a bit array resulting from a bitwise AND of the two bit arrays.
// If the two bit-arrys have different lengths, this truncates the larger of the two bit-arrays from the right.
// Thus the size of the return value is the minimum of the two provided bit arrays.
func (bA *BitArray) And(o *BitArray) *BitArray {
	if bA == nil || o == nil {
		return nil
	}
	bA.mtx.Lock()
	o.mtx.Lock()
	defer func() {
		bA.mtx.Unlock()
		o.mtx.Unlock()
	}()
	return bA.and(o)
}

func (bA *BitArray) and(o *BitArray) *BitArray {
	c := bA.copyBits(cmtmath.MinInt(bA.Bits, o.Bits))
	for i := 0; i < len(c.Elems); i++ {
		c.Elems[i] &= o.Elems[i]
	}
	return c
}

// Not returns a bit array resulting from a bitwise Not of the provided bit array.
func (bA *BitArray) Not() *BitArray {
	if bA == nil {
		return nil // Degenerate
	}
	bA.mtx.Lock()
	defer bA.mtx.Unlock()
	return bA.not()
}

func (bA *BitArray) not() *BitArray {
	c := bA.copy()
	for i := 0; i < len(c.Elems); i++ {
		c.Elems[i] = ^c.Elems[i]
	}

	// Flip count is simply total bits minus current true bits
	c.TrueBitCount = c.Bits - c.TrueBitCount
	return c
}

// Sub subtracts the two bit-arrays bitwise, without carrying the bits.
// Note that carryless subtraction of a - b is (a and not b).
// The output is the same as bA, regardless of o's size.
// If bA is longer than o, o is right padded with zeroes.
func (bA *BitArray) Sub(o *BitArray) *BitArray {
	if bA == nil || o == nil {
		// TODO: Decide if we should do 1's complement here?
		return nil
	}
	bA.mtx.Lock()
	o.mtx.Lock()
	// output is the same size as bA
	c := bA.copyBits(bA.Bits)
	// Only iterate to the minimum size between the two.
	// If o is longer, those bits are ignored.
	// If bA is longer, then skipping those iterations is equivalent
	// to right padding with 0's
	smaller := cmtmath.MinInt(len(bA.Elems), len(o.Elems))
	for i := 0; i < smaller; i++ {
		// &^ is and not in golang
		c.Elems[i] &^= o.Elems[i]
	}
	bA.mtx.Unlock()
	o.mtx.Unlock()
	return c
}

// IsEmpty returns true iff all bits in the bit array are 0.
func (bA *BitArray) IsEmpty() bool {
	if bA == nil {
		return true // should this be opposite?
	}
	bA.mtx.Lock()
	defer bA.mtx.Unlock()
	for _, e := range bA.Elems {
		if e > 0 {
			return false
		}
	}
	return true
}

// IsFull returns true iff all bits in the bit array are 1.
func (bA *BitArray) IsFull() bool {
	if bA == nil {
		return true
	}
	bA.mtx.Lock()
	defer bA.mtx.Unlock()

	// Check all elements except the last
	for _, elem := range bA.Elems[:len(bA.Elems)-1] {
		if (^elem) != 0 {
			return false
		}
	}

	// Check that the last element has (lastElemBits) 1's
	lastElemBits := (bA.Bits+63)%64 + 1
	lastElem := bA.Elems[len(bA.Elems)-1]
	return (lastElem+1)&((uint64(1)<<uint(lastElemBits))-1) == 0
}

// PickRandom returns a random index for a set bit in the bit array.
// If there is no such value, it returns 0, false.
// It uses the provided randomness to get this index.
func (bA *BitArray) PickRandom(r *rand.Rand) (int, bool) {
	if bA == nil {
		return 0, false
	}

	bA.mtx.Lock()
	if bA.TrueBitCount == 0 { // no bits set to true
		bA.mtx.Unlock()
		return 0, false
	}
	index := bA.getNthTrueIndex(r.Intn(bA.TrueBitCount))
	bA.mtx.Unlock()
	if index == -1 {
		return 0, false
	}
	return index, true
}

// getNthTrueIndex returns the index of the nth true bit in the bit array.
// n is 0 indexed. (e.g. for bitarray x__x, getNthTrueIndex(0) returns 0).
// If there is no such value, it returns -1.
func (bA *BitArray) getNthTrueIndex(n int) int {
	numElems := len(bA.Elems)
	count := 0

	// Iterate over each element
	for i := 0; i < numElems; i++ {
		// Count set bits in the current element
		setBits := bits.OnesCount64(bA.Elems[i])

		// If the count of set bits in this element plus the count so far
		// is greater than or equal to n, then the nth bit must be in this element
		if count+setBits >= n {
			// Find the index of the nth set bit within this element
			for j := 0; j < 64; j++ {
				if bA.Elems[i]&(1<<uint(j)) != 0 {
					if count == n {
						// Calculate the absolute index of the set bit
						return i*64 + j
					}
					count++
				}
			}
		} else {
			// If the count is not enough, continue to the next element
			count += setBits
		}
	}

	// If we reach here, it means n is out of range
	return -1
}

// String returns a string representation of BitArray: BA{<bit-string>},
// where <bit-string> is a sequence of 'x' (1) and '_' (0).
// The <bit-string> includes spaces and newlines to help people.
// For a simple sequence of 'x' and '_' characters with no spaces or newlines,
// see the MarshalJSON() method.
// Example: "BA{_x_}" or "nil-BitArray" for nil.
func (bA *BitArray) String() string {
	return bA.StringIndented("")
}

// StringIndented returns the same thing as String(), but applies the indent
// at every 10th bit, and twice at every 50th bit.
func (bA *BitArray) StringIndented(indent string) string {
	if bA == nil {
		return "nil-BitArray"
	}
	bA.mtx.Lock()
	defer bA.mtx.Unlock()
	return bA.stringIndented(indent)
}

func (bA *BitArray) stringIndented(indent string) string {
	lines := []string{}
	bits := ""
	for i := 0; i < bA.Bits; i++ {
		if bA.getIndex(i) {
			bits += "x"
		} else {
			bits += "_"
		}
		if i%100 == 99 {
			lines = append(lines, bits)
			bits = ""
		}
		if i%10 == 9 {
			bits += indent
		}
		if i%50 == 49 {
			bits += indent
		}
	}
	if len(bits) > 0 {
		lines = append(lines, bits)
	}
	return fmt.Sprintf("BA{%v:%v}", bA.Bits, strings.Join(lines, indent))
}

// Bytes returns the byte representation of the bits within the bitarray.
func (bA *BitArray) Bytes() []byte {
	bA.mtx.Lock()
	defer bA.mtx.Unlock()

	numBytes := (bA.Bits + 7) / 8
	bytes := make([]byte, numBytes)
	for i := 0; i < len(bA.Elems); i++ {
		elemBytes := [8]byte{}
		binary.LittleEndian.PutUint64(elemBytes[:], bA.Elems[i])
		copy(bytes[i*8:], elemBytes[:])
	}
	return bytes
}

// Update sets the bA's bits to be that of the other bit array.
// The copying begins from the begin of both bit arrays.
func (bA *BitArray) Update(o *BitArray) {
	if bA == nil || o == nil {
		return
	}

	bA.mtx.Lock()
	o.mtx.Lock()
	copy(bA.Elems, o.Elems)
	o.mtx.Unlock()
	bA.mtx.Unlock()
}

// MarshalJSON implements json.Marshaler interface by marshaling bit array
// using a custom format: a string of '-' or 'x' where 'x' denotes the 1 bit.
func (bA *BitArray) MarshalJSON() ([]byte, error) {
	if bA == nil {
		return []byte("null"), nil
	}

	bA.mtx.Lock()
	defer bA.mtx.Unlock()

	bits := `"`
	for i := 0; i < bA.Bits; i++ {
		if bA.getIndex(i) {
			bits += `x`
		} else {
			bits += `_`
		}
	}
	bits += `"`
	return []byte(bits), nil
}

var bitArrayJSONRegexp = regexp.MustCompile(`\A"([_x]*)"\z`)

// UnmarshalJSON implements json.Unmarshaler interface by unmarshaling a custom
// JSON description.
func (bA *BitArray) UnmarshalJSON(bz []byte) error {
	b := string(bz)
	if b == "null" {
		// This is required e.g. for encoding/json when decoding
		// into a pointer with pre-allocated BitArray.
		bA.Bits = 0
		bA.Elems = nil
		bA.TrueBitCount = 0
		return nil
	}

	// Validate 'b'.
	match := bitArrayJSONRegexp.FindStringSubmatch(b)
	if match == nil {
		return fmt.Errorf("bitArray in JSON should be a string of format %q but got %s", bitArrayJSONRegexp.String(), b)
	}
	bits := match[1]

	// Construct new BitArray and copy over.
	numBits := len(bits)
	bA2 := NewBitArray(numBits)
	if bA2 == nil {
		// Treat it as if we encountered the case: b == "null"
		bA.Bits = 0
		bA.Elems = nil
		bA.TrueBitCount = 0
		return nil
	}

	for i := 0; i < numBits; i++ {
		if bits[i] == 'x' {
			bA2.SetIndex(i, true)
		}
	}

	trueCount := 0
	for i := 0; i < numBits; i++ {
		if bits[i] == 'x' {
			bA2.SetIndex(i, true)
			trueCount++
		}
	}

	// Instead of *bA = *bA2
	bA.Bits = bA2.Bits
	bA.Elems = make([]uint64, len(bA2.Elems))
	bA.TrueBitCount = trueCount
	copy(bA.Elems, bA2.Elems)
	return nil
}

// ToProto converts BitArray to protobuf.
func (bA *BitArray) ToProto() *cmtprotobits.BitArray {
	if bA == nil || len(bA.Elems) == 0 {
		return nil
	}

	return &cmtprotobits.BitArray{
		Bits:  int64(bA.Bits),
		Elems: bA.Elems,
	}
}

// FromProto sets a protobuf BitArray to the given pointer.
func (bA *BitArray) FromProto(protoBitArray *cmtprotobits.BitArray) {
	if protoBitArray == nil {
		//nolint:wastedassign
		bA = nil
		return
	}

	bA.Bits = int(protoBitArray.Bits)
	if len(protoBitArray.Elems) > 0 {
		bA.Elems = protoBitArray.Elems

		// Recalculate TrueBitCount
		bA.TrueBitCount = 0
		for i := 0; i < bA.Bits; i++ {
			if bA.getIndex(i) {
				bA.TrueBitCount++
			}
		}
	}
}
