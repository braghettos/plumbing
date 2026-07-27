// Package hasher computes a cumulative, order-dependent hash over arbitrary JSON-marshalable values.
// It is used to detect drift between a desired and an observed value (e.g. a set of secret names, or an
// OAS document) without storing the full value.
package hasher

import (
	"encoding/json"
	"fmt"
	"hash"
	"hash/fnv"
)

// ObjectHash accumulates a hash across one or more calls to SumHash.
type ObjectHash struct {
	hash.Hash64
}

// SumHash JSON-marshals each value in turn and folds its bytes into the running hash. The hash is
// cumulative: calling SumHash multiple times (including across separate calls) keeps updating the same
// running total rather than starting over.
func (h *ObjectHash) SumHash(a ...any) error {
	for _, v := range a {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if _, err := h.Write(b); err != nil {
			return err
		}
	}
	return nil
}

// Reset clears the running hash back to its initial state.
func (h *ObjectHash) Reset() {
	h.Hash64.Reset()
}

// GetHash returns the current hash as a hex string.
func (h *ObjectHash) GetHash() string {
	return fmt.Sprintf("%x", h.Hash64.Sum64())
}

// NewFNVObjectHash returns an ObjectHash backed by a 64-bit FNV-1a hash.
func NewFNVObjectHash() ObjectHash {
	return ObjectHash{fnv.New64()}
}
