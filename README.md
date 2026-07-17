# QueueCTL

QueueCTL is a single-binary, embedded-storage, in-process background worker pool built in Go. It operates similarly to tools like Sidekiq or Celery but without needing external dependencies like Redis or RabbitMQ. All state is maintained locally using SQLite in WAL (Write-Ahead Logging) mode, enabling high concurrency and persistence out of the box.

## Features

- **No External Dependencies**: Single binary that handles both CLI routing and background processing.
- **Embedded Storage**: Leverages SQLite (WAL mode) with a robust locking mechanism for thread-safe concurrent job claims.
- **Resilient Processing**: Built-in exponential backoff for failed jobs and a Dead Letter Queue (DLQ) for abandoned ones.
- **Crash Recovery**: Jobs have leases. If a worker dies unexpectedly mid-job, the lease expires and another worker will seamlessly reclaim and execute it.
- **High Concurrency**: Spin up as many foreground or detached workers as needed; they will safely pull from the same SQLite queue without overlap.

---

## Building from Source

Ensure you have Go installed on your system.

```bash
git clone <repository_url>
cd QueueCTL
go build -o queuectl.exe ./main.go
```

*(On macOS/Linux, output to just `queuectl` instead of `queuectl.exe`)*

---

## Architecture Overview

1. **Storage Layer (`store/`)**: Uses SQLite with `BEGIN IMMEDIATE` transactions to prevent database deadlocks and guarantee that only one worker can claim a job at any given time.
2. **Core Logic (`core/`)**: The background worker pool. Workers poll the database, claim jobs via atomic updates, run a continuous heartbeat to maintain their lease, and securely execute tasks in a child process context (`sh -c`).
3. **CLI Interface (`cmd/`)**: Built using Cobra to provide clean routing, help texts, and detached background process launching.

---

## Usage Guide

QueueCTL provides a simple and intuitive CLI.

### 1. Enqueuing Jobs
Enqueue a JSON payload containing the command to execute.

```bash
./queuectl enqueue '{"command": "echo Hello World!"}'
./queuectl enqueue '{"command": "exit 1", "max_retries": 3}'
```

### 2. Managing Workers
Workers pull jobs from the queue and execute them.

```bash
# Start 1 worker in the foreground (blocks terminal)
./queuectl worker start --count 1

# Start 5 workers in the background (detached)
./queuectl worker start --count 5 --detach

# Stop all detached workers gracefully
./queuectl worker stop
```

### 3. Monitoring the Queue
Track your jobs globally or view system-wide stats.

```bash
# List all active/recent jobs
./queuectl list

# View high-level system metrics (includes active worker count!)
./queuectl status
```

### 4. Dead Letter Queue (DLQ)
When a job exceeds its `max_retries`, it is moved to the Dead Letter Queue.

```bash
# View dead jobs and their exact failure output
./queuectl dlq list

# Retry a specific dead job (resets attempts and moves back to pending)
./queuectl dlq retry <job_id>

# Retry all dead jobs
./queuectl dlq retry --all
```

---

## End-to-End Core Flows (Demo Scripts)

### Crash Recovery Testing
QueueCTL protects your jobs even if the host machine forcefully terminates the worker daemon. 

```bash
# 1. Enqueue a long-running job
./queuectl enqueue '{"command": "sleep 30"}'

# 2. Start a background worker
./queuectl worker start --count 1 --detach

# 3. Simulate a hard crash by forcefully killing the worker PID
kill -9 <worker_pid> 

# 4. Wait for the job's lease to expire (default 30s)
# 5. Start a new worker. It will automatically reclaim the orphaned job and finish it!
./queuectl worker start --count 1
```

### Concurrency Testing
You can flood the queue and watch multiple workers tackle the load simultaneously without conflicts.

```bash
# Enqueue 20 jobs
for i in {1..20}; do ./queuectl enqueue '{"command": "sleep 2"}'; done

# Spin up 5 background workers
./queuectl worker start --count 5 --detach

# Watch them process concurrently!
./queuectl list
```

---

## Configuration

QueueCTL reads configuration from `~/.queuectl/config.yaml` by default. You can override database path or config paths via global flags:

```bash
./queuectl --db-path ./myqueue.db --config ./config.yaml list
```
