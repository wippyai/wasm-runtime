package engine

import (
	"io/fs"

	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
	"github.com/tetratelabs/wazero/experimental/sysfs"
)

// symlinkFreeFS mounts an fs.FS that, by construction, holds no symbolic links —
// an embedded pack, a zip, a virtual tree. wazero's AdaptFS answers ENOSYS to
// readlink, which a guest libc reads as "this platform cannot resolve links" and
// fails realpath() outright; a filesystem that simply has no links must answer
// EINVAL ("not a symbolic link") so realpath() keeps walking. R hits exactly this:
// normalizePath() on an ENOSYS mount aborts, library() never resolves its lib path,
// and no package — grDevices included — ever loads.
type symlinkFreeFS struct {
	experimentalsys.FS
}

// Readlink reports that nothing in the tree is a symbolic link, which is the truth
// for an fs.FS: wazero's own Lstat makes the same assumption.
func (f *symlinkFreeFS) Readlink(path string) (string, experimentalsys.Errno) {
	if _, errno := f.Lstat(path); errno != 0 {
		return "", errno
	}

	return "", experimentalsys.EINVAL
}

func newSymlinkFreeFS(fsys fs.FS) experimentalsys.FS {
	return &symlinkFreeFS{FS: &sysfs.AdaptFS{FS: fsys}}
}
