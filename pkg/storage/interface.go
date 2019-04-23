package storage

type Storage interface {
	Upload(gZipFile, target string)
}

func NewStorageServer() Storage {
	//conf := config.GetGlobalConfig()
	//
	//switch conf.TermConfig.RePlayStorage["TYPE"] {
	//case "server":
	//	return NewJmsStorage()
	//}
	return nil
}
