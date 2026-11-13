package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

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
