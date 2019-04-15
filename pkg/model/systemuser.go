package model

import (
	"sort"
)

/*
	{"id": "fbd39f8c-fa3e-4c2b-948e-ce1e0380b4f9",
	"name": "docker_root",
	"username": "root",
	"priority": 19,
	"protocol": "ssh",
	"comment": "screencast",
	"login_mode": "auto"}
*/

type SystemUser struct {
	Id        string `json:"id"`
	Name      string `json:"name"`
	UserName  string `json:"username"`
	Priority  int    `json:"priority"`
	Protocol  string `json:"protocol"`
	Comment   string `json:"comment"`
	LoginMode string `json:"login_mode"`
}

type SystemUserAuthInfo struct {
	Id         string `json:"id"`
	Name       string `json:"name"`
	UserName   string `json:"username"`
	Protocol   string `json:"protocol"`
	LoginMode  string `json:"login_mode"`
	Password   string `json:"password"`
	PrivateKey string `json:"private_key"`
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
	return use1.Priority <= user2.Priority
}

func SortSystemUserByPriority(users []SystemUser) {
	systemUserSortBy(systemUserPrioritySort).Sort(users)
}
