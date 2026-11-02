//go:build !windows

package container

import (
	"os"
	"syscall"
)

func fileOwner(fi os.FileInfo) int { return int(fi.Sys().(*syscall.Stat_t).Uid) }
