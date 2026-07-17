# 04 — CLI Specification

Built with [`spf13/cobra`](https://github.com/spf13/cobra). Grammar is
consistently `queuectl <noun> <verb> [flags]` except for the two top-level
verbs `enqueue` and `status`, which are common enough to shortcut.

All commands support `--json` for machine-readable output (useful for the
test scripts in `06-testing-strategy.md`) and default to a human-readable
table otherwise. Exit codes are consistent: `0` success, `1` user error
(bad input), `2` runtime error (DB unreachable, etc.).

## 4.1 `queuectl enqueue`

```
queuectl enqueue '{"id":"job1","command":"sleep 2","max_retries":3}'
```

- `id` optional — a UUID is generated if omitted.
- `command` required.
- `max_retries` optional — defaults to configured `max_retries`.
- `priority` optional int, default 0 (bonus).
- `run_at` optional RFC3339 timestamp for delayed execution (bonus); default now.

Output:
```
enqueued job job1 (state=pending)
```

## 4.2 `queuectl worker start`

```
queuectl worker start --count 3 [--detach] [--foreground]
```

- `--count N` (default 1): number of workers.
- `--detach`: spawn N OS subprocesses (`queuectl worker run --id <n>`)
  instead of goroutines; PIDs written to `~/.queuectl/workers.pid`.
- `--foreground` (default when `--detach` absent): blocks the terminal,
  runs workers as goroutines, `Ctrl-C` triggers graceful shutdown.

## 4.3 `queuectl worker stop`

```
queuectl worker stop
```

Reads `~/.queuectl/workers.pid`, sends `SIGTERM` to each. Each worker
finishes its current job, releases its lease cleanly, then exits — see
`05-concurrency-and-reliability.md` §Graceful shutdown for the exact
sequence. Command blocks until all listed PIDs have exited or a
`--timeout` (default 30s) elapses, after which it reports which workers
didn't stop cleanly.

## 4.4 `queuectl status`

```
queuectl status
```

```
Jobs:
  pending:     4
  processing:  2
  completed:  18
  failed:      1
  dead:        1
Workers:
  active: 2   (pids: 4821, 4822)
```

`--json` returns the same data as a flat object for scripting.

## 4.5 `queuectl list`

```
queuectl list --state pending [--limit 20] [--json]
```

Table columns: `id`, `state`, `attempts/max_retries`, `command` (truncated),
`updated_at`.

## 4.6 `queuectl dlq list` / `queuectl dlq retry`

```
queuectl dlq list
queuectl dlq retry job1
queuectl dlq retry --all
```

`dlq retry` resets `attempts=0`, `state=pending`, `available_at=now`. Refuses
(exit 1) if the job id isn't currently `dead`, with a clear error message —
this is the kind of guardrail a grader will specifically try to break.

## 4.7 `queuectl config`

```
queuectl config set max-retries 5
queuectl config set backoff-base 2
queuectl config set job-timeout 60s
queuectl config get max-retries
queuectl config list
```

Keys are kebab-case on the CLI, mapped to snake_case columns internally
(`core/config.go` owns the mapping table — one place to add a new
setting).

## 4.8 Help text contract

`queuectl --help` and every subcommand `--help` must show: one-line
summary, full flag list with defaults, and one usage example — Cobra
generates most of this from struct tags; examples are hand-written per
command so they're never generic.

## 4.9 Output/error conventions

- Errors go to stderr, prefixed `error:`, never a bare stack trace.
- Successful mutating commands print a one-line confirmation including the
  affected job id and new state — makes CLI demo recordings self-explanatory
  without narration.
- `--json` output is always a single JSON object/array on one write (no
  streaming partial JSON), so it's trivially pipeable to `jq`.
