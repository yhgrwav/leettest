# Быстрый старт

Полный проход по возможностям LeetTest за полчаса: от установки до отчёта, который можно отдать
разработчикам, и прогона в CI. Каждое поле и флаг — в [справочнике](reference.md), как читать отчёт — в
[README](../../README.md);
здесь — как ими пользоваться вместе.

Работаете через ИИ-агента? Ему — [AGENTS.md](../../AGENTS.md): там то же самое в виде, удобном для
агента, — неинтерактивный запуск, JSON, коды выхода.

## Где запускать

LeetTest рассчитан на **Linux и macOS**: генератор ставят рядом с целью — на сервер в той же сети, в
контейнер, на раннер CI. Чем ближе генератор к цели, тем меньше в числах посторонней сети.

Windows поддерживается, но для измерений подходит хуже: часы Windows идут шагом около 0,5 мс, и
прогон с быстрым сервисом инструмент честно объявит недействительным (`clock_step`, код выхода 2) —
шаг часов сравним с самой латентностью. Для замеров берите Linux; Windows годится, чтобы
попробовать инструмент.

## 1. Установка

Два пути на выбор. Команды работают в терминале Linux и macOS как есть.

**1. Готовый бинарник** (Linux и macOS; amd64 и arm64) — Go не нужен, нужны `curl` и `tar`:

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | sh
```

Скрипт ставит последний релиз в `~/.local/bin` и сверяет контрольную сумму. Если этого каталога нет в
`PATH`, он печатает строку `export PATH=...`: выполните её в этом терминале, а чтобы сохранить,
добавьте в профиль shell. Другой каталог — переменная `LEETTEST_INSTALL_DIR`, другая версия —
`LEETTEST_VERSION`; переменная — перед `sh`, не перед `curl`:

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | LEETTEST_VERSION=v0.2.0 sh
```

Для Windows в [релизе](https://github.com/yhgrwav/leettest/releases/latest) лежит `.zip`.

**2. Через Go**: нужен Go 1.26 или новее ([установка](https://go.dev/doc/install); более старый Go с
`GOTOOLCHAIN=auto` скачает нужный сам):

```sh
go install github.com/yhgrwav/leettest/cmd/leettest@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

Проверка для обоих путей:

```sh
leettest -version
```

## 2. Цель: эталонный стенд

Для тура не нужен свой сервис: в репозитории есть эталонный стенд — gRPC-сервис с управляемым
поведением. Он отвечает на `grpc.health.v1.Health/Check` и `wallet.v1.WalletService` и отдаёт
reflection, как настоящий сервис.

Тур идёт в том же терминале и собирается из клона: нужны `git` и Go 1.26+, а стенд и `leettest` из
одного клона точно подходят к его конфигам.

```sh
git clone --depth 1 https://github.com/yhgrwav/leettest && cd leettest
go build -o bin/ ./cmd/leettest ./test/stand/cmd/stand
bin/stand -delay 20ms -hang-from 25s -life 3m > stand.log 2>&1 &
sleep 1
```

Стенд работает в фоне (`sleep 1` — секунда, чтобы он начал слушать порт) и сам остановится через 3 минуты (`-life 3m`: прогрев, 40 секунд прогона и
запас). Он отвечает за 20 мс, а через 25 секунд после первого вызова перестаёт отвечать совсем —
так выглядит сервис, который упал под нагрузкой. Правильный ответ известен заранее, поэтому видно,
говорит ли отчёт правду.

## 3. Конфиг

[`examples/ru/tour.yaml`](../../examples/ru/tour.yaml) задействует все поля, которые можно проверить на
локальном стенде:

```yaml
name: tour

app:
  target:
    ip: 127.0.0.1
    port: 50051
  tls: false
  metadata:
    authorization: Bearer ${LEETTEST_TOKEN}
    x-request-source: leettest-tour
  max_response_size: 1MiB
  connections: 1

load:
  warmup: 3s
  calls:
    - method: grpc.health.v1.Health/Check
      rps: 300
      duration: 40s
      timeout: 500ms
      data:
        service: wallet.v1.WalletService
    - method: wallet.v1.WalletService/GetBalance
      rps: 50
      duration: 40s
      timeout: 500ms
      dataset: wallets.jsonl
      dataset_order: random
      dataset_seed: 42
```

- **`name`** — как прогон называется в заголовке и в JSON-отчёте (`name`).
- **`tls: false`** — стенд без шифрования. По умолчанию TLS включён; `ca`, `cert`, `key`,
  `server_name` — в [`examples/tour-tls.yaml`](../../examples/ru/tour-tls.yaml) (ниже, «TLS»).
- **`metadata`** — заголовки каждого вызова. `${LEETTEST_TOKEN}` берётся из окружения: токен не
  попадает ни в конфиг, ни в историю shell, а значения заголовков инструмент не печатает нигде.
  Переменная не задана — ошибка до старта, а не прогон с пустым токеном.
- **`max_response_size`** — ответ больше лимита считается `bad response`, а не успехом.
- **`warmup`** — первые 3 секунды вызовы уходят к цели, но не попадают в percentiles.
- **`rps`, `duration`** — у каждого метода свои. Открытая модель: вызов уходит по расписанию,
  даже если предыдущие ещё не ответили, а латентность считается от запланированного момента.
  Поэтому медленный сервис не замедляет нагрузку и не прячет свою медлительность.
- **`timeout`** — сколько ждать ответа (по умолчанию 2 с).
- **`data`** — тело запроса обычным YAML. Схему инструмент берёт у сервиса через reflection,
  `.proto` не нужен. Ошибка в теле видна до старта.
- **`dataset`** — файл с телами запросов вместо `data`, по одному JSON на строку
  ([`examples/ru/wallets.jsonl`](../../examples/ru/wallets.jsonl)): первый вызов уходит с первой
  строкой, второй — со второй, после последней — снова с первой. Рядом со строкой метода отчёт
  говорит, сколько файла использовано. `dataset_order: random` перемешивает файл на каждом
  проходе; отчёт печатает seed, а `dataset_seed` повторяет порядок.
- **`connections`** — сколько соединений несут нагрузку (по умолчанию 1). За
  L4-балансировщиком одно соединение нагружает один бэкенд; при N ≥ 2 в отчёте появляется блок
  `Connections:` — строка на соединение.

Несколько методов — несколько вызовов, у каждого свой темп. Один метод — один вызов: отчёт идёт по
методам.

## 4. Запуск

```sh
export LEETTEST_TOKEN=demo
bin/leettest -c examples/ru/tour.yaml
```

`demo` — общий токен, который мы дарим первым пользователям.

Первый запуск в терминале спросит цветовую тему: стрелки и Enter, вопрос задаётся один раз
(`-plain` пропускает и вопрос, и живой экран).

Перед стартом инструмент подключается к цели, проверяет каждый метод через reflection, собирает
тело запроса и проверяет, что потолок запросов в полёте (`-max-in-flight`) выдержит зависание
цели. Всё, что можно узнать до прогона, — ошибка до первого вызова.

В терминале идёт живой экран: RPS, запросы в полёте, ошибки и percentiles по секундам, вкладка на
каждый метод, `?` — справка. **Остановка:** первое `q` (или Ctrl+C) — новые вызовы не уходят,
отправленные дожидаются своего таймаута; второе — обрывает их, отчёт всё равно печатается; третье
— выход без отчёта.

Без терминала (CI, перенаправление в файл) вместо экрана — строка прогресса раз в секунду в stderr.

**TLS.** Стенд с `-mtls` при старте пишет CA, свой сертификат и клиентский в `test/stand/certs` и
требует клиентский сертификат; [`examples/ru/tour-tls.yaml`](../../examples/ru/tour-tls.yaml)
указывает на эти файлы:

Стенд из §2 занимает тот же порт, поэтому перед другим его останавливают: `kill %%` завершает
последнюю фоновую команду этого терминала (если стенд уже остановился сам, ошибка скрыта
`2>/dev/null`):

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -mtls -delay 20ms -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/ru/tour-tls.yaml
```

## 5. Отчёт

Отчёт идёт в stdout, только ASCII — его можно сохранить в файл или разобрать `grep`. Тур на стенде
выше заканчивается так:

```
run finished: 127.0.0.1:50051 in 40.5s
name: tour
settings: warm-up 3s, max in flight 5000, connections 1
sent 12950, failed 5248

method                                           sent   failed    sent/s       p50       p90       p95       p99
grpc.health.v1.Health/Check                     11100     4499       300    20.5ms    >500ms    >500ms    >500ms
  plan: 300 rps for 40s
wallet.v1.WalletService/GetBalance               1850      749        50    20.5ms    >500ms    >500ms    >500ms
  plan: 50 rps for 40s
  data: 4 of 4 requests from wallets.jsonl, each used up to 500 times, random order, seed 42

warm-up 1050 sent, excluded from stats

grpc.health.v1.Health/Check: at 300 rps, 4499 of 11100 calls (40.5%) got no answer within 500ms,
and nothing after the call sent at 25.0s of the run got one.
...
failed calls by gRPC code:
grpc.health.v1.Health/Check codes sent by the target: DeadlineExceeded 2
grpc.health.v1.Health/Check codes set by the client: DeadlineExceeded 4497
...
start lag, how late calls began against their schedule: p99 109us, max 225us.
...
5248 requests were abandoned before answering. A percentile shown as "> value"
is a lower bound: the real tail lies above it. Raise the timeout to see it.
```

(Linux, Docker, 9a08876.)

Что здесь главное:

- **Какой это прогон.** `name:` и `settings:` повторяют имя прогона и с чем он запущен, а `plan:`
  под методом — заданные ему темп и длительность: отчёт, вставленный в тикет, можно сравнить
  с другим.
- **При какой нагрузке сервис перестал справляться.** «at 300 rps … nothing after the call sent at
  25.0s» — цель замолчала на 25-й секунде, ровно когда стенду велели. Момент считается по отправке
  вызова, а не по расписанию: отставание генератора не выдаётся за тишину цели.
- **`>500ms`** — не число, а нижняя граница: часть вызовов не дождалась ответа, и их настоящая
  латентность выше таймаута. Инструмент не подставляет таймаут вместо неизвестного значения.
- **Коды в двух строках.** «sent by the target» — статус пришёл от цели (или прокси перед ней);
  «set by the client» — статус поставил наш клиент, ответа не было. Это разные истории для
  разработчиков.
- **`start lag`** — насколько генератор отставал от расписания. Если он большой, узкое место —
  машина с генератором, а не цель; отчёт сам говорит, когда хвост латентности объясняется
  ожиданием на нашей стороне.
- **`clock step`** — точность часов машины; на Linux она в наносекундах, и строки нет. На Windows
  (около 0,5 мс) она появляется, а если шаг грубее четверти p50, прогон
  недействителен: числа меньше шага часов ничего не значат.

Категории отказов (`overload`, `failure`, `request error`, `timed out`, `cut off`, `unreachable` и
др.) и их смысл — в [README, «Как читать отчёт»](../../README.md#как-читать-отчёт).

Код выхода `0` не значит «сервис здоров»: читайте предел нагрузки и причины
([README, «Коды выхода»](../../README.md#коды-выхода)).

## 6. Другие поведения цели

Тот же конфиг против стенда в другом режиме — что покажет отчёт. Каждый блок сначала останавливает
прежний стенд (как в §4), затем запускает новый и прогон; токен `LEETTEST_TOKEN` из §4 уже задан.

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 20ms -freeze-at 10s -freeze-for 2s -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/ru/tour.yaml
```
Заморозка на 2 секунды при таймауте 500 мс: p50 и p90 остаются около 21 мс, а вызовы, пришедшие в
паузу, не дождались ответа — около 4% `got no answer within 500ms`, p99 печатается как `>500ms`.
Percentiles не усредняются: короткая заморозка видна в хвосте, а не размазана по среднему.

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 20ms -fail-every 10 -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/ru/tour.yaml
```
Каждый десятый вызов — `RESOURCE_EXHAUSTED`: категория `overload`, код в строке «sent by the target».

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 150ms -max-streams 1 -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/ru/tour.yaml
```
Цель разрешает один поток на соединение, и каждый ответ держит 150 мс: ответить через одно соединение
она успевает не больше чем на 6–7 вызовов в секунду. Отчёт говорит, где они застряли: `connections: 1; target
stream limit 1` и `not sent N: waited for a stream N` — вызовы ждали поток на нашей стороне и не
ушли к цели до своего таймаута. Это ограничение соединения, а не медленная цель.

## 7. Скрипты и CI

```sh
leettest -c load.yaml -output json > report.json
echo $?
```

JSON — версионированный (`schema_version`), решения — полями, а не текстом:
`invalid_reasons`, `methods[].invalid_reason`, `tail_wait_cause`, `methods[].silent_from_s`.
Описание — в [README, «JSON для скриптов и CI»](../../README.md#json-для-скриптов-и-ci).

Коды выхода:

| Код | Значение |
|---|---|
| `0` | План выполнен, отчёт полный |
| `1` | Отчёт, который просили, не получен: прогон не состоялся (флаги, конфиг, подключение) или отчёт не записан в `-json-file`. Причина — в stderr |
| `2` | Прогон недействителен: потолок in-flight, все вызовы метода — ошибки запроса, или часы хоста грубее четверти p50. Отчёт есть, числа не про нагрузку |
| `3` | Остановлен раньше плана. Отчёт покрывает то, что успело пройти |
| `4` | Законченный прогон пропустил предел `pass`. Отчёт есть, какой именно — в `thresholds` |
| `130`, `143` | Аварийный выход без отчёта после Ctrl+C или SIGTERM |

Чтобы CI падал по пределам, задайте `pass` под вызовом
([`examples/pass.yaml`](../../examples/pass.yaml), [README, «Пределы»](../../README.md#пределы-прогон-как-проверка))
и роняйте сборку на ненулевом коде выхода; поле JSON `thresholds` говорит, какой
предел пропущен или не решён.

## 8. Без сервиса вообще

```sh
bin/leettest -c examples/ru/leettest.yaml -fake -fake-delay 30ms -fake-jitter 10ms
```

`-fake` нагружает встроенную заглушку вместо цели — посмотреть на экран и отчёт без сервиса. Конфиг
берётся из клона (`examples/ru/leettest.yaml`), а `target` в нём игнорируется. Отчёт помечен
`fake target`.

## Дальше

- [Справочник](reference.md) — каждое поле и флаг; [README](../../README.md) — как читать отчёт.
- [Подводные камни](pitfalls.md) — что ломает замеры.
- [CONTRIBUTING](../../CONTRIBUTING.md) — как предложить изменение, человеку и ИИ-агенту.
