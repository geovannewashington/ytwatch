package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/geovannewashington/ytwatch/internal/update"
)

// What "ytwatch update" uses, as variables so tests do not use the network or
// replace the test program.
var (
	updateLatest     = update.Latest
	updateDownload   = update.Download
	updateExecutable = update.Executable
	updateReplace    = update.Replace
)

// runUpdate is "ytwatch update" and "ytwatch update --check". It returns the
// exit code. The check only says whether a newer release exists.
func runUpdate(args []string, current string, stdout, stderr io.Writer) int {
	check := len(args) == 1 && args[0] == "--check"
	if len(args) > 0 && !check {
		fmt.Fprintln(stderr, "usage: ytwatch update [--check]")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	latest, err := updateLatest(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "ytwatch: cannot find the latest release:", err)
		return 1
	}

	installed, err := update.ParseVersion(current)
	if err != nil {
		// A build from a checkout ("dev") has no number to compare.
		fmt.Fprintf(stdout, "ytwatch %s is a build from source. The latest release is %s.\n", current, latest)
		fmt.Fprintln(stdout, "Update it with: git pull && make install")

		return 1
	}

	if !latest.Newer(installed) {
		fmt.Fprintf(stdout, "ytwatch %s is up to date.\n", installed)
		return 0
	}

	if check {
		fmt.Fprintf(stdout, "ytwatch %s is available (you have %s). Run: ytwatch update\n", latest, installed)
		return 0
	}

	path, err := updateExecutable()
	if err != nil {
		fmt.Fprintln(stderr, "ytwatch: cannot find this program:", err)
		return 1
	}

	fmt.Fprintf(stdout, "Downloading ytwatch %s...\n", latest)

	program, err := updateDownload(ctx, latest, update.Arch())
	if err != nil {
		fmt.Fprintln(stderr, "ytwatch:", err)
		return 1
	}

	if err := updateReplace(path, program); err != nil {
		fmt.Fprintln(stderr, "ytwatch:", err)
		fmt.Fprintln(stderr, "Run the install command of the README instead: it can write where you can.")

		return 1
	}

	fmt.Fprintf(stdout, "Updated ytwatch %s to %s in %s. Checksum ok.\n", installed, latest, path)

	return 0
}
