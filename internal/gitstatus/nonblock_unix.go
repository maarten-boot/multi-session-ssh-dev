//go:build unix

package gitstatus

import "syscall"

// oNonblock keeps opening a FIFO from blocking (a file swapped in after the
// regular-file check); it has no effect on regular files.
const oNonblock = syscall.O_NONBLOCK
