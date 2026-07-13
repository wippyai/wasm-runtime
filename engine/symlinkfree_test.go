package engine

import (
	"testing"
	"testing/fstest"

	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
	"github.com/tetratelabs/wazero/experimental/sysfs"
)

func TestSymlinkFreeFSReadlink(t *testing.T) {
	tree := fstest.MapFS{
		"library/stats/DESCRIPTION": &fstest.MapFile{Data: []byte("Package: stats\n")},
	}

	t.Run("existing path is not a symlink", func(t *testing.T) {
		fsys := newSymlinkFreeFS(tree)

		for _, path := range []string{"library", "library/stats", "library/stats/DESCRIPTION"} {
			if _, errno := fsys.Readlink(path); errno != experimentalsys.EINVAL {
				t.Fatalf("Readlink(%q) = %v, want EINVAL: a guest realpath() aborts on ENOSYS", path, errno)
			}
		}
	})

	t.Run("missing path is reported missing", func(t *testing.T) {
		fsys := newSymlinkFreeFS(tree)

		if _, errno := fsys.Readlink("library/nope"); errno != experimentalsys.ENOENT {
			t.Fatalf("Readlink(missing) = %v, want ENOENT", errno)
		}
	})

	t.Run("the wazero adapter alone answers ENOSYS", func(t *testing.T) {
		adapted := &sysfs.AdaptFS{FS: tree}

		if _, errno := adapted.Readlink("library"); errno != experimentalsys.ENOSYS {
			t.Fatalf("AdaptFS.Readlink = %v, want ENOSYS; the wrapper is pointless if this changed", errno)
		}
	})
}
