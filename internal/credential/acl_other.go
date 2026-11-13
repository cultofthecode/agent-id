//go:build !darwin

package credential

import "os"

// extendedACL reports false: POSIX ACLs of other systems show in the group
// bits through the mask, which checkKeyFile already rejects.
func extendedACL(*os.File) (bool, error) {
	return false, nil
}
