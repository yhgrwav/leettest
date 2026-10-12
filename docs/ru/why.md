# Почему именно он?

[← К документации](README.md)

LeetTest построен на одном принципе: результат нагрузочного теста имеет ценность только
тогда, когда ему можно доверять без оговорок. Всё остальное в инструменте следует из этого.

Приоритеты расставлены в таком порядке: **корректность измерений**, затем **удобство**, затем
**скорость**. При конфликте выигрывает первый — оптимизация, ухудшающая точность, отклоняется.

## Сравнение с ghz, k6 и JMeter

🟢 есть · 🔴 нет · 🟡 частично или с доработкой (слово в ячейке — какой именно). Числа в
квадратных скобках — [источники](#источники) внизу страницы. JMeter — версия 5.6.3 с плагином для gRPC: сам
JMeter gRPC не умеет [20][21].

**Что можно запустить**

| Что проверяем | ghz | k6 | JMeter | LeetTest |
|---|---|---|---|---|
| gRPC без плагина | 🟢 [1] | 🟢 [13] | 🟡 плагин [20][21] | 🟢 |
| Метод без `.proto`, по server reflection | 🟢 [3] | 🟢 [14] | 🔴 нужны `.proto` [21] | 🟢 |
| Сервис без reflection: схема из `.proto` или protoset | 🟢 [3] | 🟢 [15] | 🟢 [21] | 🔴 [roadmap](roadmap.md#следующая-версия-v03) |
| Несколько методов, у каждого свой RPS, в одном прогоне | 🔴 один метод [1] | 🟡 сценарии в скрипте [10] | 🟡 группы потоков [22] | 🟢 |
| Нагрузка описана без своего кода | 🟢 флаги, JSON, TOML [2] | 🔴 JavaScript [10] | 🟢 план `.jmx` [28] | 🟢 YAML |
| Разгон: RPS растёт по времени | 🟢 [7] | 🟢 [16] | 🟡 экспериментально [23] | 🔴 [roadmap](roadmap.md#дальше) |
| Пороги и код выхода для CI | 🔴 [8] | 🟢 [17] | 🔴 [20][26] | 🔴 [roadmap](roadmap.md#дальше) |
| Поле ответа идёт в следующий запрос | 🔴 [9] | 🟡 скрипт [18] | 🟢 постпроцессоры [27] | 🔴 [roadmap](roadmap.md#дальше) |
| Поиск нагрузки, на которой цель ломается | 🔴 [8] | 🟡 скрипт [19] | 🔴 [20] | 🟢 ступени |

**Как измеряется**

| Что проверяем | ghz | k6 | JMeter | LeetTest |
|---|---|---|---|---|
| Нагрузка не падает, когда цель замедлилась (открытая модель) | 🟡 `--async`, не по умолчанию [4] | 🟡 свой исполнитель, не по умолчанию [11] | 🟡 экспериментально, не по умолчанию [23] | 🟢 единственная модель |
| Латенси от запланированного момента: ожидание у генератора входит в число | 🔴 от отправки [5] | 🔴 от отправки, остальное в `dropped_iterations` [12][13] | 🔴 от отправки [24] | 🟢 |
| Упавшие и оборванные таймаутом вызовы входят в перцентили | 🟡 `--count-errors`, не по умолчанию [6] | 🟢 [13] | 🟢 [25] | 🟢 оборванные — нижняя граница |

Открытая модель по умолчанию, латенси от запланированного момента и поиск точки отказа — строки,
где ни у ghz, ни у k6, ни у JMeter нет 🟢. Разгона, порогов для CI, связывания вызовов и работы без
reflection у LeetTest пока нет; порядок — в [roadmap](roadmap.md).

## Нагрузка, повторяющая продакшн

Инструмент описывает нагрузку так, как она существует в реальности: несколько методов
одновременно, у каждого своя интенсивность и своя длительность, всё в одном прогоне и одном
отчёте. Именно на смеси вызовов проявляются конкуренция за пул соединений, блокировки в базе и
борьба за кеш — эффекты, которых не видно, пока методы измеряются по одному.

## Измерение, устойчивое к искажению

Латенси отсчитывается от момента, на который запрос был запланирован, а не от момента, когда его
удалось отправить. Это фиксирует задержку целиком, включая время, проведённое в очереди на
стороне генератора. При зависании цели на одну секунду инструмент сообщает `1005ms` — реальную
задержку, которую увидел бы пользователь, — вместо `5ms`, отражающих лишь скорость ответа после
восстановления.

Нагрузка подаётся по расписанию: заданный RPS выдерживается независимо от того, успевает ли
сервис отвечать.

Запрос, оборванный таймаутом, не подменяется числом. Про него известно одно — он длился дольше
таймаута, — и если такие запросы могли бы занять место перцентиля, отчёт печатает не число, а
нижнюю границу: `p99 >2.0s`.

## Работа без подготовки

Для запуска достаточно адреса сервиса и имени метода. Описание метода инструмент получает у
самого сервиса через gRPC server reflection: ни файлов `.proto`, ни кодогенерации, ни сборочных
шагов. Тело запроса пишется в конфиге обычным YAML и собирается по схеме сервиса до старта.

## Декларативное описание

Нагрузка задаётся конфигурационным файлом, а не программой. Файл хранится вместе с кодом
сервиса, проходит через код-ревью и остаётся понятным спустя полгода. Он не требует отладки и не
может сам стать источником искажений в измерении.

## Внимание к работе человека

Сообщения об ошибках называют место и причину. Неизвестный ключ в конфигурации и нулевая
интенсивность считаются ошибками, а не молчаливо принимаются: прогон, который не отправил ни
одного запроса, не должен выглядеть успешным. Состояние прогона видно в реальном времени —
текущая интенсивность, количество запросов в полёте, доля ошибок, percentiles.

Качество интерфейса рассматривается наравне с корректностью измерений: неясное сообщение об
ошибке считается дефектом.

## Источники

Версии: ghz v0.121.0 (последний релиз), k6 v2.3.0, JMeter 5.6.3 с плагином jmeter-grpc-request
v1.2.6. Проверено 2026-10-12. Исходники — на теге версии, документация k6 — на коммите
[`b5fdef3`](https://github.com/grafana/k6-docs/tree/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x).

**ghz**

1. Один метод на прогон: `--call` принимает одно имя, запуск — `runner.Run(cfg.Call, …)` —
   [`cmd/ghz/main.go` L40](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L40),
   [L334](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L334).
2. Нагрузка задаётся флагами или файлом JSON/TOML —
   [`cmd/ghz/main.go` L28](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L28).
3. Описание метода берётся из `.proto`, protoset, а без них — через reflection —
   [`runner/requester.go` L79–L113](https://github.com/bojand/ghz/blob/v0.121.0/runner/requester.go#L79-L113).
4. Синхронный воркер ждёт вызов, с `--async` запускает горутину на вызов —
   [`runner/worker.go` L47–L70](https://github.com/bojand/ghz/blob/v0.121.0/runner/worker.go#L47-L70);
   флаг — [`cmd/ghz/main.go` L78](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L78).
5. Длительность вызова — `EndTime − BeginTime` из событий gRPC stats —
   [`runner/stats_handler.go` L44](https://github.com/bojand/ghz/blob/v0.121.0/runner/stats_handler.go#L44).
6. В перцентили и гистограмму идут только вызовы без ошибки, пока не задан `--count-errors` —
   [`runner/reporter.go` L259–L264](https://github.com/bojand/ghz/blob/v0.121.0/runner/reporter.go#L259-L264),
   [`cmd/ghz/main.go` L218](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L218).
   Ответ мейнтейнера: «the stats are only based on successful / OK responses» —
   [issue 248](https://github.com/bojand/ghz/issues/248).
7. `--rps` и профили `--load-schedule` const/step/line —
   [`cmd/ghz/main.go` L82–L109](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L82-L109).
8. Ненулевой код выхода — только если не удалось запустить прогон или записать отчёт; флагов
   порогов нет. Весь список флагов —
   [`cmd/ghz/main.go` L28–L279](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L28-L279),
   выход — [L334–L342, L385–L392](https://github.com/bojand/ghz/blob/v0.121.0/cmd/ghz/main.go#L385-L392).
9. Данные шаблона вызова (`CallData`) не содержат ответа предыдущего вызова —
   [`runner/calldata.go` L32–L46](https://github.com/bojand/ghz/blob/v0.121.0/runner/calldata.go#L32-L46).

**k6**

10. Несколько сценариев в одном скрипте, у каждого свой исполнитель и своя JavaScript-функция
    (`exec`) —
    [Scenarios](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/_index.md#L13-L18),
    [таблица опций, `exec`](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/_index.md#L90).
11. Открытая модель — только в исполнителях arrival-rate; `vus` с `duration` — это
    исполнитель constant-vus, то есть закрытая модель —
    [Open and closed models](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/concepts/open-vs-closed.md#L9-L13),
    [Options, `duration`](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/k6-options/reference.md#L329).
12. Без свободного VU итерация отбрасывается и попадает в счётчик `dropped_iterations` —
    [Dropped iterations](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/concepts/dropped-iterations.md#L10-L25).
13. `grpc_req_duration` пишется на событии End каждого вызова как `EndTime − BeginTime`, статус
    — тегом —
    [`internal/lib/netext/grpcext/conn.go` L282–L295](https://github.com/grafana/k6/blob/v2.3.0/internal/lib/netext/grpcext/conn.go#L282-L295).
14. `reflect: true` в параметрах `connect` —
    [Client.connect](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/javascript-api/k6-net-grpc/client/client-connect-connect-address-params.md#L26).
15. `Client.load` читает `.proto` —
    [Client.load](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/javascript-api/k6-net-grpc/client/client-load.md#L11-L18).
16. Исполнитель `ramping-arrival-rate` меняет темп по стадиям —
    [Ramping arrival rate](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/scenarios/executors/ramping-arrival-rate.md#L8-L16).
17. Проваленный порог — ненулевой код выхода, 99 —
    [Thresholds](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/using-k6/thresholds.md#L70-L80),
    [`errext/exitcodes/codes.go` L18](https://github.com/grafana/k6/blob/v2.3.0/errext/exitcodes/codes.go#L18).
18. `Client.invoke` возвращает объект ответа в скрипт —
    [Client.invoke](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/javascript-api/k6-net-grpc/client/client-invoke.md#L24-L28).
19. Тест точки отказа — `ramping-arrival-rate` и порог с `abortOnFail` —
    [Breakpoint testing](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/testing-guides/test-types/breakpoint-testing.md#L66-L75),
    [L102–L106](https://github.com/grafana/k6-docs/blob/b5fdef340160ab85c28de9b918726a6729dbc0f1/docs/sources/k6/v2.3.x/testing-guides/test-types/breakpoint-testing.md#L102-L106).

**JMeter**

20. В справочнике компонентов JMeter 5.6.3 нет gRPC-сэмплера (слова gRPC в нём нет), нет порогов
    по перцентилю и поиска точки отказа —
    [`xdocs/usermanual/component_reference.xml`](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml).
21. Плагин jmeter-grpc-request v1.2.6: только unary-вызовы, на входе папка с `.proto`;
    [репозиторий в архиве](https://github.com/zalopay-oss/jmeter-grpc-request) —
    [README L19, L28–L29](https://github.com/zalopay-oss/jmeter-grpc-request/blob/v1.2.6/README.md#L19).
22. Один план — несколько групп потоков; у Constant Throughput Timer и у Open Model Thread Group
    свой темп на группу —
    [`component_reference.xml` L5153](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L5153),
    [L5228](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L5228),
    [L6517–L6521](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L6517-L6521).
23. Open Model Thread Group «is experimental»; расписание `rate(…) random_arrivals(…) rate(…)`
    меняет темп линейно; обычная Thread Group — пул потоков с числом итераций —
    [`component_reference.xml` L6519–L6526](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L6519-L6526),
    [L6619–L6621](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L6619-L6621).
24. Elapsed time — «от момента перед отправкой запроса до получения последнего ответа» —
    [`xdocs/usermanual/glossary.xml` L34–L35](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/glossary.xml#L34-L35).
25. В Aggregate Report и в HTML-отчёт время каждого вызова попадает и при ошибке —
    [`SamplingStatCalculator.java` L185](https://github.com/apache/jmeter/blob/rel/v5.6.3/src/core/src/main/java/org/apache/jmeter/visualizers/SamplingStatCalculator.java#L185),
    [`StatisticsSummaryConsumer.java` L79–L88](https://github.com/apache/jmeter/blob/rel/v5.6.3/src/core/src/main/java/org/apache/jmeter/report/processor/StatisticsSummaryConsumer.java#L79-L88).
26. В коде запуска `System.exit(1)` — только при ошибке запуска, плюс свойство, выключенное по
    умолчанию, для остановки потоков; Duration Assertion помечает проваленным отдельный ответ, а
    не прогон —
    [`JMeter.java` L519](https://github.com/apache/jmeter/blob/rel/v5.6.3/src/core/src/main/java/org/apache/jmeter/JMeter.java#L519),
    [`get-started.xml` L476–L478](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/get-started.xml#L476-L478),
    [`component_reference.xml` L4695–L4698](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L4695-L4698).
27. Постпроцессоры JSON Extractor и Regular Expression Extractor берут значения из ответа
    сэмплера и сохраняют в переменную —
    [`component_reference.xml` L6334](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L6334),
    [L5797](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/component_reference.xml#L5797).
28. План хранится в файле `.jmx`, запускается `jmeter -n -t план.jmx` —
    [`xdocs/usermanual/get-started.xml` L474](https://github.com/apache/jmeter/blob/rel/v5.6.3/xdocs/usermanual/get-started.xml#L474).

