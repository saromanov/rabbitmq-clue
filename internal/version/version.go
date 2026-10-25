package version

import "runtime/debug"

// Build info, injected via -ldflags in release workflows when available.
var (
	version = "dev"
	commit  = "unknown"
)

// Info returns the version string for /api/version and startup logs.
func Info() map[string]string {
	v, c, goVer := version, commit, ""
	if bi, ok := debug.ReadBuildInfo(); ok {
		if goVer == "" {
			goVer = bi.GoVersion
		}
		if v == "dev" {
			if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
				v = bi.Main.Version
			}
			for _, s := range bi.Settings {
				if s.Key == "vcs.revision" && c == "unknown" {
					c = s.Value
				}
			}
		}
	}
	return map[string]string{
		"version": v,
		"commit":  c,
		"go":      goVer,
	}
}
