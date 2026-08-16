# AGENTS.md — Alexandria

## Project

Alexandria is a lightweight, self-hosted personal library server for books and technical documents.

The initial goal is deliberately narrow:

* index local libraries;
* support PDF first;
* show covers, metadata, folders, search, and reading progress;
* provide a responsive web reader;
* run comfortably on small home servers and VPS machines.

Alexandria is **not** a Komga clone. Do not copy features merely because another reader has them.

The project should optimize for simplicity, predictable resource usage, and maintainability.

## Primary constraint

Alexandria must remain usable on a machine with **1 GB of RAM**.

Treat this as an architectural constraint.

* Keep idle memory low.
* Use bounded concurrency everywhere.
* Never load an entire book into memory when streaming is possible.
* Never create unbounded goroutine pools, queues, caches, or buffers.
* Heavy work must run through explicit background workers.
* Default worker counts must be conservative.
* Avoid dependencies requiring separate infrastructure.
* Prefer disk cache over large in-memory caches.

## Stack

Backend:

* Go
* HTTP router compatible with `net/http`
* SQLite
* `database/sql`
* embedded SQL migrations
* structured logging
* `context.Context` cancellation and timeouts

Frontend:

* React
* TypeScript
* Vite
* PDF.js
* responsive web UI
* production frontend embedded using `//go:embed`

Production should converge toward one Alexandria executable plus runtime configuration/data.

## Architecture

Start as a modular monolith.

Do not introduce:

* microservices;
* Redis;
* RabbitMQ;
* Kafka;
* Elasticsearch;
* separate workers;
* Postgres;
* Kubernetes.

Expected structure:

```text
cmd/alexandria/
internal/
  api/
  config/
  database/
  library/
  books/
  scanner/
  jobs/
  thumbnails/
  progress/
  search/
  filesystem/
web/
migrations/
```

## v0.1

Implement only:

1. local libraries;
2. recursive filesystem scan;
3. PDF discovery;
4. new/changed/deleted file detection;
5. title extraction;
6. page count;
7. cover generation;
8. folder navigation;
9. search;
10. PDF web reader;
11. reading progress;
12. manual rescans;
13. Docker deployment;
14. health endpoint.

Do not implement EPUB or CBZ until PDF support is stable.

## Non-goals

Do not add without an explicit scope change:

* multi-tenancy;
* SaaS mode;
* AI;
* OCR;
* full-text PDF indexing;
* recommendations;
* external metadata providers;
* Kindle/Kobo synchronization;
* OPDS;
* SSO;
* LDAP;
* webhooks;
* complex RBAC;
* cloud storage;
* pre-rendering entire libraries.

## Filesystem

The filesystem is the source of truth for book files.

Example:

```text
/books/
  Programacao/
    Go/
    Rust/
    Python/
  Filosofia/
  Literatura/
```

Preserve this hierarchy in Alexandria.

Book libraries should preferably be mounted read-only.

Alexandria writes only to its configuration and cache directories.

## SQLite

SQLite is the only database for v0.1.

Initial entities:

```text
libraries
books
reading_progress
authors
book_authors
```

Use WAL where appropriate.

Do not introduce an ORM without a concrete reason.

Never modify an already released migration. Add another migration.

## Scanner

The scanner must be incremental.

Distinguish:

```text
new
changed
unchanged
missing
```

Use path, file size and modification timestamp before expensive processing.

Do not hash every file during every scan.

Do not re-analyze unchanged books.

Scanning must never block the HTTP server.

## Jobs

Use an in-process bounded queue.

Examples:

```text
AnalyzeBook
HashBook
GenerateThumbnail
RefreshMetadata
```

Default:

```yaml
jobs:
  workers: 1
  queue_size: 100
```

Requirements:

* bounded workers;
* bounded queue;
* cancellation;
* timeouts;
* contextual errors;
* malformed PDFs cannot stop the queue;
* no infinite retries.

Higher concurrency is opt-in.

## PDF

PDF is the only required format for v0.1.

Never read an entire PDF into RAM unnecessarily.

Reader delivery should support efficient streaming and HTTP range behavior where practical.

Use PDF.js in the browser.

The server should primarily deliver the original PDF and persist reading progress.

Do not pre-render every page.

## Thumbnails

Generate thumbnails asynchronously.

Rules:

* first useful page;
* disk cache;
* efficient web format;
* no regeneration if source file is unchanged;
* renderer must run behind an adapter;
* external renderer calls require timeouts.

Cache example:

```text
/config/
  alexandria.db
  cache/
    thumbnails/
```

Deleting the cache must not destroy library state.

## API

Example:

```text
GET    /api/health

GET    /api/libraries
POST   /api/libraries
GET    /api/libraries/{id}
POST   /api/libraries/{id}/scan

GET    /api/books
GET    /api/books/{id}
GET    /api/books/{id}/file
GET    /api/books/{id}/thumbnail

GET    /api/books/{id}/progress
PUT    /api/books/{id}/progress
```

Handlers must not contain business logic.

Validate all input.

Use consistent JSON errors.

Do not expose absolute filesystem paths.

## Security

Filesystem boundaries are mandatory from the first version.

* Prevent path traversal.
* Never serve files outside configured libraries.
* Normalize filesystem paths.
* Use parameterized SQL.
* Never interpolate user input into shell commands.
* Do not log secrets.
* External commands must receive arguments safely.

Assume Alexandria may eventually be exposed through a reverse proxy.

## Performance

Measure before optimizing.

Track:

```text
idle RSS
scan RSS
scan duration
thumbnail duration
HTTP latency
queue depth
active jobs
database size
cache size
```

Do not trade large amounts of RAM for marginal latency improvements without evidence.

## Resource targets

Initial targets:

```text
Idle RAM: <150 MB
Default workers: 1
Host minimum target: 1 GB RAM
```

Normal browsing should not cause sustained memory growth.

A library scan must remain safe on a 1 GB machine.

No unbounded parallel PDF processing.

## Configuration

Example:

```yaml
server:
  address: 0.0.0.0
  port: 8080

database:
  path: /config/alexandria.db

cache:
  path: /config/cache

jobs:
  workers: 1
  queue_size: 100
```

Defaults must favor small self-hosted machines.

## Logging

Logs should contain useful operational context:

```text
library_id
book_id
relative_path
job
duration
error
```

Do not dump complete book objects or huge payloads.

Use log levels consistently.

## Testing

Prioritize tests for:

* scanner behavior;
* path traversal protection;
* incremental scanning;
* database queries;
* migrations;
* reading progress;
* bounded jobs;
* HTTP API;
* malformed files.

Tests must use temporary directories and databases.

Never require a real user library.

## Development

The repository should provide:

```bash
make dev
make test
make lint
make build
```

`make build` should produce the production binary with embedded frontend assets.

## Docker

Expected deployment:

```yaml
services:
  alexandria:
    image: alexandria:latest
    restart: unless-stopped

    ports:
      - "127.0.0.1:8080:8080"

    volumes:
      - ./config:/config
      - /home/server/documents/books:/books:ro
```

The container should:

* run non-root when practical;
* expose only the HTTP port;
* have writable `/config`;
* allow read-only libraries;
* provide a health check;
* never require privileged mode.

## Code style

Prefer straightforward Go.

* Small functions.
* Explicit errors.
* Wrap errors with useful context.
* Avoid global mutable state.
* Inject dependencies explicitly.
* Interfaces only where substitution is useful.
* No interface for every struct.
* No generic repository abstraction hiding SQL.
* Avoid reflection-heavy frameworks.
* Avoid clever concurrency.

Comments explain why, not obvious syntax.

The project should remain understandable using normal Go tooling and `grep`.

## Change discipline

Before implementing anything:

1. identify the smallest valid change;
2. check whether it expands v0.1;
3. preserve the 1 GB RAM constraint;
4. avoid new infrastructure;
5. update tests;
6. keep migrations safe;
7. run relevant tests.

Do not mix unrelated refactors with feature implementation.

## Definition of done

A task is done only when:

* code compiles;
* relevant tests pass;
* error paths are handled;
* resource usage is bounded;
* no unrelated scope was added;
* configuration/docs are updated;
* Docker still works when runtime behavior changed.

## Product principle

Alexandria exists because a personal library server should not require enterprise infrastructure or excessive resources.

When choosing between two designs, prefer the one that is:

1. simpler;
2. easier to operate;
3. lower in baseline resource usage;
4. easier to debug;
5. sufficient for the actual requirement.

Do not build hypothetical scale before real scale exists.

