# Windows: разработка и локальные runtime

Для автономной папки, portable ZIP и установщика следуй
[короткой инструкции сборки](windows-quick-build.md).
Этот документ объясняет облегчённую разработку и платформенные ограничения.
[Фактическая готовность](project-status.md).

## Лёгкий цикл разработки

Нужны нативная Windows x64, 64-разрядный Windows PowerShell, Go 1.25+ в PATH
и установленный **системный WebView2 Runtime** для запуска dev-EXE.
Linux, WSL, Wine и кросс-компиляция не заменяют проверку Win32/API на Windows.

В PowerShell из корня проекта:

~~~powershell
& .\scripts\build-windows.ps1
& .\scripts\start-windows.ps1
~~~

Первая команда готовит:

~~~text
.local-data/build/windows-x64/ScreenShortsPro.exe
.local-data/build/windows-x64/config/runtime-windows.json
~~~

Вторая запускает уже собранный EXE. Она ничего не собирает и не устанавливает.
Прямой запуск EXE также возможен. npm, Node.js и Wails CLI для компиляции
не нужны: HTML/CSS/JS встроены в программу.

Этот режим не доставляет OCR, Python и модель. Можно работать над окном,
редактором и сохранением без обработки; для автономной проверки удобнее
полный режим -Bundle. Различие команд:

| Команда | Результат |
| --- | --- |
| scripts/build-windows.ps1 без параметров | Только dev-EXE и профиль, нужен системный WebView2 |
| build-windows.cmd или scripts/build-windows.ps1 -Bundle | Полная самостоятельная папка с runtime |

После правок Go/HTML/CSS/JS: полностью выйди через трей, повтори сборку и запуск.
Автоперезагрузки интерфейса нет. Для обычной разработки достаточно dev-EXE;
полный комплект собирай, когда нужен именно такой результат.

## Данные и профиль запуска

По умолчанию Windows использует %LOCALAPPDATA%\Skrinpolka.
Для независимой лаборатории:

~~~powershell
& .\scripts\start-windows.ps1 -DataRoot 'C:\ScreenShorts Lab'
~~~

Launcher принимает только -Executable, -DataRoot и -RuntimeConfig.
-DataRoot — абсолютный путь на локальном NTFS. Пример другого готового EXE:

~~~powershell
& .\scripts\start-windows.ps1 -Executable '.\.local-data\build\windows-bundle\ScreenShortsPro\ScreenShortsPro.exe' -DataRoot 'C:\ScreenShorts Lab'
~~~

Для стандартного dev-EXE launcher сначала ищет подготовленный профиль
.local-data/background-windows/runtime-windows.json. Если его нет, выбирает
config/runtime-windows.json рядом с EXE. Для другого EXE сразу берёт соседний
профиль; свой можно передать через -RuntimeConfig.

Те же параметры самого приложения называются --data-root и --runtime-config.
Относительные пути внутри runtime JSON разрешаются от каталога этого JSON.
[Описание настроек](../config/README.md).

## OCR при разработке

Dev-профиль ожидает структуру рядом с EXE:

~~~text
windows-x64/
  ScreenShortsPro.exe
  config/runtime-windows.json
  tools/tesseract/
    tesseract.exe
    <DLL выбранной x64-сборки>
    tessdata/rus.traineddata
    tessdata/eng.traineddata
~~~

Полная сборка сама создаёт такую структуру из закреплённых артефактов,
готовит относительный профиль с ревизиями и SHA. Не переноси только
tesseract.exe без его DLL и языков и не устанавливай ничего в Linux-venv.
При собственной dev-компоновке пути задаются явным runtime-профилем;
приложение компоненты не скачивает.

После исправления запуска комплектный OCR rus/eng и поиск проверены
на Windows 10. Это не приёмка всех сочетаний DLL, ОС и настроек защиты.

## Удаление фона при разработке

Полный -Bundle уже содержит собственный Python и модель: ручной setup
для него не требуется. Для отдельного dev-окружения можно использовать
установленный CPython 3.12/3.13 x64:

~~~powershell
py -3.12 .\scripts\setup-background.py --base-config .\.local-data\build\windows-x64\config\runtime-windows.json
& .\scripts\start-windows.ps1 -DataRoot 'C:\ScreenShorts Lab'
~~~

Первая команда явно загружает закреплённые CPU-компоненты в собственную
.local-data/background-windows/.venv и создаёт профиль. Она не запускает
модель/приложение и не изменяет исходный профиль. Python launcher py и выбранная
версия нужны только для этого dev-setup, не для полного сборщика.
[Worker и его ограничения](background-removal.md).

Функциональная приёмка удаления фона ещё не подтверждена. Подготовленные
зависимости и успешная компиляция не измеряют качество или расход памяти.

## Что устроено отдельно от Linux

Win32 отвечает за захват, координаты/DPI, глобальные комбинации, трей,
возврат фокуса и файловый буфер. Wails использует WebView2.
OCR и Python-worker запускаются через AppContainer/ACL и Job Object,
с лимитами, отменой дерева процессов и отдельным рабочим каталогом.

Сборщик держит Windows Go-кэши в .local-data/build-cache/windows-x64.
Staging, маркеры принадлежности, блокировки и откат ограничивают замену
его собственными каталогами. Работающий EXE или чужой выход не заменяются.
При ошибке прочитай сообщение; не удаляй произвольные маркеры и не снимай защиту.
Для отдельного полного выхода предусмотрен -OutputTag.

Windows и Linux не делят окружения, DLL и runtime; Go-ядро, формат данных
и пользовательские сценарии общие. Соседний Звукбук для сборки не нужен.

## Открытая ручная проверка

На Windows остаются смешанный DPI/несколько мониторов, все тихие режимы
и конфликты комбинаций, восстановление после Explorer, очереди/отмена/ошибки
workers, удаление фона, чистая ОС и цикл установщика. Ранее проверенные
сценарии перечислены в [статусе](project-status.md); полный список —
[windows-acceptance.md](windows-acceptance.md).
