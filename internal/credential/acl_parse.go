package credential

import (
	"encoding/binary"
	"errors"
)

const (
	attrReferenceStart = 4
	attrReferenceSize  = 8
	attrLengthPrefix   = 4
)

var errACLBuffer = errors.New("malformed ACL attribute buffer")

// parseACLBuffer reads the buffer that fgetattrlist fills for
// ATTR_CMN_EXTENDED_SECURITY. The first four bytes hold the total length
// returned, which must cover the length prefix and the attribute reference and
// must fit in the buffer. The reference follows: a four-byte data offset and a
// four-byte data length. A data length of zero means no ACL. Any malformed
// buffer returns an error, which the caller treats as an unreadable ACL.
func parseACLBuffer(buf []byte) (bool, error) {
	if len(buf) < attrLengthPrefix {
		return false, errACLBuffer
	}
	total := binary.NativeEndian.Uint32(buf)
	if total < attrReferenceStart+attrReferenceSize || uint64(total) > uint64(len(buf)) {
		return false, errACLBuffer
	}
	return binary.NativeEndian.Uint32(buf[attrReferenceStart+4:]) != 0, nil
}
