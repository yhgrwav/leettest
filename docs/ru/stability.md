# Что не меняется внутри версии 1.x

[← К документации](README.md)

**Действует с v1.0.0; до этого — раздел «Upgrading» в [заметках к релизу](https://github.com/yhgrwav/leettest/releases).** До 1.0 минорная версия (`0.7`, `0.8`) может сломать то, что
работало раньше, и каждая такая поломка названа в «Upgrading».

Версия записывается как `мажорная.минорная.патч`. С v1.0.0 скрипт, конфиг или дашборд, которые
работают на `1.0`, продолжают работать на любой `1.y`. Что именно сюда входит — ниже: значения
взяты из кода и справочника, как они есть сейчас.

Ниже две части: что меняется только в новой мажорной версии (`2.0`) и что можно менять в минорной.
Текст человеческого отчёта в обещание не входит: скрипты читают JSON и код выхода.

## Не меняется до `2.0`

### Коды выхода

| Код | Что случилось |
|---|---|
| `0` | План выполнен, отчёт полный; с `pass` — все пределы выдержаны |
| `1` | Отчёт, который просили, не получен: прогон не состоялся или отчёт не записан |
| `2` | Прогон недействителен или предел `pass` нельзя решить |
| `3` | Прогон остановлен раньше плана |
| `4` | Законченный прогон пропустил предел `pass` |
| `130` | Аварийный выход без отчёта после Ctrl+C |
| `143` | Аварийный выход без отчёта после SIGTERM |

Полное описание — [коды выхода](report.md#коды-выхода).

### JSON отчёта: `-output json`, `schema_version` 1

Каждое существующее поле сохраняет имя, тип и смысл, а `schema_version` растёт только с мажорной
версией. Поля верхнего уровня:

- Прогон (`mode: "run"`): `schema_version`, `mode`, `leettest_version`, `target`, `name`,
  `methods_source`, `outcome`, `invalid_reasons`, `tail_wait_cause`,
  `generator_limit`, `stop_reason`, `stop`, `seq_overflow`, `clock_step_ns`, `started_at`, `duration_us`, `planned_us`, `warmup_us`,
  `settings`, `passport`, `sent`, `failed`, `aborted`, `not_sent`, `not_sent_generator`,
  `not_sent_stream`, `not_sent_connection`, `warmup_sent`, `warmup_failed`, `warmup_not_sent`,
  `cap_hit`, `start_lag`, `schedule`, `connections`, `client_waits`, `generator_cpu`, `traffic`,
  `methods`, `unchecked`, `thresholds`, `notes`.
- Поиск точки отказа (`mode: "breakpoint"`): `schema_version`, `mode`, `leettest_version`, `target`,
  `name`, `methods_source`, `method`, `started_at`, `settings`, `passport`, `breakpoint`,
  `thresholds`.

Вложенные поля, их типы и смысл — в [полях JSON](reference.md#поля-json). Правила схемы (единицы,
`null` вместо `0`, что делать с неизвестными полями) — в
[JSON для скриптов и CI](report.md#json-для-скриптов-и-ci). `notes` и `unchecked[].error` — текст
для людей, его формулировки меняются свободно, разбирать его не нужно. Значения полей-перечислений
(`outcome`, `invalid_reasons`, `stop_reason`, `unchecked[].reason`, коды в `failure_codes`) могут
пополняться: скрипт обязан не падать на незнакомом значении.

### 11 названий категорий вызова

`success`, `request_error`, `overload`, `failure`, `timed_out`, `cut_off`, `unreachable`,
`client_error`, `bad_response`, `aborted`, `unclassified`. Они же — имена полей в JSON и значения
метки `category` в Prometheus. Что значит каждая — в
[как читать отчёт](report.md#как-читать-отчёт).

### Ключи конфига и флаги

Каждый ключ конфига и каждый флаг, которые есть в 1.0: имя, смысл и допустимые значения.
Полный список — [конфиг](reference.md#конфиг), [флаги](reference.md#флаги) и
[сравнение отчётов](reference.md#сравнение-отчётов-leettest-compare) со своими флагами.

### Метрики Prometheus

Имена серий и имена меток, и их смысл. Сейчас серии такие:

`leettest_planned_rps`, `leettest_calls_total`, `leettest_latency_seconds`,
`leettest_censored_total`, `leettest_excluded_total`, `leettest_not_sent_total`,
`leettest_in_flight`, `leettest_start_lag_seconds`, `leettest_warmup_sent_total`,
`leettest_warmup`.

Метки: `method`, `category`, `cause`, `le`. У гистограмм — обычные `_bucket`, `_sum`, `_count`. Какая
серия что считает, какие у неё метки и тип — в [метриках Prometheus](reference.md#метрики-prometheus).

## Можно менять в минорной версии

- Добавлять поля JSON, флаги, ключи конфига, серии и метки.
- Менять текст человеческого отчёта.
- Менять стенд и инструменты для CI.
- Ужесточать проверку конфига: отвергается только то, что и раньше было неверным или не определено.
- Исправлять неверное число: число, которое лгало, — ошибка, а не изменение обещания.

## Устаревание

Устаревший флаг, ключ или поле объявляются в «Upgrading» заметок к релизу, при использовании печатают
одно предупреждение в stderr, продолжают работать до конца мажорной версии и уходят в следующей.
Сейчас ничего не объявлено устаревшим.

## Где смотреть дальше

- [Справочник](reference.md) — каждый ключ, флаг, серия и поле JSON.
- [Отчёт, коды выхода, JSON](report.md) — как читать отчёт и что в нём можно разбирать скриптом.
- [Чего LeetTest не делает](limitations.md) — границы, в которых числа отчёта перестают быть про цель.
