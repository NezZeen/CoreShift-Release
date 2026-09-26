package service

import "strconv"

// The build stamps these with -ldflags -X (packaging/windows/build.ps1):
// Version from the repository's VERSION file, Build the number of commits,
// Commit the short hash, with "-dirty" when the tree had uncommitted
// changes. A plain go build leaves them as they are here.
var (
	Version = "dev"
	Build   = "0"
	Commit  = ""
)

// BuildNumber is Build as a number, 0 when unknown.
func BuildNumber() int {
	n, _ := strconv.Atoi(Build)
	return n
}

// VersionString is how the version is shown: "0.2.0 (build 14, a1b2c3d)".
func VersionString() string {
	s := Version
	if n := BuildNumber(); n > 0 {
		s += " (build " + strconv.Itoa(n)
		if Commit != "" {
			s += ", " + Commit
		}
		s += ")"
	}
	return s
}
