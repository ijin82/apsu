package server

import (
	"bufio"
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/webdav"
	"qsync/core/internal/storage"
)

// VirtualFS routes the root directory to mapped shared folders
type VirtualFS struct {
	store *storage.Manager
}

func NewVirtualFS(store *storage.Manager) *VirtualFS {
	return &VirtualFS{store: store}
}

// splitPath splits a clean path into top-level folder name and subpath.
// e.g. "/Photos/vacation/1.jpg" -> "Photos", "/vacation/1.jpg"
// "/Photos" -> "Photos", "/"
// "/" -> "", "/"
func splitPath(p string) (string, string) {
	clean := path.Clean("/" + strings.TrimPrefix(p, "/"))
	if clean == "/" || clean == "." {
		return "", "/"
	}
	parts := strings.SplitN(strings.TrimPrefix(clean, "/"), "/", 2)
	if len(parts) == 1 {
		return parts[0], "/"
	}
	return parts[0], "/" + parts[1]
}

func (v *VirtualFS) findFolder(topName string) (storage.SharedFolder, bool) {
	cfg := v.store.GetConfig()
	for _, f := range cfg.Folders {
		if strings.EqualFold(f.Name, topName) {
			return f, true
		}
	}
	return storage.SharedFolder{}, false
}

func (v *VirtualFS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	top, sub := splitPath(name)
	if top == "" || sub == "/" {
		return os.ErrPermission
	}
	folder, found := v.findFolder(top)
	if !found {
		return os.ErrNotExist
	}
	_ = os.MkdirAll(folder.Path, 0755)
	return webdav.Dir(folder.Path).Mkdir(ctx, sub, perm)
}

func (v *VirtualFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	top, sub := splitPath(name)
	if top == "" {
		if flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) != 0 {
			return nil, os.ErrPermission
		}
		// Return root directory listing
		cfg := v.store.GetConfig()
		var infos []os.FileInfo
		for _, f := range cfg.Folders {
			fi, err := os.Stat(f.Path)
			if err != nil {
				// If directory cannot be accessed, provide a fallback virtual dir info
				fi = &virtualFileInfo{
					name:    f.Name,
					size:    4096,
					mode:    os.ModeDir | 0755,
					modTime: time.Now(),
				}
			} else {
				fi = &virtualFileInfo{
					name:    f.Name,
					size:    fi.Size(),
					mode:    fi.Mode() | os.ModeDir,
					modTime: fi.ModTime(),
				}
			}
			infos = append(infos, fi)
		}
		return &virtualRootDir{infos: infos}, nil
	}

	folder, found := v.findFolder(top)
	if !found {
		return nil, os.ErrNotExist
	}

	// If writing or creating a file, ensure the folder and intermediate subdirectories exist
	if flag&(os.O_CREATE|os.O_WRONLY|os.O_RDWR) != 0 {
		_ = os.MkdirAll(folder.Path, 0755)
		if sub != "/" && sub != "" {
			targetDir := filepath.Dir(filepath.Join(folder.Path, filepath.FromSlash(path.Clean(sub))))
			_ = os.MkdirAll(targetDir, 0755)
		}
	}

	f, err := webdav.Dir(folder.Path).OpenFile(ctx, sub, flag, perm)
	if err != nil {
		if os.IsNotExist(err) {
			_ = os.MkdirAll(folder.Path, 0755)
			if sub != "/" && sub != "" {
				targetDir := filepath.Dir(filepath.Join(folder.Path, filepath.FromSlash(path.Clean(sub))))
				_ = os.MkdirAll(targetDir, 0755)
			}
			f, err = webdav.Dir(folder.Path).OpenFile(ctx, sub, flag, perm)
		}
		if err != nil {
			return nil, err
		}
	}

	// If opening the top folder itself ("/"), wrap its File to report the virtual name
	if sub == "/" {
		return &folderRootFile{File: f, virtualName: folder.Name}, nil
	}

	// If opening for write, wrap in bufferedFileWriter (256 KB buffer) to optimize Android flash writes
	if flag&(os.O_WRONLY|os.O_RDWR) != 0 {
		return newBufferedFileWriter(f, 256*1024), nil
	}

	return f, nil
}

func (v *VirtualFS) RemoveAll(ctx context.Context, name string) error {
	top, sub := splitPath(name)
	if top == "" || sub == "/" {
		// Do not allow deleting the root or the mapped folders themselves
		return os.ErrPermission
	}
	folder, found := v.findFolder(top)
	if !found {
		return os.ErrNotExist
	}
	return webdav.Dir(folder.Path).RemoveAll(ctx, sub)
}

func (v *VirtualFS) Rename(ctx context.Context, oldName, newName string) error {
	oldTop, oldSub := splitPath(oldName)
	newTop, newSub := splitPath(newName)

	if oldTop == "" || oldSub == "/" || newTop == "" || newSub == "/" {
		return os.ErrPermission
	}

	if !strings.EqualFold(oldTop, newTop) {
		// Renaming across different root folders is not supported via direct rename
		return os.ErrPermission
	}

	folder, found := v.findFolder(oldTop)
	if !found {
		return os.ErrNotExist
	}

	destDir := filepath.Dir(filepath.Join(folder.Path, filepath.FromSlash(path.Clean(newSub))))
	_ = os.MkdirAll(destDir, 0755)

	return webdav.Dir(folder.Path).Rename(ctx, oldSub, newSub)
}

func (v *VirtualFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	top, sub := splitPath(name)
	if top == "" {
		return &virtualFileInfo{
			name:    "/",
			size:    4096,
			mode:    os.ModeDir | 0755,
			modTime: time.Now(),
		}, nil
	}

	folder, found := v.findFolder(top)
	if !found {
		return nil, os.ErrNotExist
	}

	if sub == "/" {
		fi, err := os.Stat(folder.Path)
		if err != nil {
			return &virtualFileInfo{
				name:    folder.Name,
				size:    4096,
				mode:    os.ModeDir | 0755,
				modTime: time.Now(),
			}, nil
		}
		return &virtualFileInfo{
			name:    folder.Name,
			size:    fi.Size(),
			mode:    fi.Mode() | os.ModeDir,
			modTime: fi.ModTime(),
		}, nil
	}

	return webdav.Dir(folder.Path).Stat(ctx, sub)
}

// virtualFileInfo implements os.FileInfo with custom name
type virtualFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func (v *virtualFileInfo) Name() string       { return v.name }
func (v *virtualFileInfo) Size() int64        { return v.size }
func (v *virtualFileInfo) Mode() os.FileMode  { return v.mode }
func (v *virtualFileInfo) ModTime() time.Time { return v.modTime }
func (v *virtualFileInfo) IsDir() bool        { return v.mode.IsDir() }
func (v *virtualFileInfo) Sys() any           { return nil }

// virtualRootDir implements webdav.File for the root listing
type virtualRootDir struct {
	infos   []os.FileInfo
	readPos int
}

func (d *virtualRootDir) Close() error { return nil }
func (d *virtualRootDir) Read(p []byte) (n int, err error) {
	return 0, os.ErrInvalid
}
func (d *virtualRootDir) ReadAt(p []byte, off int64) (n int, err error) {
	return 0, os.ErrInvalid
}
func (d *virtualRootDir) Seek(offset int64, whence int) (int64, error) {
	return 0, os.ErrInvalid
}
func (d *virtualRootDir) Write(p []byte) (n int, err error) {
	return 0, os.ErrPermission
}
func (d *virtualRootDir) Stat() (os.FileInfo, error) {
	return &virtualFileInfo{
		name:    "/",
		size:    4096,
		mode:    os.ModeDir | 0755,
		modTime: time.Now(),
	}, nil
}
func (d *virtualRootDir) Readdir(count int) ([]os.FileInfo, error) {
	if d.readPos >= len(d.infos) {
		if count <= 0 {
			return nil, nil
		}
		return nil, io.EOF
	}

	if count <= 0 {
		res := d.infos[d.readPos:]
		d.readPos = len(d.infos)
		return res, nil
	}

	end := d.readPos + count
	if end > len(d.infos) {
		end = len(d.infos)
	}
	res := d.infos[d.readPos:end]
	d.readPos = end

	if len(res) == 0 {
		return nil, io.EOF
	}
	return res, nil
}

// folderRootFile wraps the root file of a mounted shared folder to report its virtual folder name
type folderRootFile struct {
	webdav.File
	virtualName string
}

func (f *folderRootFile) Stat() (os.FileInfo, error) {
	fi, err := f.File.Stat()
	if err != nil {
		return nil, err
	}
	return &virtualFileInfo{
		name:    f.virtualName,
		size:    fi.Size(),
		mode:    fi.Mode() | os.ModeDir,
		modTime: fi.ModTime(),
	}, nil
}

// Ensure interface compliance
var _ webdav.FileSystem = (*VirtualFS)(nil)
var _ webdav.File = (*virtualRootDir)(nil)
var _ webdav.File = (*bufferedFileWriter)(nil)

// bufferedFileWriter buffers file writes in RAM before committing to flash storage,
// dramatically reducing write IOPS and system call overhead on Android flash memory.
type bufferedFileWriter struct {
	webdav.File
	bw *bufio.Writer
}

func newBufferedFileWriter(f webdav.File, bufSize int) *bufferedFileWriter {
	return &bufferedFileWriter{
		File: f,
		bw:   bufio.NewWriterSize(f, bufSize),
	}
}

func (b *bufferedFileWriter) Write(p []byte) (int, error) {
	return b.bw.Write(p)
}

func (b *bufferedFileWriter) Seek(offset int64, whence int) (int64, error) {
	if err := b.bw.Flush(); err != nil {
		return 0, err
	}
	return b.File.Seek(offset, whence)
}

func (b *bufferedFileWriter) Close() error {
	flushErr := b.bw.Flush()
	closeErr := b.File.Close()
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}
