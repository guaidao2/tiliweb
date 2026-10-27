package version

// These values are overridden with -ldflags at release time.
var (
	Version   = "1.0.0"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func String() string {
	return Version
}
