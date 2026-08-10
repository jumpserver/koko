package httpd

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/logger"
)

const (
	defaultZipMaxSize     = 1024 * 1024 * 1024 // 1G
	defaultTmpPath        = "/tmp"
	maxWebEditorFileSize  = 10 * 1024 * 1024
	webSftpConflictErrMsg = "remote file changed"
)

var ErrWebSftpFileConflict = errors.New(webSftpConflictErrMsg)

type FileInfo struct {
	Name    string `json:"name"`
	Size    string `json:"size"`
	Perm    string `json:"perm"`
	ModTime string `json:"mod_time"`
	Type    string `json:"type"`
	IsDir   bool   `json:"is_dir"`
	Version string `json:"version"`
}

type FileData struct {
	Reader io.ReadCloser
	Size   int64
	IsDir  bool
}

func NewUserWebVolume(userVolume *UserVolume) *UserWebVolume {
	uVolume := &UserWebVolume{
		userVolume,
	}
	return uVolume
}

type UserWebVolume struct {
	*UserVolume
}

func newWebSftpFileInfo(info os.FileInfo) FileInfo {
	return FileInfo{
		Name:    info.Name(),
		Size:    strconv.FormatInt(info.Size(), 10),
		Perm:    info.Mode().String(),
		ModTime: strconv.FormatInt(info.ModTime().Unix(), 10),
		IsDir:   info.IsDir(),
		Version: webSftpFileVersion(info),
	}
}

func webSftpFileVersion(info os.FileInfo) string {
	return fmt.Sprintf("%d\x00%d\x00%s", info.Size(), info.ModTime().Unix(), info.Mode().String())
}

func webSftpContentVersion(reader io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil)), nil
}

func (u *UserWebVolume) List(path string) []FileInfo {
	logger.Debug("Volume List: ", path)
	files := make([]FileInfo, 0)

	originFiles, err := u.UserSftp.ReadDir(path)
	if err != nil {
		logger.Errorf("ReadDir %s failed: %s", path, err)
		return files
	}

	for _, info := range originFiles {
		files = append(files, newWebSftpFileInfo(info))
	}
	return files
}

func (u *UserWebVolume) Download(path string, isDir bool) (FileData, string, error) {
	logger.Debug("WebVolume Download: ", path)
	var rest FileData
	fileName := filepath.Base(path)
	if !isDir {
		file, err := u.GetFile(path)
		if err != nil {
			logger.Errorf("Download file failed: %s", err)
			return rest, fileName, err
		}
		return file, fileName, nil
	}

	filename := fmt.Sprintf("%s-%s.zip",
		filepath.Base(path), time.Now().UTC().Format("20060102150405"))
	zipTmpPath := filepath.Join(defaultTmpPath, filename)

	dstFd, err := os.Create(zipTmpPath)
	if err != nil {
		return rest, fileName, err
	}
	defer dstFd.Close()

	zipWriter := zip.NewWriter(dstFd)
	defer zipWriter.Close()

	if err := u.zipFolder(zipWriter, path, ""); err != nil {
		logger.Errorf("Zip folder failed: %s", err)
		return rest, fileName, err
	}

	file, err := os.Open(zipTmpPath)
	if err != nil {
		logger.Errorf("Open zip file failed: %s", err)
		return rest, fileName, err
	}

	fileInfo, err := file.Stat()
	if err != nil {
		logger.Errorf("Get zip file stat failed: %s", err)
		return rest, fileName, err
	}

	return FileData{
		Reader: file,
		Size:   fileInfo.Size(),
		IsDir:  false,
	}, filename, nil
}

func (u *UserWebVolume) zipFolder(zipWriter *zip.Writer, remotePath, basePath string) error {
	entries, err := u.UserSftp.ReadDir(remotePath)
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

func (u *UserWebVolume) zipFile(zipWriter *zip.Writer, remotePath, zipPath string) error {
	remoteFile, err := u.UserSftp.Open(remotePath)
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

func (u *UserWebVolume) GetFile(path string) (fileData FileData, err error) {
	logger.Debug("WebVolume GetFile path: ", path)
	var rest FileData
	sf, err := u.UserSftp.Open(path)
	if err != nil {
		return rest, err
	}

	fileInfo, err := sf.Stat()
	if err != nil {
		_ = sf.Close()
		return rest, err
	}
	size := fileInfo.Size()

	if err1 := u.recorder.ChunkedRecord(sf.FTPLog, sf, 0, size); err1 != nil {
		logger.Errorf("Record file err: %s", err1)
	}

	_, _ = sf.Seek(0, io.SeekStart)
	fileData = FileData{sf, size, fileInfo.IsDir()}
	return fileData, nil
}

func (u *UserWebVolume) Rename(oldNamePath, newName string) error {
	logger.Debug("WebVolume Rename")
	newNamePath := filepath.Join(filepath.Dir(oldNamePath), newName)
	err := u.UserSftp.Rename(
		filepath.Join(u.basePath, oldNamePath),
		filepath.Join(u.basePath, newNamePath),
	)
	return err
}

func (u *UserWebVolume) MakeDir(path string) error {
	logger.Debug("WebVolume MakeDir")
	err := u.UserSftp.MkdirAll(filepath.Join(u.basePath, path))
	return err
}

func (u *UserWebVolume) UploadFile(path string, reader io.Reader, totalSize int64) error {
	logger.Debug("WebVolume upload file path: ", path)
	fd, err := u.UserSftp.Create(filepath.Join(path))
	if err != nil {
		return err
	}
	defer fd.Close()

	if err1 := u.recorder.Record(fd.FTPLog, reader); err1 != nil {
		logger.Errorf("Record file err: %s", err1)
	}

	readerAt, ok := reader.(io.ReaderAt)
	if !ok {
		logger.Debug("reader is not io.ReaderAt, use io.SeekStart")
		return fmt.Errorf("reader is not io.ReaderAt")
	}

	err = common.ChunkedFileTransfer(fd, readerAt, 0, totalSize)
	if err != nil {
		return err
	}
	return nil
}

func (u *UserWebVolume) SaveFile(
	path string,
	reader *bytes.Reader,
	totalSize int64,
	expectedVersion *string,
	force bool,
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

	if expectedVersion != nil && !force {
		if err := u.verifyExpectedVersion(path, *expectedVersion, false); err != nil {
			return result, err
		}
	}

	tempPath := filepath.Join(filepath.Dir(path), fmt.Sprintf(".jumpserver-editor-%s.tmp", common.UUID()))
	removeTemp := true
	defer func() {
		if !removeTemp {
			return
		}
		if err := u.UserSftp.DiscardUploadTemp(tempPath); err != nil && !os.IsNotExist(err) {
			logger.Warnf("Discard editor temp file %s failed: %s", tempPath, err)
		}
	}()

	fd, err := u.UserSftp.CreateEditorTemp(tempPath, path)
	if err != nil {
		return result, err
	}
	if err := u.recorder.Record(fd.FTPLog, reader); err != nil {
		logger.Errorf("Record file err: %s", err)
	}
	if err := common.ChunkedFileTransfer(fd, reader, 0, totalSize); err != nil {
		_ = fd.Close()
		return result, err
	}
	if err := fd.Close(); err != nil {
		return result, err
	}

	if expectedVersion != nil && !force {
		if err := u.verifyExpectedVersion(path, *expectedVersion, true); err != nil {
			return result, err
		}
	}
	if err := u.UserSftp.AtomicReplace(tempPath, path); err != nil {
		return result, err
	}
	removeTemp = false

	info, err := u.UserSftp.Stat(path)
	if err != nil {
		return result, err
	}
	result = newWebSftpFileInfo(info)
	result.Version = contentVersion
	return result, nil
}

func (u *UserWebVolume) verifyExpectedVersion(path, expectedVersion string, verifyContent bool) error {
	info, err := u.UserSftp.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
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
	file, err := u.UserSftp.OpenForChecksum(path)
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

func (u *UserWebVolume) UploadChunk(cid string, path string, offset, dataSize int64, readerAt io.ReaderAt) error {
	logger.Debug("WebVolume upload chunk file path: ", path)
	var err error
	u.lock.Lock()
	fd, ok := u.chunkFilesMap[cid]
	ftpLog := u.ftpLogMap[cid]
	u.lock.Unlock()
	if !ok {
		f, err := u.UserSftp.Create(path)
		if err != nil {
			return err
		}
		fd = f.File
		ftpLog = f.FTPLog
		_, err = fd.Seek(offset, 0)
		if err != nil {
			return err
		}
		u.lock.Lock()
		u.chunkFilesMap[cid] = fd
		u.ftpLogMap[cid] = ftpLog
		u.lock.Unlock()
	}

	if err2 := u.recorder.ChunkedRecord(ftpLog, readerAt, offset, dataSize); err2 != nil {
		logger.Errorf("Record file err: %s", err2)
	}

	err = common.ChunkedFileTransfer(fd, readerAt, offset, dataSize)

	if err != nil {
		_ = fd.Close()
		u.lock.Lock()
		delete(u.chunkFilesMap, cid)
		delete(u.ftpLogMap, cid)
		u.lock.Unlock()
	}
	return err
}

func (u *UserWebVolume) MergeChunk(cid string, path string) error {
	logger.Debug("WebVolume merge chunk path: ", path)
	u.lock.Lock()
	defer u.lock.Unlock()
	fd, ok := u.chunkFilesMap[cid]
	if !ok {
		return fmt.Errorf("chunk file not found %s", cid)
	}
	_ = fd.Close()
	ftpLog := u.ftpLogMap[cid]
	delete(u.chunkFilesMap, cid)
	u.recorder.FinishFTPFile(ftpLog.ID)
	delete(u.ftpLogMap, cid)
	return nil
}
