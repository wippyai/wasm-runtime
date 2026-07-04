package engine

import (
	"io/fs"

	"github.com/tetratelabs/wazero"
	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
	wazerosysfs "github.com/tetratelabs/wazero/experimental/sysfs"
)

type readOnlyMountFS struct {
	*wazerosysfs.AdaptFS
}

func withReadOnlyFSMount(cfg wazero.FSConfig, fsys fs.FS, guest string) wazero.FSConfig {
	sysCfg := cfg.(wazerosysfs.FSConfig)
	return sysCfg.WithSysFSMount(&readOnlyMountFS{
		AdaptFS: &wazerosysfs.AdaptFS{FS: fsys},
	}, guest)
}

func (f *readOnlyMountFS) Readlink(path string) (string, experimentalsys.Errno) {
	if _, errno := f.Lstat(path); errno != 0 {
		return "", errno
	}
	return "", experimentalsys.EINVAL
}
