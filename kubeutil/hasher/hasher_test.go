package hasher

import "testing"

func TestHash(t *testing.T) {
	tests := []struct {
		name    string
		input   []any
		wantErr bool
	}{
		{
			name:    "Single string input",
			input:   []any{"test"},
			wantErr: false,
		},
		{
			name:    "Multiple inputs",
			input:   []any{"test", 123, true},
			wantErr: false,
		},
		{
			name:    "Empty input",
			input:   []any{},
			wantErr: false,
		},
		{
			name:    "Nil input",
			input:   nil,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewFNVObjectHash()
			err := h.SumHash(tt.input...)
			if (err != nil) != tt.wantErr {
				t.Errorf("SumHash() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			got := h.GetHash()
			if got == "" && !tt.wantErr {
				t.Errorf("GetHash() returned empty string, expected valid hash")
			}
		})
	}
}

func TestHashIsCumulativeAndOrderDependent(t *testing.T) {
	a := NewFNVObjectHash()
	if err := a.SumHash("x"); err != nil {
		t.Fatalf("SumHash: %v", err)
	}
	if err := a.SumHash("y"); err != nil {
		t.Fatalf("SumHash: %v", err)
	}

	b := NewFNVObjectHash()
	if err := b.SumHash("x", "y"); err != nil {
		t.Fatalf("SumHash: %v", err)
	}
	if a.GetHash() != b.GetHash() {
		t.Fatalf("expected cumulative SumHash calls to match a single combined call: %q != %q", a.GetHash(), b.GetHash())
	}

	c := NewFNVObjectHash()
	if err := c.SumHash("y", "x"); err != nil {
		t.Fatalf("SumHash: %v", err)
	}
	if a.GetHash() == c.GetHash() {
		t.Fatalf("expected order to change the hash, got same value %q for both orders", a.GetHash())
	}
}

func TestReset(t *testing.T) {
	h := NewFNVObjectHash()
	if err := h.SumHash("test"); err != nil {
		t.Fatalf("SumHash: %v", err)
	}
	withValue := h.GetHash()

	h.Reset()
	empty := h.GetHash()
	if empty == withValue {
		t.Fatalf("expected Reset to change the hash")
	}

	fresh := NewFNVObjectHash()
	if fresh.GetHash() != empty {
		t.Fatalf("expected Reset hash %q to equal a fresh hash %q", empty, fresh.GetHash())
	}
}
