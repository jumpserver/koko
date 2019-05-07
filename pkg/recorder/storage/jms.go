package storage

//var client = service.Client

func NewJmsStorage() ReplayStorage {
	//appService := auth.GetGlobalService()
	//return &Server{
	//	StorageType: "jms",
	//	service:     appService,
	//}
	return &Server{}
}

type Server struct {
	StorageType string
}

func (s *Server) Upload(gZipFilePath, target string) {
	//sessionID := strings.Split(filepath.Base(gZipFilePath), ".")[0]
	//_ = client.PushSessionReplay(gZipFilePath, sessionID)
}
