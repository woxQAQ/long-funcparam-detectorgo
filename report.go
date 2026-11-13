package main

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

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
