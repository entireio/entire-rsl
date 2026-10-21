// Package versioninfo carries the build stamp of entire-rsl and the versions
// of the components it links.
package versioninfo

import (
	"runtime"
	"runtime/debug"
)

// Version and Commit are stamped by goreleaser through -ldflags -X.
var (
	Version = "dev"
	Commit  = "unknown"
)

const gittufModule = "github.com/gittuf/gittuf"

// Info describes the running binary.
type Info struct {
	Version       string
	Commit        string
	GoVersion     string
	GittufVersion string
}

// Get returns the build stamp and the linked gittuf module version.
func Get() Info {
	return Info{
		Version:       Version,
		Commit:        Commit,
		GoVersion:     runtime.Version(),
		GittufVersion: gittufVersion(),
	}
}

// gittufVersion names the gittuf module the binary was built with. A replace
// directive is reported as its target, since that is the code actually linked.
func gittufVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}

	for _, dep := range info.Deps {
		if dep.Path != gittufModule {
			continue
		}
		if dep.Replace != nil {
			return dep.Replace.Path + " " + dep.Replace.Version + " (replaces " + dep.Version + ")"
		}
		return dep.Path + " " + dep.Version
	}

	return "unknown"
}
