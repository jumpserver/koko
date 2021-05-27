package model

type RemoteAPP struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	AssetId    string             `json:"asset"`
	Parameters RemoteAppParameter `json:"parameter_remote_app"`
}

type RemoteAppParameter struct {
	Parameters       string `json:"parameters"`
	Program          string `json:"program"`
	WorkingDirectory string `json:"working_directory"`
}
