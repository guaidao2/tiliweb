package version

// These values are overridden with -ldflags at release time.
var (
	Version   = "1.0.1"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func String() string {
	return Version
}
