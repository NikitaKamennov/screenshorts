# Сторонние компоненты ScreenShorts Pro

## Linux-комплект

Новый Linux-сборщик собирает динамические GTK3/WebKitGTK, Tesseract/языки,
CPython, CPU wheels и вспомогательные компоненты отдельно от Windows.
Точные версии и SHA находятся в `runtime-manifest.json` готового комплекта;
полный состав — в `release-manifest.json`. Системные copyright-уведомления
сохранены в `licenses/system/`, полные общие тексты — в `licenses/common/`,
Go — `licenses/go/`, wheel-уведомления — внутри Python dist-info.

Копия WebKit имеет только фиксируемые в manifest замены абсолютных имён
bwrap/xdg-dbus-proxy на имена из PATH комплекта. Защита не отключается;
исходный SHA, итоговый SHA и замены сохранены. Остальные библиотечные файлы
берутся из пакетов Ubuntu/Mint без изменения содержимого.
Состав portable не расширяет MIT приложения на сторонние библиотеки/модель.
Матрица приёмки и аудит условий публичного распространения зависимостей
остаются отдельными пунктами; наличие SHA не обозначает завершённый аудит.

MIT в [LICENSE](LICENSE) относится к собственному коду и указанным в нём
уведомлениям. Он не заменяет лицензии зависимостей, runtime или будущих моделей.

- **Zvukbook:** адаптированы каркасные решения, Go lock-набор и четыре
  MIT-исходника Windows AppContainer/Job Object в `internal/sandboxwin/`.
  [Сохранённое MIT-уведомление](licenses/zvukbook/LICENSE),
  [описание переноса](docs/scaffold-provenance.md).
- **Wails v2.15.0:** MIT, [официальный исходник лицензии выбранной версии](https://github.com/wailsapp/wails/blob/v2.15.0/LICENSE).
  Точные Go-зависимости и контрольные суммы — `go.mod` / `go.sum`.
  Сборщик полного Windows-комплекта читает `go version -m` из готового EXE,
  сверяет модули с `go.sum` и собирает их license/NOTICE/PATENTS из локального
  кэша в `licenses/go/modules/`. Уведомления фактического Go toolchain и его
  vendored-кода находятся в `licenses/go/toolchain/`; список/SHA — в
  `licenses/go/manifest.json`. Чтение build info не запускает приложение.
- **Windows WebView2 Fixed Version 157.0.4341.60 x64:** в W3 выбран и получен
  официальный CAB с закреплённым SHA. Полный runtime распакован и явно выбран
  в bundle-EXE; облегчённый dev-режим использует системный runtime. Условия Microsoft и все
  вложенные third-party notices сохраняются при комплектовании.
- **Inno Setup 6.7.4:** закреплённый компилятор нужен только режиму `-Release`.
  Подготавливается внутри проекта в upstream portable-режиме без системной
  установки; лицензия сохраняется рядом с установщиком. Исходник режима:
  [isportable.iss](https://github.com/jrsoftware/issrc/blob/is-6_7_4/isportable.iss),
  [лицензия](https://github.com/jrsoftware/issrc/blob/is-6_7_4/license.txt).
- **GTK/WebKitGTK в Linux:** системные зависимости с собственными лицензиями.
- **github.com/jezek/xgb v1.1.1:** X11 protocol bindings, BSD с дополнительным
  патентным разрешением; [лицензия выбранной версии](https://github.com/jezek/xgb/blob/v1.1.1/LICENSE).
- **github.com/godbus/dbus/v5 v5.1.0:** D-Bus, StatusNotifierItem и portals,
  BSD-2-Clause; [лицензия](https://github.com/godbus/dbus/blob/v5.1.0/LICENSE).

- **Tesseract OCR и языки rus/eng:** в коде подключён внешний локальный движок;
  получены и распакованы Windows NSIS 5.5.0.20241111 и tessdata_fast 4.1.0
  rus/eng, без системной установки/исполнения движка. У Tesseract
  [Apache-2.0](https://github.com/tesseract-ocr/tesseract/blob/main/LICENSE),
  у официального tessdata_fast —
  [Apache-2.0](https://github.com/tesseract-ocr/tessdata_fast/blob/main/LICENSE).
  Apache-лицензия двигателя и лицензия языков включены в комплект. Полный
  перечень уведомлений DLL Windows-дистрибутива и требования к соответствующим
  исходникам ещё уточняются; MIT приложения и Apache движка их не заменяют.

- **U²-NetP:** выбран лёгкий ONNX-экспорт из релиза rembg v0.0.0,
  [upstream U-2-Net](https://github.com/xuebinqin/U-2-Net), Apache-2.0 в исходном
  проекте. [Уведомление](licenses/u2net/LICENSE). Вес не распространяется
  в Git; SHA-256 и URL конкретного экспорта — в
  [manifest](workers/background/runtime-manifest.json). Для расчёта SHA байты
  публичного экспорта прочитаны без инференса. Linux-компоненты затем явно
  подготовлены в игнорируемом `.local-data/background-linux/`; в Git их нет.
- **rembg:** MIT, [уведомление](licenses/rembg/LICENSE). Публичный контракт
  нормализации U²-NetP/выходной маски использован как справочный; сам пакет
  rembg не устанавливается, его загрузчик/облачные адаптеры не используются.
- **ONNX Runtime CPU 1.32.0:** MIT, [уведомление upstream](licenses/onnxruntime/LICENSE).
  Wheels и все транзитивные CPU-зависимости закреплены с SHA в
  [requirements.lock](workers/background/requirements.lock). NumPy — BSD-3-Clause,
  Pillow — MIT-CMU, FlatBuffers — Apache-2.0, packaging — Apache-2.0/BSD-2-Clause,
  protobuf — BSD-3-Clause. Полные `.dist-info`, в том числе вложенные лицензии
  NumPy/Pillow/packaging/protobuf, сохраняются из wheels. ONNX Runtime хранит
  LICENSE и ThirdPartyNotices.txt внутри самого пакета `onnxruntime/`.
  В выбранном FlatBuffers wheel лицензии нет; дополнительно включён
  [LICENSE версии 25.12.19](licenses/flatbuffers/LICENSE) из upstream, его
  [источник/размер/SHA](licenses/flatbuffers/source.json) проверяются сборщиком.
- **CPython:** Windows-комплект включает официальный embeddable x64 3.13.17
  с SHA из SPDX, PSF-лицензией и вложенными уведомлениями в
  `runtime/python/LICENSE.txt`. Совместимые cp313 wheels распакованы без pip.
  Linux-dev использует отдельно установленный системный CPython 3.12/3.13;
  Windows-окружение не переносится в Linux. Runtime не включается в Git.
- **Microsoft Visual C++ CRT 14.53.36930 x64:** выбран app-local VSIX из
  каталога Visual Studio, publisher SHA сверён. Системная установка runtime
  не выполняется. Действуют Microsoft Software License Terms и условия
  [распространяемого кода](https://learn.microsoft.com/en-us/cpp/windows/redistributing-visual-cpp-files).
- **7-Zip 26.06:** закреплённый распаковщик сборщика, в пользовательском runtime
  не требуется. Действуют LGPL и вложенные уведомления/ограничение unRAR;
  [источник лицензии](https://github.com/ip7z/7zip/blob/26.06/DOC/License.txt).
Выбранные Windows-артефакты, происхождение и SHA — в
[windows-downloads.json](config/windows-downloads.json). Кэш вне Git;
полная папка, ZIP и установщик собраны. `licenses/runtime-notices.json` явно
помечает аудит `incomplete`: Microsoft WebView2/CRT и полный набор Tesseract DLL
ещё требуют доведения. [Текущий состав уведомлений](docs/windows-notices.md).

Энкодеры поиска остаются кандидатами; visual captioning исключён из реализации.
Лицензию одного экспорта модели нельзя автоматически переносить на всё семейство.

Готовые EXE, архивы, ML-модели и runtime не входят в исходный репозиторий.
Сборка не означает завершённый аудит лицензий или ручную приёмку выпуска.
