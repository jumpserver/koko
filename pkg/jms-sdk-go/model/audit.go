package model

import (
	"github.com/jumpserver/koko/pkg/jms-sdk-go/common"
)

type FTPLog struct {
	User       string         `json:"user"`
	Hostname   string         `json:"asset"`
	OrgID      string         `json:"org_id"`
	SystemUser string         `json:"system_user"`
	RemoteAddr string         `json:"remote_addr"`
	Operate    string         `json:"operate"`
	Path       string         `json:"filename"`
	DateStart  common.UTCTime `json:"date_start"`
	IsSuccess  bool           `json:"is_success"`
}

const (
	OperateDownload = "download"
	OperateUpload   = "upload"
)

const (
	OperateRemoveDir = "rmdir"
	OperateRename    = "rename"
	OperateMkdir     = "mkdir"
	OperateDelete    = "delete"
	OperateSymlink   = "symlink"
)
