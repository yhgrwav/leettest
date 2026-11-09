# Первый прогон

[← К документации](../README.md)

Здесь — путь от установки до первого отчёта: поставить LeetTest, написать конфиг на два метода,
запустить и прочитать результат. Свой сервис для этого не нужен: ниже есть тестовый сервис, который
идёт вместе с LeetTest. Работаете через ИИ-агента — дайте ему [docs/agent.md](../../agent.md) (фраза для
него — в [README](../../../README.md#установка)): он сделает те же шаги сам.

## Где запускать

Запускайте на Linux или macOS рядом с сервисом: в той же сети, в том же кластере или на раннере CI.
Всё, что лежит между LeetTest и сервисом, входит в задержку и выглядит как время сервиса: на 1000
запросах в секунду через проброс портов Docker Desktop p99 был 38 мс, внутри сети Docker — 2 мс
(наш стенд, одна машина; в других условиях числа будут другими).

Windows подходит, чтобы попробовать. На нашей машине с Windows 11 шаг часов был около 0,5 мс (LeetTest
измеряет его сам и печатает), и прогон с быстрым сервисом LeetTest может объявить недействительным (`clock_step`, код выхода 2): шаг часов сравним с самой
задержкой. Для замеров берите Linux. Остальное про место запуска — в
[справочнике](../reference.md#где-запускать).

## Установка

**Linux и macOS** (amd64 и arm64). Нужны `curl` и `tar`:

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | sh
```

Скрипт ставит последний релиз в `~/.local/bin` и сверяет контрольную сумму. Если этого каталога нет
в `PATH`, он печатает строку для профиля shell: выполните её в этом терминале, а чтобы сохранить,
добавьте в профиль. Другой каталог — переменная `LEETTEST_INSTALL_DIR`, другая версия —
`LEETTEST_VERSION`; переменную пишут перед `sh`, не перед `curl`:

```sh
curl -fsSL https://raw.githubusercontent.com/yhgrwav/leettest/main/install.sh | LEETTEST_VERSION=v0.2.0 sh
```

**Windows.** Архив `.zip` со [страницы релизов](https://github.com/yhgrwav/leettest/releases/latest):
распакуйте `leettest.exe` в каталог из `PATH`.

**Любая ОС с Go 1.26 или новее** ([установка Go](https://go.dev/doc/install); более старый Go с
`GOTOOLCHAIN=auto` скачает нужный сам):

```sh
go install github.com/yhgrwav/leettest/cmd/leettest@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

Проверка для любого способа:

```sh
leettest -version
```

## Запуск

Сохраните в `leettest.yaml`, подставив адрес и методы своего сервиса:

```yaml
app:
  target:
    ip: localhost
    port: 50051
  tls: false

load:
  warmup: 5s
  calls:
    - method: wallet.v1.WalletService/GetBalance
      rps: 800
      duration: 1m

    - method: wallet.v1.WalletService/Transfer
      rps: 50
      duration: 1m
```

- `tls: false` — сервис без шифрования. Без этой строки TLS включён.
- `method` — полное имя: `пакет.Сервис/Метод`. `.proto` не нужен: схему LeetTest берёт у сервиса
  через gRPC server reflection (сервис сам отдаёт список своих методов и сообщений). Если
  reflection на сервисе выключен, см. [подключение](connection.md).
- `rps` — запросов в секунду для этого метода, `duration` — сколько его нагружать.
- `warmup` — первые секунды идут к сервису, но не попадают в статистику.

Проверить конфиг без нагрузки: LeetTest подключится, проверит методы, пошлёт по одному запросу на
каждый и покажет запрос и ответ. Отчёта не будет.

```sh
leettest -c leettest.yaml -dry-run
```

Запуск:

```sh
leettest -c leettest.yaml
```

Перед стартом LeetTest подключается к сервису и проверяет каждый метод и тело запроса. Ошибка в
конфиге, неизвестный метод, ошибка в теле запроса, отказ в доступе `Unauthenticated` при заданных
заголовках — код выхода 1 и ни одного запроса к сервису. Ответ `PermissionDenied` прогон не
останавливает, подробности — в [подключении](connection.md#что-появится-в-отчёте).

В терминале идёт живой экран: запросы в секунду, запросы без ответа, ошибки и перцентили по
секундам, вкладка на каждый метод, `?` — справка. Остановка: первое `q` (или Ctrl+C) — новые
запросы не уходят, отправленные ждут своего тайм-аута; второе — обрывает их, отчёт всё равно
печатается; третье — выход без отчёта. Без терминала (CI, вывод в файл, ssh) вместо экрана — строка
прогресса раз в секунду в stderr; `-plain` включает её и в терминале:

```text
32.0s  sent 25600  rps 800  in-flight 47  failed 51  not-sent 0  p99 43ms
```

`in-flight` — запросы, отправленные и ещё не получившие ответ; `not-sent` — запросы, которые по плану
должны были уйти, но не ушли (например, ждали свободное соединение).

Отчёт идёт в stdout, прогресс и ошибки — в stderr. Первый запуск в терминале спросит цветовую тему
(один раз; `-plain` пропускает вопрос).

## Что будет в отчёте

В конце — строка на каждый метод: сколько отправлено, сколько отказов, `sent/s` и перцентили
p50, p90, p95, p99. Главное:

- Задержка считается от **запланированного** момента запроса. Если сервис или сам LeetTest
  задержал запрос, эта задержка входит в измеренную. Запрос уходит по расписанию, не дожидаясь
  ответов на предыдущие.
- `>2.0s` — не число, а «не меньше 2 секунд»: часть запросов оборвалась по тайм-ауту, и их
  настоящая задержка выше. LeetTest не подставляет тайм-аут вместо неизвестного значения.
- Ошибки разложены по категориям (`overload`, `failure`, `timed out` и другие), а под отчётом
  коды gRPC стоят в двух строках: «sent by the target» — статус прислал сервис или прокси перед
  ним, «set by the client» — статус поставил LeetTest, ответа не было.
- Код выхода `0` не значит «сервис здоров»: он значит, что план выполнен. Чтобы прогон сам
  выдавал вердикт «прошёл / не прошёл», задайте [пределы](../report.md#пределы-прогон-как-проверка).

Каждая цифра и категория — в разделе [Как читать отчёт](../report.md#как-читать-отчёт).

## Пробный прогон на тестовом сервисе

Тестовый сервис лежит в репозитории: gRPC-сервис с управляемым поведением. Он отвечает на
`grpc.health.v1.Health/Check` и `wallet.v1.WalletService` и отдаёт reflection, как настоящий.
Правильный ответ известен заранее, поэтому отчёт можно сверить с тем, что сервис делал на самом деле. Нужны `git` и Go 1.26+;
тестовый сервис и `leettest` из одного клона соответствуют его конфигам по версии.

```sh
git clone --depth 1 https://github.com/yhgrwav/leettest && cd leettest
go build -o bin/ ./cmd/leettest ./test/stand/cmd/stand
bin/stand -delay 20ms -hang-from 25s -life 3m > stand.log 2>&1 &
sleep 1
```

Сервис работает в фоне (`sleep 1` — секунда, чтобы он начал слушать порт) и сам остановится через
3 минуты (`-life 3m`: прогрев, 40 секунд прогона и запас). Он отвечает за 20 мс, а через 25 секунд после первого запроса перестаёт отвечать совсем —
так выглядит сервис, который упал под нагрузкой.

Конфиг [`examples/ru/tour.yaml`](../../../examples/ru/tour.yaml) задействует все поля, которые можно
проверить на локальном сервисе:

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

- `name` — как прогон называется в заголовке и в JSON-отчёте.
- `metadata` — заголовки каждого запроса. `${LEETTEST_TOKEN}` берётся из окружения: токен не попадает
  ни в конфиг, ни в историю shell; значения заголовков не попадают в отчёт. Переменная не
  задана — ошибка до старта, а не прогон с пустым токеном.
- `max_response_size` — ответ больше лимита считается `bad response`, а не успехом.
- `timeout` — сколько ждать ответа (по умолчанию 2 с).
- `data` — тело запроса обычным YAML. Ошибка в теле видна до старта.
- `dataset` — файл с телами запросов вместо `data`, по одному JSON на строку
  ([`examples/ru/wallets.jsonl`](../../../examples/ru/wallets.jsonl)): первый запрос уходит с первой
  строкой, второй — со второй, после последней — снова с первой. `dataset_order: random`
  перемешивает файл на каждом проходе; отчёт печатает seed, а `dataset_seed` повторяет порядок.
  Рядом со строкой метода отчёт говорит, сколько файла использовано.
- `connections` — сколько соединений несут нагрузку (по умолчанию 1). За L4-балансировщиком одно
  соединение нагружает один сервер; при двух и больше в отчёте появляется блок `Connections:` —
  строка на соединение.

Несколько методов — несколько вызовов в `calls`, у каждого свой темп. Один метод — один вызов.

```sh
export LEETTEST_TOKEN=demo
bin/leettest -c examples/ru/tour.yaml
```

`demo` — токен для этого тура. Прогон заканчивается так:

```text
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

(Linux, Docker, 9a08876.) Что здесь главное:

- **Какой это прогон.** `name:` и `settings:` повторяют имя прогона и с чем он запущен, `plan:` под
  методом — заданные ему темп и длительность: отчёт, вставленный в тикет, можно сравнить с другим.
- **При какой нагрузке сервис перестал справляться.** «at 300 rps … nothing after the call sent at
  25.0s» — сервис замолчал на 25-й секунде, ровно когда ему велели. Момент считается по отправке
  запроса, а не по расписанию: отставание LeetTest не выдаётся за тишину сервиса.
- **`>500ms`** — «не меньше 500 мс»: часть запросов не дождалась ответа.
- **`start lag`** — насколько LeetTest отставал от расписания. Если он большой, узкое место — машина,
  где запущен LeetTest, а не сервис; отчёт сам говорит, когда хвост задержки объясняется
  задержками внутри LeetTest, не в сервисе.
- **`clock step`** — точность часов машины. На Linux она в наносекундах, и строки нет. На Windows
  (у нас около 0,5 мс) строка появляется, а если шаг грубее четверти p50, прогон недействителен: числа
  меньше шага часов ничего не значат.

Тот же конфиг против тестового сервиса в другом режиме. Каждый блок сначала останавливает прежний
сервис (`kill %%` завершает последнюю фоновую команду этого терминала), затем запускает новый и
прогон; токен `LEETTEST_TOKEN` уже задан.

Сервис замирает на 2 секунды при тайм-ауте 500 мс:

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 20ms -freeze-at 10s -freeze-for 2s -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/ru/tour.yaml
```

p50 и p90 остаются около 21 мс, а запросы, пришедшие в паузу, не дождались ответа: около 4%
`got no answer within 500ms`, p99 печатается как `>500ms`. Перцентили не усредняются: короткая
заморозка видна в хвосте, а не размазана по среднему.

Каждый десятый ответ — ошибка `RESOURCE_EXHAUSTED`:

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 20ms -fail-every 10 -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/ru/tour.yaml
```

Категория `overload`, код в строке «sent by the target».

Сервис разрешает один поток на соединение, и каждый ответ занимает 150 мс:

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -delay 150ms -max-streams 1 -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/ru/tour.yaml
```

По одному соединению сервис успевает ответить не больше чем на 6–7 запросов в секунду. Отчёт
говорит, где они застряли: `connections: 1; target stream limit 1` и `not sent N: waited for a
stream N` — запросы ждали свободный поток внутри LeetTest и не ушли к сервису до своего тайм-аута.
Это ограничение соединения, а не медленный сервис.

С TLS: тестовый сервис с `-mtls` (mTLS — взаимный TLS: сервис проверяет не только свой сертификат
для клиента, но и сертификат клиента) при старте пишет сертификаты в `test/stand/certs` и требует
клиентский сертификат; [`examples/ru/tour-tls.yaml`](../../../examples/ru/tour-tls.yaml) указывает на
эти файлы.

```sh
kill %% 2>/dev/null; sleep 1
bin/stand -mtls -delay 20ms -life 3m > stand.log 2>&1 &
sleep 1
bin/leettest -c examples/ru/tour-tls.yaml
```

Совсем без сервиса: `-fake` нагружает встроенную заглушку вместо сервиса. Конфиг берётся из клона,
а `target` в нём игнорируется. Отчёт помечен `fake target`.

```sh
bin/leettest -c examples/ru/leettest.yaml -fake -fake-delay 30ms -fake-jitter 10ms
```

## Частые ошибки

- **`0` принят за «сервис здоров».** Код `0` значит «план выполнен». Здоровье проверяют пределы
  `pass` и чтение отчёта.
- **Сервис без шифрования, а `tls: false` нет.** TLS включён по умолчанию, и подключение к
  сервису без шифрования падает до старта с кодом 1.
- **Метод написан не полностью.** Нужно `пакет.Сервис/Метод`; опечатку LeetTest находит до первого
  запроса.
- **Reflection на сервисе выключен.** Метод с `data` не запустится; схему можно дать файлом
  `.proto` или protoset (набор описаний методов в одном файле) — [подключение](connection.md).
- **Код 2 и «clock step» на Windows.** Запустите с Linux рядом с сервисом.
- **Запуск через проброс портов Docker Desktop.** Задержки включат этот путь.

## Дальше

- [Подключение](connection.md) — TLS, заголовки, токены.
- [Нагрузка](load.md) — методы, темп, разгон, тело запроса.
- [Справочник](../reference.md) — каждое поле и флаг.
- [Подводные камни](../pitfalls.md) — что ломает замеры.
