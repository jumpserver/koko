package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jumpserver-dev/usql/feature"
	"github.com/jumpserver-dev/usql/handler"
	"github.com/jumpserver-dev/usql/internal/nosql"
	"github.com/jumpserver-dev/usql/store"
	"github.com/xo/usql/metacmd"
	"io"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/xo/dburl"
	"github.com/xo/usql/drivers"
	"github.com/xo/usql/env"
	"github.com/xo/usql/rline"
	"github.com/xo/usql/text"
)

// ContextExecutor is the command context.
type ContextExecutor interface {
	ExecuteContext(context.Context) error
}

// New builds the command context.
func New(cliargs []string) ContextExecutor {
	args := &Args{}
	var (
		bashCompletion       bool
		zshCompletion        bool
		fishCompletion       bool
		powershellCompletion bool
		noDescriptions       bool
		badHelp              bool
		listDrivers          bool
	)
	v := viper.New()
	c := &cobra.Command{
		Use:                text.CommandName + " [flags]... [DSN]",
		Short:              text.Short(),
		Version:            text.CommandVersion,
		SilenceErrors:      true,
		SilenceUsage:       true,
		DisableAutoGenTag:  true,
		DisableSuggestions: true,
		Args: func(_ *cobra.Command, cliargs []string) error {
			if len(cliargs) > 1 {
				return text.ErrWrongNumberOfArguments
			}
			return nil
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			commandUpper := text.CommandUpper()
			configFile := strings.TrimSpace(os.Getenv(commandUpper + "_CONFIG"))
			cmd.Flags().VisitAll(func(f *pflag.Flag) {
				if s := strings.TrimSpace(f.Value.String()); f.Name == "config" && s != "" {
					configFile = s
				}
			})
			if configFile != "" {
				v.SetConfigFile(configFile)
			} else {
				v.SetConfigName(text.ConfigName)
				if configDir, err := os.UserConfigDir(); err == nil {
					v.AddConfigPath(filepath.Join(configDir, text.CommandName))
				}
			}
			if err := v.ReadInConfig(); err != nil {
				if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
					return err
				}
			}
			v.SetEnvPrefix(commandUpper)
			v.AutomaticEnv()
			cmd.Flags().VisitAll(func(f *pflag.Flag) {
				if f.Name == "config" {
					return
				}
				_ = v.BindEnv(f.Name, commandUpper+"_"+strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_")))
				if !f.Changed && v.IsSet(f.Name) {
					_ = cmd.Flags().Set(f.Name, fmt.Sprintf("%v", v.Get(f.Name)))
				}
			})
			// unhide params
			switch {
			case bashCompletion,
				zshCompletion,
				fishCompletion,
				powershellCompletion,
				cmd.Name() == "__complete":
				flags := cmd.Root().Flags()
				for _, name := range []string{"no-psqlrc", "no-usqlrc", "var", "variable"} {
					flags.Lookup(name).Hidden = false
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, cliargs []string) error {
			// completions and short circuits
			switch {
			case bashCompletion:
				return cmd.GenBashCompletionV2(os.Stdout, !noDescriptions)
			case zshCompletion:
				if noDescriptions {
					return cmd.GenZshCompletionNoDesc(os.Stdout)
				}
				return cmd.GenZshCompletion(os.Stdout)
			case fishCompletion:
				return cmd.GenFishCompletion(os.Stdout, !noDescriptions)
			case powershellCompletion:
				if noDescriptions {
					return cmd.GenPowerShellCompletion(os.Stdout)
				}
				return cmd.GenPowerShellCompletionWithDesc(os.Stdout)
			case badHelp:
				return errors.New("unknown shorthand flag: 'h' in -h")
			case listDrivers:
				return writeAvailableDrivers(os.Stdout)
			}
			// run
			if len(cliargs) > 0 {
				args.DSN = cliargs[0]
			}
			return Run(cmd.Context(), args, v.GetStringMap("connections"), v.GetString("init"))
		},
	}

	c.SetVersionTemplate("{{ .Name }} {{ .Version }}\n")
	c.SetArgs(cliargs[1:])
	c.SetUsageTemplate(text.UsageTemplate)

	flags := c.Flags()
	flags.SortFlags = false

	// completions / short circuits
	flags.BoolVar(&bashCompletion, "completion-script-bash", false, "output bash completion script and exit")
	flags.BoolVar(&zshCompletion, "completion-script-zsh", false, "output zsh completion script and exit")
	flags.BoolVar(&fishCompletion, "completion-script-fish", false, "output fish completion script and exit")
	flags.BoolVar(&powershellCompletion, "completion-script-powershell", false, "output powershell completion script and exit")
	flags.BoolVar(&noDescriptions, "no-descriptions", false, "disable descriptions in completion scripts")
	flags.BoolVarP(&badHelp, "bad-help", "h", false, "bad help")
	flags.BoolVar(&listDrivers, "drivers", false, "list available database drivers and exit")

	// command / file flags
	flags.VarP(commandOrFile{args, true}, "command", "c", "run only single command (SQL or internal) and exit")
	flags.VarP(commandOrFile{args, false}, "file", "f", "execute commands from file and exit")

	// general flags
	flags.BoolVarP(&args.NoPassword, "no-password", "w", false, "never prompt for password")
	flags.BoolVarP(&args.NoInit, "no-init", "X", false, "do not execute initialization scripts (aliases: --no-rc --no-psqlrc --no-usqlrc)")
	flags.BoolVar(&args.NoInit, "no-rc", false, "do not read startup file")
	flags.BoolVar(&args.NoInit, "no-psqlrc", false, "do not read startup file")
	flags.BoolVar(&args.NoInit, "no-usqlrc", false, "do not read startup file")
	flags.VarP(filevar{&args.Out}, "out", "o", "output file")
	flags.BoolVarP(&args.ForcePassword, "password", "W", false, "force password prompt (should happen automatically)")
	flags.BoolVarP(&args.SingleTransaction, "single-transaction", "1", false, "execute as a single transaction (if non-interactive)")

	ss := func(v *[]string, name, short, usage, placeholder string, vals ...string) {
		f := flags.VarPF(vs{v, vals, placeholder}, name, short, usage)
		if placeholder == "" {
			f.DefValue, f.NoOptDefVal = "true", "true"
		}
	}
	// set
	ss(&args.Vars, "set", "v", `set variable NAME to VALUE (see \set command, aliases: --var --variable)`, "NAME=VALUE")
	ss(&args.Vars, "var", "", "set variable NAME to VALUE", "NAME=VALUE")
	ss(&args.Vars, "variable", "", "set variable NAME to VALUE", "NAME=VALUE")
	// cset
	ss(&args.Cvars, "cset", "N", `set named connection NAME to DSN (see \cset command)`, "NAME=DSN")
	// pset
	ss(&args.Pvars, "pset", "P", `set printing option VAR to ARG (see \pset command)`, "VAR=ARG")
	// pset flags
	ss(&args.Pvars, "field-separator", "F", `field separator for unaligned and CSV output (default "|" and ",")`, "FIELD-SEPARATOR", "fieldsep=%q", "csv_fieldsep=%q")
	ss(&args.Pvars, "record-separator", "R", `record separator for unaligned and CSV output (default \n)`, "RECORD-SEPARATOR", "recordsep=%q")
	ss(&args.Pvars, "table-attr", "T", "set HTML table tag attributes (e.g., width, border)", "TABLE-ATTR", "tableattr=%q")
	// pset bools
	ss(&args.Pvars, "no-align", "A", "unaligned table output mode", "", "format=unaligned")
	ss(&args.Pvars, "html", "H", "HTML table output mode", "", "format=html")
	ss(&args.Pvars, "tuples-only", "t", "print rows only", "", "tuples_only=on")
	ss(&args.Pvars, "expanded", "x", "turn on expanded table output", "", "expanded=on")
	ss(&args.Pvars, "field-separator-zero", "z", "set field separator for unaligned and CSV output to zero byte", "", "fieldsep_zero=on")
	ss(&args.Pvars, "record-separator-zero", "0", "set record separator for unaligned and CSV output to zero byte", "", "recordsep_zero=on")
	ss(&args.Pvars, "json", "J", "JSON output mode", "", "format=json")
	ss(&args.Pvars, "csv", "C", "CSV output mode", "", "format=csv")
	ss(&args.Pvars, "vertical", "G", "vertical output mode", "", "format=vertical")
	// set bools
	ss(&args.Vars, "quiet", "q", "run quietly (no messages, only query output)", "", "QUIET=on")

	// app config
	_ = flags.StringP("config", "", "", "config file")

	// manually set --version, see github.com/spf13/cobra/command.go
	_ = flags.BoolP("version", "V", false, "output version information, then exit")
	_ = flags.SetAnnotation("version", cobra.FlagSetByCobraAnnotation, []string{"true"})

	// manually set --help, see github.com/spf13/cobra/command.go
	_ = flags.BoolP("help", "?", false, "show this help, then exit")
	_ = c.Flags().SetAnnotation("help", cobra.FlagSetByCobraAnnotation, []string{"true"})

	// mark hidden
	for _, name := range []string{
		"no-rc", "no-psqlrc", "no-usqlrc", "var", "variable",
		"completion-script-bash", "completion-script-zsh", "completion-script-fish",
		"completion-script-powershell", "no-descriptions",
		"bad-help",
	} {
		flags.Lookup(name).Hidden = true
	}

	// expose to metacmd
	metacmd.Usage = func(w io.Writer, banner bool) {
		s := c.UsageString()
		if banner {
			s = text.Short() + "\n\n" + s
		}
		_, _ = w.Write([]byte(s))
	}
	return c
}

func writeAvailableDrivers(w io.Writer) error {
	available := drivers.Available()
	nameSet := make(map[string]struct{}, len(available))
	for name := range available {
		nameSet[name] = struct{}{}
		// URL aliases are supported by the same registered driver. Include them
		// so callers can check asset protocols such as mariadb and postgresql.
		_, aliases := dburl.SchemeDriverAndAliases(name)
		for _, alias := range aliases {
			nameSet[alias] = struct{}{}
		}
	}
	// These native sessions use the same binary and terminal, without passing
	// document/RESP commands through database/sql or the SQL statement parser.
	for _, name := range []string{"mongodb", "mongodb+srv", "redis", "rediss"} {
		nameSet[name] = struct{}{}
	}
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if _, err := fmt.Fprintln(w, name); err != nil {
			return err
		}
	}
	return nil
}

// Run runs the application.
func Run(ctx context.Context, args *Args, connections map[string]interface{}, initstr string) (runErr error) {
	// get user
	u, err := user.Current()
	if err != nil {
		return err
	}
	// get working directory
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	// determine if interactive
	interactive := isatty.IsTerminal(os.Stdout.Fd()) && isatty.IsTerminal(os.Stdin.Fd())
	cygwin := isatty.IsCygwinTerminal(os.Stdout.Fd()) && isatty.IsCygwinTerminal(os.Stdin.Fd())
	forceNonInteractive := len(args.CommandOrFiles) != 0

	/*
		// enable term graphics
		if !forceNonInteractive && interactive && !cygwin {
			// NOTE: this is done here and not in the env.init() package, because
			// NOTE: we need to determine if it is interactive first, otherwise it
			// NOTE: could mess up the non-interactive output with control characters
			var typ string
			if s, _ := env.Getenv(text.CommandUpper()+"_TERM_GRAPHICS", "TERM_GRAPHICS"); s != "" {
				typ = s
			}
			if err := env.Set("TERM_GRAPHICS", typ); err != nil {
				return err
			}
		}
	*/

	// configured named connections
	for name, v := range connections {
		if err := setConn(name, v); err != nil && !forceNonInteractive && interactive {
			fmt.Fprintln(os.Stderr, fmt.Sprintf(text.InvalidNamedConnection, name, err))
		}
	}

	// fmt.Fprintf(os.Stdout, "VARS: %v\nCVARS: %v\nPVARS: %v\n", args.Vars, args.Cvars, args.Pvars)

	// set vars
	for _, v := range args.Vars {
		if i := strings.Index(v, "="); i != -1 {
			_ = env.Set(v[:i], v[i+1:])
		} else {
			_ = env.Unset(v)
		}
	}
	// set cvars
	for _, v := range args.Cvars {
		if i := strings.Index(v, "="); i != -1 {
			s := v[i+1:]
			if c := s[0]; c == '\'' || c == '"' {
				if s, err = env.Dequote(s, c); err != nil {
					return err
				}
			}
			if err = env.Cset(v[:i], s); err != nil {
				return err
			}
		} else {
			if err = env.Cset(v, ""); err != nil {
				return err
			}
		}
	}
	// set pvars
	for _, v := range args.Pvars {
		if i := strings.Index(v, "="); i != -1 {
			s := v[i+1:]
			if c := s[0]; c == '\'' || c == '"' {
				if s, err = env.Dequote(s, c); err != nil {
					return err
				}
			}
			if _, err = env.Pset(v[:i], s); err != nil {
				return err
			}
		} else {
			if _, err = env.Ptoggle(v, ""); err != nil {
				return err
			}
		}
	}
	// create input/output
	l, err := rline.New(interactive, cygwin, forceNonInteractive, args.Out, env.HistoryFile(u))
	if err != nil {
		return err
	}
	defer l.Close()
	dsn := args.DSN
	if named, ok := env.Cget(dsn); ok && len(named) == 1 {
		dsn = named[0]
	}
	if nosql.IsDSN(dsn) {
		if args.SingleTransaction {
			return errors.New("--single-transaction is supported by SQL sessions only")
		}
		format, _ := env.Pget("format")
		if format != "aligned" && format != "unaligned" && format != "json" {
			return fmt.Errorf("%s output is supported by SQL sessions only", format)
		}
		config := nosql.Config{
			DSN: dsn, ForcePassword: args.ForcePassword, NoPassword: args.NoPassword,
			JSON: format == "json", Quiet: env.Get("QUIET") == "on",
			StopOnError: env.Get("ON_ERROR_STOP") == "on",
		}
		for _, input := range args.CommandOrFiles {
			config.Inputs = append(config.Inputs, nosql.Input{Command: input.Command, Value: input.Value})
		}
		if !args.NoInit {
			config.StartupFile, config.Init = env.RCFile(u), initstr
		}
		return nosql.Run(ctx, l, config)
	}
	// create handler
	h := handler.New(l, u, wd, args.NoPassword)
	defer func() {
		_ = h.Rollback()
		_ = h.Close()
	}()
	// force password
	if args.ForcePassword {
		if dsn, err = h.Password(dsn); err != nil {
			return err
		}
	}

	// 从 dsn 中 解析脱敏的参数

	var queryPart string
	parts := strings.SplitN(dsn, "?", 2)
	base := parts[0] // 去掉 query 后的部分
	if len(parts) > 1 {
		queryPart = parts[1]
	}

	values, err := url.ParseQuery(queryPart)
	if err != nil {
		return err
	}

	if values.Has(feature.DataMaskingKey) {
		rulesJson := values.Get(feature.DataMaskingKey)
		var rules []feature.DataMaskingRule
		if err := json.Unmarshal([]byte(rulesJson), &rules); err != nil {
			return err
		}
		rules = slices.DeleteFunc(rules, func(r feature.DataMaskingRule) bool { return !r.Enabled() })
		store.GetGlobalStore().Set(feature.DataMaskingKey, rules)
		if len(rules) != 0 {
			h.MaskDiagnostics()
			// Non-interactive failures and the last error at EOF reach main's logger.
			defer func() { runErr = h.DiagnosticError(runErr) }()
		}
		// 删除某个参数
		values.Del(feature.DataMaskingKey)
		// 如果还剩参数就重新拼回 DSN
		newDSN := base
		if len(values.Encode()) > 0 {
			newDSN = fmt.Sprintf("%s?%s", base, values.Encode())
		}
		dsn = newDSN
	}

	// open dsn
	if err = h.Open(ctx, dsn); err != nil {
		return err
	}
	// start transaction
	if args.SingleTransaction {
		if h.IO().Interactive() {
			return text.ErrSingleTransactionCannotBeUsedWithInteractiveMode
		}
		if err = h.BeginTx(ctx, nil); err != nil {
			return err
		}
	}
	// scripts
	if !args.NoInit {
		// rc file
		if rc := env.RCFile(u); rc != "" {
			if err = h.Include(rc, false); err != nil && err != text.ErrNoSuchFileOrDirectory {
				return err
			}
		}
		if s := strings.TrimSpace(initstr); s != "" {
			h.Reset([]rune(s + "\n"))
		}
	}
	// setup runner
	f := h.Run
	if len(args.CommandOrFiles) != 0 {
		f = runCommandOrFiles(h, args.CommandOrFiles)
	}
	// run
	if err = f(); err != nil {
		return err
	}
	// commit
	if args.SingleTransaction {
		return h.Commit()
	}
	return nil
}

// Args are the command line arguments.
type Args struct {
	DSN               string
	CommandOrFiles    []CommandOrFile
	Out               string
	ForcePassword     bool
	NoPassword        bool
	NoInit            bool
	SingleTransaction bool
	Vars              []string
	Cvars             []string
	Pvars             []string
}

// CommandOrFile is a special type to deal with interspersed -c, -f,
// command-line options, to ensure proper order execution.
type CommandOrFile struct {
	Command bool
	Value   string
}

// commandOrFile provides a [pflag.Value] to wrap the command or file value in
// [Args].
type commandOrFile struct {
	args    *Args
	command bool
}

// Set satisfies the [pflag.Value] interface.
func (c commandOrFile) Set(value string) error {
	c.args.CommandOrFiles = append(c.args.CommandOrFiles, CommandOrFile{
		Command: c.command,
		Value:   value,
	})
	return nil
}

// String satisfies the [pflag.Value] interface.
func (c commandOrFile) String() string {
	return ""
}

// Type satisfies the [pflag.Value] interface.
func (c commandOrFile) Type() string {
	if c.command {
		return "COMMAND"
	}
	return "FILE"
}

// vs handles setting vars with predefined values.
type vs struct {
	vars *[]string
	vals []string
	typ  string
}

// Set satisfies the [pflag.Value] interface.
func (p vs) Set(value string) error {
	if len(p.vals) != 0 {
		for _, v := range p.vals {
			if strings.Contains(v, "%") {
				*p.vars = append(*p.vars, fmt.Sprintf(v, value))
			} else {
				*p.vars = append(*p.vars, v)
			}
		}
	} else {
		*p.vars = append(*p.vars, value)
	}
	return nil
}

// String satisfies the [pflag.Value] interface.
func (vs) String() string {
	return ""
}

// Type satisfies the [pflag.Value] interface.
func (p vs) Type() string {
	if p.typ == "" {
		return "bool"
	}
	return p.typ
}

// filevar is a file var.
type filevar struct {
	v *string
}

// Set satisfies the [pflag.Value] interface.
func (p filevar) Set(value string) error {
	*p.v = value
	return nil
}

// String satisfies the [pflag.Value] interface.
func (filevar) String() string {
	return ""
}

// Type satisfies the [pflag.Value] interface.
func (filevar) Type() string {
	return "FILE"
}

// setConn sets a named connection.
func setConn(name string, v interface{}) error {
	switch x := v.(type) {
	case string:
		return env.Cset(name, x)
	case []string:
		return env.Cset(name, x...)
	case map[string]interface{}:
		urlstr, err := dburl.BuildURL(x)
		if err != nil {
			return err
		}
		return env.Cset(name, urlstr)
	}
	return text.ErrInvalidConfig
}

// runCommandOrFiles processes all the supplied commands or files.
func runCommandOrFiles(h *handler.Handler, commandsOrFiles []CommandOrFile) func() error {
	return func() error {
		for _, c := range commandsOrFiles {
			h.SetSingleLineMode(c.Command)
			if c.Command {
				h.Reset([]rune(c.Value))
				if err := h.Run(); err != nil {
					return err
				}
			} else {
				if err := h.Include(c.Value, false); err != nil {
					return err
				}
			}
		}
		return nil
	}
}
