package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("go-func-params-too-long-detector", flag.ContinueOnError)
	flags.SetOutput(stderr)
	minimum := flags.Int("min", 5, "报告参数数量大于等于此值的函数")
	depth := flags.Int("group-depth", 1, "路径前缀分组深度，0 表示平铺输出")
	var ignored ignoreRules
	flags.Func("ignore", "忽略路径或通配符模式，可重复指定；相对于当前工作目录", ignored.add)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "用法: go-func-params-too-long-detector [选项] [文件或目录 ...]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *minimum < 1 {
		fmt.Fprintln(stderr, "错误: -min 必须大于等于 1")
		return 2
	}
	if *depth < 0 {
		fmt.Fprintln(stderr, "错误: -group-depth 必须大于等于 0")
		return 2
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
		return 2
	}
	paths := flags.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}

	var findings []finding
	failed := false
	seen := make(map[string]bool)
	reportError := func(err error) {
		fmt.Fprintf(stderr, "错误: %v\n", err)
		failed = true
	}
	scan := func(path string) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			reportError(err)
			return
		}
		if seen[absolute] {
			return
		}
		seen[absolute] = true
		displayPath, err := filepath.Rel(cwd, absolute)
		if err != nil {
			reportError(err)
			return
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			reportError(err)
			return
		}
		ast.Inspect(file, func(node ast.Node) bool {
			var name string
			var signature *ast.FuncType
			switch fn := node.(type) {
			case *ast.FuncDecl:
				name, signature = fn.Name.Name, fn.Type
				if fn.Recv != nil && len(fn.Recv.List) > 0 {
					name = "(" + receiverName(fn.Recv.List[0].Type) + ")." + name
				}
			case *ast.FuncLit:
				name, signature = "<anonymous>", fn.Type
			default:
				return true
			}
			count := signature.Params.NumFields()
			if count >= *minimum {
				var params bytes.Buffer
				if err := format.Node(&params, token.NewFileSet(), &ast.FuncType{Params: signature.Params}); err != nil {
					reportError(fmt.Errorf("%s: format parameters for %s: %w", path, name, err))
					return true
				}
				pos := fset.PositionFor(signature.Pos(), false)
				findings = append(findings, finding{
					path: displayPath, line: pos.Line, column: pos.Column,
					name: name, params: strings.TrimPrefix(params.String(), "func"),
				})
			}
			return true
		})
	}

	for _, path := range paths {
		if ignored.matches(path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			reportError(err)
			continue
		}
		if !info.IsDir() {
			if filepath.Ext(path) != ".go" {
				reportError(fmt.Errorf("%s 不是 Go 文件", path))
				continue
			}
			scan(path)
			continue
		}
		err = filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
			if ignored.matches(current) {
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if walkErr != nil {
				reportError(walkErr)
				return nil
			}
			if entry.IsDir() {
				if current != path && (entry.Name() == "vendor" || strings.HasPrefix(entry.Name(), ".")) {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink == 0 && filepath.Ext(current) == ".go" {
				scan(current)
			}
			return nil
		})
		if err != nil {
			reportError(err)
		}
	}
	writeFindings(stdout, findings, *depth)
	if failed {
		return 2
	}
	if len(findings) > 0 {
		return 1
	}
	return 0
}

// Generic receivers display only the type name to keep method names concise.
func receiverName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.StarExpr:
		return "*" + receiverName(expr.X)
	case *ast.IndexExpr:
		return receiverName(expr.X)
	case *ast.IndexListExpr:
		return receiverName(expr.X)
	default:
		return "?"
	}
}
