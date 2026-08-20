// Package version holds the application version, injected at build time
// via ldflags -X (see build/*/Taskfile.yml). Without injection it reports
// "dev" (development builds).
package version

// Version is the application version string.
var Version = "dev"
