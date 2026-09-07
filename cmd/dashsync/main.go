// Command dashsync — CLI-инструмент, который превращает запущенные
// Docker-контейнеры в статические конфиги self-hosted дашбордов.
//
// Весь код здесь — только передача os.Args/os.Stdout/os.Stderr в
// internal/cli.Run и код возврата процесса; любая логика живёт в internal/*.
package main

import (
	"os"

	"github.com/NikitaMikhailov/dashsync/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
