package httpd

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/srvconn"
)

const (
	maxWebEditorFileSize  = 10 * 1024 * 1024
	webSftpConflictErrMsg = "remote file changed"
	webSftpAbsentVersion  = "absent"
)

var ErrWebSftpFileConflict = errors.New(webSftpConflictErrMsg)

type FileInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size,string"`
	Perm    string `json:"perm"`
	ModTime int64  `json:"mod_time,string"`
	Type    string `json:"type"`
	IsDir   bool   `json:"is_dir"`
	Version string `json:"version"`
}

func newWebSftpFileInfo(info os.FileInfo) FileInfo {
	return FileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		Perm:    info.Mode().String(),
		ModTime: info.ModTime().Unix(),
		IsDir:   info.IsDir(),
		Version: webSftpFileVersion(info),
	}
}

func webSftpFileVersion(info os.FileInfo) string {
	// This token crosses JSON and model/tool boundaries. Keep it printable and
	// distinct from the sha256: content versions used by the text editor.
	return fmt.Sprintf("stat:%d:%d:%s", info.Size(), info.ModTime().Unix(), info.Mode().String())
}

func webSftpContentVersion(reader io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil)), nil
}

func (u *sftpVolume) List(path string) ([]FileInfo, string, error) {
	if u.closed.Load() {
		return nil, "", os.ErrClosed
	}
	logger.Debug("Volume List: ", path)
	files := make([]FileInfo, 0)

	originFiles, currentPath, err := u.conn.ReadDirWithCurrentPath(path)
	if err != nil {
		logger.Errorf("ReadDir %s failed: %s", path, err)
		return files, currentPath, err
	}

	for _, info := range originFiles {
		files = append(files, newWebSftpFileInfo(info))
	}
	return files, currentPath, nil
}

func (u *sftpVolume) Download(path string, isDir bool) (io.ReadCloser, string, error) {
	if u.closed.Load() {
		return nil, "", os.ErrClosed
	}
	logger.Debug("WebVolume Download: ", path)
	fileName := filepath.Base(path)
	if !isDir {
		file, err := u.GetFile(path)
		if err != nil {
			logger.Errorf("Download file failed: %s", err)
			return nil, fileName, err
		}
		return file, fileName, nil
	}

	filename := fmt.Sprintf("%s-%s.zip",
		filepath.Base(path), time.Now().UTC().Format("20060102150405"))
	file, err := os.CreateTemp("", "koko-download-*.zip")
	if err != nil {
		return nil, fileName, err
	}
	archive := &temporaryDownload{File: file}
	complete := false
	defer func() {
		if !complete {
			_ = archive.Close()
		}
	}()
	writer := zip.NewWriter(file)
	if err := u.zipFolder(writer, path, ""); err != nil {
		return nil, fileName, err
	}
	if err := writer.Close(); err != nil {
		return nil, fileName, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fileName, err
	}
	complete = true
	return archive, filename, nil
}

// The returned download owns the archive, including deletion on Close.
type temporaryDownload struct{ *os.File }

func (f *temporaryDownload) Close() error {
	return errors.Join(f.File.Close(), os.Remove(f.Name()))
}

func (u *sftpVolume) zipFolder(zipWriter *zip.Writer, remotePath, basePath string) error {
	if u.closed.Load() {
		return os.ErrClosed
	}
	entries, err := u.conn.ReadDir(remotePath)
	if err != nil {
		return fmt.Errorf("failed to read remote directory: %v", err)
	}

	if len(entries) == 0 {
		header := &zip.FileHeader{
			Name:   basePath + "/",
			Method: zip.Store,
		}
		header.Modified = time.Now().UTC()

		_, err := zipWriter.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("failed to create zip header for empty folder: %v", err)
		}
		return nil
	}

	for _, entry := range entries {
		remoteFilePath := filepath.Join(remotePath, entry.Name())
		localRelativePath := filepath.Join(basePath, entry.Name())

		if entry.IsDir() {
			if err := u.zipFolder(zipWriter, remoteFilePath, localRelativePath); err != nil {
				return err
			}
		} else {
			if err := u.zipFile(zipWriter, remoteFilePath, localRelativePath); err != nil {
				return err
			}
		}
	}
	return nil
}

func (u *sftpVolume) zipFile(zipWriter *zip.Writer, remotePath, zipPath string) error {
	remoteFile, err := u.conn.Open(remotePath)
	if err != nil {
		return fmt.Errorf("failed to open remote file: %v", err)
	}
	defer remoteFile.Close()

	header := &zip.FileHeader{
		Name:   zipPath,
		Method: zip.Deflate,
	}

	header.Modified = time.Now().UTC()

	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("failed to create zip header: %v", err)
	}

	_, err = io.Copy(writer, remoteFile)
	if err != nil {
		return fmt.Errorf("failed to copy file content to zip: %v", err)
	}

	return nil
}

func (u *sftpVolume) GetFile(path string) (io.ReadCloser, error) {
	if u.closed.Load() {
		return nil, os.ErrClosed
	}
	logger.Debug("WebVolume GetFile path: ", path)
	sf, err := u.conn.Open(path)
	if err != nil {
		return nil, err
	}

	fileInfo, err := sf.Stat()
	if err != nil {
		_ = sf.Close()
		return nil, err
	}
	size := fileInfo.Size()

	u.recordFileChunk(sf, sf, 0, size)
	u.finishFileRecord(sf)

	_, _ = sf.Seek(0, io.SeekStart)
	return sf, nil
}

func (u *sftpVolume) UploadFile(path string, reader *bytes.Reader, totalSize int64) error {
	if totalSize < 0 || int64(reader.Len()) != totalSize {
		return fmt.Errorf("invalid file size")
	}
	u.lock.Lock()
	defer u.lock.Unlock()
	if u.closed.Load() {
		return os.ErrClosed
	}
	file, err := u.conn.Create(path)
	if err != nil {
		return err
	}
	if err := common.ChunkedFileTransfer(file, reader, 0, totalSize); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	u.recordFileChunk(file, reader, 0, totalSize)
	u.finishFileRecord(file)
	return nil
}

func (u *sftpVolume) SaveFile(
	ctx context.Context,
	path string,
	reader *bytes.Reader,
	totalSize int64,
	options fileMutationOptions,
) (FileInfo, error) {
	var result FileInfo
	if totalSize < 0 || int64(reader.Len()) != totalSize {
		return result, fmt.Errorf("invalid file size")
	}
	if totalSize > maxWebEditorFileSize {
		return result, fmt.Errorf("file exceeds the editor limit")
	}
	contentVersion, err := webSftpContentVersion(io.NewSectionReader(reader, 0, totalSize))
	if err != nil {
		return result, err
	}

	u.lock.Lock()
	defer u.lock.Unlock()
	paths, err := u.mutationPaths(ctx, options.confined, path)
	if err != nil {
		return result, err
	}
	path = paths[0]

	if options.expectedVersion != nil && !options.force {
		if err := u.verifyExpectedVersion(path, *options.expectedVersion, false); err != nil {
			return result, err
		}
	}

	tempPath := filepath.Join(filepath.Dir(path), fmt.Sprintf(".jumpserver-editor-%s.tmp", common.UUID()))
	removeTemp := true
	defer func() {
		if !removeTemp {
			return
		}
		if err := u.conn.DiscardUploadTemp(tempPath); err != nil && !isSftpNotExist(err) {
			logger.Warnf("Discard editor temp file %s failed: %s", tempPath, err)
		}
	}()

	fd, err := u.conn.CreateEditorTemp(tempPath, path)
	if err != nil {
		return result, err
	}
	if err := common.ChunkedFileTransfer(fd, reader, 0, totalSize); err != nil {
		_ = fd.Close()
		return result, err
	}
	if err := fd.Close(); err != nil {
		return result, err
	}

	if options.expectedVersion != nil && !options.force {
		if err := u.verifyExpectedVersion(path, *options.expectedVersion, true); err != nil {
			return result, err
		}
	}
	if err := u.check(ctx); err != nil {
		return result, err
	}
	createOnly := options.expectedVersion != nil && !options.force && *options.expectedVersion == webSftpAbsentVersion
	if createOnly {
		err = u.conn.AtomicCreate(tempPath, path)
	} else {
		err = u.conn.AtomicReplace(tempPath, path)
	}
	if err != nil {
		if createOnly {
			if _, statErr := u.conn.Stat(path); statErr == nil {
				return result, ErrWebSftpFileConflict
			}
		}
		return result, err
	}
	removeTemp = false
	u.recordFileChunk(fd, reader, 0, totalSize)
	u.finishFileRecord(fd)

	info, err := u.conn.Stat(path)
	if err != nil {
		return result, err
	}
	result = newWebSftpFileInfo(info)
	result.Version = contentVersion
	return result, nil
}

func (u *sftpVolume) verifyExpectedVersion(path, expectedVersion string, verifyContent bool) error {
	info, err := u.conn.Stat(path)
	if expectedVersion == webSftpAbsentVersion {
		switch {
		case err == nil:
			return ErrWebSftpFileConflict
		case isSftpNotExist(err):
			return nil
		default:
			return err
		}
	}
	if err != nil {
		if isSftpNotExist(err) {
			return ErrWebSftpFileConflict
		}
		return err
	}
	if !strings.HasPrefix(expectedVersion, "sha256:") {
		if webSftpFileVersion(info) != expectedVersion {
			return ErrWebSftpFileConflict
		}
		return nil
	}
	if !verifyContent {
		return nil
	}
	if info.IsDir() || info.Size() > maxWebEditorFileSize {
		return ErrWebSftpFileConflict
	}
	file, err := u.conn.OpenForChecksum(path)
	if err != nil {
		return err
	}
	defer file.Close()
	currentVersion, err := webSftpContentVersion(io.LimitReader(file, maxWebEditorFileSize+1))
	if err != nil {
		return err
	}
	if currentVersion != expectedVersion {
		return ErrWebSftpFileConflict
	}
	return nil
}

func (u *sftpVolume) UploadChunk(cid int, path string, offset, dataSize int64, readerAt io.ReaderAt) error {
	u.lock.Lock()
	defer u.lock.Unlock()
	if u.closed.Load() {
		return os.ErrClosed
	}
	if offset < 0 || dataSize < 0 {
		return fmt.Errorf("invalid file range")
	}
	upload, ok := u.uploads[cid]
	if ok && upload.path != path {
		return fmt.Errorf("upload path does not match")
	}
	if !ok {
		file, err := u.conn.Create(path)
		if err != nil {
			return err
		}
		upload = &sftpUpload{path: path, file: file}
		u.uploads[cid] = upload
	}
	if err := common.ChunkedFileTransfer(upload.file, readerAt, offset, dataSize); err != nil {
		_ = upload.file.Close()
		u.discardFileRecord(upload.file)
		delete(u.uploads, cid)
		return err
	}
	u.recordFileChunk(upload.file, readerAt, offset, dataSize)
	return nil
}

func (u *sftpVolume) MergeChunk(cid int, path string) error {
	u.lock.Lock()
	defer u.lock.Unlock()
	if u.closed.Load() {
		return os.ErrClosed
	}
	upload, ok := u.uploads[cid]
	if !ok {
		return fmt.Errorf("chunk file not found %d", cid)
	}
	if upload.path != path {
		return fmt.Errorf("upload path does not match")
	}
	delete(u.uploads, cid)
	if err := upload.file.Close(); err != nil {
		u.discardFileRecord(upload.file)
		return err
	}
	u.finishFileRecord(upload.file)
	return nil
}

func (u *sftpVolume) recordFileChunk(file *srvconn.SftpFile, reader io.ReaderAt, offset, size int64) {
	if u.recorder == nil || file.FTPLog == nil {
		return
	}
	if err := u.recorder.ChunkedRecord(file.FTPLog, reader, offset, size); err != nil {
		logger.Errorf("Record file err: %s", err)
		u.discardFileRecord(file)
	}
}
