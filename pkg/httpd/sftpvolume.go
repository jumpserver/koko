package httpd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/LeeEirc/elfinder"
	"github.com/pkg/sftp"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/srvconn"
)

type volumeOption struct {
	addr         string
	user         *model.User
	asset        *model.Asset
	connectToken *model.ConnectToken
}
type VolumeOption func(*volumeOption)

func WithUser(user *model.User) VolumeOption {
	return func(opts *volumeOption) {
		opts.user = user
	}
}

func WithAddr(addr string) VolumeOption {
	return func(opts *volumeOption) {
		opts.addr = addr
	}
}

func WithAsset(asset *model.Asset) VolumeOption {
	return func(opts *volumeOption) {
		opts.asset = asset
	}
}

func WithConnectToken(connectToken *model.ConnectToken) VolumeOption {
	return func(opts *volumeOption) {
		opts.connectToken = connectToken
	}
}

func NewUserVolume(jmsService *service.JMService, opts ...VolumeOption) *UserVolume {
	var volOpts volumeOption
	for _, opt := range opts {
		opt(&volOpts)
	}
	homeName := "Home"
	basePath := "/"
	asset := volOpts.asset
	if volOpts.connectToken != nil {
		asset = &volOpts.connectToken.Asset
	}
	if asset != nil {
		folderName := asset.Name
		if strings.Contains(folderName, "/") {
			folderName = strings.ReplaceAll(folderName, "/", "_")
		}
		homeName = folderName
		basePath = filepath.Join("/", homeName)
	}
	sftpOpts := make([]srvconn.UserSftpOption, 0, 5)
	if volOpts.connectToken != nil {
		sftpOpts = append(sftpOpts, srvconn.WithConnectToken(volOpts.connectToken))
	}
	if volOpts.asset != nil {
		sftpOpts = append(sftpOpts, srvconn.WithAssets([]model.Asset{*volOpts.asset}))
	}
	sftpOpts = append(sftpOpts, srvconn.WithUser(volOpts.user))
	sftpOpts = append(sftpOpts, srvconn.WithRemoteAddr(volOpts.addr))
	sftpOpts = append(sftpOpts, srvconn.WithLoginFrom(model.LoginFromWeb))
	userSftp := srvconn.NewUserSftpConn(jmsService, sftpOpts...)
	rawID := fmt.Sprintf("%s@%s", volOpts.user.Username, volOpts.addr)

	recorder := proxy.GetFTPFileRecorder(jmsService)
	uVolume := &UserVolume{
		Uuid:          elfinder.GenerateID(rawID),
		UserSftp:      userSftp,
		HomeName:      homeName,
		basePath:      basePath,
		chunkFilesMap: make(map[int]*sftp.File),
		lock:          new(sync.Mutex),
		recorder:      recorder,
		ftpLogMap:     make(map[int]*model.FTPLog),
	}
	return uVolume
}

type UserVolume struct {
	Uuid     string
	UserSftp *srvconn.UserSftpConn
	HomeName string
	basePath string

	chunkFilesMap map[int]*sftp.File
	ftpLogMap     map[int]*model.FTPLog
	lock          *sync.Mutex

	recorder *proxy.FTPFileRecorder
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
		if originFileInfolist[i].Mode()&os.ModeSymlink != 0 {
			linkInfo := NewElfinderFileInfo(u.Uuid, path, originFileInfolist[i])
			_, err := u.UserSftp.ReadDir(filepath.Join(u.basePath, path, originFileInfolist[i].Name()))
			if err != nil {
				logger.Errorf("link file %s is not dir err: %s", originFileInfolist[i].Name(), err)
			} else {
				logger.Infof("link file %s is dir", originFileInfolist[i].Name())
				linkInfo.Mime = "directory"
				linkInfo.Dirs = 1
			}
			dirs = append(dirs, linkInfo)
			continue
		}

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
	sf, err := u.UserSftp.Open(filepath.Join(u.basePath, TrimPrefix(path)))
	if err != nil {
		return nil, err
	}
	if err1 := u.recorder.Record(sf.FTPLog, sf); err1 != nil {
		logger.Errorf("Record file err: %s", err1)
	}
	_, _ = sf.Seek(0, io.SeekStart)
	// 屏蔽 sftp*File 的 WriteTo 方法，防止调用 sftp stat 命令
	return &fileReader{sf}, nil
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
	logger.Debug("Volume upload file path: ", path, "|", filename, "|", uploadPath)
	var rest elfinder.FileDir
	fd, err := u.UserSftp.Create(filepath.Join(u.basePath, path))
	if err != nil {
		return rest, err
	}
	defer fd.Close()
	if err1 := u.recorder.Record(fd.FTPLog, reader); err1 != nil {
		logger.Errorf("Record file err: %s", err1)
	}
	_, _ = reader.(io.Seeker).Seek(0, io.SeekStart)
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
	ftpLog := u.ftpLogMap[cid]
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
		f, err := u.UserSftp.Create(filepath.Join(u.basePath, path))
		if err != nil {
			return err
		}
		fd = f.File
		ftpLog = f.FTPLog
		_, err = fd.Seek(rangeData.Offset, 0)
		if err != nil {
			return err
		}
		u.lock.Lock()
		u.chunkFilesMap[cid] = fd
		u.ftpLogMap[cid] = ftpLog
		u.lock.Unlock()
	}
	if err2 := u.recorder.Record(ftpLog, reader); err2 != nil {
		logger.Errorf("Record file err: %s", err2)
	}
	_, _ = reader.(io.Seeker).Seek(0, io.SeekStart)
	_, err = io.Copy(fd, reader)
	if err != nil {
		_ = fd.Close()
		u.lock.Lock()
		delete(u.chunkFilesMap, cid)
		delete(u.ftpLogMap, cid)
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
		delete(u.ftpLogMap, cid)
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
	if err1 := u.recorder.Record(fd.FTPLog, fd); err1 != nil {
		logger.Errorf("Record file err: %s", err1)
	}
	_, _ = fd.Seek(0, io.SeekStart)
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
	var (
		size int64
	)
	tz := time.Now().UnixNano()
	readPem := byte(1)
	writePem := byte(0)
	if fInfo, err := u.UserSftp.Stat(u.basePath); err == nil {
		size = fInfo.Size()
		tz = fInfo.ModTime().Unix()
		readPem, writePem = elfinder.ReadWritePem(fInfo.Mode())
	}
	var rest elfinder.FileDir
	rest.Name = u.HomeName
	rest.Hash = hashPath(u.Uuid, "/")
	rest.Size = size
	rest.Volumeid = u.Uuid
	rest.Mime = "directory"
	rest.Dirs = 1
	rest.Read, rest.Write = readPem, writePem
	rest.Locked = 1
	rest.Ts = tz
	return rest
}

func (u *UserVolume) Close() {
	u.UserSftp.Close()
	logger.Infof("User %s's volume close", u.UserSftp.User.Name)
}

func (u *UserVolume) Search(path, key string, mimes ...string) (res []elfinder.FileDir, err error) {
	originFileInfolist, err := u.UserSftp.Search(key)
	if err != nil {
		return nil, err
	}
	res = make([]elfinder.FileDir, 0, len(originFileInfolist))
	searchPath := fmt.Sprintf("/%s", srvconn.SearchFolderName)
	for i := 0; i < len(originFileInfolist); i++ {
		res = append(res, NewElfinderFileInfo(u.Uuid, searchPath, originFileInfolist[i]))

	}
	return
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

var (
	_ io.ReadCloser = (*fileReader)(nil)
)

type fileReader struct {
	read io.ReadCloser
}

func (f *fileReader) Read(p []byte) (nr int, err error) {
	return f.read.Read(p)
}

func (f *fileReader) Close() error {
	return f.read.Close()
}
