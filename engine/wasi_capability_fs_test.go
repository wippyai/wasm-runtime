package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"

	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
	wazerosysfs "github.com/tetratelabs/wazero/experimental/sysfs"

	"github.com/wippyai/wasm-runtime/wat"
)

type rootedCapabilityFS struct {
	root   *os.Root
	closes atomic.Int32
}

func newRootedCapabilityFS(t *testing.T, directory string) *rootedCapabilityFS {
	t.Helper()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return &rootedCapabilityFS{root: root}
}

func (f *rootedCapabilityFS) Open(name string) (fs.File, error) {
	return f.root.Open(name)
}

func (f *rootedCapabilityFS) OpenFile(name string, flag int, perm fs.FileMode) (fs.File, error) {
	file, err := f.root.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return &closeCountingFile{File: file, closes: &f.closes}, nil
}

func (f *rootedCapabilityFS) Stat(name string) (fs.FileInfo, error)  { return f.root.Stat(name) }
func (f *rootedCapabilityFS) Lstat(name string) (fs.FileInfo, error) { return f.root.Lstat(name) }
func (f *rootedCapabilityFS) Mkdir(name string, perm fs.FileMode) error {
	return f.root.Mkdir(name, perm)
}
func (f *rootedCapabilityFS) Rename(oldName, newName string) error {
	return f.root.Rename(oldName, newName)
}

// directoryRecordingCapability is a forwarding oracle. Its OpenDirectory
// fixture opens a known ordinary directory; the production bridge supplies
// the atomic O_DIRECTORY/O_NOFOLLOW operation.
type directoryRecordingCapability struct {
	*rootedCapabilityFS
	name      string
	openName  string
	mu        sync.Mutex
	dirCalls  atomic.Int32
	openCalls atomic.Int32
	noFollow  bool
}

func newDirectoryRecordingCapability(t *testing.T, directory string) *directoryRecordingCapability {
	return &directoryRecordingCapability{rootedCapabilityFS: newRootedCapabilityFS(t, directory)}
}

func (f *directoryRecordingCapability) OpenFile(name string, flag int, perm fs.FileMode) (fs.File, error) {
	f.mu.Lock()
	f.openName = name
	f.mu.Unlock()
	f.openCalls.Add(1)
	return f.rootedCapabilityFS.OpenFile(name, flag, perm)
}

func (f *directoryRecordingCapability) OpenDirectory(name string, noFollow bool) (fs.File, error) {
	f.mu.Lock()
	f.name, f.noFollow = name, noFollow
	f.mu.Unlock()
	f.dirCalls.Add(1)
	// The test provider implements the directory-only contract using the opened
	// descriptor. Windows reports ERROR_PATH_NOT_FOUND for a regular file with
	// a trailing slash, so let the provider enforce that final type explicitly.
	// Preserve every interior component, including parent traversal.
	openName := strings.TrimRight(name, "/")
	if openName == "" {
		openName = "."
	}
	file, err := f.root.Open(openName)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.IsDir() {
		_ = file.Close()
		return nil, &fs.PathError{Op: "open-directory", Path: name, Err: experimentalsys.ENOTDIR}
	}
	return &closeCountingFile{File: file, closes: &f.closes}, nil
}

func (f *directoryRecordingCapability) directoryOpen() (name string, noFollow bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.name, f.noFollow
}

type closeCountingFile struct {
	*os.File
	closes *atomic.Int32
}

func (f *closeCountingFile) Close() error {
	f.closes.Add(1)
	return f.File.Close()
}

func TestCapabilityMountFS_OpenFlagsWriteAndClose(t *testing.T) {
	root := t.TempDir()
	capability := newRootedCapabilityFS(t, root)
	mount := &capabilityMountFS{fsys: capability}

	file, errno := mount.OpenFile("nested.txt", experimentalsys.O_RDWR|experimentalsys.O_CREAT|experimentalsys.O_TRUNC, 0600)
	if errno != 0 {
		t.Fatalf("OpenFile create: %v", errno)
	}
	if n, errno := file.Write([]byte("written")); errno != 0 || n != len("written") {
		t.Fatalf("Write = (%d, %v)", n, errno)
	}
	if errno := file.Close(); errno != 0 {
		t.Fatalf("Close: %v", errno)
	}
	if got, err := os.ReadFile(filepath.Join(root, "nested.txt")); err != nil || string(got) != "written" {
		t.Fatalf("host data = %q, %v", got, err)
	}
	if got := capability.closes.Load(); got != 1 {
		t.Fatalf("underlying file closes = %d, want 1", got)
	}

	if errno := mount.Mkdir("dir", 0700); errno != 0 {
		t.Fatalf("Mkdir: %v", errno)
	}
	if errno := mount.Rename("nested.txt", "dir/moved.txt"); errno != 0 {
		t.Fatalf("Rename: %v", errno)
	}
	if got, err := os.ReadFile(filepath.Join(root, "dir", "moved.txt")); err != nil || string(got) != "written" {
		t.Fatalf("renamed data = %q, %v", got, err)
	}
}

func TestCapabilityMountFS_RejectsEscapeAndUnsupportedFlags(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	mount := &capabilityMountFS{fsys: newRootedCapabilityFS(t, root)}

	if _, errno := mount.OpenFile("../secret.txt", experimentalsys.O_RDONLY, 0); errno == 0 {
		t.Fatal("rooted capability allowed parent traversal")
	}
	if _, errno := mount.OpenFile("/secret.txt", experimentalsys.O_RDONLY, 0); errno != experimentalsys.EPERM {
		t.Fatalf("absolute path errno = %v, want EPERM", errno)
	}
	if _, errno := mount.OpenFile("outside/secret.txt", experimentalsys.O_RDONLY|experimentalsys.O_NOFOLLOW, 0); errno != experimentalsys.ENOSYS {
		t.Fatalf("O_NOFOLLOW without an atomic capability = %v, want ENOSYS", errno)
	}
	if _, errno := mount.OpenFile("file.txt", experimentalsys.O_DIRECTORY, 0); errno != experimentalsys.ENOSYS {
		t.Fatalf("O_DIRECTORY without a directory-open capability = %v, want ENOSYS", errno)
	}
	if _, errno := mount.OpenFile("file.txt", experimentalsys.Oflag(1<<30), 0); errno != experimentalsys.ENOSYS {
		t.Fatalf("unknown open flag = %v, want ENOSYS", errno)
	}
	if _, errno := mount.OpenFile("outside/secret.txt", experimentalsys.O_RDONLY, 0); errno == 0 {
		t.Fatal("os.Root-backed capability followed a symlink outside its root")
	}
	if errno := mount.Rmdir("dir"); errno != experimentalsys.ENOSYS {
		t.Fatalf("Rmdir without an atomic directory capability = %v, want ENOSYS", errno)
	}
	if errno := mount.Unlink("file"); errno != experimentalsys.ENOSYS {
		t.Fatalf("Unlink without an atomic file capability = %v, want ENOSYS", errno)
	}
}

func TestCapabilityMountFS_OpenDirectoryAndNoFollow(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	capability := newDirectoryRecordingCapability(t, root)
	mount := &capabilityMountFS{fsys: capability}

	file, errno := mount.OpenFile("dir", experimentalsys.O_DIRECTORY|experimentalsys.O_NOFOLLOW, 0)
	if errno != 0 {
		t.Fatalf("directory OpenFile: %v", errno)
	}
	if isDir, errno := file.IsDir(); errno != 0 || !isDir {
		t.Fatalf("directory IsDir = (%v, %v)", isDir, errno)
	}
	if errno := file.Close(); errno != 0 {
		t.Fatalf("directory Close: %v", errno)
	}
	if got := capability.dirCalls.Load(); got != 1 {
		t.Fatalf("OpenDirectory calls = %d, want 1", got)
	}
	if name, noFollow := capability.directoryOpen(); name != "dir" || !noFollow {
		t.Fatalf("OpenDirectory args = (%q, %v), want (dir, true)", name, noFollow)
	}
	if got := capability.openCalls.Load(); got != 0 {
		t.Fatalf("ordinary OpenFile calls = %d, want 0", got)
	}
	if got := capability.closes.Load(); got != 1 {
		t.Fatalf("directory close count = %d, want 1", got)
	}
}

func TestCapabilityMountFS_PreservesTrailingSlashAndParentComponents(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("contents"), 0600); err != nil {
		t.Fatal(err)
	}
	capability := newDirectoryRecordingCapability(t, root)
	mount := &capabilityMountFS{fsys: capability}

	dir, errno := mount.OpenFile("dir/", experimentalsys.O_RDONLY|experimentalsys.O_NOFOLLOW|experimentalsys.O_NONBLOCK, 0)
	if errno != 0 {
		t.Fatalf("directory trailing-slash open: %v", errno)
	}
	if errno := dir.Close(); errno != 0 {
		t.Fatalf("directory Close: %v", errno)
	}
	if name, noFollow := capability.directoryOpen(); name != "dir/" || !noFollow {
		t.Fatalf("trailing-slash OpenDirectory args = (%q, %v), want (dir/, true)", name, noFollow)
	}
	if _, errno := mount.OpenFile("file/", experimentalsys.O_RDONLY|experimentalsys.O_NOFOLLOW, 0); errno != experimentalsys.ENOTDIR {
		t.Fatalf("regular-file trailing-slash open = %v, want ENOTDIR", errno)
	}
	if got := capability.dirCalls.Load(); got != 2 {
		t.Fatalf("OpenDirectory calls = %d, want 2", got)
	}
	if got := capability.openCalls.Load(); got != 0 {
		t.Fatalf("ordinary OpenFile calls for trailing slash = %d, want 0", got)
	}
	if _, errno := mount.OpenFile("file", experimentalsys.O_RDONLY|experimentalsys.O_NONBLOCK, 0); errno != experimentalsys.ENOSYS {
		t.Fatalf("regular-file O_NONBLOCK = %v, want ENOSYS", errno)
	}
	if got := capability.openCalls.Load(); got != 0 {
		t.Fatalf("ordinary OpenFile calls after rejected O_NONBLOCK = %d, want 0", got)
	}

	file, errno := mount.OpenFile("dir/../file", experimentalsys.O_RDONLY, 0)
	if errno != 0 {
		t.Fatalf("component-wise open: %v", errno)
	}
	if errno := file.Close(); errno != 0 {
		t.Fatalf("component-wise Close: %v", errno)
	}
	capability.mu.Lock()
	openedPath := capability.openName
	capability.mu.Unlock()
	if openedPath != "dir/../file" {
		t.Fatalf("ordinary OpenFile path = %q, want original component path", openedPath)
	}
	if err := os.MkdirAll(filepath.Join(root, "dir", "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("dir/nested", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	file, errno = mount.OpenFile("link/../../file", experimentalsys.O_RDONLY, 0)
	if errno != 0 {
		t.Fatalf("symlink-expanded parent traversal: %v", errno)
	}
	if errno := file.Close(); errno != 0 {
		t.Fatalf("symlink-expanded Close: %v", errno)
	}
	capability.mu.Lock()
	openedPath = capability.openName
	capability.mu.Unlock()
	if openedPath != "link/../../file" {
		t.Fatalf("symlink-expanded OpenFile path = %q, want original component path", openedPath)
	}
	if _, errno := mount.OpenFile("missing/", experimentalsys.O_RDONLY|experimentalsys.O_NOFOLLOW, 0); errno != experimentalsys.ENOENT {
		t.Fatalf("missing-directory trailing-slash open = %v, want ENOENT", errno)
	}
}

func TestCapabilityMountFS_DirectoryMutationDeniedBeforeOpen(t *testing.T) {
	capability := newDirectoryRecordingCapability(t, t.TempDir())
	mount := &capabilityMountFS{fsys: capability}

	for _, tc := range []struct {
		name string
		flag experimentalsys.Oflag
		want experimentalsys.Errno
	}{
		{"create", experimentalsys.O_DIRECTORY | experimentalsys.O_CREAT | experimentalsys.O_RDWR, experimentalsys.EINVAL},
		{"write", experimentalsys.O_DIRECTORY | experimentalsys.O_WRONLY, experimentalsys.EISDIR},
		{"append", experimentalsys.O_DIRECTORY | experimentalsys.O_APPEND, experimentalsys.EISDIR},
		{"truncate", experimentalsys.O_DIRECTORY | experimentalsys.O_TRUNC, experimentalsys.EISDIR},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, errno := mount.OpenFile("dir", tc.flag, 0); errno != tc.want {
				t.Fatalf("OpenFile(%v) = %v, want %v", tc.flag, errno, tc.want)
			}
		})
	}
	if got := capability.dirCalls.Load(); got != 0 {
		t.Fatalf("OpenDirectory calls after rejected mutations = %d, want 0", got)
	}
	if got := capability.openCalls.Load(); got != 0 {
		t.Fatalf("ordinary OpenFile calls after rejected mutations = %d, want 0", got)
	}
}

func TestReadOnlyMountFS_RetainsDirectoryCapability(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	capability := newDirectoryRecordingCapability(t, root)
	mount := &readOnlyMountFS{ReadFS: &wazerosysfs.ReadFS{FS: &capabilityMountFS{fsys: capability}}}

	file, errno := mount.OpenFile("dir", experimentalsys.O_DIRECTORY|experimentalsys.O_NOFOLLOW, 0)
	if errno != 0 {
		t.Fatalf("read-only directory OpenFile: %v", errno)
	}
	if errno := file.Close(); errno != 0 {
		t.Fatalf("directory Close: %v", errno)
	}
	if name, noFollow := capability.directoryOpen(); name != "dir" || !noFollow {
		t.Fatalf("read-only OpenDirectory args = (%q, %v), want (dir, true)", name, noFollow)
	}
	if _, errno := mount.OpenFile("dir", experimentalsys.O_DIRECTORY|experimentalsys.O_RDWR, 0); errno != experimentalsys.EISDIR {
		t.Fatalf("read-only writable directory OpenFile = %v, want EISDIR", errno)
	}
	if got := capability.dirCalls.Load(); got != 1 {
		t.Fatalf("OpenDirectory calls after writable attempt = %d, want 1", got)
	}
}

func TestReadOnlyMountFS_DeniesMutationIntentAndWriter(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	capability := newRootedCapabilityFS(t, root)
	base := &capabilityMountFS{fsys: capability}
	mount := &readOnlyMountFS{ReadFS: &wazerosysfs.ReadFS{FS: base}}

	for _, flag := range []experimentalsys.Oflag{
		experimentalsys.O_WRONLY,
		experimentalsys.O_RDWR,
		experimentalsys.O_CREAT,
		experimentalsys.O_TRUNC,
		experimentalsys.O_APPEND,
	} {
		if _, errno := mount.OpenFile("file.txt", flag, 0600); errno != experimentalsys.EROFS {
			t.Fatalf("OpenFile flag %v errno = %v, want EROFS", flag, errno)
		}
	}
	file, errno := mount.OpenFile("file.txt", experimentalsys.O_RDONLY, 0)
	if errno != 0 {
		t.Fatalf("read-only OpenFile: %v", errno)
	}
	if _, errno := file.Write([]byte("mutate")); errno == 0 {
		t.Fatal("write through a read-only descriptor succeeded")
	}
	if errno := file.Close(); errno != 0 {
		t.Fatalf("Close: %v", errno)
	}
	if got, err := os.ReadFile(filepath.Join(root, "file.txt")); err != nil || string(got) != "before" {
		t.Fatalf("read-only mount changed host data: %q, %v", got, err)
	}
}

type unsupportedNoFollowCapability struct {
	fs.FS
	openCalls     atomic.Int32
	noFollowCalls atomic.Int32
}

func (f *unsupportedNoFollowCapability) OpenFile(string, int, fs.FileMode) (fs.File, error) {
	f.openCalls.Add(1)
	return nil, errors.ErrUnsupported
}

func (f *unsupportedNoFollowCapability) OpenFileNoFollow(string, int, fs.FileMode) (fs.File, error) {
	f.noFollowCalls.Add(1)
	return nil, errors.ErrUnsupported
}

func TestCapabilityMountFS_DelegatesNoFollowOnlyToAtomicCapability(t *testing.T) {
	capability := &unsupportedNoFollowCapability{FS: os.DirFS(t.TempDir())}
	mount := &capabilityMountFS{fsys: capability}
	if _, errno := mount.OpenFile("file.txt", experimentalsys.O_RDONLY|experimentalsys.O_NOFOLLOW, 0); errno != experimentalsys.ENOSYS {
		t.Fatalf("unsupported atomic O_NOFOLLOW = %v, want ENOSYS", errno)
	}
	if got := capability.noFollowCalls.Load(); got != 1 {
		t.Fatalf("atomic O_NOFOLLOW calls = %d, want 1", got)
	}
	if got := capability.openCalls.Load(); got != 0 {
		t.Fatalf("ordinary OpenFile calls after O_NOFOLLOW = %d, want 0", got)
	}
	if _, errno := mount.OpenFile("file.txt", experimentalsys.O_RDONLY, 0); errno != experimentalsys.ENOSYS {
		t.Fatalf("unsupported OpenFile = %v, want ENOSYS", errno)
	}
	if got := capability.openCalls.Load(); got != 1 {
		t.Fatalf("ordinary OpenFile calls = %d, want 1", got)
	}
}

func TestCapabilityErrnoNormalizesNestedAndJoinedErrors(t *testing.T) {
	syscallNotDirectory := experimentalsys.ENOTDIR
	if runtime.GOOS == "windows" {
		// Go aliases ENOTDIR to ERROR_PATH_NOT_FOUND on Windows. That error is
		// also used for missing parents and must retain the ENOENT mapping.
		syscallNotDirectory = experimentalsys.ENOENT
	}
	nestedNotExist := &fs.PathError{Op: "outer", Path: "missing", Err: &fs.PathError{Op: "inner", Path: "missing", Err: fs.ErrNotExist}}
	for _, tc := range []struct {
		err  error
		name string
		want experimentalsys.Errno
	}{
		{name: "not-exist", err: nestedNotExist, want: experimentalsys.ENOENT},
		{name: "exist", err: errors.Join(errors.New("context"), fs.ErrExist), want: experimentalsys.EEXIST},
		{name: "permission", err: errors.Join(errors.New("context"), fs.ErrPermission), want: experimentalsys.EPERM},
		{name: "invalid", err: errors.Join(errors.New("context"), fs.ErrInvalid), want: experimentalsys.EINVAL},
		{name: "closed", err: errors.Join(errors.New("context"), fs.ErrClosed), want: experimentalsys.EBADF},
		{name: "unsupported", err: errors.Join(errors.New("context"), errors.ErrUnsupported), want: experimentalsys.ENOSYS},
		{name: "wrapped-explicit-access", err: fmt.Errorf("open capability: %w", experimentalsys.EACCES), want: experimentalsys.EACCES},
		{name: "joined-explicit-not-directory", err: errors.Join(errors.New("context"), experimentalsys.ENOTDIR), want: experimentalsys.ENOTDIR},
		{name: "wazero-errno", err: errors.Join(errors.New("context"), experimentalsys.ELOOP), want: experimentalsys.ELOOP},
		{name: "syscall-loop", err: errors.Join(errors.New("context"), syscall.ELOOP), want: experimentalsys.ELOOP},
		{name: "syscall-not-directory", err: &fs.PathError{Op: "open", Path: "directory", Err: syscall.ENOTDIR}, want: syscallNotDirectory},
		{name: "explicit-not-directory", err: &fs.PathError{Op: "open", Path: "directory", Err: experimentalsys.ENOTDIR}, want: experimentalsys.ENOTDIR},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := capabilityErrno(tc.err); got != tc.want {
				t.Fatalf("capabilityErrno(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
	if got := capabilityErrno(errors.New("unmapped")); got == 0 {
		t.Fatal("unmapped error reported success")
	}
}

type nestedErrorCapability struct {
	fs.FS
	err error
}

func (f nestedErrorCapability) OpenFile(string, int, fs.FileMode) (fs.File, error) {
	return nil, f.err
}

func TestCapabilityMountFS_NormalizesErrorsBeforeAdaptFS(t *testing.T) {
	nestedNotExist := &fs.PathError{Op: "outer", Path: "missing", Err: &fs.PathError{Op: "inner", Path: "missing", Err: fs.ErrNotExist}}
	for _, tc := range []struct {
		fsys fs.FS
		name string
	}{
		{name: "open-file-capability", fsys: nestedErrorCapability{FS: os.DirFS(t.TempDir()), err: nestedNotExist}},
		{name: "read-only-fallback", fsys: errorReadFS{err: nestedNotExist}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mount := &capabilityMountFS{fsys: tc.fsys}
			if _, errno := mount.OpenFile("missing", experimentalsys.O_RDONLY, 0); errno != experimentalsys.ENOENT {
				t.Fatalf("nested missing-file errno = %v, want ENOENT", errno)
			}
		})
	}
}

type errorReadFS struct{ err error }

func (f errorReadFS) Open(string) (fs.File, error) { return nil, f.err }

func TestCapabilityMountFS_ActualWASIGuestWritesRegisteredFilesystem(t *testing.T) {
	ctx := context.Background()
	engine, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(ctx)
	wasm, err := wat.Compile(`(module
		(import "wasi_snapshot_preview1" "path_open"
			(func $path_open (param i32 i32 i32 i32 i32 i64 i64 i32 i32) (result i32)))
		(import "wasi_snapshot_preview1" "fd_write"
			(func $fd_write (param i32 i32 i32 i32) (result i32)))
		(import "wasi_snapshot_preview1" "fd_close"
			(func $fd_close (param i32) (result i32)))
		(memory (export "memory") 1)
		(data (i32.const 8) "guest.txt")
		(data (i32.const 32) "hello")
		(func (export "run_errno") (result i32)
			(local $fd i32) (local $errno i32)
			i32.const 3 i32.const 1 i32.const 8 i32.const 9 i32.const 1
			i64.const 64 i64.const 0 i32.const 0 i32.const 72
			call $path_open local.set $errno
			(local.get $errno)
			(if (result i32)
				(then (local.get $errno))
				(else
					(i32.const 72 i32.load local.set $fd)
					(i32.const 48 i32.const 32 i32.store)
					(i32.const 52 i32.const 5 i32.store)
					(local.get $fd i32.const 48 i32.const 1 i32.const 56 call $fd_write)
					(local.get $fd call $fd_close drop)))))`)
	if err != nil {
		t.Fatal(err)
	}
	module, err := engine.LoadModule(ctx, wasm)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Compile(ctx, nil); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	capability := newRootedCapabilityFS(t, root)
	instance, err := module.InstantiateWithConfig(ctx, &InstanceConfig{Mounts: []Mount{{Guest: "/data", FS: capability}}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close(ctx)
	fn := instance.instance.ExportedFunction("run_errno")
	if fn == nil {
		t.Fatal("run_errno was not exported")
	}
	results, err := fn.Call(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0] != 0 {
		t.Fatalf("guest write errno = %#v", results)
	}
	if got, err := os.ReadFile(filepath.Join(root, "guest.txt")); err != nil || string(got) != "hello" {
		t.Fatalf("guest output = %q, %v", got, err)
	}
	if got := capability.closes.Load(); got != 1 {
		t.Fatalf("guest descriptor did not close exactly once: %d", got)
	}
}

type nestedMissingRootCapability struct{ *rootedCapabilityFS }

func (f nestedMissingRootCapability) OpenFile(name string, flag int, perm fs.FileMode) (fs.File, error) {
	if name == "missing" {
		return nil, &fs.PathError{Op: "outer", Path: name, Err: &fs.PathError{Op: "inner", Path: name, Err: fs.ErrNotExist}}
	}
	return f.rootedCapabilityFS.OpenFile(name, flag, perm)
}

func TestCapabilityMountFS_ActualWASIGuestTrailingSlashRequiresDirectory(t *testing.T) {
	ctx := context.Background()
	engine, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(ctx)
	wasm, err := wat.Compile(`(module
		(import "wasi_snapshot_preview1" "path_open"
			(func $path_open (param i32 i32 i32 i32 i32 i64 i64 i32 i32) (result i32)))
		(import "wasi_snapshot_preview1" "fd_close"
			(func $fd_close (param i32) (result i32)))
		(memory (export "memory") 1)
		(data (i32.const 8) "dir/")
		(data (i32.const 16) "file/")
		(func (export "open_dir_errno") (result i32)
			(local $fd i32) (local $errno i32)
			i32.const 3 i32.const 0 i32.const 8 i32.const 4 i32.const 0
			i64.const 0 i64.const 0 i32.const 4 i32.const 72
			call $path_open local.set $errno
			(local.get $errno)
			(if (result i32)
				(then (local.get $errno))
				(else
					(i32.const 72 i32.load local.set $fd)
					(local.get $fd call $fd_close drop)
					i32.const 0)))
		(func (export "open_file_errno") (result i32)
			i32.const 3 i32.const 0 i32.const 16 i32.const 5 i32.const 0
			i64.const 0 i64.const 0 i32.const 0 i32.const 72
			call $path_open))`)
	if err != nil {
		t.Fatal(err)
	}
	module, err := engine.LoadModule(ctx, wasm)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Compile(ctx, nil); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("contents"), 0600); err != nil {
		t.Fatal(err)
	}
	capability := newDirectoryRecordingCapability(t, root)
	instance, err := module.InstantiateWithConfig(ctx, &InstanceConfig{Mounts: []Mount{{Guest: "/data", FS: capability}}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close(ctx)
	openDir := instance.instance.ExportedFunction("open_dir_errno")
	openFile := instance.instance.ExportedFunction("open_file_errno")
	if openDir == nil || openFile == nil {
		t.Fatal("trailing-slash functions were not exported")
	}
	if results, err := openDir.Call(ctx); err != nil || len(results) != 1 || results[0] != 0 {
		t.Fatalf("guest dir/ result = %#v, %v", results, err)
	}
	// WASI snapshot preview 1 encodes ENOTDIR as 54.
	const wasiErrnoNotdir = uint64(54)
	if results, err := openFile.Call(ctx); err != nil || len(results) != 1 || results[0] != wasiErrnoNotdir {
		t.Fatalf("guest file/ result = %#v, %v; want ENOTDIR (%d)", results, err, wasiErrnoNotdir)
	}
	if got := capability.dirCalls.Load(); got != 2 {
		t.Fatalf("guest OpenDirectory calls = %d, want 2", got)
	}
	if name, noFollow := capability.directoryOpen(); name != "file/" || !noFollow {
		t.Fatalf("last guest OpenDirectory args = (%q, %v), want (file/, true)", name, noFollow)
	}
	if got := capability.closes.Load(); got != 1 {
		t.Fatalf("guest dir/ descriptor close count = %d, want 1", got)
	}
}

func TestCapabilityMountFS_ActualWASIGuestNestedMissingFileIsENOENT(t *testing.T) {
	ctx := context.Background()
	engine, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(ctx)
	wasm, err := wat.Compile(`(module
		(import "wasi_snapshot_preview1" "path_open"
			(func $path_open (param i32 i32 i32 i32 i32 i64 i64 i32 i32) (result i32)))
		(memory (export "memory") 1)
		(data (i32.const 8) "missing")
		(func (export "run_errno") (result i32)
			i32.const 3 i32.const 1 i32.const 8 i32.const 7 i32.const 0
			i64.const 0 i64.const 0 i32.const 0 i32.const 72
			call $path_open))`)
	if err != nil {
		t.Fatal(err)
	}
	module, err := engine.LoadModule(ctx, wasm)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Compile(ctx, nil); err != nil {
		t.Fatal(err)
	}
	capability := nestedMissingRootCapability{newRootedCapabilityFS(t, t.TempDir())}
	instance, err := module.InstantiateWithConfig(ctx, &InstanceConfig{Mounts: []Mount{{Guest: "/data", FS: capability}}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close(ctx)
	fn := instance.instance.ExportedFunction("run_errno")
	if fn == nil {
		t.Fatal("run_errno was not exported")
	}
	results, err := fn.Call(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// WASI snapshot preview 1 encodes ENOENT as 44. An EIO here was the
	// regression caused by AdaptFS seeing only the outer PathError.
	const wasiErrnoNoent = uint64(44)
	if len(results) != 1 || results[0] != wasiErrnoNoent {
		t.Fatalf("guest nested missing-file errno = %#v, want ENOENT (%d)", results, wasiErrnoNoent)
	}
}

func TestCapabilityMountFS_ActualWASIGuestOpensDirectoryAtomically(t *testing.T) {
	ctx := context.Background()
	engine, err := NewWazeroEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(ctx)
	wasm, err := wat.Compile(`(module
		(import "wasi_snapshot_preview1" "path_open"
			(func $path_open (param i32 i32 i32 i32 i32 i64 i64 i32 i32) (result i32)))
		(import "wasi_snapshot_preview1" "fd_close"
			(func $fd_close (param i32) (result i32)))
		(import "wasi_snapshot_preview1" "fd_fdstat_get"
			(func $fd_fdstat_get (param i32 i32) (result i32)))
		(memory (export "memory") 1)
		(data (i32.const 8) "subdir")
		(func (export "run_errno") (result i32)
			(local $fd i32) (local $errno i32)
			i32.const 3 i32.const 0 i32.const 8 i32.const 6 i32.const 2
			;; FD_NONBLOCK is accepted for a directory-only capability.
			i64.const 0 i64.const 0 i32.const 4 i32.const 72
			call $path_open local.set $errno
			(local.get $errno)
			(if (result i32)
				(then (local.get $errno))
				(else
					(i32.const 72 i32.load local.set $fd)
					(local.get $fd i32.const 80 call $fd_fdstat_get local.set $errno)
					(local.get $fd call $fd_close drop)
					(local.get $errno)
					(if (result i32)
						(then (local.get $errno))
						(else (i32.const 82 i32.load16_u)))))))`)
	if err != nil {
		t.Fatal(err)
	}
	module, err := engine.LoadModule(ctx, wasm)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Compile(ctx, nil); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "subdir"), 0700); err != nil {
		t.Fatal(err)
	}
	capability := newDirectoryRecordingCapability(t, root)
	instance, err := module.InstantiateWithConfig(ctx, &InstanceConfig{Mounts: []Mount{{Guest: "/data", FS: capability}}})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close(ctx)
	dirCallsBefore := capability.dirCalls.Load()
	openCallsBefore := capability.openCalls.Load()
	closesBefore := capability.closes.Load()
	fn := instance.instance.ExportedFunction("run_errno")
	if fn == nil {
		t.Fatal("run_errno was not exported")
	}
	results, err := fn.Call(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// fd_fdstat_get must retain the accepted FD_NONBLOCK bit.
	if len(results) != 1 || results[0] != 4 {
		t.Fatalf("guest directory fd flags = %#v, want FD_NONBLOCK (4)", results)
	}
	if got := capability.dirCalls.Load() - dirCallsBefore; got != 1 {
		t.Fatalf("guest OpenDirectory calls = %d, want 1", got)
	}
	if name, noFollow := capability.directoryOpen(); name != "subdir" || !noFollow {
		t.Fatalf("guest OpenDirectory args = (%q, %v), want (subdir, true)", name, noFollow)
	}
	// Wazero lazily opens the preopened root as "." while resolving fd 3.
	// The requested child must still use OpenDirectory, as checked above.
	if got := capability.openCalls.Load() - openCallsBefore; got != 1 {
		t.Fatalf("preopened-root OpenFile calls = %d, want 1", got)
	}
	capability.mu.Lock()
	openName := capability.openName
	capability.mu.Unlock()
	if openName != "." {
		t.Fatalf("ordinary OpenFile path = %q, want preopened root .", openName)
	}
	if got := capability.closes.Load() - closesBefore; got != 1 {
		t.Fatalf("guest directory descriptor close count = %d, want 1", got)
	}
}
