package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestTypesHost_MethodDescriptorGetType(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	fileDesc := preview2.NewDescriptorResource(testFile, false, true)
	fileHandle := resources.Add(fileDesc)

	dtype, err := host.MethodDescriptorGetType(ctx, fileHandle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dtype != DescriptorTypeRegularFile {
		t.Errorf("expected regular file, got %d", dtype)
	}

	dirDesc := preview2.NewDescriptorResource(tempDir, true, true)
	dirHandle := resources.Add(dirDesc)

	dtype, err = host.MethodDescriptorGetType(ctx, dirHandle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dtype != DescriptorTypeDirectory {
		t.Errorf("expected directory, got %d", dtype)
	}
}

func TestTypesHost_MethodDescriptorRead(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	testContent := []byte("hello world")

	err := os.WriteFile(testFile, testContent, 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	desc := preview2.NewDescriptorResource(testFile, false, true)
	handle := resources.Add(desc)

	data, fsErr := host.MethodDescriptorRead(ctx, handle, 5, 0)
	if fsErr != nil {
		t.Fatalf("unexpected error: %v", fsErr)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got '%s'", data)
	}

	data, fsErr = host.MethodDescriptorRead(ctx, handle, 5, 6)
	if fsErr != nil {
		t.Fatalf("unexpected error: %v", fsErr)
	}
	if string(data) != "world" {
		t.Errorf("expected 'world', got '%s'", data)
	}
}

func TestTypesHost_MethodDescriptorRead_Directory(t *testing.T) {
	tempDir := t.TempDir()

	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	desc := preview2.NewDescriptorResource(tempDir, true, true)
	handle := resources.Add(desc)

	_, fsErr := host.MethodDescriptorRead(ctx, handle, 10, 0)
	if fsErr == nil {
		t.Fatal("expected error when reading directory")
	}
	if fsErr.Code != ErrorIsDirectory {
		t.Errorf("expected ErrorIsDirectory, got %d", fsErr.Code)
	}
}

func TestTypesHost_MethodDescriptorStat(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	testContent := []byte("hello world")

	err := os.WriteFile(testFile, testContent, 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	desc := preview2.NewDescriptorResource(testFile, false, true)
	handle := resources.Add(desc)

	stat, fsErr := host.MethodDescriptorStat(ctx, handle)
	if fsErr != nil {
		t.Fatalf("unexpected error: %v", fsErr)
	}
	if stat.Type != DescriptorTypeRegularFile {
		t.Errorf("expected regular file type, got %d", stat.Type)
	}
	if stat.Size != uint64(len(testContent)) {
		t.Errorf("expected size %d, got %d", len(testContent), stat.Size)
	}
}

func TestTypesHost_MethodDescriptorOpenAt(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	err := os.Mkdir(subDir, 0755)
	if err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	testFile := filepath.Join(subDir, "test.txt")
	err = os.WriteFile(testFile, []byte("content"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	baseDesc := preview2.NewDescriptorResource(tempDir, true, true)
	baseHandle := resources.Add(baseDesc)

	newHandle, fsErr := host.MethodDescriptorOpenAt(ctx, baseHandle, 0, "subdir/test.txt", 0, 0)
	if fsErr != nil {
		t.Fatalf("unexpected error: %v", fsErr)
	}

	r, ok := resources.Get(newHandle)
	if !ok {
		t.Fatal("opened descriptor not in resource table")
	}

	openedDesc, ok := r.(*preview2.DescriptorResource)
	if !ok {
		t.Fatal("resource is not a DescriptorResource")
	}

	if openedDesc.IsDir() {
		t.Error("expected file, got directory")
	}
}

func TestTypesHost_MethodDescriptorReadDirectory(t *testing.T) {
	tempDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tempDir, "file1.txt"), []byte("content"), 0644)
	if err != nil {
		t.Fatalf("failed to create file1: %v", err)
	}

	err = os.WriteFile(filepath.Join(tempDir, "file2.txt"), []byte("content"), 0644)
	if err != nil {
		t.Fatalf("failed to create file2: %v", err)
	}

	err = os.Mkdir(filepath.Join(tempDir, "subdir"), 0755)
	if err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	desc := preview2.NewDescriptorResource(tempDir, true, true)
	handle := resources.Add(desc)

	streamHandle, fsErr := host.MethodDescriptorReadDirectory(ctx, handle)
	if fsErr != nil {
		t.Fatalf("unexpected error: %v", fsErr)
	}

	r, ok := resources.Get(streamHandle)
	if !ok {
		t.Fatal("stream not in resource table")
	}

	_, ok = r.(*preview2.DirectoryEntryStreamResource)
	if !ok {
		t.Fatal("resource is not a DirectoryEntryStreamResource")
	}

	entries := make(map[string]bool)
	for {
		entry, err := host.MethodDirectoryEntryStreamReadDirectoryEntry(ctx, streamHandle)
		if err != nil {
			t.Fatalf("unexpected error reading entry: %v", err)
		}
		if entry == nil {
			break
		}
		entries[entry.Name] = true
	}

	if !entries["file1.txt"] {
		t.Error("file1.txt not found in directory")
	}
	if !entries["file2.txt"] {
		t.Error("file2.txt not found in directory")
	}
	if !entries["subdir"] {
		t.Error("subdir not found in directory")
	}
}

func TestTypesHost_MethodDescriptorReadDirectory_NotDirectory(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	err := os.WriteFile(testFile, []byte("content"), 0644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	desc := preview2.NewDescriptorResource(testFile, false, true)
	handle := resources.Add(desc)

	_, fsErr := host.MethodDescriptorReadDirectory(ctx, handle)
	if fsErr == nil {
		t.Fatal("expected error when reading directory from file")
	}
	if fsErr.Code != ErrorNotDirectory {
		t.Errorf("expected ErrorNotDirectory, got %d", fsErr.Code)
	}
}

func TestTypesHost_MethodDescriptorIsSameObject(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	desc1 := preview2.NewDescriptorResource("/tmp/test1.txt", false, true)
	handle1 := resources.Add(desc1)

	desc2 := preview2.NewDescriptorResource("/tmp/test2.txt", false, true)
	handle2 := resources.Add(desc2)

	if !host.MethodDescriptorIsSameObject(ctx, handle1, handle1) {
		t.Error("same handle should be same object")
	}

	if host.MethodDescriptorIsSameObject(ctx, handle1, handle2) {
		t.Error("different handles should not be same object")
	}
}

func TestTypesHost_InvalidHandles(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	_, err := host.MethodDescriptorGetType(ctx, 9999)
	if err == nil {
		t.Fatal("expected error for invalid handle")
	}
	if err.Code != ErrorBadDescriptor {
		t.Errorf("expected ErrorBadDescriptor, got %d", err.Code)
	}

	_, err = host.MethodDescriptorRead(ctx, 9999, 10, 0)
	if err == nil {
		t.Error("expected error for invalid handle")
	}
}

func TestTypesHost_Namespace(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)

	ns := host.Namespace()
	expected := "wasi:filesystem/types@0.2.3"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestPreopensHost_GetDirectories(t *testing.T) {
	tempDir := t.TempDir()

	preopens := map[string]string{
		"/":     tempDir,
		"/home": filepath.Join(tempDir, "home"),
	}

	resources := preview2.NewResourceTable()
	host := NewPreopensHost(resources, preopens)
	ctx := context.Background()

	dirs := host.GetDirectories(ctx)

	if len(dirs) != 2 {
		t.Errorf("expected 2 preopened directories, got %d", len(dirs))
	}

	for _, dir := range dirs {
		handle := dir[0].(uint32)
		logicalPath := dir[1].(string)

		r, ok := resources.Get(handle)
		if !ok {
			t.Fatalf("preopen descriptor %d not in resource table", handle)
		}

		desc, ok := r.(*preview2.DescriptorResource)
		if !ok {
			t.Fatal("resource is not a DescriptorResource")
		}

		if !desc.IsDir() {
			t.Errorf("preopen %s should be a directory", logicalPath)
		}

		if logicalPath != "/" && logicalPath != "/home" {
			t.Errorf("unexpected logical path: %s", logicalPath)
		}
	}
}

func TestPreopensHost_EmptyPreopens(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewPreopensHost(resources, nil)
	ctx := context.Background()

	dirs := host.GetDirectories(ctx)

	if len(dirs) != 0 {
		t.Errorf("expected 0 preopened directories, got %d", len(dirs))
	}
}

func TestPreopensHost_Namespace(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewPreopensHost(resources, nil)

	ns := host.Namespace()
	expected := "wasi:filesystem/preopens@0.2.3"
	if ns != expected {
		t.Errorf("expected namespace %s, got %s", expected, ns)
	}
}

func TestError_Error(t *testing.T) {
	tests := []struct {
		name     string
		contains string
		code     ErrorCode
	}{
		{"access", "access", ErrorAccess},
		{"bad-descriptor", "bad-descriptor", ErrorBadDescriptor},
		{"io", "io", ErrorIo},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := &Error{Code: tc.code}
			msg := err.Error()
			if !strings.Contains(msg, tc.contains) {
				t.Errorf("expected error message to contain %q, got %q", tc.contains, msg)
			}
		})
	}
}

func TestTypesHost_DescriptorSync(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	handle := resources.Add(preview2.NewDescriptorResource(tmpFile, false, true))

	if err := host.MethodDescriptorSync(ctx, handle); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	writableHandle := resources.Add(preview2.NewDescriptorResource(tmpFile, false, false))

	if err := host.MethodDescriptorSync(ctx, writableHandle); err != nil {
		t.Fatalf("unexpected error for writable descriptor: %v", err)
	}

	if err := host.MethodDescriptorSync(ctx, 9999); err == nil {
		t.Error("expected error for invalid handle")
	}
}

func TestTypesHost_DescriptorSyncData(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	handle := resources.Add(preview2.NewDescriptorResource(tmpFile, false, true))

	if err := host.MethodDescriptorSyncData(ctx, handle); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := host.MethodDescriptorSyncData(ctx, 9999); err == nil {
		t.Error("expected error for invalid handle")
	}
}

func TestTypesHost_DescriptorGetFlags(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	handle := resources.Add(preview2.NewDescriptorResource(tmpFile, false, true))

	flags, err := host.MethodDescriptorGetFlags(ctx, handle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if flags != 0 {
		t.Errorf("expected flags 0, got %d", flags)
	}

	_, err = host.MethodDescriptorGetFlags(ctx, 9999)
	if err == nil {
		t.Error("expected error for invalid handle")
	}
}

func TestTypesHost_DescriptorAdvise(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	handle := resources.Add(preview2.NewDescriptorResource(tmpFile, false, true))

	if err := host.MethodDescriptorAdvise(ctx, handle, 0, 100, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := host.MethodDescriptorAdvise(ctx, 9999, 0, 100, 0); err == nil {
		t.Error("expected error for invalid handle")
	}
}

func TestTypesHost_DescriptorWrite(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	handle := resources.Add(preview2.NewDescriptorResource(testFile, false, false))

	n, fsErr := host.MethodDescriptorWrite(ctx, handle, []byte("world"), 0)
	if fsErr != nil {
		t.Fatalf("unexpected error: %v", fsErr)
	}
	if n != 5 {
		t.Errorf("expected 5 bytes written, got %d", n)
	}

	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}
	if string(data) != "world" {
		t.Errorf("expected 'world', got '%s'", data)
	}

	_, fsErr = host.MethodDescriptorWrite(ctx, 9999, []byte("x"), 0)
	if fsErr == nil {
		t.Fatal("expected error for invalid handle")
	}
	if fsErr.Code != ErrorBadDescriptor {
		t.Errorf("expected ErrorBadDescriptor, got %d", fsErr.Code)
	}
}

func TestTypesHost_DescriptorSeek(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	handle := resources.Add(preview2.NewDescriptorResource(testFile, false, true))

	pos, fsErr := host.MethodDescriptorSeek(ctx, handle, 6, 0)
	if fsErr != nil {
		t.Fatalf("unexpected error: %v", fsErr)
	}
	if pos != 6 {
		t.Errorf("expected position 6, got %d", pos)
	}

	_, fsErr = host.MethodDescriptorSeek(ctx, 9999, 0, 0)
	if fsErr == nil {
		t.Fatal("expected error for invalid handle")
	}
	if fsErr.Code != ErrorBadDescriptor {
		t.Errorf("expected ErrorBadDescriptor, got %d", fsErr.Code)
	}
}

func TestTypesHost_DescriptorSetSize(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	handle := resources.Add(preview2.NewDescriptorResource(testFile, false, false))

	fsErr := host.MethodDescriptorSetSize(ctx, handle, 5)
	if fsErr != nil {
		t.Fatalf("unexpected error: %v", fsErr)
	}

	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("failed to stat test file: %v", err)
	}
	if info.Size() != 5 {
		t.Errorf("expected size 5, got %d", info.Size())
	}

	fsErr = host.MethodDescriptorSetSize(ctx, 9999, 5)
	if fsErr == nil {
		t.Fatal("expected error for invalid handle")
	}
	if fsErr.Code != ErrorBadDescriptor {
		t.Errorf("expected ErrorBadDescriptor, got %d", fsErr.Code)
	}
}

func TestTypesHost_DescriptorMetadataHash(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	handle := resources.Add(preview2.NewDescriptorResource(testFile, false, true))

	hash, fsErr := host.MethodDescriptorMetadataHash(ctx, handle)
	if fsErr != nil {
		t.Fatalf("unexpected error: %v", fsErr)
	}
	if hash == 0 {
		t.Error("expected non-zero metadata hash")
	}

	_, fsErr = host.MethodDescriptorMetadataHash(ctx, 9999)
	if fsErr == nil {
		t.Fatal("expected error for invalid handle")
	}
	if fsErr.Code != ErrorBadDescriptor {
		t.Errorf("expected ErrorBadDescriptor, got %d", fsErr.Code)
	}
}

func TestTypesHost_ResourceDropDescriptor(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	handle := resources.Add(preview2.NewDescriptorResource(testFile, false, true))

	if _, ok := resources.Get(handle); !ok {
		t.Fatal("descriptor should be in table before drop")
	}

	host.ResourceDropDescriptor(ctx, handle)

	if _, ok := resources.Get(handle); ok {
		t.Fatal("descriptor should be removed from table after drop")
	}
}

func TestTypesHost_ResourceDropDirectoryEntryStream(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewTypesHost(resources)
	ctx := context.Background()

	entries := []preview2.DirectoryEntry{
		{Name: "file1.txt", Type: uint8(DescriptorTypeRegularFile)},
	}
	stream := preview2.NewDirectoryEntryStreamResource(entries)
	handle := resources.Add(stream)

	if _, ok := resources.Get(handle); !ok {
		t.Fatal("stream should be in table before drop")
	}

	host.ResourceDropDirectoryEntryStream(ctx, handle)

	if _, ok := resources.Get(handle); ok {
		t.Fatal("stream should be removed from table after drop")
	}
}
