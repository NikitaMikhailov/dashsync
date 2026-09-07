// Command dashsync turns running Docker containers into static config
// files for self-hosted dashboards.
//
// Everything here is just forwarding os.Args/os.Stdout/os.Stderr to
// internal/cli.Run and returning its exit code; all the logic lives under
// internal/*.
package main

import (
	"os"

	"github.com/NikitaMikhailov/dashsync/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
