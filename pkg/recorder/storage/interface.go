package storage

type ReplayStorage interface {
	Upload(gZipFile, target string)
}

func NewStorageServer() ReplayStorage {
	return nil
}
