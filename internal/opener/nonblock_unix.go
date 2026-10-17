//go:build unix

package opener

import "syscall"

// oNonblock keeps opening a FIFO from blocking; it has no effect on regular
// files.
const oNonblock = syscall.O_NONBLOCK
