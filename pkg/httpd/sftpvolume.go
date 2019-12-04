package httpd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/LeeEirc/elfinder"
	"github.com/pkg/sftp"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/srvconn"
)

func NewUserVolume(user *model.User, addr, hostId string) *UserVolume {
	var userSftp *srvconn.UserNewSftp
	homename := "Home"
	basePath := "/"
	switch hostId {
	case "":
		userSftp = srvconn.NewUserNewSftp(user, addr)
	default:
		assets := service.GetUserAssetByID(user.ID, hostId)
		if len(assets) == 1 {
			homename = assets[0].Hostname
			if assets[0].OrgID != "" {
				homename = fmt.Sprintf("%s.%s", assets[0].Hostname, assets[0].OrgName)
			}
			basePath = filepath.Join("/", homename)
			userSftp = srvconn.NewUserNewSftpWithAsset(user, addr, assets[0])
		}
	}
	rawID := fmt.Sprintf("%s@%s", user.Username, addr)
	uVolume := &UserVolume{
		Uuid:          elfinder.GenerateID(rawID),
		UserSftp:      userSftp,
		Homename:      homename,
		basePath:      basePath,
		chunkFilesMap: make(map[int]*sftp.File),
		lock:          new(sync.Mutex),
	}
	return uVolume
}

type UserVolume struct {
	Uuid     string
	UserSftp *srvconn.UserNewSftp
	Homename string
	basePath string

	chunkFilesMap map[int]*sftp.File
	lock          *sync.Mutex
}

func (u *UserVolume) ID() string {
	return u.Uuid
}

func (u *UserVolume) Info(path string) (elfinder.FileDir, error) {
	logger.Debug("Volume Info: ", path)
	var rest elfinder.FileDir
	if path == "/" {
		return u.RootFileDir(), nil
	}
	originFileInfo, err := u.UserSftp.Stat(filepath.Join(u.basePath, path))
	if err != nil {
		return rest, err
	}
	dirPath := filepath.Dir(path)
	filename := filepath.Base(path)
	rest.Read, rest.Write = elfinder.ReadWritePem(originFileInfo.Mode())
	if filename != originFileInfo.Name() {
		rest.Read, rest.Write = 1, 1
		logger.Debug("Info filename no equal")
	}
	if filename == "." {
		filename = originFileInfo.Name()
	}
	rest.Name = filename
	rest.Hash = hashPath(u.Uuid, path)
	rest.Phash = hashPath(u.Uuid, dirPath)
	if rest.Hash == rest.Phash {
		rest.Phash = ""
	}
	rest.Size = originFileInfo.Size()
	rest.Ts = originFileInfo.ModTime().Unix()
	rest.Volumeid = u.Uuid
	if originFileInfo.IsDir() {
		rest.Mime = "directory"
		rest.Dirs = 1
	} else {
		rest.Mime = "file"
		rest.Dirs = 0
	}
	return rest, err
}

func (u *UserVolume) List(path string) []elfinder.FileDir {
	dirs := make([]elfinder.FileDir, 0)
	logger.Debug("Volume List: ", path)
	originFileInfolist, err := u.UserSftp.ReadDir(filepath.Join(u.basePath, path))
	if err != nil {
		return dirs
	}
	for i := 0; i < len(originFileInfolist); i++ {
		dirs = append(dirs, NewElfinderFileInfo(u.Uuid, path, originFileInfolist[i]))
	}
	return dirs
}

func (u *UserVolume) Parents(path string, dep int) []elfinder.FileDir {
	logger.Debug("volume Parents: ", path)
	dirs := make([]elfinder.FileDir, 0)
	dirPath := path
	for {
		tmps, err := u.UserSftp.ReadDir(filepath.Join(u.basePath, dirPath))
		if err != nil {
			return dirs
		}

		for i := 0; i < len(tmps); i++ {
			dirs = append(dirs, NewElfinderFileInfo(u.Uuid, dirPath, tmps[i]))
		}

		if dirPath == "/" {
			break
		}
		dirPath = filepath.Dir(dirPath)
	}
	return dirs
}

func (u *UserVolume) GetFile(path string) (reader io.ReadCloser, err error) {
	logger.Debug("GetFile path: ", path)
	return u.UserSftp.Open(filepath.Join(u.basePath, TrimPrefix(path)))
}

func (u *UserVolume) UploadFile(dirPath, uploadPath, filename string, reader io.Reader) (elfinder.FileDir, error) {
	var path string
	switch {
	case strings.Contains(uploadPath, filename):
		path = filepath.Join(dirPath, TrimPrefix(uploadPath))
	case uploadPath != "":
		path = filepath.Join(dirPath, TrimPrefix(uploadPath), filename)
	default:
		path = filepath.Join(dirPath, filename)

	}
	logger.Debug("Volume upload file path: ", path, " ", filename, " ", uploadPath)
	var rest elfinder.FileDir
	fd, err := u.UserSftp.Create(filepath.Join(u.basePath, path))
	if err != nil {
		return rest, err
	}
	defer fd.Close()

	_, err = io.Copy(fd, reader)
	if err != nil {
		return rest, err
	}
	return u.Info(path)
}

func (u *UserVolume) UploadChunk(cid int, dirPath, uploadPath, filename string, rangeData elfinder.ChunkRange, reader io.Reader) error {
	var err error
	var path string
	u.lock.Lock()
	fd, ok := u.chunkFilesMap[cid]
	u.lock.Unlock()
	if !ok {
		switch {
		case strings.Contains(uploadPath, filename):
			path = filepath.Join(dirPath, TrimPrefix(uploadPath))
		case uploadPath != "":
			path = filepath.Join(dirPath, TrimPrefix(uploadPath), filename)
		default:
			path = filepath.Join(dirPath, filename)

		}
		fd, err = u.UserSftp.Create(filepath.Join(u.basePath, path))
		if err != nil {
			return err
		}
		_, err = fd.Seek(rangeData.Offset, 0)
		if err != nil {
			return err
		}
		u.lock.Lock()
		u.chunkFilesMap[cid] = fd
		u.lock.Unlock()
	}
	_, err = io.Copy(fd, reader)
	if err != nil {
		_ = fd.Close()
		u.lock.Lock()
		delete(u.chunkFilesMap, cid)
		u.lock.Unlock()
	}
	return err
}

func (u *UserVolume) MergeChunk(cid, total int, dirPath, uploadPath, filename string) (elfinder.FileDir, error) {
	var path string
	switch {
	case strings.Contains(uploadPath, filename):
		path = filepath.Join(dirPath, TrimPrefix(uploadPath))
	case uploadPath != "":
		path = filepath.Join(dirPath, TrimPrefix(uploadPath), filename)
	default:
		path = filepath.Join(dirPath, filename)

	}
	logger.Debug("Merge chunk path: ", path)
	u.lock.Lock()
	if fd, ok := u.chunkFilesMap[cid]; ok {
		_ = fd.Close()
		delete(u.chunkFilesMap, cid)
	}
	u.lock.Unlock()
	return u.Info(path)
}

func (u *UserVolume) MakeDir(dir, newDirname string) (elfinder.FileDir, error) {
	logger.Debug("Volume Make Dir: ", newDirname)
	path := filepath.Join(dir, TrimPrefix(newDirname))
	var rest elfinder.FileDir
	err := u.UserSftp.MkdirAll(filepath.Join(u.basePath, path))
	if err != nil {
		return rest, err
	}
	return u.Info(path)
}

func (u *UserVolume) MakeFile(dir, newFilename string) (elfinder.FileDir, error) {
	logger.Debug("Volume MakeFile")

	path := filepath.Join(dir, newFilename)
	var rest elfinder.FileDir
	fd, err := u.UserSftp.Create(filepath.Join(u.basePath, path))
	if err != nil {
		return rest, err
	}
	_ = fd.Close()
	res, err := u.UserSftp.Stat(filepath.Join(u.basePath, path))

	return NewElfinderFileInfo(u.Uuid, dir, res), err
}

func (u *UserVolume) Rename(oldNamePath, newName string) (elfinder.FileDir, error) {

	logger.Debug("Volume Rename")
	var rest elfinder.FileDir
	newNamePath := filepath.Join(filepath.Dir(oldNamePath), newName)
	err := u.UserSftp.Rename(filepath.Join(u.basePath, oldNamePath), filepath.Join(u.basePath, newNamePath))
	if err != nil {
		return rest, err
	}
	return u.Info(newNamePath)
}

func (u *UserVolume) Remove(path string) error {

	logger.Debug("Volume remove", path)
	var res os.FileInfo
	var err error
	res, err = u.UserSftp.Stat(filepath.Join(u.basePath, path))
	if err != nil {
		return err
	}
	if res.IsDir() {
		return u.UserSftp.RemoveDirectory(filepath.Join(u.basePath, path))
	}
	return u.UserSftp.Remove(filepath.Join(u.basePath, path))
}

func (u *UserVolume) Paste(dir, filename, suffix string, reader io.ReadCloser) (elfinder.FileDir, error) {
	defer reader.Close()
	var rest elfinder.FileDir
	path := filepath.Join(dir, filename)
	_, err := u.UserSftp.Stat(filepath.Join(u.basePath, path))
	if err == nil {
		path += suffix
	}
	fd, err := u.UserSftp.Create(filepath.Join(u.basePath, path))
	logger.Debug("volume paste: ", path, err)
	if err != nil {
		return rest, err
	}
	defer fd.Close()
	_, err = io.Copy(fd, reader)
	if err != nil {
		return rest, err
	}
	return u.Info(path)
}

func (u *UserVolume) RootFileDir() elfinder.FileDir {
	logger.Debug("Root File Dir")
	fInfo, _ := u.UserSftp.Stat(u.basePath)
	var rest elfinder.FileDir
	rest.Name = u.Homename
	rest.Hash = hashPath(u.Uuid, "/")
	rest.Size = fInfo.Size()
	rest.Volumeid = u.Uuid
	rest.Mime = "directory"
	rest.Dirs = 1
	rest.Read, rest.Write = 1, 1
	rest.Locked = 1
	rest.Ts = fInfo.ModTime().Unix()
	return rest
}

func (u *UserVolume) Close() {
	u.UserSftp.Close()
}

func NewElfinderFileInfo(id, dirPath string, originFileInfo os.FileInfo) elfinder.FileDir {
	var rest elfinder.FileDir
	rest.Name = originFileInfo.Name()
	rest.Hash = hashPath(id, filepath.Join(dirPath, originFileInfo.Name()))
	rest.Phash = hashPath(id, dirPath)
	if rest.Hash == rest.Phash {
		rest.Phash = ""
	}
	rest.Size = originFileInfo.Size()
	rest.Volumeid = id
	if originFileInfo.IsDir() {
		rest.Mime = "directory"
		rest.Dirs = 1
	} else {
		rest.Mime = "file"
		rest.Dirs = 0
	}
	rest.Ts = originFileInfo.ModTime().Unix()
	rest.Read, rest.Write = elfinder.ReadWritePem(originFileInfo.Mode())
	return rest
}

func hashPath(id, path string) string {
	return elfinder.CreateHash(id, path)
}

func TrimPrefix(path string) string {
	return strings.TrimPrefix(path, "/")
}
