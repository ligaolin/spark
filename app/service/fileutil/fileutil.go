// Package fileutil provides small helpers shared by the local / SFTP / FTP
// services for file search and the built-in text editor (binary detection,
// name/content matching, replacement and size limits).
package fileutil

import (
	"regexp"
	"strings"
)

const (
	// MaxContentSearchSize is the largest file (bytes) inspected during
	// content search; bigger files are skipped.
	MaxContentSearchSize = 10 << 20
	// MaxEditSize is the largest file (bytes) the built-in editor opens.
	MaxEditSize = 20 << 20
	// MaxMatchesPerFile caps how many content matches are returned per file.
	MaxMatchesPerFile = 200
	// MaxSearchResults caps the total number of results returned per search.
	MaxSearchResults = 5000
)

// LineHit is one matched line from a content search.
type LineHit struct {
	LineNo int
	Line   string
}

// IsBinary reports whether data looks binary by sampling for NUL bytes.
func IsBinary(data []byte) bool {
	n := len(data)
	if n > 8192 {
		n = 8192
	}
	for _, b := range data[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}

// MatchName reports whether a file name matches pattern (case-insensitive
// substring match). Kept for the documents service which has no case option.
func MatchName(name, pattern string) bool {
	return MatchNameOpt(name, pattern, false)
}

// MatchNameOpt is MatchName with explicit case sensitivity.
func MatchNameOpt(name, pattern string, caseSensitive bool) bool {
	if caseSensitive {
		return strings.Contains(name, pattern)
	}
	return strings.Contains(strings.ToLower(name), strings.ToLower(pattern))
}

// MatchLines searches data line by line for pattern (case-insensitive),
// returning up to max hits. Long lines are truncated for display.
// Kept for the documents service; use MatchLinesOpt for case/regex control.
func MatchLines(data []byte, pattern string, max int) []LineHit {
	hits, _ := MatchLinesOpt(data, pattern, max, false, false)
	return hits
}

// MatchLinesOpt searches data line by line for pattern, honouring case
// sensitivity and (for content search) regex mode. It returns an error when
// the regex pattern is invalid.
func MatchLinesOpt(data []byte, pattern string, max int, caseSensitive, useRegex bool) ([]LineHit, error) {
	if max <= 0 {
		max = MaxMatchesPerFile
	}
	re, err := compileContentMatcher(pattern, caseSensitive, useRegex)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	hits := make([]LineHit, 0, 8)
	for i, line := range lines {
		if re.MatchString(line) {
			line = strings.TrimRight(line, "\r")
			if len(line) > 500 {
				line = line[:500] + "…"
			}
			hits = append(hits, LineHit{LineNo: i + 1, Line: line})
			if len(hits) >= max {
				break
			}
		}
	}
	return hits, nil
}

// ReplaceAllContent replaces every content match of pattern in data with
// replacement, returning the new content and the number of replaced matches.
// replacement follows Go regexp expansion rules ($1 etc.), matching VS Code.
func ReplaceAllContent(data []byte, pattern, replacement string, caseSensitive, useRegex bool) ([]byte, int, error) {
	re, err := compileContentMatcher(pattern, caseSensitive, useRegex)
	if err != nil {
		return nil, 0, err
	}
	count := len(re.FindAllIndex(data, -1))
	out := re.ReplaceAll(data, []byte(replacement))
	return out, count, nil
}

// splitExcludePatterns splits an exclude string on commas and newlines and
// trims each resulting pattern.
func splitExcludePatterns(exclude string) []string {
	fields := strings.FieldsFunc(exclude, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(fields))
	for _, s := range fields {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// CompileExcludes compiles comma/newline-separated glob patterns into regexps.
// Invalid patterns are silently skipped.
func CompileExcludes(exclude string) []*regexp.Regexp {
	patterns := splitExcludePatterns(exclude)
	if len(patterns) == 0 {
		return nil
	}
	res := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if re, err := regexp.Compile(globToRegexp(p)); err == nil {
			res = append(res, re)
		}
	}
	return res
}

// MatchesExclude reports whether fullPath (normalized to forward slashes) or
// its base name matches any compiled exclude glob.
func MatchesExclude(res []*regexp.Regexp, fullPath, baseName string) bool {
	if len(res) == 0 {
		return false
	}
	norm := strings.ReplaceAll(fullPath, "\\", "/")
	for _, re := range res {
		if re.MatchString(norm) || re.MatchString(baseName) {
			return true
		}
	}
	return false
}

// globToRegexp converts a shell-style glob supporting ** / * / ? into a
// regular expression string (path separator is '/').
func globToRegexp(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					b.WriteString("(.*/)?")
					i += 2
				} else {
					b.WriteString(".*")
					i++
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	b.WriteString("$")
	return b.String()
}

// ValidateContentPattern returns an error if pattern is not a valid regular
// expression in regex mode. Literal mode never fails.
func ValidateContentPattern(pattern string, caseSensitive, useRegex bool) error {
	_, err := compileContentMatcher(pattern, caseSensitive, useRegex)
	return err
}

// compileContentMatcher builds a regexp for content matching. When useRegex is
// false the pattern is treated as a literal substring (quoted); case
// insensitivity is applied via the (?i) flag.
func compileContentMatcher(pattern string, caseSensitive, useRegex bool) (*regexp.Regexp, error) {
	expr := pattern
	if !useRegex {
		expr = regexp.QuoteMeta(pattern)
	}
	if !caseSensitive {
		expr = "(?i)" + expr
	}
	return regexp.Compile(expr)
}
