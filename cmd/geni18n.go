package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strconv"
	"strings"
)

var (
	debug         = flag.Bool("debug", false, "enable debug mode and print AST")
	dirName       = flag.String("in", "", "input dir: /path/to/go/pkg")
	outputDir     = flag.String("out", "", "output dir: /path/to/i18n/files")
	domain        = flag.String("domain", "default", "Domain")
	currentDomain = "default"

	fset        *token.FileSet
	domainFiles map[string]*os.File
	currentFile string
	msgids      = map[string]int{}
)

func main() {
	flag.Parse()
	currentDomain = *domain

	// Init logger
	log.SetFlags(0)

	// Init domain files
	domainFiles = make(map[string]*os.File)

	// Check if dir name parameter is valid
	log.Println(*dirName)
	f, err := os.Stat(*dirName)
	if err != nil {
		log.Fatal(err)
	}

	// Process file or dir
	if f.IsDir() {
		ParseDir(*dirName)
	} else {
		parseFile(*dirName)
	}
}

func getDomainFile(domain string) *os.File {
	// Return existent when available
	if f, ok := domainFiles[domain]; ok {
		return f
	}

	// If the file doesn't exist, create it.
	filePath := path.Join(*outputDir, domain+".po")
	f, err := os.OpenFile(filePath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	domainFiles[domain] = f
	writePoHeader(f)

	return f
}

func writePoHeader(f *os.File) {
	h := `msgid ""
msgstr ""
"Plural-Forms: nplurals=2; plural=(n != 1);\n"
"MIME-Version: 1.0\n"
"Content-Type: text/plain; charset=UTF-8\n"
"Content-Transfer-Encoding: 8bit\n"
"Language: \n"
"X-Generator: xgotext\n"
	`
	f.Write([]byte(h))
}

func write(dom, msgid string) {
	if _, ok := msgids[msgid]; ok {
		return
	}
	f := getDomainFile(dom)
	f.Write([]byte("\nmsgid " + msgid))
	f.Write([]byte("\nmsgstr \"\""))
	f.Write([]byte("\n"))
	msgids[msgid] = 0
}

func writePlural(dom, msgid, msgidPlural string) {
	f := getDomainFile(dom)
	f.Write([]byte("\nmsgid " + msgid))
	f.Write([]byte("\nmsgid_plural " + msgidPlural))
	f.Write([]byte("\nmsgstr[0] \"\""))
	f.Write([]byte("\nmsgstr[1] \"\""))
	f.Write([]byte("\n"))
}

func writeContext(dom, ctx string) {
	f := getDomainFile(dom)
	f.Write([]byte("\nmsgctxt " + ctx))
}

func writeComments(dom, file, call string) {
	f := getDomainFile(dom)
	f.Write([]byte("\n#: " + file))
	f.Write([]byte("\n#. " + call))
}

func GetAllFiles(dirPth string) (files []string, err error) {
	var dirs []string
	dir, err := ioutil.ReadDir(dirPth)
	if err != nil {
		return nil, err
	}

	PthSep := string(os.PathSeparator)
	//suffix = strings.ToUpper(suffix) //忽略后缀匹配的大小写

	for _, fi := range dir {
		if fi.IsDir() { // 目录, 递归遍历
			dirs = append(dirs, dirPth+PthSep+fi.Name())
			GetAllFiles(dirPth + PthSep + fi.Name())
		} else {
			// 过滤指定格式
			ok := strings.HasSuffix(fi.Name(), ".go")
			if ok {
				files = append(files, dirPth+PthSep+fi.Name())
			}
		}
	}

	// 读取子目录下文件
	for _, table := range dirs {
		temp, _ := GetAllFiles(table)
		for _, temp1 := range temp {
			files = append(files, temp1)
		}
	}

	return files, nil
}

func ParseDir(dirName string) error {
	files, err := GetAllFiles(dirName)
	if err != nil {
		return err
	}

	for _, fn := range files {
		parseFile(fn)
	}

	return nil
}

func parseDir(dirName string) error {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dirName, nil, parser.AllErrors)
	if err != nil {
		log.Fatal(err)
	}

	for _, pkg := range pkgs {
		for fn := range pkg.Files {
			fmt.Println(fn)
			//parseFile(fn)
		}
	}

	return nil
}

func parseFile(fileName string) error {
	// Remember current file to write comments on .po file
	currentFile = fileName

	// Parse AST
	fset = token.NewFileSet()
	node, err := parser.ParseFile(fset, fileName, nil, parser.AllErrors)
	if err != nil {
		log.Fatal(err)
		fmt.Println("Error: ", err)
		return err
	}

	//Debug mode
	if *debug {
		ast.Print(fset, node)
	}

	ast.Inspect(node, inspectFile)

	return nil
}

func inspectFile(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.CallExpr:
		inspectCallExpr(x)
	}

	return true
}

func inspectCallExpr(n *ast.CallExpr) {
	if se, ok := n.Fun.(*ast.SelectorExpr); ok {
		switch se.Sel.String() {
		case "T":
			parseGet(n)

		case "GetN":
			parseGetN(n)

		case "GetD":
			parseGetD(n)

		case "GetND":
			parseGetND(n)

		case "GetC":
			parseGetC(n)

		case "GetNC":
			parseGetNC(n)

		case "GetDC":
			parseGetDC(n)

		case "GetNDC":
			parseGetNDC(n)

		case "SetDomain":
			parseSetDomain(n)

		}
	}
}

func parseGet(call *ast.CallExpr) {
	if call.Args != nil && len(call.Args) > 0 {
		if lit, ok := call.Args[0].(*ast.BasicLit); ok {
			if lit.Kind == token.STRING {
				selectExpr := call.Fun.(*ast.SelectorExpr).X
				exprStr := call.Fun.(*ast.SelectorExpr).Sel.String()
				exprPath := fmt.Sprintf("%s:%d", fset.Position(call.Lparen).Filename, fset.Position(call.Lparen).Line)
				var ok bool
				for !ok {
					switch v := selectExpr.(type) {
					case *ast.SelectorExpr:
						selectExpr = v.X
						exprStr = fmt.Sprintf("%s.%s", v.Sel.Name, exprStr)
					case *ast.Ident:
						exprStr = fmt.Sprintf("%s.%s", v.Name, exprStr)
						ok = true
					default:
						log.Panicf("do not found selectExpr type: %v", v)
					}
				}
				writeComments(currentDomain,
					exprPath,
					exprStr,
				)
				write(currentDomain, lit.Value)
			}
		}
	}
}

func parseGetN(call *ast.CallExpr) {
	if call.Args == nil || len(call.Args) < 3 {
		return
	}

	if lit, ok := call.Args[0].(*ast.BasicLit); ok {
		if lit1, ok1 := call.Args[1].(*ast.BasicLit); ok1 {
			if lit.Kind == token.STRING && lit1.Kind == token.STRING {
				switch x := call.Args[2].(type) {
				case *ast.BasicLit:
					if x.Kind != token.INT {
						return
					}

				case *ast.Ident:
					if x.Obj.Kind != ast.Var && x.Obj.Kind != ast.Con {
						return
					}
				default:
					return
				}
				writeComments(currentDomain,
					fmt.Sprintf("%s:%d", fset.Position(call.Lparen).Filename, fset.Position(call.Lparen).Line),
					fmt.Sprintf("%s.%s", call.Fun.(*ast.SelectorExpr).X.(*ast.Ident).Name, call.Fun.(*ast.SelectorExpr).Sel.String()),
				)
				writePlural(currentDomain, lit.Value, lit1.Value)
			}
		}
	}
}

func parseGetD(call *ast.CallExpr) {
	if call.Args != nil && len(call.Args) > 1 {
		if lit, ok := call.Args[0].(*ast.BasicLit); ok {
			if lit1, ok := call.Args[1].(*ast.BasicLit); ok {
				if lit.Kind == token.STRING && lit1.Kind == token.STRING {
					dom, err := strconv.Unquote(lit.Value)
					if err != nil {
						log.Fatal(err)
					}
					writeComments(dom,
						fmt.Sprintf("%s:%d", fset.Position(call.Lparen).Filename, fset.Position(call.Lparen).Line),
						fmt.Sprintf("%s.%s", call.Fun.(*ast.SelectorExpr).X.(*ast.Ident).Name, call.Fun.(*ast.SelectorExpr).Sel.String()),
					)
					write(dom, lit1.Value)
				}
			}
		}
	}
}

func parseGetND(call *ast.CallExpr) {
	if call.Args != nil && len(call.Args) > 2 {
		if lit, ok := call.Args[0].(*ast.BasicLit); ok {
			if lit1, ok := call.Args[1].(*ast.BasicLit); ok {
				if lit2, ok := call.Args[2].(*ast.BasicLit); ok {
					if lit.Kind == token.STRING && lit1.Kind == token.STRING && lit2.Kind == token.STRING {
						dom, err := strconv.Unquote(lit.Value)
						if err != nil {
							log.Fatal(err)
						}
						writeComments(dom,
							fmt.Sprintf("%s:%d", fset.Position(call.Lparen).Filename, fset.Position(call.Lparen).Line),
							fmt.Sprintf("%s.%s", call.Fun.(*ast.SelectorExpr).X.(*ast.Ident).Name, call.Fun.(*ast.SelectorExpr).Sel.String()),
						)
						writePlural(dom, lit1.Value, lit2.Value)
					}
				}
			}
		}
	}
}

func parseGetC(call *ast.CallExpr) {
	if call.Args != nil && len(call.Args) > 1 {
		if lit, ok := call.Args[0].(*ast.BasicLit); ok {
			if lit1, ok := call.Args[1].(*ast.BasicLit); ok {
				if lit.Kind == token.STRING && lit1.Kind == token.STRING {
					writeComments(currentDomain,
						fmt.Sprintf("%s:%d", fset.Position(call.Lparen).Filename, fset.Position(call.Lparen).Line),
						fmt.Sprintf("%s.%s", call.Fun.(*ast.SelectorExpr).X.(*ast.Ident).Name, call.Fun.(*ast.SelectorExpr).Sel.String()),
					)
					writeContext(currentDomain, lit1.Value)
					write(currentDomain, lit.Value)
				}
			}
		}
	}
}

func parseGetNC(call *ast.CallExpr) {
	if call.Args != nil && len(call.Args) > 3 {
		if lit, ok := call.Args[0].(*ast.BasicLit); ok {
			if lit1, ok := call.Args[1].(*ast.BasicLit); ok {
				if lit3, ok := call.Args[3].(*ast.BasicLit); ok {
					if lit.Kind == token.STRING && lit1.Kind == token.STRING && lit3.Kind == token.STRING {
						writeComments(currentDomain,
							fmt.Sprintf("%s:%d", fset.Position(call.Lparen).Filename, fset.Position(call.Lparen).Line),
							fmt.Sprintf("%s.%s", call.Fun.(*ast.SelectorExpr).X.(*ast.Ident).Name, call.Fun.(*ast.SelectorExpr).Sel.String()),
						)
						writeContext(currentDomain, lit3.Value)
						writePlural(currentDomain, lit.Value, lit1.Value)
					}
				}
			}
		}
	}
}

func parseGetDC(call *ast.CallExpr) {
	if call.Args != nil && len(call.Args) > 2 {
		if lit, ok := call.Args[0].(*ast.BasicLit); ok {
			if lit1, ok := call.Args[1].(*ast.BasicLit); ok {
				if lit2, ok := call.Args[2].(*ast.BasicLit); ok {
					if lit.Kind == token.STRING && lit1.Kind == token.STRING && lit2.Kind == token.STRING {
						dom, err := strconv.Unquote(lit.Value)
						if err != nil {
							log.Fatal(err)
						}
						writeComments(dom,
							fmt.Sprintf("%s:%d", fset.Position(call.Lparen).Filename, fset.Position(call.Lparen).Line),
							fmt.Sprintf("%s.%s", call.Fun.(*ast.SelectorExpr).X.(*ast.Ident).Name, call.Fun.(*ast.SelectorExpr).Sel.String()),
						)
						writeContext(dom, lit2.Value)
						write(dom, lit1.Value)
					}
				}
			}
		}
	}
}

func parseGetNDC(call *ast.CallExpr) {
	if call.Args != nil && len(call.Args) > 4 {
		if lit, ok := call.Args[0].(*ast.BasicLit); ok {
			if lit1, ok := call.Args[1].(*ast.BasicLit); ok {
				if lit2, ok := call.Args[2].(*ast.BasicLit); ok {
					if lit4, ok := call.Args[4].(*ast.BasicLit); ok {
						if lit.Kind == token.STRING && lit1.Kind == token.STRING && lit2.Kind == token.STRING && lit4.Kind == token.STRING {
							dom, err := strconv.Unquote(lit.Value)
							if err != nil {
								log.Fatal(err)
							}
							writeComments(dom,
								fmt.Sprintf("%s:%d", fset.Position(call.Lparen).Filename, fset.Position(call.Lparen).Line),
								fmt.Sprintf("%s.%s", call.Fun.(*ast.SelectorExpr).X.(*ast.Ident).Name, call.Fun.(*ast.SelectorExpr).Sel.String()),
							)
							writeContext(dom, lit4.Value)
							writePlural(dom, lit1.Value, lit2.Value)
						}
					}
				}
			}
		}
	}
}

func parseSetDomain(call *ast.CallExpr) {
	if call.Args != nil && len(call.Args) == 1 {
		if lit, ok := call.Args[0].(*ast.BasicLit); ok {
			if lit.Kind == token.STRING {
				cd, err := strconv.Unquote(lit.Value)
				if err == nil {
					currentDomain = cd
				}
			}
		}
	}
}
