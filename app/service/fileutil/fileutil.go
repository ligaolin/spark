// Package fileutil provides small helpers shared by the local / SFTP / FTP
// services for file search and the built-in text editor (binary detection,
// name/content matching and size limits).
package fileutil

import "strings"

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
// substring match).
func MatchName(name, pattern string) bool {
	return strings.Contains(strings.ToLower(name), strings.ToLower(pattern))
}

// MatchLines searches data line by line for pattern (case-insensitive),
// returning up to max hits. Long lines are truncated for display.
func MatchLines(data []byte, pattern string, max int) []LineHit {
	if max <= 0 {
		max = MaxMatchesPerFile
	}
	lower := strings.ToLower(pattern)
	lines := strings.Split(string(data), "\n")
	hits := make([]LineHit, 0, 8)
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), lower) {
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
	return hits
}
