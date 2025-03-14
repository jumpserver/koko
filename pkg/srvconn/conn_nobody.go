package srvconn

import (
	"os"
	"os/user"
	"strconv"
	"syscall"

	"github.com/jumpserver/koko/pkg/localcommand"
	"github.com/jumpserver/koko/pkg/logger"
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
	envs := make([]string, 0, 2)
	redisCliFile := os.Getenv("REDISCLI_RCFILE")
	if redisCliFile != "" {
		envs = append(envs, "REDISCLI_RCFILE="+redisCliFile)
		logger.Infof("rediscli rcfile: %s", redisCliFile)
	}
	nobodyCredential := localcommand.WithCmdCredential(&syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)})
	nobodyOpts = append(nobodyOpts, localcommand.WithEnv(envs))
	nobodyOpts = append(nobodyOpts, nobodyCredential)
	return nobodyOpts, nil
}
