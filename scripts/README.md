# Скрипты ScreenShooter Pro

Команды выполняются из корня репозитория. Скрипты запуска программы
отделены от сборщиков: **сборка сама не открывает приложение и не запускает
тесты, OCR или инференс**. Явная подготовка зависимостей может использовать сеть.
[Состояние проверок](../docs/project-status.md).

## Linux

| Команда | Назначение | Результат / данные |
| --- | --- | --- |
| bash scripts/start-dev.sh | Компилирует временную программу через go run и запускает её | Данные .local-data/linux-dev; после правок нужен перезапуск |
| bash scripts/build-linux.sh | Собирает обычный бинарник с системными GTK/WebKit | .local-data/build/linux-x64/ScreenShooterPro и config/runtime-linux.json |
| bash scripts/start-local.sh | Запускает бинарник предыдущей команды; ничего не собирает | Данные .local-data/linux-dev |
| bash scripts/build-linux-portable.sh | Собирает автономную папку, .run и .tar.gz с CPU-runtime | .local-data/linux-x64/dist/; спецификация, SHA и latest-build.json |

start-dev.sh и start-local.sh выбирают подготовленный профиль
.local-data/background-linux/runtime-linux.json, если он есть; иначе
config/runtime-linux.json из исходников. Дополнительные аргументы передаются
приложению, например --data-root и --runtime-config.

Параметры автономного сборщика:

| Параметр | Значение |
| --- | --- |
| Без параметров | Свежая компиляция приложения/launchers, повторное использование проверяемого runtime-кэша |
| --offline | Запрет загрузок; все компоненты и Go-модули уже должны быть в кэше |
| --refresh-runtime | Явное пересоздание runtime после изменения lock/модели/системных пакетов; версии берутся из конфигов |
| --offline --refresh-runtime | Пересоздание runtime только из уже имеющихся локальных компонентов |

Параметры --offline/--refresh-runtime относятся к build-linux-portable.sh,
не к обычному build-linux.sh.
[Зависимости лаборатории](../docs/build-linux.md),
[машина сборки и результаты автономного комплекта](../docs/linux-portable.md).

## Windows

Три .cmd лежат **в корне проекта**, остальные скрипты — в scripts/.

| Команда PowerShell | Назначение | Результат |
| --- | --- | --- |
| & .\scripts\build-windows.ps1 | Только dev-EXE, без runtime | .local-data/build/windows-x64/ |
| .\build-windows.cmd | Полная папка; вызывает -Bundle | .local-data/build/windows-bundle/ScreenShooterPro/ |
| .\build-portable.cmd | Папка и ZIP; вызывает -Portable | Папка как выше; ZIP в .local-data/releases/windows-x64/portable/ |
| .\build-release.cmd | Папка, ZIP и setup.exe; вызывает -Release | Папка как выше; архивы в .local-data/releases/windows-x64/release/ |
| & .\scripts\start-windows.ps1 | Запускает готовый dev-EXE либо указанный EXE | Использует профиль и отдельные пользовательские данные |
| & .\scripts\prepare-windows-runtime.ps1 | Предварительно загружает закреплённые артефакты, без сборки приложения | .local-data/windows-artifact-cache/ |

Параметры build-windows.ps1:

- -Bundle / -Portable / -Release: один выбранный полный режим.
- -Offline: готовый кэш без сети; доступен также dev-режиму.
- -OutputTag preview: отдельные выходные каталоги с суффиксом -preview,
  только для полных режимов; имена файлов внутри не меняются.

.cmd передают параметры дальше. Для отключения паузы в обёртках есть
переменная процесса SCREENSHOOTER_NO_PAUSE=1.
[Подробные команды и пути](../docs/windows-quick-build.md).

start-windows.ps1 принимает -Executable, -DataRoot, -RuntimeConfig.
По умолчанию запускает dev-EXE, выбирает его подготовленный профиль фона
либо соседний config/runtime-windows.json. Для другого EXE использует его
соседний профиль. Сборку/установку не выполняет.

prepare-windows-runtime.ps1 принимает -Offline, -IncludeInstaller, -Extract.
Без -Extract только загружает/проверяет; с ним распаковывает компоненты
в .local-data/windows-runtime/. -IncludeInstaller включает артефакты Inno Setup.
Полные режимы сборки сами вызывают подготовщик; вручную выполнять его
перед каждой сборкой не нужно.

## Подготовка удаления фона для разработки

~~~bash
python3 scripts/setup-background.py
~~~

Скрипт работает нативно на Linux x86_64 или Windows x64 с CPython 3.12/3.13.
Создаёт свою `.local-data/background-<os>/.venv`, устанавливает hash-locked
CPU wheels, загружает и сверяет модель, пишет профиль `runtime-<os>.json`
и receipt. Единственный необязательный параметр — --base-config ПУТЬ.
Он сохраняет OCR из указанного базового профиля; исходный JSON не перезаписывает.

Это явная сетевая подготовка, без запуска приложения или модели.
Полные Windows/Linux-сборщики готовят собственные runtime: этот dev-setup
для них не является обязательным предварительным шагом.
[Инструкция worker](../docs/background-removal.md).

## Служебные файлы

Их не нужно запускать по отдельности.

| Файл / каталог | Роль |
| --- | --- |
| linux/package.py | Комплектование установленного Linux-runtime, wheels/модели, сборка трёх Go-программ, manifests и архивы |
| linux/launch.sh | Launcher готовой Linux-папки: библиотечные пути, ресурсы WebKit/GTK и профиль |
| linux/README-portable.txt | README, попадающий в Linux-комплект |
| windows/common.ps1 | Пути, маркеры принадлежности, блокировки, quoting, публикация staging |
| windows/go-build.ps1 | Компиляция EXE и отдельные Windows Go-кэши |
| windows/downloads.ps1 | Закреплённый manifest, загрузки, размер/SHA и кэш |
| windows/archives.ps1 | Извлечение проверенных артефактов |
| windows/components.ps1 | Сборка runtime-компонентов и относительного профиля |
| windows/package.ps1 | Полная папка, ZIP, SHA и release manifest |
| windows/notices.ps1 | Уведомления фактически собранных Go/runtime-компонентов |
| windows/installer.ps1, installer.iss, installer-root.txt | Подготовка Inno Setup и установщик текущего пользователя |
| tools/notice-extract/ | Отдельный Go-инструмент чтения build info для уведомлений, не приложение |

Исходники дополнительных Linux-программ находятся в cmd/linux-portable/
и cmd/linux-worker-guard/. Зависимости выпусков закреплены в config/
и workers/background/; перечень и ограничения лицензий — в
[THIRD-PARTY.md](../THIRD-PARTY.md).

Локальные кэши, данные, модели и выходные комплекты исключены из Git.
Команды сборки и запуска выполняются явно; таблица выше описывает их действие.
