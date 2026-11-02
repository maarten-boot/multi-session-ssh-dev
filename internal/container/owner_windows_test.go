package container

import "os"

func fileOwner(os.FileInfo) int { return -1 }
