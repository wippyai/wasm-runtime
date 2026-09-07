package engine

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/tetratelabs/wazero"
	experimentalsys "github.com/tetratelabs/wazero/experimental/sys"
	wazerosysfs "github.com/tetratelabs/wazero/experimental/sysfs"
	wazerosys "github.com/tetratelabs/wazero/sys"
)

// Capability interfaces deliberately use only standard Go types. Runtime
// integrations can adapt their own filesystem capability without exposing the
// experimental Wazero API through Mount.
type openFileCapability interface {
	OpenFile(string, int, fs.FileMode) (fs.File, error)
}

// openFileNoFollowCapability opens a path without following its final
// component atomically. Implementations are responsible for adding the
// platform-specific no-follow flag to their capability-relative open.
type openFileNoFollowCapability interface {
	OpenFileNoFollow(string, int, fs.FileMode) (fs.File, error)
}

// openDirectoryCapability opens a directory relative to its capability. It
// must enforce noFollow atomically when noFollow is true.
type openDirectoryCapability interface {
	OpenDirectory(string, bool) (fs.File, error)
}

type statCapability interface {
	Stat(string) (fs.FileInfo, error)
}
type lstatCapability interface {
	Lstat(string) (fs.FileInfo, error)
}
type mkdirCapability interface {
	Mkdir(string, fs.FileMode) error
}
type renameCapability interface {
	Rename(string, string) error
}

// capabilityMountFS is the Wazero-side view of a registered filesystem. It
// never derives a host path: every operation goes back through fsys.
type capabilityMountFS struct {
	experimentalsys.UnimplementedFS
	fsys fs.FS
}

func withCapabilityFSMount(cfg wazero.FSConfig, fsys fs.FS, guest string, readOnly bool) wazero.FSConfig {
	sysCfg := cfg.(wazerosysfs.FSConfig)
	base := &capabilityMountFS{fsys: fsys}
	if !readOnly {
		return sysCfg.WithSysFSMount(base, guest)
	}
	return sysCfg.WithSysFSMount(&readOnlyMountFS{
		ReadFS: &wazerosysfs.ReadFS{FS: base},
	}, guest)
}

func (f *capabilityMountFS) OpenFile(name string, flag experimentalsys.Oflag, perm fs.FileMode) (experimentalsys.File, experimentalsys.Errno) {
	name, errno := cleanCapabilityPath(name)
	if errno != 0 {
		return nil, errno
	}
	adaptedName := capabilityAdaptPath(name)
	requiresDirectory := flag&experimentalsys.O_DIRECTORY != 0 || capabilityPathNeedsDirectory(name)
	openFlag, errno := capabilityOpenFlag(flag, requiresDirectory)
	if errno != 0 {
		return nil, errno
	}

	if requiresDirectory {
		if flag&experimentalsys.O_DIRECTORY != 0 {
			if errno := capabilityDirectoryOpenErr(flag); errno != 0 {
				return nil, errno
			}
		} else if capabilityWriteIntent(flag) {
			// The trailing slash requires an atomic directory open, but this
			// capability has no mutation operation for that directory handle.
			return nil, experimentalsys.ENOSYS
		}
		opener, ok := f.fsys.(openDirectoryCapability)
		if !ok {
			return nil, experimentalsys.ENOSYS
		}
		file, errno := (&wazerosysfs.AdaptFS{FS: capabilityOpener{
			name: adaptedName,
			open: func() (fs.File, error) {
				return opener.OpenDirectory(name, flag&experimentalsys.O_NOFOLLOW != 0)
			},
		}}).OpenFile(name, flag, perm)
		if errno != 0 {
			return nil, errno
		}
		return newDirectoryDescriptor(file, flag&experimentalsys.O_NONBLOCK != 0), 0
	}

	if flag&experimentalsys.O_NOFOLLOW != 0 {
		opener, ok := f.fsys.(openFileNoFollowCapability)
		if !ok {
			// An Lstat followed by OpenFile is not an implementation of
			// O_NOFOLLOW: the final component could change between calls.
			return nil, experimentalsys.ENOSYS
		}
		return (&wazerosysfs.AdaptFS{FS: capabilityOpener{
			name: adaptedName,
			open: func() (fs.File, error) {
				return opener.OpenFileNoFollow(name, openFlag, perm)
			},
		}}).OpenFile(name, flag, perm)
	}

	if opener, ok := f.fsys.(openFileCapability); ok {
		// AdaptFS supplies Wazero's complete public sys.File wrapper. The opener
		// is immutable per call, so reopen-for-Readdir retains this descriptor's
		// original path, flags, and permissions without shared mutable state.
		return (&wazerosysfs.AdaptFS{FS: capabilityOpener{
			name: adaptedName,
			open: func() (fs.File, error) {
				return opener.OpenFile(name, openFlag, perm)
			},
		}}).OpenFile(name, flag, perm)
	}
	if openFlag != os.O_RDONLY {
		return nil, experimentalsys.ENOSYS
	}
	return (&wazerosysfs.AdaptFS{FS: capabilityOpener{
		name: adaptedName,
		open: func() (fs.File, error) {
			return f.fsys.Open(name)
		},
	}}).OpenFile(name, flag, perm)
}

func (f *capabilityMountFS) Stat(name string) (wazerosys.Stat_t, experimentalsys.Errno) {
	name, errno := cleanCapabilityPath(name)
	if errno != 0 {
		return wazerosys.Stat_t{}, errno
	}
	if statter, ok := f.fsys.(statCapability); ok {
		info, err := statter.Stat(name)
		if err != nil {
			return wazerosys.Stat_t{}, capabilityErrno(err)
		}
		return wazerosys.NewStat_t(info), 0
	}
	file, errno := f.OpenFile(name, experimentalsys.O_RDONLY, 0)
	if errno != 0 {
		return wazerosys.Stat_t{}, errno
	}
	defer file.Close()
	return file.Stat()
}

func (f *capabilityMountFS) Lstat(name string) (wazerosys.Stat_t, experimentalsys.Errno) {
	name, errno := cleanCapabilityPath(name)
	if errno != 0 {
		return wazerosys.Stat_t{}, errno
	}
	lstat, ok := f.fsys.(lstatCapability)
	if !ok {
		return wazerosys.Stat_t{}, experimentalsys.ENOSYS
	}
	info, err := lstat.Lstat(name)
	if err != nil {
		return wazerosys.Stat_t{}, capabilityErrno(err)
	}
	return wazerosys.NewStat_t(info), 0
}

func (f *capabilityMountFS) Mkdir(name string, perm fs.FileMode) experimentalsys.Errno {
	name, errno := cleanCapabilityPath(name)
	if errno != 0 {
		return errno
	}
	maker, ok := f.fsys.(mkdirCapability)
	if !ok {
		return experimentalsys.ENOSYS
	}
	return capabilityErrno(maker.Mkdir(name, perm))
}

func (f *capabilityMountFS) Rename(from, to string) experimentalsys.Errno {
	from, errno := cleanCapabilityPath(from)
	if errno != 0 {
		return errno
	}
	to, errno = cleanCapabilityPath(to)
	if errno != 0 {
		return errno
	}
	renamer, ok := f.fsys.(renameCapability)
	if !ok {
		return experimentalsys.ENOSYS
	}
	return capabilityErrno(renamer.Rename(from, to))
}

// readOnlyMountFS is stricter than Wazero's generic fs.FS adapter: creation,
// truncation, append and write-intent opens all fail as EROFS before reaching
// a possibly writer-capable fs.File.
type readOnlyMountFS struct{ *wazerosysfs.ReadFS }

func (f *readOnlyMountFS) OpenFile(name string, flag experimentalsys.Oflag, perm fs.FileMode) (experimentalsys.File, experimentalsys.Errno) {
	if flag&experimentalsys.O_DIRECTORY != 0 {
		if errno := capabilityDirectoryOpenErr(flag); errno != 0 {
			return nil, errno
		}
	}
	if flag&(experimentalsys.O_WRONLY|experimentalsys.O_RDWR|experimentalsys.O_APPEND|experimentalsys.O_CREAT|experimentalsys.O_EXCL|experimentalsys.O_TRUNC) != 0 {
		return nil, experimentalsys.EROFS
	}
	return f.ReadFS.OpenFile(name, flag, perm)
}

func (f *readOnlyMountFS) Readlink(name string) (string, experimentalsys.Errno) {
	if _, errno := f.Stat(name); errno != 0 {
		return "", errno
	}
	return "", experimentalsys.EINVAL
}

type capabilityOpener struct {
	open func() (fs.File, error)
	name string
}

// directoryDescriptor owns WASI descriptor flags after AdaptFS has preserved
// the provider's actual file interfaces. Wrapping fs.File earlier would either
// erase optional methods or advertise methods the provider does not implement.
// All directory I/O and closure remain delegated to the same adapted descriptor.
type directoryDescriptor struct {
	experimentalsys.File
	nonblock atomic.Bool
}

func newDirectoryDescriptor(file experimentalsys.File, enabled bool) *directoryDescriptor {
	d := &directoryDescriptor{File: file}
	d.nonblock.Store(enabled)
	return d
}

func (d *directoryDescriptor) IsNonblock() bool { return d.nonblock.Load() }

func (d *directoryDescriptor) SetNonblock(enabled bool) experimentalsys.Errno {
	if _, errno := d.Stat(); errno != 0 {
		return errno
	}
	d.nonblock.Store(enabled)
	return 0
}

func (d *directoryDescriptor) Poll(flag experimentalsys.Pflag, timeoutMillis int32) (bool, experimentalsys.Errno) {
	if file, ok := d.File.(experimentalsys.Pollable); ok {
		return file.Poll(flag, timeoutMillis)
	}
	return false, experimentalsys.ENOSYS
}

func (o capabilityOpener) Open(name string) (fs.File, error) {
	if name != o.name {
		return nil, experimentalsys.EPERM
	}
	file, err := o.open()
	if err != nil {
		// AdaptFS unwraps only its immediate error. Convert every capability
		// error here so nested PathError and joined errors retain their errno.
		return nil, capabilityErrno(err)
	}
	return file, nil
}

func cleanCapabilityPath(name string) (string, experimentalsys.Errno) {
	if name == "" {
		return ".", 0
	}
	if strings.HasPrefix(name, "/") || strings.ContainsRune(name, '\x00') {
		return "", experimentalsys.EPERM
	}

	// Keep every relative component intact. Whether .. escapes depends on
	// symlink expansion, so a lexical depth check would reject valid paths such
	// as link/../../file when link expands below the mounted root. Confinement
	// belongs to the registered capability (for example, os.Root), which sees
	// and resolves the original path component by component.
	return name, 0
}

// capabilityAdaptPath is only the name used by Wazero's AdaptFS. The opener
// closure retains the original capability path, so its internal path.Clean
// cannot alter the provider operation.
func capabilityAdaptPath(name string) string { return path.Clean(name) }

func capabilityPathNeedsDirectory(name string) bool { return strings.HasSuffix(name, "/") }

func capabilityWriteIntent(flag experimentalsys.Oflag) bool {
	return flag&(experimentalsys.O_WRONLY|experimentalsys.O_RDWR|experimentalsys.O_APPEND|experimentalsys.O_CREAT|experimentalsys.O_EXCL|experimentalsys.O_TRUNC) != 0
}

func capabilityOpenFlag(flag experimentalsys.Oflag, directoryOpen bool) (int, experimentalsys.Errno) {
	const known = experimentalsys.O_RDWR | experimentalsys.O_WRONLY |
		experimentalsys.O_APPEND | experimentalsys.O_CREAT | experimentalsys.O_DIRECTORY |
		experimentalsys.O_DSYNC | experimentalsys.O_EXCL | experimentalsys.O_NOFOLLOW |
		experimentalsys.O_NONBLOCK | experimentalsys.O_RSYNC | experimentalsys.O_SYNC |
		experimentalsys.O_TRUNC
	const unsupported = experimentalsys.O_DSYNC | experimentalsys.O_RSYNC | experimentalsys.O_SYNC

	if flag&^known != 0 || flag&unsupported != 0 {
		return 0, experimentalsys.ENOSYS
	}
	if flag&experimentalsys.O_NONBLOCK != 0 && !directoryOpen {
		return 0, experimentalsys.ENOSYS
	}
	// The directory capability returns a directory-only descriptor. Linux's
	// O_NONBLOCK has no directory-specific operation to preserve here, while
	// forwarding it to a generic file open could change FIFO/device behavior.
	// Therefore, it is accepted only on this atomic directory-open route.

	var result int
	switch flag & (experimentalsys.O_RDWR | experimentalsys.O_WRONLY) {
	case experimentalsys.O_RDONLY:
		result = os.O_RDONLY
	case experimentalsys.O_WRONLY:
		result = os.O_WRONLY
	case experimentalsys.O_RDWR:
		result = os.O_RDWR
	default:
		return 0, experimentalsys.EINVAL
	}
	for _, pair := range []struct {
		sys experimentalsys.Oflag
		os  int
	}{
		{experimentalsys.O_APPEND, os.O_APPEND},
		{experimentalsys.O_CREAT, os.O_CREATE},
		{experimentalsys.O_EXCL, os.O_EXCL},
		{experimentalsys.O_TRUNC, os.O_TRUNC},
	} {
		if flag&pair.sys != 0 {
			result |= pair.os
		}
	}
	return result, 0
}

// capabilityDirectoryOpenErr rejects mutation before dispatching an atomic
// directory open. Wazero's pathOpenFn returns EINVAL for O_DIRECTORY|O_CREAT,
// while its sysfs.OpenFSFile returns EISDIR for a writable directory open.
func capabilityDirectoryOpenErr(flag experimentalsys.Oflag) experimentalsys.Errno {
	if flag&experimentalsys.O_CREAT != 0 {
		return experimentalsys.EINVAL
	}
	if flag&(experimentalsys.O_WRONLY|experimentalsys.O_RDWR|experimentalsys.O_APPEND|experimentalsys.O_TRUNC) != 0 {
		return experimentalsys.EISDIR
	}
	return 0
}

func capabilityErrno(err error) experimentalsys.Errno {
	if err == nil {
		return 0
	}
	// Check standard sentinel chains before falling back to Wazero's direct
	// unwrapping, which intentionally only recognizes a single PathError.
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return experimentalsys.ENOENT
	case errors.Is(err, fs.ErrExist):
		return experimentalsys.EEXIST
	case errors.Is(err, fs.ErrPermission):
		return experimentalsys.EPERM
	case errors.Is(err, fs.ErrInvalid):
		return experimentalsys.EINVAL
	case errors.Is(err, fs.ErrClosed):
		return experimentalsys.EBADF
	case errors.Is(err, errors.ErrUnsupported):
		return experimentalsys.ENOSYS
	}
	var errno experimentalsys.Errno
	if errors.As(err, &errno) {
		return errno
	}
	var syscallErrno syscall.Errno
	if errors.As(err, &syscallErrno) {
		return experimentalsys.UnwrapOSError(syscallErrno)
	}
	return experimentalsys.UnwrapOSError(err)
}
