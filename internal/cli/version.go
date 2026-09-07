package cli

import (
	"github.com/spf13/cobra"

	"github.com/NikitaMikhailov/dashsync/internal/buildinfo"
)

// newVersionCmd строит подкоманду `version`.
//
// TODO(автор): реализовать. Контракт и идиомы — ниже; точный формат вывода
// специфицирован тестами в version_test.go.
//
// Параметр info — функция, возвращающая метаданные сборки, а не сам пакет
// buildinfo: в реальном дереве команд (см. NewRootCmd) сюда передаётся
// buildinfo.Get, а в тестах — функция-заглушка с фиксированным значением.
// Так version_test.go проверяет только форматирование и разбор флага, не
// завися от того, реализован ли уже internal/buildinfo — идиома "принимай
// интерфейсы, возвращай структуры" в деле: здесь минимальный интерфейс из
// одной функции, а не целый пакет.
//
// Флаг --output принимает "text" (значение по умолчанию) и "json"; любое
// другое значение — ошибка, а не молчаливый фолбэк на text.
//
// Контракт формата (точные строки — в version_test.go):
//
//   - text: ровно три строки, в этом порядке —
//     "dashsync {Version}\n"
//     "commit:  {Commit, либо \"unknown\" если пусто}\n"
//     "built:   {Date, либо \"unknown\" если пусто}\n"
//   - json: encoding/json от значения info(), теги version/commit/date уже
//     расставлены на buildinfo.Info — смотри internal/buildinfo/buildinfo.go.
//   - неизвестное значение --output: вернуть ошибку, текст которой
//     упоминает само переданное значение (тест ищет его подстрокой) — так
//     видно, что именно написано не так, а не просто "bad flag".
//
// Не забудь cmd.SilenceUsage = true и на этой команде тоже: version_test.go
// строит её в изоляции, без родителя, и в таком виде cobra по умолчанию сама
// печатает usage под каждой ошибкой.
func newVersionCmd(info func() buildinfo.Info) *cobra.Command {
	panic("TODO: реализуй согласно контракту выше и тестам в version_test.go")
}
