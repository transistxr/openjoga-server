# openjoga-server

Self-hosted organiser mainly for sports like pickup football, five-a-side,
hiking, board games. People create groups, schedule games, RSVP, get told when a
game is on or off, and see what each session costs them.


## Running it

```sh
go build ./cmd/openjoga
OPENJOGA_DEV=1 ./openjoga
```

## Configuration

Everything is an environment variable.

| Variable | Default | |
|---|---|---|
| `OPENJOGA_DEV` | off | Relaxes secret requirements, prints email to stdout, allows insecure cookies |
| `OPENJOGA_LISTEN` | `:8080` | Listen address |
| `OPENJOGA_BASE_URL` | `http://localhost:8080` | Public origin — used to build links that get emailed and shared |
| `OPENJOGA_WEB_ORIGIN` | `http://localhost:5173` | Where `openjoga-web` is served from, for CORS and CSRF |
| `OPENJOGA_DB` | `openjoga.db` | SQLite file |
| `OPENJOGA_CODE_PEPPER` | — | **Required in production.** Keys the HMAC over sign-in codes |
| `OPENJOGA_TRUST_PROXY` | off | Believe `X-Forwarded-For` for rate limiting. Only with a proxy in front |
| `OPENJOGA_CURRENCY` | `GBP` | |
| `OPENJOGA_SMTP_HOST` / `_PORT` / `_USER` / `_PASS` | — | Any transactional provider |
| `OPENJOGA_EMAIL_FROM` | `openjoga@localhost` | |
| `OPENJOGA_VAPID_PUBLIC` / `_PRIVATE` | — | Web push keys, from `./openjoga vapid` |
| `OPENJOGA_VAPID_SUBJECT` | `mailto:admin@localhost` | Contact address for push services |

### The pepper

`OPENJOGA_CODE_PEPPER` must be set in production and must not change — changing
it invalidates every sign-in code in flight.

```sh
OPENJOGA_CODE_PEPPER=$(head -c 32 /dev/urandom | base64)
```

### Web push

Push is the only channel that reaches someone who RSVP'd with
just a name and gave no contact details.


```sh
./openjoga vapid   # prints the two env vars to set
```

## Development

```sh
go test ./...           # everything
go test ./internal/...  # skip the compose root
go vet ./...
gofmt -l .
```

### Layout

```
cmd/openjoga/       compose root: config, wiring, graceful shutdown
internal/
  config/           env loading
  store/            the only package that talks to SQLite
    migrations/     embedded .sql, applied by goose on boot
  service/          domain rules: cost, RSVP, events, invites, members
  auth/             sessions, email challenges, merge, access tiers
  notify/           email, web push, in-app
  worker/           job queue, cutoff and completion sweeps
  http/             router, middleware, DTOs, handlers
  id/               random slugs, hashes, HMAC'd codes
```
