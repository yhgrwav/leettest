<div align="center">

<h1><img src="assets/logo.png" width="360" alt="LeetTest"></h1>

[Русский](docs/ru/) · [English](docs/en/) · [Deutsch](docs/de/) · [简体中文](docs/zh-CN/)

[![CI](https://github.com/yhgrwav/leettest/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/yhgrwav/leettest/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/yhgrwav/leettest.svg)](https://pkg.go.dev/github.com/yhgrwav/leettest)
[![Go version](https://img.shields.io/github/go-mod/go-version/yhgrwav/leettest)](go.mod)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Поддержать](https://img.shields.io/badge/поддержать-USDT%20TRC20-26A17B?logo=tether&logoColor=white)](docs/support.md)

</div>

---

# LeetTest

Нагрузочное тестирование gRPC-сервисов.

<img src="assets/demo.gif" width="100%" alt="Запуск LeetTest в терминале: экран нагрузки, сервис перестаёт отвечать, отчёт показывает запросы без ответа и высокий p99">

### Пример

Сервис посреди прогона перестаёт отвечать на 2 секунды. Нагрузка — 200 запросов в секунду.

| | p99 |
|---|---|
| ghz, по умолчанию (`-c 50`) | 11.6 ms |
| ghz, `--async` | 1.71 s |
| LeetTest | 1.71 s |

p99 — задержка, которую превысил 1 запрос из 100. [ghz](https://github.com/bojand/ghz) — другой
инструмент нагрузки на gRPC: `-c 50` — его 50 воркеров по умолчанию (каждый ждёт ответа перед
следующим запросом), `--async` — режим, где запросы уходят по расписанию, не дожидаясь ответов.
Методика замера и другие режимы — [сравнение с ghz](docs/ru/compare-ghz.md).

### Установка

**Через ИИ-агента** (Claude Code, Cursor и подобные): вставьте ему это и ответьте на его вопросы

```text
Set up LeetTest for my gRPC service following https://raw.githubusercontent.com/yhgrwav/leettest/main/docs/agent.md
```

**Вручную**

Linux и macOS (нужны `curl` и `sh`):

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | sh
```

Windows: архив `.zip` со [страницы релизов](https://github.com/yhgrwav/leettest/releases/latest).

Любая ОС с Go 1.26 или новее:

```sh
go install github.com/yhgrwav/leettest/cmd/leettest@latest
```

**Проверить скачанное.** У каждого релиза есть `checksums.txt`, подписанный workflow'ом релиза без
ключа (`checksums.txt.sigstore.json`, [cosign](https://docs.sigstore.dev/cosign/installation/)), и SBOM
на каждый архив (`<архив>.sbom.json`, SPDX JSON; контрольные суммы покрывают и его). Скачайте со
[страницы релизов](https://github.com/yhgrwav/leettest/releases/latest) `checksums.txt`,
`checksums.txt.sigstore.json` и свой архив в одну папку и выполните в ней. Сначала проверка, что
подпись принадлежит workflow'у релиза:

```sh
cosign verify-blob --bundle checksums.txt.sigstore.json --certificate-identity-regexp 'https://github.com/yhgrwav/leettest/.github/workflows/release.yml@refs/tags/v.*' --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
```

Затем что архив тот, что в списке (на macOS: `shasum -a 256 -c --ignore-missing checksums.txt`):

```sh
sha256sum -c --ignore-missing checksums.txt
```

`install.sh` сверяет архив с `checksums.txt`, но не подпись: эти две команды и проверяют, что файл
выпущен workflow'ом релиза.

Сохраните конфиг как `leettest.yaml`; адрес, порт и метод здесь для примера, подставьте свои:

```yaml
app:
  target:
    ip: localhost
    port: 50051
  tls: false

load:
  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m
```

```sh
leettest -c leettest.yaml
```

Где запускать и как прочитать отчёт — [первый прогон](docs/ru/guide/first-run.md).

### Документация

| | |
|---|---|
| [Первый прогон](docs/ru/guide/first-run.md) | Пошаговая инструкция: установка, конфиг, отчёт |
| [Подключение](docs/ru/guide/connection.md) | Адрес, TLS, заголовки и токены |
| [Нагрузка](docs/ru/guide/load.md) | Методы, запросы в секунду, длительность, разгон |
| [Поиск точки отказа](docs/ru/report.md#поиск-точки-отказа) | При какой нагрузке сервис перестаёт справляться |
| [Данные в запросах](docs/ru/reference.md#каждый-раз-другой-запрос-dataset) | Разные данные в каждом запросе: файл, шаблоны |
| [Несколько соединений](docs/ru/reference.md#доступ-к-сервису) | Нагрузка на все серверы за балансировщиком |
| [Схема из `.proto`](examples/ru/proto.yaml) | Для сервиса без reflection |
| [Сравнение прогонов](docs/ru/report.md#сравнить-два-прогона) | До и после изменения |
| [Grafana](docs/ru/reference.md#метрики-prometheus) | Прогон в реальном времени |
| [Пороги](docs/ru/report.md#пределы-прогон-как-проверка) | Критерии прохождения прогона, в том числе для CI |
| [Отчёт](docs/ru/report.md#как-читать-отчёт) | Что значит каждая цифра |
| [Недостоверный прогон](docs/ru/report.md#когда-прогон-недействителен) | Когда отчёт отказывается от цифр и почему |
| [CI](docs/ru/report.md#коды-выхода) | Коды выхода, JSON |
| [Справочник](docs/ru/reference.md) | Все поля конфига и флаги |

### О проекте

[Сравнение с ghz, k6, JMeter](docs/ru/why.md#сравнение-с-ghz-k6-и-jmeter) · [План](docs/ru/roadmap.md) ·
[Платная версия](docs/ru/commercial.md) · [Вопросы и отзывы](docs/ru/feedback.md) ·
[Участие](CONTRIBUTING.md) · [Безопасность](SECURITY.md) · [Поддержать](docs/support.md) · Apache 2.0, [лицензия](LICENSE)
