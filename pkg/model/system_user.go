package model

import "sort"

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
	Comment              string   `json:"comment"`
	LoginMode            string   `json:"login_mode"`
	Password             string   `json:"password"`
	PrivateKey           string   `json:"private_key"`
	Actions              []string `json:"actions"`
	SftpRoot             string   `json:"sftp_root"`
	OrgId                string   `json:"org_id"`
	OrgName              string   `json:"org_name"`
	UsernameSameWithUser bool     `json:"username_same_with_user"`
	Token                string   `json:"token"`
}

type SystemUserAuthInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	UserName   string `json:"username"`
	Protocol   string `json:"protocol"`
	LoginMode  string `json:"login_mode"`
	Password   string `json:"password"`
	PrivateKey string `json:"private_key"`
	Token      string `json:"token"`
}

type systemUserSortBy func(user1, user2 *SystemUser) bool

func (by systemUserSortBy) Sort(users []SystemUser) {
	nodeSorter := &systemUserSorter{
		users:  users,
		sortBy: by,
	}
	sort.Sort(nodeSorter)
}

type systemUserSorter struct {
	users  []SystemUser
	sortBy func(user1, user2 *SystemUser) bool
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

func SortSystemUserByPriority(users []SystemUser) {
	systemUserSortBy(systemUserPrioritySort).Sort(users)
}