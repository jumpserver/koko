package httpd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/LeeEirc/elfinder"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/srvconn"
)

func NewUserVolume(user *model.User, addr, hostId string) *UserVolume {
	var assets []model.Asset
	homename := "Home"
	switch hostId {
	case "":
		assets = service.GetUserAssets(user.ID, "1", "")

	default:
		assets = service.GetUserAssets(user.ID, "1", hostId)
		if len(assets) == 1 {
			homename = assets[0].Hostname
			if assets[0].OrgID != "" {
				homename = fmt.Sprintf("%s.%s", assets[0].Hostname, assets[0].OrgName)
			}
		}
	}
	conf := config.GetConf()
	rawID := fmt.Sprintf("%s@%s", user.Username, addr)
	uVolume := &UserVolume{
		Uuid:         elfinder.GenerateID(rawID),
		UserSftp:     srvconn.NewUserSFTP(user, addr, assets...),
		Homename:     homename,
		basePath:     filepath.Join("/", homename),
		localTmpPath: filepath.Join(conf.RootPath, "data", "tmp"),
	}
	return uVolume
}

type UserVolume struct {
	Uuid string
	*srvconn.UserSftp
	localTmpPath string
	Homename     string
	basePath     string
}

func (u *UserVolume) ID() string {
	return u.Uuid
}

func (u *UserVolume) Info(path string) (elfinder.FileDir, error) {
	logger.Debug("volume Info: ", path)

	if path == "/" {
		return u.RootFileDir(), nil
	}

	var rest elfinder.FileDir
	originFileInfo, err := u.Stat(path)
	if err != nil {
		return rest, err
	}
	dirPath := filepath.Dir(path)
	filename := filepath.Base(path)
	rest.Read, rest.Write = elfinder.ReadWritePem(originFileInfo.Mode())
	if filename != originFileInfo.Name() {
		rest.Read, rest.Write = 1, 1
	}
	if filename == "." {
		filename = originFileInfo.Name()
	}
	rest.Name = filename
	rest.Hash = hashPath(u.Uuid, filepath.Join(dirPath, filename))
	rest.Phash = hashPath(u.Uuid, dirPath)
	if rest.Hash == rest.Phash {
		rest.Phash = ""
	}
	rest.Size = originFileInfo.Size()
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
	logger.Debug("volume List: ", path)
	dirInfo, err := u.Info(path)
	if err != nil {
		return dirs
	}
	dirs = append(dirs, dirInfo)
	originFileInfolist, err := u.UserSftp.ReadDir(path)
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
		tmps := u.List(dirPath)
		dirs = append(dirs, tmps...)
		if dirPath == "/" {
			break
		}
		dirPath = filepath.Dir(dirPath)
	}
	return dirs
}

func (u *UserVolume) GetFile(path string) (reader io.ReadCloser, err error) {
	return u.UserSftp.Open(path)
}

func (u *UserVolume) UploadFile(dir, filename string, reader io.Reader) (elfinder.FileDir, error) {
	path := filepath.Join(dir, filename)
	logger.Debug("Volume upload file path: ", path)
	var rest elfinder.FileDir
	fd, err := u.UserSftp.Create(path)
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

func (u *UserVolume) UploadChunk(cid int, dirPath, chunkName string, reader io.Reader) error {
	//chunkName format "filename.[NUMBER]_[TOTAL].part"
	var err error
	tmpDir := filepath.Join(u.localTmpPath, dirPath)
	err = common.EnsureDirExist(tmpDir)
	if err != nil {
		return err
	}
	chunkRealPath := fmt.Sprintf("%s_%d",
		filepath.Join(tmpDir, chunkName), cid)

	fd, err := os.Create(chunkRealPath)
	defer fd.Close()
	if err != nil {
		return err
	}
	_, err = io.Copy(fd, reader)
	return err
}

func (u *UserVolume) MergeChunk(cid, total int, dirPath, filename string) (elfinder.FileDir, error) {
	path := filepath.Join(dirPath, filename)
	logger.Debug("merge chunk path: ", path)
	var rest elfinder.FileDir
	fd, err := u.UserSftp.Create(path)
	if err != nil {
		for i := 0; i <= total; i++ {
			partPath := fmt.Sprintf("%s.%d_%d.part_%d",
				filepath.Join(u.localTmpPath, dirPath, filename), i, total, cid)
			_ = os.Remove(partPath)
		}
		return rest, err
	}
	defer fd.Close()

	for i := 0; i <= total; i++ {
		partPath := fmt.Sprintf("%s.%d_%d.part_%d",
			filepath.Join(u.localTmpPath, dirPath, filename), i, total, cid)

		partFD, err := os.Open(partPath)
		if err != nil {
			logger.Debug(err)
			_ = os.Remove(partPath)
			continue
		}
		_, err = io.Copy(fd, partFD)
		if err != nil {
			return rest, os.ErrNotExist
		}
		_ = partFD.Close()
		_ = os.Remove(partPath)
	}
	return u.Info(path)
}

func (u *UserVolume) CompleteChunk(cid, total int, dirPath, filename string) bool {
	for i := 0; i <= total; i++ {
		partPath := fmt.Sprintf("%s.%d_%d.part_%d",
			filepath.Join(u.localTmpPath, dirPath, filename), i, total, cid)
		_, err := os.Stat(partPath)
		if err != nil {
			return false
		}
	}
	return true
}

func (u *UserVolume) MakeDir(dir, newDirname string) (elfinder.FileDir, error) {
	path := filepath.Join(dir, newDirname)
	var rest elfinder.FileDir
	err := u.UserSftp.MkdirAll(path)
	if err != nil {
		return rest, err
	}
	return u.Info(path)
}

func (u *UserVolume) MakeFile(dir, newFilename string) (elfinder.FileDir, error) {
	path := filepath.Join(dir, newFilename)
	var rest elfinder.FileDir
	fd, err := u.UserSftp.Create(path)
	if err != nil {
		return rest, err
	}
	defer fd.Close()
	return u.Info(path)
}

func (u *UserVolume) Rename(oldNamePath, newName string) (elfinder.FileDir, error) {
	var rest elfinder.FileDir
	newNamePath := filepath.Join(filepath.Dir(oldNamePath), newName)
	err := u.UserSftp.Rename(oldNamePath, newNamePath)
	if err != nil {
		return rest, err
	}
	return u.Info(newNamePath)
}

func (u *UserVolume) Remove(path string) error {
	var res os.FileInfo
	var err error
	res, err = u.UserSftp.Stat(path)
	if err != nil {
		return err
	}
	if res.IsDir() {
		return u.UserSftp.RemoveDirectory(path)
	}
	return u.UserSftp.Remove(path)
}

func (u *UserVolume) Paste(dir, filename, suffix string, reader io.ReadCloser) (elfinder.FileDir, error) {
	var rest elfinder.FileDir
	path := filepath.Join(dir, filename)
	rest, err := u.Info(path)
	if err != nil {
		path += suffix
	}
	fd, err := u.UserSftp.Create(path)
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
	fInfo, _ := u.UserSftp.Info()
	var rest elfinder.FileDir
	rest.Name = u.Homename
	rest.Hash = hashPath(u.Uuid, "/")
	rest.Size = fInfo.Size()
	rest.Volumeid = u.Uuid
	rest.Mime = "directory"
	rest.Dirs = 1
	rest.Read, rest.Write = 1, 1
	rest.Locked = 1
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
	rest.Read, rest.Write = elfinder.ReadWritePem(originFileInfo.Mode())
	return rest
}

func hashPath(id, path string) string {
	return elfinder.CreateHash(id, path)
}
