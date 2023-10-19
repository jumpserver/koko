package srvconn

import (
	"os/user"
	"strconv"
	"syscall"

	"github.com/jumpserver/koko/pkg/localcommand"
)

func BuildNobodyWithOpts(opts ...localcommand.Option) (nobodyOpts []localcommand.Option, err error) {
	nobody, err := user.Lookup("nobody")
	if err != nil {
		return nil, err
	}
	uid, _ := strconv.Atoi(nobody.Uid)
	gid, _ := strconv.Atoi(nobody.Gid)
	nobodyOpts = make([]localcommand.Option, 0, len(opts)+1)
	nobodyOpts = append(nobodyOpts, opts...)
	nobodyCredential := localcommand.WithCmdCredential(&syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)})
	nobodyOpts = append(nobodyOpts, localcommand.WithEnv(make([]string, 0)))
	nobodyOpts = append(nobodyOpts, nobodyCredential)
	return nobodyOpts, nil
}
