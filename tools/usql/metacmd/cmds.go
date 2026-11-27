package metacmd

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/xo/dburl"
	"github.com/xo/usql/drivers"
	"github.com/xo/usql/env"
	"github.com/xo/usql/text"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
)

// Cmd is a command implementation.
type Cmd struct {
	Section Section
	Name    string
	Desc    Desc
	Aliases map[string]Desc
	Process func(*Params) error
}

// cmds is the set of commands.
var cmds []Cmd

// cmdMap is the map of commands and their aliases.
var cmdMap map[string]Metacmd

// sectMap is the map of sections to its respective commands.
var sectMap map[Section][]Metacmd

func init() {
	cmds = []Cmd{
		Drivers: {
			Section: SectionGeneral,
			Name:    "drivers",
			Desc:    Desc{"display information about available database drivers", ""},
			Process: func(p *Params) error {
				stdout, stderr := p.Handler.IO().Stdout(), p.Handler.IO().Stderr()
				var cmd *exec.Cmd
				var wc io.WriteCloser
				if pager := env.Get("PAGER"); p.Handler.IO().Interactive() && pager != "" {
					var err error
					if wc, cmd, err = env.Pipe(stdout, stderr, pager); err != nil {
						return err
					}
					stdout = wc
				}
				available := drivers.Available()
				names := make([]string, len(available))
				var z int
				for k := range available {
					names[z] = k
					z++
				}
				sort.Strings(names)
				fmt.Fprintln(stdout, text.AvailableDrivers)
				for _, n := range names {
					s := "  " + n
					driver, aliases := dburl.SchemeDriverAndAliases(n)
					if driver != n {
						s += " (" + driver + ")"
					}
					if len(aliases) > 0 {
						if len(aliases) > 0 {
							s += " [" + strings.Join(aliases, ", ") + "]"
						}
					}
					fmt.Fprintln(stdout, s)
				}
				if cmd != nil {
					if err := wc.Close(); err != nil {
						return err
					}
					return cmd.Wait()
				}
				return nil
			},
		},
		Connect: {
			Section: SectionConnection,
			Name:    "c",
			Desc:    Desc{"connect to new database", "database name"},
			Aliases: map[string]Desc{
				"connect": {},
			},
			Process: func(p *Params) error {
				vals, err := p.GetAll(true)
				if err != nil {
					return err
				}
				ctx, cancel := signal.NotifyContext(context.WithValue(context.Background(), "CHANGE_DATABASE", "1"), os.Interrupt)
				defer cancel()
				return p.Handler.Open(ctx, vals...)
			},
		},
		Question: {
			Section: SectionHelp,
			Name:    "?",
			Desc:    Desc{"show help on backslash commands", "[commands]"},
			Aliases: map[string]Desc{
				"?":  {"show help on " + text.CommandName + " command-line options", "options"},
				"? ": {"show help on special variables", "variables"},
			},
			Process: func(p *Params) error {
				name, err := p.Get(false)
				if err != nil {
					return err
				}
				stdout, stderr := p.Handler.IO().Stdout(), p.Handler.IO().Stderr()
				var cmd *exec.Cmd
				var wc io.WriteCloser
				if pager := env.Get("PAGER"); p.Handler.IO().Interactive() && pager != "" {
					if wc, cmd, err = env.Pipe(stdout, stderr, pager); err != nil {
						return err
					}
					stdout = wc
				}
				switch name = strings.TrimSpace(strings.ToLower(name)); {
				case name == "options":
					Usage(stdout, true)
				case name == "variables":
					env.Listing(stdout)
				default:
					Listing(stdout)
				}
				if cmd != nil {
					if err := wc.Close(); err != nil {
						return err
					}
					return cmd.Wait()
				}
				return nil
			},
		},
		Quit: {
			Section: SectionGeneral,
			Name:    "q",
			Desc:    Desc{"quit " + text.CommandName, ""},
			Aliases: map[string]Desc{"quit": {}},
			Process: func(p *Params) error {
				p.Option.Quit = true
				return nil
			},
		},
		Transact: {
			Section: SectionTransaction,
			Name:    "begin",
			Desc:    Desc{"begin a transaction", ""},
			Aliases: map[string]Desc{
				"begin":    {"begin a transaction with isolation level", "[-read-only] [ISOLATION]"},
				"commit":   {"commit current transaction", ""},
				"rollback": {"rollback (abort) current transaction", ""},
				"abort":    {},
			},
			Process: func(p *Params) error {
				switch p.Name {
				case "commit":
					return p.Handler.Commit()
				case "rollback", "abort":
					return p.Handler.Rollback()
				}
				// read begin params
				readOnly := false
				ok, n, err := p.GetOptional(true)
				if ok {
					if n != "read-only" {
						return fmt.Errorf(text.InvalidOption, n)
					}
					readOnly = true
					if n, err = p.Get(true); err != nil {
						return err
					}
				}
				// build tx options
				var txOpts *sql.TxOptions
				if readOnly || n != "" {
					isolation := sql.LevelDefault
					switch strings.ToLower(n) {
					case "default", "":
					case "read-uncommitted":
						isolation = sql.LevelReadUncommitted
					case "read-committed":
						isolation = sql.LevelReadCommitted
					case "write-committed":
						isolation = sql.LevelWriteCommitted
					case "repeatable-read":
						isolation = sql.LevelRepeatableRead
					case "snapshot":
						isolation = sql.LevelSnapshot
					case "serializable":
						isolation = sql.LevelSerializable
					case "linearizable":
						isolation = sql.LevelLinearizable
					default:
						return text.ErrInvalidIsolationLevel
					}
					txOpts = &sql.TxOptions{
						Isolation: isolation,
						ReadOnly:  readOnly,
					}
				}
				// begin
				return p.Handler.Begin(txOpts)
			},
		},
		Describe: {
			Section: SectionInformational,
			Name:    "d[S+]",
			Desc:    Desc{"list tables, views, and sequences or describe table, view, sequence, or index", "[NAME]"},
			Aliases: map[string]Desc{
				"da[S+]": {"list aggregates", "[PATTERN]"},
				"df[S+]": {"list functions", "[PATTERN]"},
				"dm[S+]": {"list materialized views", "[PATTERN]"},
				"dv[S+]": {"list views", "[PATTERN]"},
				"ds[S+]": {"list sequences", "[PATTERN]"},
				"dn[S+]": {"list schemas", "[PATTERN]"},
				"dt[S+]": {"list tables", "[PATTERN]"},
				"di[S+]": {"list indexes", "[PATTERN]"},
				"dp[S]":  {"list table, view, and sequence access privileges", "[PATTERN]"},
				"l[+]":   {"list databases", ""},
			},
			Process: func(p *Params) error {
				ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
				defer cancel()
				m, err := p.Handler.MetadataWriter(ctx)
				if err != nil {
					return err
				}
				verbose := strings.ContainsRune(p.Name, '+')
				showSystem := strings.ContainsRune(p.Name, 'S')
				name := strings.TrimRight(p.Name, "S+")
				pattern, err := p.Get(true)
				if err != nil {
					return err
				}
				switch name {
				case "d":
					if pattern != "" {
						return m.DescribeTableDetails(p.Handler.URL(), pattern, verbose, showSystem)
					}
					return m.ListTables(p.Handler.URL(), "tvmsE", pattern, verbose, showSystem)
				case "df", "da":
					return m.DescribeFunctions(p.Handler.URL(), name, pattern, verbose, showSystem)
				case "dt", "dtv", "dtm", "dts", "dv", "dm", "ds":
					return m.ListTables(p.Handler.URL(), name, pattern, verbose, showSystem)
				case "dn":
					return m.ListSchemas(p.Handler.URL(), pattern, verbose, showSystem)
				case "di":
					return m.ListIndexes(p.Handler.URL(), pattern, verbose, showSystem)
				case "l":
					return m.ListAllDbs(p.Handler.URL(), pattern, verbose)
				case "dp":
					return m.ListPrivilegeSummaries(p.Handler.URL(), pattern, showSystem)
				}
				return nil
			},
		},
		SetPrintVar: {
			Section: SectionFormatting,
			Name:    "pset",
			Desc:    Desc{"set table output option", "[NAME [VALUE]]"},
			Aliases: map[string]Desc{
				"a": {"toggle between unaligned and aligned output mode", ""},
				"C": {"set table title, or unset if none", "[STRING]"},
				"f": {"show or set field separator for unaligned query output", "[STRING]"},
				"H": {"toggle HTML output mode", ""},
				"T": {"set HTML <table> tag attributes, or unset if none", "[STRING]"},
				"t": {"show only rows", "[on|off]"},
				"x": {"toggle expanded output", "[on|off|auto]"},
			},
			Process: func(p *Params) error {
				var ok bool
				var val string
				var err error
				switch p.Name {
				case "a", "H":
				default:
					ok, val, err = p.GetOK(true)
					if err != nil {
						return err
					}
				}
				// display variables
				if p.Name == "pset" && !ok {
					return env.Pwrite(p.Handler.IO().Stdout())
				}
				var field, extra string
				switch p.Name {
				case "pset":
					field = val
					ok, val, err = p.GetOK(true)
					if err != nil {
						return err
					}
				case "a":
					field = "format"
				case "C":
					field = "title"
				case "f":
					field = "fieldsep"
				case "H":
					field, extra = "format", "html"
				case "t":
					field = "tuples_only"
				case "T":
					field = "tableattr"
				case "x":
					field = "expanded"
				}
				if !ok {
					if val, err = env.Ptoggle(field, extra); err != nil {
						return err
					}
				} else {
					if val, err = env.Pset(field, val); err != nil {
						return err
					}
				}
				// special replacement name for expanded field, when 'auto'
				if field == "expanded" && val == "auto" {
					field = "expanded_auto"
				}
				// format output
				mask := text.FormatFieldNameSetMap[field]
				unsetMask := text.FormatFieldNameUnsetMap[field]
				switch {
				case strings.Contains(mask, "%d"):
					i, _ := strconv.Atoi(val)
					p.Handler.Print(mask, i)
				case unsetMask != "" && val == "":
					p.Handler.Print(unsetMask)
				case !strings.Contains(mask, "%"):
					p.Handler.Print(mask)
				default:
					if field == "time" {
						val = fmt.Sprintf("%q", val)
						if tfmt := env.GoTime(); tfmt != val {
							val = fmt.Sprintf("%s (%q)", val, tfmt)
						}
					}
					p.Handler.Print(mask, val)
				}
				return nil
			},
		},
		Stats: {
			Section: SectionInformational,
			Name:    "ss[+]",
			Desc:    Desc{"show stats for a table or a query", "[TABLE|QUERY] [k]"},
			Process: func(p *Params) error {
				ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
				defer cancel()
				m, err := p.Handler.MetadataWriter(ctx)
				if err != nil {
					return err
				}
				verbose := strings.ContainsRune(p.Name, '+')
				name := strings.TrimRight(p.Name, "+")
				pattern, err := p.Get(true)
				if err != nil {
					return err
				}
				k := 0
				if verbose {
					k = 3
				}
				if name == "ss" {
					name = "sswnulhmkf"
				}
				ok, val, err := p.GetOK(true)
				if err != nil {
					return err
				}
				if ok {
					verbose = true
					k, err = strconv.Atoi(val)
					if err != nil {
						return err
					}
				}
				return m.ShowStats(p.Handler.URL(), name, pattern, verbose, k)
			},
		},
	}
	// set up map
	cmdMap = make(map[string]Metacmd, len(cmds))
	sectMap = make(map[Section][]Metacmd, len(SectionOrder))
	for i, c := range cmds {
		mc := Metacmd(i)
		if mc == None {
			continue
		}
		name := c.Name
		if pos := strings.IndexRune(name, '['); pos != -1 {
			mods := strings.TrimRight(name[pos+1:], "]")
			name = name[:pos]
			cmdMap[name+mods] = mc
			if len(mods) > 1 {
				for _, r := range mods {
					cmdMap[name+string(r)] = mc
				}
			}
		}
		cmdMap[name] = mc
		for alias := range c.Aliases {
			if pos := strings.IndexRune(alias, '['); pos != -1 {
				mods := strings.TrimRight(alias[pos+1:], "]")
				alias = alias[:pos]
				cmdMap[alias+mods] = mc
				if len(mods) > 1 {
					for _, r := range mods {
						cmdMap[alias+string(r)] = mc
					}
				}
			}
			cmdMap[alias] = mc
		}
		sectMap[c.Section] = append(sectMap[c.Section], mc)
	}
}

// Usage is used by the [Question] command to display command line options.
var Usage = func(io.Writer, bool) {
}
