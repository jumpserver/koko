package model

import (
	"fmt"
	"sort"
	"strings"
)

const LoginModeManual = "manual"

const (
	AllAction      = "all"
	ConnectAction  = "connect"
	UploadAction   = "upload_file"
	DownloadAction = "download_file"
)

type SystemUser struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Username             string   `json:"username"`
	Priority             int      `json:"priority"`
	Protocol             string   `json:"protocol"`
	AdDomain             string   `json:"ad_domain"`
	Comment              string   `json:"comment"`
	LoginMode            string   `json:"login_mode"`
	Password             string   `json:"-"`
	PrivateKey           string   `json:"-"`
	Actions              []string `json:"actions"`
	SftpRoot             string   `json:"sftp_root"`
	OrgId                string   `json:"org_id"`
	OrgName              string   `json:"org_name"`
	UsernameSameWithUser bool     `json:"username_same_with_user"`
	Token                string   `json:"-"`
	SuEnabled            bool     `json:"su_enabled"`
	SuFrom               string   `json:"su_from"`
	SuType               string   `json:"su_type"`
	SuExtra              string   `json:"su_extra"`
}

func (s *SystemUser) String() string {
	return fmt.Sprintf("%s(%s)", s.Name, s.Username)
}

func (s *SystemUser) IsProtocol(p string) bool {
	return strings.EqualFold(s.Protocol, p)
}

type SystemUserAuthInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Username   string `json:"username"`
	Protocol   string `json:"protocol"`
	LoginMode  string `json:"login_mode"`
	Password   string `json:"password"`
	PrivateKey string `json:"private_key"`
	AdDomain   string `json:"ad_domain"`
	Token      string `json:"token"`
	OrgId      string `json:"org_id"`
	OrgName    string `json:"org_name"`
	PublicKey  string `json:"public_key"`

	UsernameSameWithUser bool `json:"username_same_with_user"`
}

func (s *SystemUserAuthInfo) String() string {
	return fmt.Sprintf("%s(%s)", s.Name, s.Username)
}

type systemUserSortBy func(sys1, sys2 *SystemUser) bool

func (by systemUserSortBy) Sort(sysUsers []SystemUser) {
	nodeSorter := &systemUserSorter{
		users:  sysUsers,
		sortBy: by,
	}
	sort.Sort(nodeSorter)
}

type systemUserSorter struct {
	users  []SystemUser
	sortBy systemUserSortBy
}

func (s *systemUserSorter) Len() int {
	return len(s.users)
}

func (s *systemUserSorter) Swap(i, j int) {
	s.users[i], s.users[j] = s.users[j], s.users[i]
}

func (s *systemUserSorter) Less(i, j int) bool {
	return s.sortBy(&s.users[i], &s.users[j])
}

func systemUserPrioritySort(use1, user2 *SystemUser) bool {
	return use1.Priority < user2.Priority
}

func SortSystemUserByPriority(sysUsers []SystemUser) {
	systemUserSortBy(systemUserPrioritySort).Sort(sysUsers)
}
