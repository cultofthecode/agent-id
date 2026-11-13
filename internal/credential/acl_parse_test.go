package credential

import (
	"encoding/binary"
	"errors"
	"testing"
)

func aclBuffer(size int, total, offset, length uint32) []byte {
	buf := make([]byte, size)
	if size >= 4 {
		binary.NativeEndian.PutUint32(buf, total)
	}
	if size >= 12 {
		binary.NativeEndian.PutUint32(buf[4:], offset)
		binary.NativeEndian.PutUint32(buf[8:], length)
	}
	return buf
}

func TestParseACLBuffer(t *testing.T) {
	tests := []struct {
		name    string
		buf     []byte
		want    bool
		wantErr bool
	}{
		{"no ACL", aclBuffer(64, 12, 8, 0), false, false},
		{"ACL present", aclBuffer(128, 80, 8, 68), true, false},
		{"ACL present, total equals the buffer", aclBuffer(64, 64, 8, 52), true, false},
		{"no ACL, total equals the minimum", aclBuffer(12, 12, 8, 0), false, false},
		{"nil buffer", nil, false, true},
		{"empty buffer", []byte{}, false, true},
		{"buffer shorter than the length prefix", []byte{1, 2, 3}, false, true},
		{"buffer holds only the length prefix", aclBuffer(4, 4, 0, 0), false, true},
		{"total zero", aclBuffer(64, 0, 8, 0), false, true},
		{"total below the reference end", aclBuffer(64, 11, 8, 5), false, true},
		{"total larger than the buffer", aclBuffer(64, 65, 8, 52), false, true},
		{"total at the uint32 maximum", aclBuffer(64, 1<<32-1, 8, 5), false, true},
		{"total fits the length but buffer is short", aclBuffer(8, 12, 8, 0), false, true},
		{"length at the uint32 maximum", aclBuffer(64, 64, 8, 1<<32-1), true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseACLBuffer(tc.buf)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error %v", err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, errACLBuffer) {
				t.Errorf("error = %v, want %v", err, errACLBuffer)
			}
			if got != tc.want {
				t.Errorf("parseACLBuffer = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseACLBufferErrorCarriesNoACLClaim(t *testing.T) {
	for size := 0; size <= 16; size++ {
		for _, total := range []uint32{0, 4, 11, 12, 13, 1 << 31, 1<<32 - 1} {
			got, err := parseACLBuffer(aclBuffer(size, total, 8, 0))
			if err != nil && got {
				t.Errorf("size %d total %d: error with ACL=true", size, total)
			}
		}
	}
}
