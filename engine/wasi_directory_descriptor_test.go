package engine

import (
	"io/fs"
	"testing"
	"testing/fstest"

	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
)

type readDirOnlyCapability struct{ fs.FS }

func (f readDirOnlyCapability) OpenDirectory(name string, noFollow bool) (fs.File, error) {
	return f.Open(name)
}

func TestCapabilityDirectoryNonblockPreservesReadDir(t *testing.T) {
	provider := readDirOnlyCapability{fstest.MapFS{"entry": &fstest.MapFile{Data: []byte("value")}}}
	mount := &capabilityMountFS{fsys: provider}
	for _, flags := range []experimentalsys.Oflag{experimentalsys.O_DIRECTORY, experimentalsys.O_DIRECTORY | experimentalsys.O_NONBLOCK} {
		file, errno := mount.OpenFile(".", flags, 0)
		if errno != 0 {
			t.Fatalf("open: %v", errno)
		}
		entries, errno := file.Readdir(-1)
		if errno != 0 || len(entries) != 1 || entries[0].Name != "entry" {
			t.Errorf("flags=%v entries=%v errno=%v", flags, entries, errno)
		}
		pollable, ok := file.(experimentalsys.PollableFile)
		if !ok {
			t.Fatal("directory descriptor lacks flag state")
		}
		if pollable.IsNonblock() != (flags&experimentalsys.O_NONBLOCK != 0) {
			t.Error("initial flag lost")
		}
		if errno := pollable.SetNonblock(true); errno != 0 {
			t.Errorf("enable: %v", errno)
		}
		if !pollable.IsNonblock() {
			t.Error("enable not retained")
		}
		if errno := pollable.SetNonblock(false); errno != 0 {
			t.Errorf("disable: %v", errno)
		}
		if pollable.IsNonblock() {
			t.Error("disable not retained")
		}
		if errno := file.Close(); errno != 0 {
			t.Errorf("close: %v", errno)
		}
		if errno := pollable.SetNonblock(true); errno != experimentalsys.EBADF {
			t.Errorf("closed set flags=%v want EBADF", errno)
		}
	}
}
