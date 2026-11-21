# Windows: автономная папка, portable ZIP и установщик

Команды относятся к **ScreenShooter Pro**, исходный каталог может называться
skrinpolka. Все пути ниже считаются от корня репозитория.

Для текущей разработки владелец выбрал **только EXE**; portable и установщик
собираются по отдельному сигналу. Локальная копия после переименования:
`.local-data\build\windows-bundle-shelf\ScreenShooterPro\ScreenShooterPro.exe`.
Её компиляция использует уже подготовленные соседние runtime без их пересборки.
Команды комплектования ниже описаны для отдельного запроса полного набора.

Готовый полный комплект содержит WebView2 Fixed Version, Tesseract с rus/eng,
собственный Python, ONNX Runtime CPU и U²-NetP. На пользовательской машине
Go/Python/OCR/WebView2 отдельно устанавливать не требуется.
[Текущий статус и незавершённые проверки](project-status.md).

## Подготовка машины сборки

Нужна нативная **Windows x64**, 64-разрядный Windows PowerShell и **Go 1.25+**
в PATH. Извлеки исходники целиком в обычную папку на локальном NTFS.
Не собирай Windows-версию из WSL и не используй Linux-окружение.

Первый проход требует интернета для Go-модулей и закреплённых компонентов.
Python, Tesseract, npm/Node, Wails CLI, 7-Zip и Inno Setup заранее ставить
не нужно: полные режимы сами подготавливают нужные им инструменты в .local-data.
Готовое приложение не скачивает эти компоненты при первом запуске.

Открой PowerShell в корне проекта. Перед пересборкой полностью выйди из
приложения через меню трея: закрытие окна может только скрывать программу.

## Выбери один результат

| Команда | Что собирает | Куда пишет |
| --- | --- | --- |
| .\build-windows.cmd | Самостоятельную папку с EXE и всеми runtime | .local-data\build\windows-bundle\ScreenShooterPro\ |
| .\build-portable.cmd | Такую же папку + portable ZIP | Папка как выше; ZIP в .local-data\releases\windows-x64\portable\ |
| .\build-release.cmd | Папку + portable ZIP + установщик | Папка как выше; ZIP/setup в .local-data\releases\windows-x64\release\ |

Например, для полного набора:

~~~powershell
.\build-release.cmd
~~~

Получатся:

~~~text
.local-data/build/windows-bundle/ScreenShooterPro/ScreenShooterPro.exe
.local-data/releases/windows-x64/release/ScreenShooterPro-windows-x64-portable.zip
.local-data/releases/windows-x64/release/ScreenShooterPro-windows-x64-setup.exe
.local-data/releases/windows-x64/release/SHA256SUMS.txt
.local-data/releases/windows-x64/release/release-manifest.json
~~~

Папка включает config, tools, runtime, workers, models, licenses,
README, manifest и контрольные суммы. Переносить нужно **папку целиком**,
а не один EXE.

Сборщики компилируют и упаковывают, но не запускают приложение, тесты,
OCR, модель или полученный установщик. Режим Release подготавливает и запускает
компилятор Inno Setup в собственной portable-папке; общесистемная установка
Inno Setup, ассоциации и ярлыки этого компилятора не создаются.

Те же команды через PowerShell-скрипт:

~~~powershell
& .\scripts\build-windows.ps1 -Bundle
& .\scripts\build-windows.ps1 -Portable
& .\scripts\build-windows.ps1 -Release
~~~

Это **три альтернативы**, их не нужно выполнять подряд. Предварительная сборка
dev-EXE и ручной запуск подготовщика компонентов не требуются.

Важно: scripts/build-windows.ps1 **без переключателей** собирает только
EXE разработчика, без runtime. Это другой режим:
[разработка Windows](windows-build.md).

## Повторная сборка и параметры

После изменения Go/HTML/CSS/JS повтори выбранную команду. Компоненты берутся
из проверяемого кэша, исходники и встроенный интерфейс компилируются заново.

Когда все зависимости уже подготовлены:

~~~powershell
.\build-release.cmd -Offline
~~~

-Offline запрещает сетевую подготовку, требует кэш runtime, нужные Go-модули
и установленный подходящий Go toolchain. Нехватка компонента завершает сборку
ошибкой, без загрузки вместо офлайн-режима. Параметр доступен и другим режимам.

Для отдельного комплекта, сохранив прежний выход:

~~~powershell
.\build-release.cmd -OutputTag preview
~~~

Результаты будут в .local-data/build/windows-bundle-preview/ScreenShooterPro/
и .local-data/releases/windows-x64/release-preview/.
Метка меняет **каталоги выхода**; имена ZIP/setup и каталог пользовательских
данных остаются прежними. Допустимы строчные латинские буквы, цифры и дефис,
первый символ — буква или цифра, длина до 48. Параметры можно сочетать:

~~~powershell
.\build-portable.cmd -Offline -OutputTag preview
~~~

.cmd-обёртки можно открыть двойным щелчком. Они вызывают Windows PowerShell
с ExecutionPolicy Bypass только для этого процесса и оставляют вывод на экране.
Системная политика при этом не меняется; политика организации может запретить
такой запуск. Для вызова из другого скрипта без ожидания клавиши:

~~~powershell
$env:SCREENSHOOTER_NO_PAUSE = '1'
.\build-windows.cmd
~~~

## Запуск папки и ZIP

Для ZIP сначала распакуй архив целиком на локальный NTFS, затем открой
ScreenShooterPro.exe из извлечённой папки. Запуск из папки сборки:

~~~powershell
& .\.local-data\build\windows-bundle\ScreenShooterPro\ScreenShooterPro.exe
~~~

С отдельными данными для пробы:

~~~powershell
& .\.local-data\build\windows-bundle\ScreenShooterPro\ScreenShooterPro.exe --data-root "$PWD\.local-data\manual-windows"
~~~

По умолчанию настройки и библиотека находятся в %LOCALAPPDATA%\Skrinpolka.
**Portable означает переносимую папку программы:** данные автоматически
рядом с EXE не размещаются. Папку снимков выбираешь в настройках,
каталог всех данных можно задать флагом --data-root.

При повторном запуске работает одна копия приложения. Если открылась старая
версия, выбери «Выйти» в её трее и запусти новую ещё раз.

## Установка, обновление и удаление

Режим Release создаёт установщик для текущего пользователя, без прав
администратора и сети. По умолчанию установка идёт в
%LOCALAPPDATA%\Programs\ScreenShooterPro; ярлык — в меню «Пуск»,
ярлык рабочего стола необязателен. Приложение после установки само не запускается.

Перед установкой новой версии или удалением выйди из программы через трей.
Установщик не устанавливает общие Python/Tesseract/WebView2, не меняет PATH
и не регистрирует автозапуск. Каталог данных хранится отдельно.

Реализованы защита от перезаписи чужой непустой папки и очистка только файлов
предыдущей установки по её перечню; библиотека и настройки не удаляются.
**Эти сценарии ещё требуют ручной приёмки**: наличие setup.exe не означает,
что установка/обновление/удаление уже подтверждены на всех целевых системах.

## Кэш и происхождение компонентов

| Путь | Содержимое |
| --- | --- |
| config/windows-downloads.json | Закреплённые URL, размеры, SHA и происхождение артефактов |
| config/windows-release.json | Версия приложения, имена результатов и статус приёмки |
| .local-data/windows-artifact-cache/ | Скачанные и проверенные артефакты |
| .local-data/windows-runtime/ | Подготовленные компоненты для комплектования |
| .local-data/build-cache/windows-x64/ | Отдельные Go-кэши Windows-сборщика |

Для предварительной подготовки компонентов без сборки приложения существует:

~~~powershell
& .\scripts\prepare-windows-runtime.ps1 -IncludeInstaller -Extract
~~~

-IncludeInstaller включает компоненты компилятора установщика,
-Extract распаковывает runtime. Без -Extract готовятся только загруженные
артефакты; с -Offline требуется готовый кэш. Обычная полная сборка сама вызывает
этот шаг с нужными параметрами.

Кэш, runtime и готовые файлы исключены из Git. SHA-проверка подтверждает
соответствие закреплённым байтам, а не прохождение функциональной приёмки.
[Уведомления и оставшиеся вопросы лицензий](windows-notices.md),
[ручная матрица Windows](windows-acceptance.md),
[все скрипты](../scripts/README.md).
