package srvconn

import (
	"os/user"
	"strconv"
	"syscall"

	"github.com/jumpserver/koko/pkg/localcommand"
)

var debug string

func BuildNobodyWithOpts(opts ...localcommand.Option) (nobodyOpts []localcommand.Option, err error) {
	nobodyOpts = make([]localcommand.Option, 0, len(opts)+1)
	nobodyOpts = append(nobodyOpts, opts...)
	envs := make([]string, 0, 2)
	nobodyOpts = append(nobodyOpts, localcommand.WithEnv(envs))
	if debug == "true" {
		return nobodyOpts, nil
	}
	nobody, err := user.Lookup("nobody")
	if err != nil {
		return nil, err
	}
	uid, _ := strconv.Atoi(nobody.Uid)
	gid, _ := strconv.Atoi(nobody.Gid)
	nobodyCredential := &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	nobodyOpts = append(nobodyOpts, localcommand.WithCmdCredential(nobodyCredential))
	return nobodyOpts, nil
}
