package credential

import (
	"encoding/binary"
	"os"
	"syscall"
	"unsafe"
)

const (
	attrBitmapCount         = 5
	attrCmnExtendedSecurity = 0x00400000
	attrReferenceStart      = 4
)

type attrList struct {
	bitmapCount uint16
	reserved    uint16
	common      uint32
	volume      uint32
	dir         uint32
	file        uint32
	fork        uint32
}

// extendedACL reports whether the opened file carries an access control list
// (chmod +a). The ACL does not show in the mode bits, and fgetxattr refuses
// the com.apple.system.Security attribute that holds it, so the descriptor is
// asked for ATTR_CMN_EXTENDED_SECURITY through fgetattrlist. A reference of
// length zero means no ACL.
func extendedACL(file *os.File) (bool, error) {
	list := attrList{bitmapCount: attrBitmapCount, common: attrCmnExtendedSecurity}
	buf := make([]byte, 64<<10)
	_, _, errno := syscall.Syscall6(syscall.SYS_FGETATTRLIST, file.Fd(),
		uintptr(unsafe.Pointer(&list)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0)
	if errno != 0 {
		return false, errno
	}
	if len(buf) < attrReferenceStart+8 {
		return false, syscall.EINVAL
	}
	length := binary.NativeEndian.Uint32(buf[attrReferenceStart+4:])
	return length != 0, nil
}
