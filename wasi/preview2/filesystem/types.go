package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type TypesHost struct {
	resources *preview2.ResourceTable
}

func NewTypesHost(resources *preview2.ResourceTable) *TypesHost {
	return &TypesHost{resources: resources}
}

func (h *TypesHost) Namespace() string {
	return "wasi:filesystem/types@0.2.3"
}

type Error struct {
	Code ErrorCode
}

type ErrorCode uint8

const (
	ErrorAccess ErrorCode = iota
	ErrorWouldBlock
	ErrorAlready
	ErrorBadDescriptor
	ErrorBusy
	ErrorDeadlock
	ErrorQuota
	ErrorExist
	ErrorFileTooLarge
	ErrorIllegalByteSequence
	ErrorInProgress
	ErrorInterrupted
	ErrorInvalid
	ErrorIo
	ErrorIsDirectory
	ErrorLoop
	ErrorTooManyLinks
	ErrorMessageSize
	ErrorNameTooLong
	ErrorNoDevice
	ErrorNoEntry
	ErrorNoLock
	ErrorInsufficientMemory
	ErrorInsufficientSpace
	ErrorNotDirectory
	ErrorNotEmpty
	ErrorNotRecoverable
	ErrorUnsupported
	ErrorNoTty
	ErrorNoSuchDevice
	ErrorOverflow
	ErrorNotPermitted
	ErrorPipe
	ErrorReadOnly
	ErrorInvalidSeek
	ErrorTextFileBusy
	ErrorCrossDevice
)

func (e *Error) Error() string {
	return "filesystem error"
}

func mapOSError(err error) *Error {
	if err == nil {
		return nil
	}
	if os.IsNotExist(err) {
		return &Error{Code: ErrorNoEntry}
	}
	if os.IsPermission(err) {
		return &Error{Code: ErrorAccess}
	}
	if os.IsExist(err) {
		return &Error{Code: ErrorExist}
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		var errno syscall.Errno
		if errors.As(pathErr.Err, &errno) {
			return mapErrno(errno)
		}
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		var errno syscall.Errno
		if errors.As(linkErr.Err, &errno) {
			return mapErrno(errno)
		}
	}
	return &Error{Code: ErrorIo}
}

func mapErrno(errno syscall.Errno) *Error {
	switch errno {
	case syscall.EACCES, syscall.EPERM:
		return &Error{Code: ErrorAccess}
	case syscall.ENOENT:
		return &Error{Code: ErrorNoEntry}
	case syscall.EEXIST:
		return &Error{Code: ErrorExist}
	case syscall.ENOTDIR:
		return &Error{Code: ErrorNotDirectory}
	case syscall.EISDIR:
		return &Error{Code: ErrorIsDirectory}
	case syscall.ENOTEMPTY:
		return &Error{Code: ErrorNotEmpty}
	case syscall.ENAMETOOLONG:
		return &Error{Code: ErrorNameTooLong}
	case syscall.ENOSPC:
		return &Error{Code: ErrorInsufficientSpace}
	case syscall.EROFS:
		return &Error{Code: ErrorReadOnly}
	case syscall.EXDEV:
		return &Error{Code: ErrorCrossDevice}
	case syscall.ELOOP:
		return &Error{Code: ErrorLoop}
	case syscall.EMLINK:
		return &Error{Code: ErrorTooManyLinks}
	case syscall.EBUSY:
		return &Error{Code: ErrorBusy}
	case syscall.EINVAL:
		return &Error{Code: ErrorInvalid}
	default:
		return &Error{Code: ErrorIo}
	}
}

type DescriptorType uint8

const (
	DescriptorTypeUnknown DescriptorType = iota
	DescriptorTypeBlockDevice
	DescriptorTypeCharacterDevice
	DescriptorTypeDirectory
	DescriptorTypeFifo
	DescriptorTypeSymbolicLink
	DescriptorTypeRegularFile
	DescriptorTypeSocket
)

func (h *TypesHost) getDescriptor(handle uint32) (*preview2.DescriptorResource, *Error) {
	r, ok := h.resources.Get(handle)
	if !ok {
		return nil, &Error{Code: ErrorBadDescriptor}
	}
	desc, ok := r.(*preview2.DescriptorResource)
	if !ok {
		return nil, &Error{Code: ErrorBadDescriptor}
	}
	return desc, nil
}

// resolvePath resolves a path relative to a descriptor. Returns error if path escapes the sandbox.
func (h *TypesHost) resolvePath(desc *preview2.DescriptorResource, path string) (string, *Error) {
	if filepath.IsAbs(path) {
		return "", &Error{Code: ErrorAccess}
	}
	fullPath := filepath.Join(desc.Path(), path)
	fullPath = filepath.Clean(fullPath)

	// Ensure path doesn't escape the descriptor's directory
	rel, err := filepath.Rel(desc.Path(), fullPath)
	if err != nil || len(rel) > 0 && rel[0] == '.' {
		return "", &Error{Code: ErrorAccess}
	}
	return fullPath, nil
}

func (h *TypesHost) FilesystemErrorCode(_ context.Context, err *Error) ErrorCode {
	if err == nil {
		return ErrorIo
	}
	return err.Code
}

func (h *TypesHost) MethodDescriptorRead(_ context.Context, self uint32, length uint64, offset uint64) ([]byte, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return nil, err
	}

	if desc.IsDir() {
		return nil, &Error{Code: ErrorIsDirectory}
	}

	f, osErr := os.Open(desc.Path())
	if osErr != nil {
		return nil, mapOSError(osErr)
	}
	defer f.Close()

	if offset > 0 {
		_, osErr = f.Seek(int64(offset), 0)
		if osErr != nil {
			return nil, mapOSError(osErr)
		}
	}

	// Limit allocation size to prevent DoS
	if length > preview2.MaxAllocationSize {
		length = preview2.MaxAllocationSize
	}

	buf := make([]byte, length)
	n, osErr := f.Read(buf)
	if osErr != nil && n == 0 {
		return nil, mapOSError(osErr)
	}

	return buf[:n], nil
}

func (h *TypesHost) MethodDescriptorWrite(_ context.Context, self uint32, buffer []byte, offset uint64) (uint64, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	if desc.ReadOnly() {
		return 0, &Error{Code: ErrorReadOnly}
	}

	if desc.IsDir() {
		return 0, &Error{Code: ErrorIsDirectory}
	}

	f, osErr := os.OpenFile(desc.Path(), os.O_WRONLY, 0)
	if osErr != nil {
		return 0, mapOSError(osErr)
	}
	defer f.Close()

	n, osErr := f.WriteAt(buffer, int64(offset))
	if osErr != nil {
		return uint64(n), mapOSError(osErr)
	}

	return uint64(n), nil
}

func (h *TypesHost) MethodDescriptorGetType(_ context.Context, self uint32) (DescriptorType, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return DescriptorTypeUnknown, err
	}

	info, osErr := os.Lstat(desc.Path())
	if osErr != nil {
		return DescriptorTypeUnknown, mapOSError(osErr)
	}

	return fileInfoToDescriptorType(info), nil
}

func fileInfoToDescriptorType(info os.FileInfo) DescriptorType {
	mode := info.Mode()
	switch {
	case mode.IsDir():
		return DescriptorTypeDirectory
	case mode.IsRegular():
		return DescriptorTypeRegularFile
	case mode&os.ModeSymlink != 0:
		return DescriptorTypeSymbolicLink
	case mode&os.ModeNamedPipe != 0:
		return DescriptorTypeFifo
	case mode&os.ModeSocket != 0:
		return DescriptorTypeSocket
	case mode&os.ModeDevice != 0:
		if mode&os.ModeCharDevice != 0 {
			return DescriptorTypeCharacterDevice
		}
		return DescriptorTypeBlockDevice
	default:
		return DescriptorTypeUnknown
	}
}

func (h *TypesHost) MethodDescriptorStat(_ context.Context, self uint32) (*DescriptorStat, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return nil, err
	}

	info, osErr := os.Stat(desc.Path())
	if osErr != nil {
		return nil, mapOSError(osErr)
	}

	return &DescriptorStat{
		Type: fileInfoToDescriptorType(info),
		Size: uint64(info.Size()),
	}, nil
}

type DescriptorStat struct {
	Type DescriptorType
	Size uint64
}

func (h *TypesHost) MethodDescriptorSeek(_ context.Context, self uint32, offset int64, whence uint8) (uint64, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	if desc.IsDir() {
		return 0, &Error{Code: ErrorIsDirectory}
	}

	var newPosition int64
	switch whence {
	case 0: // Set - absolute position
		newPosition = offset
	case 1: // Cur - relative to current position
		newPosition = desc.Position() + offset
	case 2: // End - relative to file end
		info, osErr := os.Stat(desc.Path())
		if osErr != nil {
			return 0, mapOSError(osErr)
		}
		newPosition = info.Size() + offset
	default:
		return 0, &Error{Code: ErrorInvalid}
	}

	if newPosition < 0 {
		return 0, &Error{Code: ErrorInvalid}
	}

	desc.SetPosition(newPosition)
	return uint64(newPosition), nil
}

func (h *TypesHost) MethodDescriptorGetFlags(_ context.Context, _ uint32) (uint32, *Error) {
	return 0, nil
}

func (h *TypesHost) MethodDescriptorOpenAt(_ context.Context, self uint32, _ uint32, path string, openFlags uint32, _ uint32) (uint32, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	fullPath, err := h.resolvePath(desc, path)
	if err != nil {
		return 0, err
	}

	// Check if create flag is set
	createFlag := (openFlags & 1) != 0

	info, osErr := os.Stat(fullPath)
	if osErr != nil {
		if os.IsNotExist(osErr) && createFlag && !desc.ReadOnly() {
			// Create file
			f, createErr := os.Create(fullPath)
			if createErr != nil {
				return 0, mapOSError(createErr)
			}
			f.Close()
			info, osErr = os.Stat(fullPath)
			if osErr != nil {
				return 0, mapOSError(osErr)
			}
		} else {
			return 0, mapOSError(osErr)
		}
	}

	newDesc := preview2.NewDescriptorResource(fullPath, info.IsDir(), desc.ReadOnly())
	handle := h.resources.Add(newDesc)
	return handle, nil
}

func (h *TypesHost) MethodDescriptorCreateDirectoryAt(_ context.Context, self uint32, path string) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if desc.ReadOnly() {
		return &Error{Code: ErrorReadOnly}
	}

	fullPath, err := h.resolvePath(desc, path)
	if err != nil {
		return err
	}

	osErr := os.Mkdir(fullPath, 0755)
	if osErr != nil {
		return mapOSError(osErr)
	}

	return nil
}

func (h *TypesHost) MethodDescriptorReadDirectory(_ context.Context, self uint32) (uint32, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	if !desc.IsDir() {
		return 0, &Error{Code: ErrorNotDirectory}
	}

	entries, osErr := os.ReadDir(desc.Path())
	if osErr != nil {
		return 0, mapOSError(osErr)
	}

	dirEntries := make([]preview2.DirectoryEntry, 0, len(entries))
	for _, entry := range entries {
		// entry.Info() may fail for various reasons (permissions, race conditions, etc.)
		// Fall back to basic type detection from IsDir() if info is unavailable.
		info, _ := entry.Info()
		var dtype uint8
		if info != nil {
			dtype = uint8(fileInfoToDescriptorType(info))
		} else if entry.IsDir() {
			dtype = uint8(DescriptorTypeDirectory)
		} else {
			dtype = uint8(DescriptorTypeRegularFile)
		}
		dirEntries = append(dirEntries, preview2.DirectoryEntry{
			Type: dtype,
			Name: entry.Name(),
		})
	}

	stream := preview2.NewDirectoryEntryStreamResource(dirEntries)
	handle := h.resources.Add(stream)
	return handle, nil
}

func (h *TypesHost) MethodDescriptorSync(_ context.Context, _ uint32) *Error {
	return nil
}

func (h *TypesHost) MethodDescriptorSyncData(_ context.Context, _ uint32) *Error {
	return nil
}

func (h *TypesHost) MethodDescriptorReadViaStream(_ context.Context, self uint32, offset uint64) (uint32, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	if desc.IsDir() {
		return 0, &Error{Code: ErrorIsDirectory}
	}

	data, osErr := os.ReadFile(desc.Path())
	if osErr != nil {
		return 0, mapOSError(osErr)
	}

	if offset > uint64(len(data)) {
		data = nil
	} else {
		data = data[offset:]
	}

	stream := preview2.NewInputStreamResource(data)
	handle := h.resources.Add(stream)
	return handle, nil
}

func (h *TypesHost) MethodDescriptorWriteViaStream(_ context.Context, self uint32, offset uint64) (uint32, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}
	if desc.IsDir() {
		return 0, &Error{Code: ErrorIsDirectory}
	}

	stream, osErr := preview2.NewFileOutputStreamResource(desc.Path(), int64(offset), false)
	if osErr != nil {
		return 0, mapOSError(osErr)
	}

	handle := h.resources.Add(stream)
	return handle, nil
}

func (h *TypesHost) MethodDescriptorAppendViaStream(_ context.Context, self uint32) (uint32, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}
	if desc.IsDir() {
		return 0, &Error{Code: ErrorIsDirectory}
	}

	stream, osErr := preview2.NewFileOutputStreamResource(desc.Path(), 0, true)
	if osErr != nil {
		return 0, mapOSError(osErr)
	}

	handle := h.resources.Add(stream)
	return handle, nil
}

func (h *TypesHost) MethodDescriptorMetadataHash(_ context.Context, self uint32) (uint64, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	info, osErr := os.Stat(desc.Path())
	if osErr != nil {
		return 0, mapOSError(osErr)
	}

	// Simple hash based on size and modification time
	hash := uint64(info.Size()) ^ uint64(info.ModTime().UnixNano())
	return hash, nil
}

func (h *TypesHost) MethodDescriptorMetadataHashAt(_ context.Context, self uint32, _ uint32, path string) (uint64, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return 0, err
	}

	fullPath, err := h.resolvePath(desc, path)
	if err != nil {
		return 0, err
	}

	info, osErr := os.Stat(fullPath)
	if osErr != nil {
		return 0, mapOSError(osErr)
	}

	hash := uint64(info.Size()) ^ uint64(info.ModTime().UnixNano())
	return hash, nil
}

func (h *TypesHost) MethodDescriptorRenameAt(_ context.Context, self uint32, oldPath string, newDescriptor uint32, newPath string) *Error {
	oldDesc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if oldDesc.ReadOnly() {
		return &Error{Code: ErrorReadOnly}
	}

	newDesc, err := h.getDescriptor(newDescriptor)
	if err != nil {
		return err
	}

	if newDesc.ReadOnly() {
		return &Error{Code: ErrorReadOnly}
	}

	oldFullPath, err := h.resolvePath(oldDesc, oldPath)
	if err != nil {
		return err
	}

	newFullPath, err := h.resolvePath(newDesc, newPath)
	if err != nil {
		return err
	}

	osErr := os.Rename(oldFullPath, newFullPath)
	if osErr != nil {
		return mapOSError(osErr)
	}

	return nil
}

func (h *TypesHost) MethodDescriptorUnlinkFileAt(_ context.Context, self uint32, path string) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if desc.ReadOnly() {
		return &Error{Code: ErrorReadOnly}
	}

	fullPath, err := h.resolvePath(desc, path)
	if err != nil {
		return err
	}

	info, osErr := os.Lstat(fullPath)
	if osErr != nil {
		return mapOSError(osErr)
	}

	if info.IsDir() {
		return &Error{Code: ErrorIsDirectory}
	}

	osErr = os.Remove(fullPath)
	if osErr != nil {
		return mapOSError(osErr)
	}

	return nil
}

func (h *TypesHost) MethodDescriptorRemoveDirectoryAt(_ context.Context, self uint32, path string) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if desc.ReadOnly() {
		return &Error{Code: ErrorReadOnly}
	}

	fullPath, err := h.resolvePath(desc, path)
	if err != nil {
		return err
	}

	info, osErr := os.Lstat(fullPath)
	if osErr != nil {
		return mapOSError(osErr)
	}

	if !info.IsDir() {
		return &Error{Code: ErrorNotDirectory}
	}

	osErr = os.Remove(fullPath)
	if osErr != nil {
		return mapOSError(osErr)
	}

	return nil
}

func (h *TypesHost) MethodDescriptorStatAt(_ context.Context, self uint32, pathFlags uint32, path string) (*DescriptorStat, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return nil, err
	}

	fullPath, err := h.resolvePath(desc, path)
	if err != nil {
		return nil, err
	}

	var info os.FileInfo
	var osErr error
	if pathFlags&1 != 0 { // symlink-follow flag
		info, osErr = os.Stat(fullPath)
	} else {
		info, osErr = os.Lstat(fullPath)
	}
	if osErr != nil {
		return nil, mapOSError(osErr)
	}

	return &DescriptorStat{
		Type: fileInfoToDescriptorType(info),
		Size: uint64(info.Size()),
	}, nil
}

func (h *TypesHost) MethodDescriptorSymlinkAt(_ context.Context, self uint32, oldPath string, newPath string) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if desc.ReadOnly() {
		return &Error{Code: ErrorReadOnly}
	}

	// Validate symlink target - must be relative and not escape sandbox
	// Clean and normalize the path first
	cleanPath := filepath.Clean(oldPath)

	// Block absolute paths
	if filepath.IsAbs(cleanPath) {
		return &Error{Code: ErrorAccess}
	}

	// Block paths that start with or contain .. after normalization
	// This catches attempts to escape the sandbox
	if cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) ||
		strings.Contains(cleanPath, string(filepath.Separator)+".."+string(filepath.Separator)) ||
		strings.HasSuffix(cleanPath, string(filepath.Separator)+"..") {
		return &Error{Code: ErrorAccess}
	}

	fullNewPath, err := h.resolvePath(desc, newPath)
	if err != nil {
		return err
	}

	osErr := os.Symlink(oldPath, fullNewPath)
	if osErr != nil {
		return mapOSError(osErr)
	}

	return nil
}

func (h *TypesHost) MethodDescriptorReadlinkAt(_ context.Context, self uint32, path string) (string, *Error) {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return "", err
	}

	fullPath, err := h.resolvePath(desc, path)
	if err != nil {
		return "", err
	}

	target, osErr := os.Readlink(fullPath)
	if osErr != nil {
		return "", mapOSError(osErr)
	}

	// Validate returned symlink target - reject absolute paths and paths escaping sandbox
	cleanTarget := filepath.Clean(target)
	if filepath.IsAbs(cleanTarget) {
		return "", &Error{Code: ErrorAccess}
	}
	if cleanTarget == ".." || strings.HasPrefix(cleanTarget, ".."+string(filepath.Separator)) {
		return "", &Error{Code: ErrorAccess}
	}

	return target, nil
}

func (h *TypesHost) MethodDescriptorLinkAt(_ context.Context, self uint32, _ uint32, oldPath string, newDescriptor uint32, newPath string) *Error {
	oldDesc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	newDesc, err := h.getDescriptor(newDescriptor)
	if err != nil {
		return err
	}

	if newDesc.ReadOnly() {
		return &Error{Code: ErrorReadOnly}
	}

	oldFullPath, err := h.resolvePath(oldDesc, oldPath)
	if err != nil {
		return err
	}

	newFullPath, err := h.resolvePath(newDesc, newPath)
	if err != nil {
		return err
	}

	osErr := os.Link(oldFullPath, newFullPath)
	if osErr != nil {
		return mapOSError(osErr)
	}

	return nil
}

func (h *TypesHost) MethodDescriptorSetTimes(_ context.Context, self uint32, dataAccessTimestamp uint64, dataModificationTimestamp uint64) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if desc.ReadOnly() {
		return &Error{Code: ErrorReadOnly}
	}

	atime := time.Unix(0, int64(dataAccessTimestamp))
	mtime := time.Unix(0, int64(dataModificationTimestamp))

	osErr := os.Chtimes(desc.Path(), atime, mtime)
	if osErr != nil {
		return mapOSError(osErr)
	}

	return nil
}

func (h *TypesHost) MethodDescriptorSetTimesAt(_ context.Context, self uint32, _ uint32, path string, dataAccessTimestamp uint64, dataModificationTimestamp uint64) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if desc.ReadOnly() {
		return &Error{Code: ErrorReadOnly}
	}

	fullPath, err := h.resolvePath(desc, path)
	if err != nil {
		return err
	}

	atime := time.Unix(0, int64(dataAccessTimestamp))
	mtime := time.Unix(0, int64(dataModificationTimestamp))

	osErr := os.Chtimes(fullPath, atime, mtime)
	if osErr != nil {
		return mapOSError(osErr)
	}

	return nil
}

func (h *TypesHost) MethodDescriptorSetSize(_ context.Context, self uint32, size uint64) *Error {
	desc, err := h.getDescriptor(self)
	if err != nil {
		return err
	}

	if desc.ReadOnly() {
		return &Error{Code: ErrorReadOnly}
	}

	if desc.IsDir() {
		return &Error{Code: ErrorIsDirectory}
	}

	osErr := os.Truncate(desc.Path(), int64(size))
	if osErr != nil {
		return mapOSError(osErr)
	}

	return nil
}

func (h *TypesHost) MethodDescriptorAdvise(_ context.Context, _ uint32, _ uint64, _ uint64, _ uint8) *Error {
	return nil
}

func (h *TypesHost) ResourceDropDescriptor(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

func (h *TypesHost) ResourceDropDirectoryEntryStream(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

func (h *TypesHost) MethodDescriptorIsSameObject(_ context.Context, self uint32, other uint32) bool {
	selfDesc, err := h.getDescriptor(self)
	if err != nil {
		return false
	}

	otherDesc, err := h.getDescriptor(other)
	if err != nil {
		return false
	}

	return selfDesc.Path() == otherDesc.Path()
}

func (h *TypesHost) MethodDirectoryEntryStreamReadDirectoryEntry(_ context.Context, self uint32) (*preview2.DirectoryEntry, *Error) {
	r, ok := h.resources.Get(self)
	if !ok {
		return nil, &Error{Code: ErrorBadDescriptor}
	}

	stream, ok := r.(*preview2.DirectoryEntryStreamResource)
	if !ok {
		return nil, &Error{Code: ErrorBadDescriptor}
	}

	entry := stream.ReadNext()
	if entry == nil {
		return nil, nil
	}

	return entry, nil
}

func (h *TypesHost) Register() map[string]any {
	return map[string]any{
		"filesystem-error-code": h.FilesystemErrorCode,
		// Descriptor methods
		"[method]descriptor.read":                h.MethodDescriptorRead,
		"[method]descriptor.write":               h.MethodDescriptorWrite,
		"[method]descriptor.get-type":            h.MethodDescriptorGetType,
		"[method]descriptor.stat":                h.MethodDescriptorStat,
		"[method]descriptor.stat-at":             h.MethodDescriptorStatAt,
		"[method]descriptor.seek":                h.MethodDescriptorSeek,
		"[method]descriptor.get-flags":           h.MethodDescriptorGetFlags,
		"[method]descriptor.open-at":             h.MethodDescriptorOpenAt,
		"[method]descriptor.create-directory-at": h.MethodDescriptorCreateDirectoryAt,
		"[method]descriptor.read-directory":      h.MethodDescriptorReadDirectory,
		"[method]descriptor.sync":                h.MethodDescriptorSync,
		"[method]descriptor.sync-data":           h.MethodDescriptorSyncData,
		"[method]descriptor.read-via-stream":     h.MethodDescriptorReadViaStream,
		"[method]descriptor.write-via-stream":    h.MethodDescriptorWriteViaStream,
		"[method]descriptor.append-via-stream":   h.MethodDescriptorAppendViaStream,
		"[method]descriptor.metadata-hash":       h.MethodDescriptorMetadataHash,
		"[method]descriptor.metadata-hash-at":    h.MethodDescriptorMetadataHashAt,
		"[method]descriptor.rename-at":           h.MethodDescriptorRenameAt,
		"[method]descriptor.unlink-file-at":      h.MethodDescriptorUnlinkFileAt,
		"[method]descriptor.remove-directory-at": h.MethodDescriptorRemoveDirectoryAt,
		"[method]descriptor.symlink-at":          h.MethodDescriptorSymlinkAt,
		"[method]descriptor.readlink-at":         h.MethodDescriptorReadlinkAt,
		"[method]descriptor.link-at":             h.MethodDescriptorLinkAt,
		"[method]descriptor.set-times":           h.MethodDescriptorSetTimes,
		"[method]descriptor.set-times-at":        h.MethodDescriptorSetTimesAt,
		"[method]descriptor.set-size":            h.MethodDescriptorSetSize,
		"[method]descriptor.advise":              h.MethodDescriptorAdvise,
		"[method]descriptor.is-same-object":      h.MethodDescriptorIsSameObject,
		// Directory entry stream methods
		"[method]directory-entry-stream.read-directory-entry": h.MethodDirectoryEntryStreamReadDirectoryEntry,
		// Resource drops
		"[resource-drop]descriptor":             h.ResourceDropDescriptor,
		"[resource-drop]directory-entry-stream": h.ResourceDropDirectoryEntryStream,
	}
}
