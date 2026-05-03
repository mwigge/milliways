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
	"net/http"
	"os"
	"path/filepath"
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

	return runUpgradeScript(stdout, stderr)
}

// runUpgradeScript finds or downloads upgrade.sh then executes it.
// Unlike other scripts, upgrade.sh may not exist on machines that installed
// via `go install` or `go build` without ever running install.sh.
// In that case we download it fresh from the latest GitHub release so the
// user gets a working upgrade path.
func runUpgradeScript(stdout, stderr io.Writer) int {
	// Try the normal search first (git checkout, package install, binary install).
	if code := runInstallScript("scripts/upgrade.sh", stdout, stderr); code != 1 {
		return code // found and ran (0=ok, anything else=real error)
	}

	// Not found locally — bootstrap by downloading from GitHub.
	fmt.Fprintln(stderr, "upgrade.sh not found locally; downloading from GitHub...")
	home, _ := os.UserHomeDir()
	shareScripts := filepath.Join(home, ".local", "share", "milliways", "scripts")
	if err := os.MkdirAll(shareScripts, 0o755); err != nil {
		fmt.Fprintf(stderr, "milliwaysctl upgrade: cannot create scripts dir: %v\n", err)
		return 1
	}
	dest := filepath.Join(shareScripts, "upgrade.sh")
	url := "https://raw.githubusercontent.com/mwigge/milliways/master/scripts/upgrade.sh"
	resp, err := http.Get(url) //nolint:gosec,noctx // intentional bootstrap URL
	if err != nil {
		fmt.Fprintf(stderr, "milliwaysctl upgrade: download failed: %v\n", err)
		fmt.Fprintf(stderr, "  Run manually: curl -sSf https://raw.githubusercontent.com/mwigge/milliways/master/install.sh | bash\n")
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Fprintf(stderr, "milliwaysctl upgrade: download HTTP %d\n", resp.StatusCode)
		return 1
	}
	data := make([]byte, 0, 1<<20)
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		data = append(data, buf[:n]...)
		if readErr != nil {
			break
		}
	}
	if err := os.WriteFile(dest, data, 0o755); err != nil {
		fmt.Fprintf(stderr, "milliwaysctl upgrade: write %s: %v\n", dest, err)
		return 1
	}
	fmt.Fprintf(stderr, "Downloaded upgrade.sh → %s\n", dest)
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
