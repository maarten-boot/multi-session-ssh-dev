package share

import (
	"io/fs"
	"time"
)

func unixTime(sec uint32) time.Time { return time.Unix(int64(sec), 0) }

// linkInfo carries a symlink target as the name of a FileInfo, which is how
// the sftp request server returns Readlink results.
type linkInfo string

func (l linkInfo) Name() string       { return string(l) }
func (l linkInfo) Size() int64        { return 0 }
func (l linkInfo) Mode() fs.FileMode  { return fs.ModeSymlink }
func (l linkInfo) ModTime() time.Time { return time.Time{} }
func (l linkInfo) IsDir() bool        { return false }
func (l linkInfo) Sys() any           { return nil }
