package pathmap

import (
	"errors"
	"testing"
)

func TestToRemote(t *testing.T) {
	const nfs = "/net/192.168.122.1/home/mboot/share-local"
	tests := []struct {
		local, prefix, remote, want string
	}{
		// Unix
		{"/home/mboot/share-local/DEV/proj", "/home/mboot/share-local", nfs, nfs + "/DEV/proj"},
		{"/home/mboot/share-local", "/home/mboot/share-local", nfs, nfs},
		{"/home/mboot/share-local/", "/home/mboot/share-local/", nfs + "/", nfs},
		{"/home/mboot//share-local/./a/../b", "/home/mboot/share-local", nfs, nfs + "/b"},
		{"/home/mboot/share-local/with space/it's", "/home/mboot/share-local", nfs, nfs + "/with space/it's"},
		{"/x/y", "/", "/mnt", "/mnt/x/y"},
		{"/x/y", "/x", "/", "/y"},
		// Windows drive paths, either separator, case-insensitive prefix
		{`C:\Users\m\share\proj\sub`, `C:\Users\m\share`, "/srv/share", "/srv/share/proj/sub"},
		{`c:/users/M/Share/Proj`, `C:\Users\m\share`, "/srv/share", "/srv/share/Proj"},
		{`D:\`, `d:\`, "/d", "/d"},
		// UNC
		{`\\nas\home\m\proj`, `\\NAS\home`, "/net/nas/home", "/net/nas/home/m/proj"},
	}
	for _, tt := range tests {
		got, err := ToRemote(tt.local, tt.prefix, tt.remote)
		if err != nil || got != tt.want {
			t.Errorf("ToRemote(%q, %q, %q) = %q, %v; want %q", tt.local, tt.prefix, tt.remote, got, err, tt.want)
		}
	}
}

func TestToRemoteErrors(t *testing.T) {
	tests := []struct {
		local, prefix, remote string
		want                  error
	}{
		{"/home/mboot/share-localX/a", "/home/mboot/share-local", "/r", ErrNotUnderPrefix},
		{"/home/other", "/home/mboot/share-local", "/r", ErrNotUnderPrefix},
		{"/home/mboot/share-local/../x", "/home/mboot/share-local", "/r", ErrNotUnderPrefix},
		{"/Home/mboot/share-local/a", "/home/mboot/share-local", "/r", ErrNotUnderPrefix}, // Unix is case-sensitive
		{`D:\share\a`, `C:\share`, "/r", ErrNotUnderPrefix},
		{`C:\share\a`, "/share", "/r", ErrNotUnderPrefix},
		{"relative/a", "/home", "/r", ErrNotAbsolute},
		{`C:share\a`, `C:\`, "/r", ErrNotAbsolute},
		{"/../etc", "/", "/r", ErrNotAbsolute},
		{"/home/a", "/home", "relative", ErrNotAbsolute},
		{`\\nas`, `\\nas\x`, "/r", ErrNotAbsolute},
	}
	for _, tt := range tests {
		got, err := ToRemote(tt.local, tt.prefix, tt.remote)
		if !errors.Is(err, tt.want) {
			t.Errorf("ToRemote(%q, %q, %q) = %q, %v; want error %v", tt.local, tt.prefix, tt.remote, got, err, tt.want)
		}
	}
}
