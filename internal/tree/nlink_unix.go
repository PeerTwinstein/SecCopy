//go:build unix

package tree

import (
	"os"
	"syscall"
)

func nlinks(_ string, info os.FileInfo) (uint64, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 1, nil
	}
	return uint64(st.Nlink), nil
}
