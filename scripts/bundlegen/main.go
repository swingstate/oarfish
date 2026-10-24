// Command bundlegen builds and checks the Oarfish release bundle.
//
// It is the single place that knows the bundle layout, the manifest format and
// the checksums format, so local builds (make release-bundle) and the release
// workflow cannot diverge. It uses only the standard library; tar and gzip are
// written by Go rather than by the platform tar, so macOS and Linux produce the
// same bytes for the same inputs and Go version.
//
// Subcommands:
//
//	bundlegen bundle    -binary F -license F -version V -commit C -epoch N -out DIR
//	bundlegen checksums -dir DIR FILE...
//	bundlegen check     -dir DIR
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "bundle":
		err = runBundle(os.Args[2:])
	case "checksums":
		err = runChecksums(os.Args[2:])
	case "check":
		err = runCheck(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "bundlegen:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: bundlegen bundle|checksums|check [flags]")
	os.Exit(2)
}
