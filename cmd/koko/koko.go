package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jumpserver/koko/pkg/chen"
	"github.com/jumpserver/koko/pkg/koko"
)

var (
	Buildstamp = ""
	Githash    = ""
	Goversion  = ""
	Version    = "unknown"

	infoFlag = false

	configPath = ""
	localSQL   chen.StandaloneOptions
	localFlags = flag.NewFlagSet("local SQL workspace", flag.ContinueOnError)
)

func init() {
	flag.StringVar(&configPath, "f", "config.yml", "config.yml path")
	flag.BoolVar(&infoFlag, "V", false, "version info")
	localSQL.RegisterFlags(localFlags)
	localFlags.VisitAll(func(f *flag.Flag) { flag.Var(f.Value, f.Name, f.Usage) })
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: koko [-f config.yml] [-V]\n       koko --selector NAME [--selector NAME ...] [options]")
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "unexpected argument: %s\n", flag.Arg(0))
		flag.Usage()
		os.Exit(2)
	}
	if infoFlag {
		fmt.Printf("Version:             %s\n", Version)
		fmt.Printf("Git Commit Hash:     %s\n", Githash)
		fmt.Printf("UTC Build Time :     %s\n", Buildstamp)
		fmt.Printf("Go Version:          %s\n", Goversion)
		return
	}
	flag.Visit(func(f *flag.Flag) {
		if len(localSQL.Selectors) == 0 && localFlags.Lookup(f.Name) != nil {
			fmt.Fprintf(os.Stderr, "--%s requires --selector\n", f.Name)
			os.Exit(2)
		}
		if len(localSQL.Selectors) > 0 && f.Name == "f" {
			fmt.Fprintln(os.Stderr, "-f is for managed mode; use --backends with --selector")
			os.Exit(2)
		}
	})
	if len(localSQL.Selectors) > 0 {
		if err := localSQL.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	fmt.Printf(startWelcomeMsg, time.Now().Format(timeFormat), Version)
	koko.RunForever(configPath)
}

const (
	timeFormat      = "2006-01-02 15:04:05"
	startWelcomeMsg = `%s
KoKo Version %s, more see https://www.jumpserver.com
Quit the server with CONTROL-C.
`
)
