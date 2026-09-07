// Package buildinfo предоставляет метаданные о версии текущего бинаря
// dashsync: номер версии, коммит и дата сборки.
//
// TODO(автор): реализовать resolve(). Сигнатуры и контракт ниже; поведение
// специфицировано таблицей тестов в buildinfo_test.go — запускай
// `go test ./internal/buildinfo/...` пока все случаи не станут зелёными.
package buildinfo

import "runtime/debug"

// Три переменные ниже — единственное осознанное исключение из правила
// "нет глобального состояния" в CLAUDE.md. Инструмент, который их
// патчит, — флаг компоновщика -ldflags, а он умеет присваивать значения
// только пакетным переменным верхнего уровня: доступа к полям структуры или
// к параметрам функции у него нет. На релизной сборке GoReleaser линковка
// будет выглядеть примерно так:
//
//	go build -ldflags "-X .../internal/buildinfo.version=v0.3.0 \
//	                    -X .../internal/buildinfo.commit=abc1234 \
//	                    -X .../internal/buildinfo.date=2026-09-07T12:00:00Z"
//
// При обычной сборке (`go build` без ldflags) все три остаются такими, как
// объявлены ниже, и тогда Get() должен добрать то, что может, из
// runtime/debug.ReadBuildInfo().
var (
	//nolint:gochecknoglobals // устанавливается через -ldflags, см. комментарий выше
	version = "dev"
	//nolint:gochecknoglobals // устанавливается через -ldflags, см. комментарий выше
	commit = ""
	//nolint:gochecknoglobals // устанавливается через -ldflags, см. комментарий выше
	date = ""
)

// Info — метаданные сборки одного бинаря dashsync. Теги json нужны команде
// `dashsync version --output json` в internal/cli — без них ключи в выводе
// стали бы "Version"/"Commit"/"Date" вместо принятых в CLI-мире строчных
// имён.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// Get возвращает метаданные текущего бинаря.
//
// Единственная ответственность Get — сходить в рантайм за
// debug.ReadBuildInfo() и передать всё, что есть, в resolve(). Сама логика
// "что показать" живёт в resolve(), и туда не проникает ни один вызов
// рантайма — это то же разделение "ввод-вывод только на краях" из
// PROJECT_PLAN.md §2.1, только в масштабе одного пакета вместо всего
// приложения. Ровно поэтому resolve() тестируется без единого мока.
func Get() Info {
	bi, ok := debug.ReadBuildInfo()
	return resolve(version, commit, date, bi, ok)
}

// resolve принимает уже прочитанные ldflags-значения и результат
// debug.ReadBuildInfo() и решает, что показать пользователю. Контракт:
//
//  1. Если ldVersion != "dev" (его прошили через -ldflags на релизной
//     сборке) — версия, коммит и дата берутся из ldflags как есть,
//     debug.BuildInfo не используется вообще.
//  2. Иначе, если bi.Main.Version — настоящий семвер (например, "v1.4.0" —
//     так бывает при `go install github.com/.../dashsync@v1.4.0`), он
//     становится Version. debug.Module — структура с полем Version, это
//     как раз тот случай.
//  3. Иначе Version остаётся "dev": локальный `go build`/`go run` не знает
//     номера версии, и выдумывать его не нужно.
//  4. Commit и Date, если их не прошили через ldflags, ищутся среди
//     bi.Settings — среза debug.BuildSetting{Key, Value}. Начиная с Go 1.18
//     компилятор сам кладёт туда "vcs.revision" (полный git-хэш) и
//     "vcs.time" при сборке внутри git-репозитория — без всякого участия
//     ldflags. Commit — это первые 7 символов "vcs.revision" (короткий хэш,
//     как в `git rev-parse --short`), с суффиксом "+dirty", если значение
//     "vcs.modified" равно "true".
//  5. Если ok == false или подходящих ключей в Settings нет — соответствующее
//     поле Info остаётся пустой строкой (для Version — "dev", см. п.3). Это
//     тот случай, где идиома "не паникуй, обработай" отличает инфраструктурный
//     инструмент от учебного скрипта: бинарь может быть собран не из
//     VCS-чекаута (например, распакован из тарбола) — это не ошибка.
func resolve(ldVersion, ldCommit, ldDate string, bi *debug.BuildInfo, ok bool) Info {
	panic("TODO: реализуй согласно контракту выше и тестам в buildinfo_test.go")
}
