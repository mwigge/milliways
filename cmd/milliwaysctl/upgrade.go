// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

// `milliwaysctl upgrade` — upgrade milliways to the latest release.
//
// Delegates entirely to scripts/upgrade.sh (same approach as install-local-server
// delegates to install_local.sh), so the shell script stays the single source
// of truth for the upgrade logic across all install tiers (deb/rpm/pacman/binary/macOS).
//
// Flags:
//   --check   print current vs latest versions and exit; do not install
//   --yes     skip the interactive confirmation prompt
//   --version <tag>  target a specific version instead of latest

import (
	"fmt"
	"io"
	"os"
)

// runUpgrade dispatches `milliwaysctl upgrade [--check] [--yes] [--version <tag>]`.
func runUpgrade(args []string, stdout, stderr io.Writer) int {
	check := false
	yes := false
	targetVersion := ""

	// Minimal flag parsing — no external dependency.
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help", "help":
			printUpgradeUsage(stdout)
			return 0
		case "--check", "-check", "check":
			check = true
		case "--yes", "-yes", "-y":
			yes = true
		case "--version", "-version":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "milliwaysctl upgrade: --version requires an argument")
				return 2
			}
			i++
			targetVersion = args[i]
		default:
			fmt.Fprintf(stderr, "milliwaysctl upgrade: unknown flag %q\n", args[i])
			printUpgradeUsage(stderr)
			return 2
		}
	}

	// Translate flags to the env vars upgrade.sh reads.
	if check {
		os.Setenv("UPGRADE_CHECK", "1")
	}
	if yes {
		os.Setenv("UPGRADE_YES", "1")
	}
	if targetVersion != "" {
		os.Setenv("MILLIWAYS_VERSION", targetVersion)
	}

	return runInstallScript("scripts/upgrade.sh", stdout, stderr)
}

func printUpgradeUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: milliwaysctl upgrade [--check] [--yes] [--version <tag>]")
	fmt.Fprintln(w, "       /upgrade              (from inside the milliways chat)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Upgrades milliways to the latest release (or the specified version).")
	fmt.Fprintln(w, "Install tier used matches how milliways was originally installed:")
	fmt.Fprintln(w, "  deb/rpm/pacman  — native package manager upgrade")
	fmt.Fprintln(w, "  binary/macOS    — binary replacement + MilliWays.app")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  --check           print current vs latest versions; exit 1 if upgrade available")
	fmt.Fprintln(w, "  --yes             skip the confirmation prompt")
	fmt.Fprintln(w, "  --version <tag>   target a specific version tag (e.g. v1.3.0)")
}
