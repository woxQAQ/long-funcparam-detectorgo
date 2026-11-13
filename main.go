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
	"sort"
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

type ignoreRules []string

func (rules *ignoreRules) add(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("ignore 规则不能为空")
	}
	pattern = filepath.Clean(pattern)
	if _, err := filepath.Match(pattern, ""); err != nil {
		return fmt.Errorf("无效的 ignore 规则 %q: %w", pattern, err)
	}
	// Resolve patterns containing path separators against the working directory; match bare names at any level.
	if strings.ContainsRune(pattern, filepath.Separator) || pattern == "." {
		absolute, err := filepath.Abs(pattern)
		if err != nil {
			return err
		}
		pattern = absolute
	}
	*rules = append(*rules, pattern)
	return nil
}

func (rules ignoreRules) matches(path string) bool {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	// Match ancestor directories so explicitly specified files follow the same ignore rules.
	for current := absolute; ; current = filepath.Dir(current) {
		for _, pattern := range rules {
			candidate := current
			if !filepath.IsAbs(pattern) {
				candidate = filepath.Base(current)
			}
			if matched, _ := filepath.Match(pattern, candidate); matched {
				return true
			}
		}
		if filepath.Dir(current) == current {
			return false
		}
	}
}

type finding struct {
	path         string
	line, column int
	name         string
	params       string
}

func writeFindings(out io.Writer, findings []finding, depth int) {
	groups := make(map[string][]finding)
	for _, item := range findings {
		prefix := ""
		if depth > 0 {
			dir := filepath.ToSlash(filepath.Dir(item.path))
			parts := strings.Split(dir, "/")
			if len(parts) > depth {
				parts = parts[:depth]
			}
			prefix = strings.Join(parts, "/")
		}
		groups[prefix] = append(groups[prefix], item)
	}
	prefixes := make([]string, 0, len(groups))
	for prefix := range groups {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	for index, prefix := range prefixes {
		items := groups[prefix]
		sort.Slice(items, func(i, j int) bool {
			if items[i].path != items[j].path {
				return items[i].path < items[j].path
			}
			if items[i].line != items[j].line {
				return items[i].line < items[j].line
			}
			return items[i].column < items[j].column
		})
		indent := ""
		if depth > 0 {
			if index > 0 {
				fmt.Fprintln(out)
			}
			fmt.Fprintf(out, "%s (%d 个函数)\n", prefix, len(items))
			indent = "  "
		}
		for _, item := range items {
			fmt.Fprintf(out, "%s%s:%d:%d: %s%s\n", indent, item.path, item.line, item.column, item.name, item.params)
		}
	}
}
