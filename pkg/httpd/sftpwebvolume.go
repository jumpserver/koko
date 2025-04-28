package httpd

import (
	"archive/zip"
	"fmt"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/logger"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	defaultZipMaxSize = 1024 * 1024 * 1024 // 1G
	defaultTmpPath    = "/tmp"
)

type FileInfo struct {
	Name    string `json:"name"`
	Size    string `json:"size"`
	Perm    string `json:"perm"`
	ModTime string `json:"mod_time"`
	Type    string `json:"type"`
	IsDir   bool   `json:"is_dir"`
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

func (u *UserWebVolume) List(path string) []FileInfo {
	logger.Debug("Volume List: ", path)
	files := make([]FileInfo, 0)

	originFiles, err := u.UserSftp.ReadDir(path)
	if err != nil {
		logger.Errorf("ReadDir %s failed: %s", path, err)
		return files
	}

	for _, info := range originFiles {
		size := fmt.Sprintf("%d", info.Size())
		modTime := strconv.FormatInt(info.ModTime().Unix(), 10)

		fileInfo := FileInfo{
			Name:    info.Name(),
			Size:    size,
			Perm:    info.Mode().String(),
			ModTime: modTime,
			IsDir:   info.IsDir(),
		}

		files = append(files, fileInfo)
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
	size := fileInfo.Size()
	if err != nil {
		return rest, err
	}

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

func (u *UserWebVolume) UploadChunk(cid int, path string, offset, dataSize int64, readerAt io.ReaderAt) error {
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

func (u *UserWebVolume) MergeChunk(cid int, path string) error {
	logger.Debug("WebVolume merge chunk path: ", path)
	u.lock.Lock()
	defer u.lock.Unlock()
	fd, ok := u.chunkFilesMap[cid]
	if !ok {
		return fmt.Errorf("chunk file not found %d", cid)
	}
	_ = fd.Close()
	ftpLog := u.ftpLogMap[cid]
	delete(u.chunkFilesMap, cid)
	u.recorder.FinishFTPFile(ftpLog.ID)
	delete(u.ftpLogMap, cid)
	return nil
}
