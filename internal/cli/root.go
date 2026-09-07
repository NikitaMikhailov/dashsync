// Package cli содержит cobra-обвязку dashsync: построение дерева команд и
// точку входа, которую вызывает cmd/dashsync/main.go.
//
// TODO(автор): реализовать NewRootCmd() и Run(). Контракт и идиомы — в
// комментариях ниже; поведение специфицировано тестами в root_test.go.
package cli

import (
	"io"

	"github.com/spf13/cobra"
)

// NewRootCmd строит корневую команду dashsync со всеми подкомандами.
//
// Идиомы, которые здесь пригодятся:
//
//   - cobra.Command{Use, Short} — минимум для читаемого --help. Use — это
//     то самое "dashsync", которое попадёт и в usage-текст, и в имя команды
//     при поиске подкоманд (cmd.Commands()[i].Name()).
//   - SilenceUsage: true — без него любая ошибка в RunE подкоманды
//     допечатывает под сообщением об ошибке ещё и полный usage-текст, что
//     для CLI-инструмента обычно шум, а не помощь.
//   - SilenceErrors: true — cobra по умолчанию сама печатает ошибку в
//     cmd.ErrOrStderr(). Здесь эту ответственность явно забирает себе Run(),
//     чтобы у процесса был ровно один код, решающий, что напечатать и с
//     каким exit-кодом выйти.
//   - Подкоманды регистрируются через cmd.AddCommand(...), и каждая
//     собирается своим конструктором (newVersionCmd), а не читает
//     глобальные переменные — тот самый принцип "нет глобального
//     состояния" из CLAUDE.md.
//
// Внутри NewRootCmd вызови newVersionCmd(buildinfo.Get) — здесь и только
// здесь настоящий buildinfo.Get подключается к дереву команд. Сама
// newVersionCmd ничего не знает о том, что источник данных — именно
// debug.ReadBuildInfo(), только о сигнатуре func() buildinfo.Info. Поэтому
// version_test.go тестирует форматирование вывода, вообще не завися от
// состояния internal/buildinfo.
func NewRootCmd() *cobra.Command {
	panic("TODO: реализуй согласно контракту выше и тестам в root_test.go")
}

// Run — единственная точка входа, которую вызывает cmd/dashsync/main.go.
// Она сама решает код возврата процесса, поэтому main.go сводится к
// os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr)).
//
// Контракт:
//   - построить дерево через NewRootCmd();
//   - cmd.SetArgs(args), cmd.SetOut(stdout), cmd.SetErr(stderr) — явная
//     передача зависимостей вместо чтения os.Args/os.Stdout напрямую внутри
//     cobra-команд. Без этого Run() нельзя протестировать без реального
//     процесса и без порчи stdout настоящего терминала во время `go test`;
//   - если cmd.Execute() вернула ошибку — напечатать её в stderr (формат на
//     твой вкус, но root_test.go ищет подстроку "unknown command" для
//     случая несуществующей подкоманды) и вернуть 1;
//   - иначе вернуть 0.
func Run(args []string, stdout, stderr io.Writer) int {
	panic("TODO: реализуй согласно контракту выше и тестам в root_test.go")
}
