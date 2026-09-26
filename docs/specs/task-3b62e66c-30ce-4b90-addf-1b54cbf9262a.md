# Web Push foundation: internal/push

Kukátko has no push notifications at all today. This task builds the foundation only: the place browser push subscriptions are stored, and the code that encrypts and sends one Web Push message. Nothing calls it yet — the delivery job, the HTTP surface and the frontend are separate tasks.

Mirror `internal/mailer`: it is the only way to send an e-mail, it exposes a `Sender` interface, it has a real SMTP implementation, a no-op for when mail is off and a socket-free fake for tests. `internal/push` is that same shape for Web Push.

## Requirements

- New Go package `internal/push`.
- Sending uses the standard Web Push protocol with VAPID (Voluntary Application Server Identification). Use `github.com/SherClockHolmes/webpush-go` at v1.4.0 — pure Go, its only dependencies are `github.com/golang-jwt/jwt/v5` and `golang.org/x/crypto`, so it builds with `CGO_ENABLED=0` as this project requires. Do **not** add an FCM/Firebase dependency and do **not** add a `gcm_sender_id` to the web manifest: VAPID alone is what every current browser needs, Chrome on Android included.
- A `Sender` interface with one method that delivers one notification payload to one subscription. Three implementations: the real VAPID sender, a no-op used when push is disabled instance-wide, and a network-free fake for tests (follow `internal/mailer/fake.go`).
- The notification payload is a small typed struct — title, body, a deeplink path, a kind, and a collapse tag — marshalled to JSON. The service worker in the frontend is its only reader, so keep the field names stable and obvious.
- Config keys under `push.` in `internal/config`: `push.enabled` (default false), `push.vapid.public_key`, `push.vapid.private_key`, `push.vapid.subject` (a `mailto:` or `https:` URL identifying the sender, which VAPID requires). Env override as usual (`KUKATKO_PUSH_ENABLED`, `KUKATKO_PUSH_VAPID_PRIVATE_KEY`, …).
- **Push disabled must be completely inert**, exactly as mail is: with `push.enabled` false the no-op sender is wired and nothing is ever dialled, and validation must not demand keys. Conversely, push enabled with a missing or malformed key pair must fail at startup rather than silently dropping every notification — a sender that looks configured and quietly discards messages is the worst of the three states.
- A migration adding a `push_subscriptions` table: one row per browser/device, holding the account it belongs to (foreign key to `users`, `ON DELETE CASCADE`), the push endpoint URL (**unique** — the same browser re-subscribing must update its row, not add a second), the two client keys the payload encryption needs (`p256dh` and `auth`), an optional user-agent string so the person can later recognise which device this is, created and last-used timestamps, and failure bookkeeping (a count and the time of the last failure).
- A store over pgx in the package: upsert a subscription by endpoint, list one account's subscriptions, delete by endpoint, delete by id, record a successful send, record a failure, and delete every subscription of an account.
- **Classify the new table in `internal/reset/tables.go`.** A migration that adds a table which is not classified there fails the integration tests in `internal/reset`, and it fails late — do it in this same commit. A push subscription is account data and not library data: it belongs with the password and the profile, not with the photos, so the guarded library wipe should keep it. State that reasoning in the file's own terms.
- A CLI subcommand under the existing Cobra tree in `cmd/kukatko` that generates and prints a fresh VAPID key pair, so an operator can mint one without writing a Go program. It prints both keys plus a reminder that the private key belongs in the environment and never in a committed file.

## Edge cases

- A subscription whose endpoint the push service rejects as gone (HTTP 404 or 410) is dead forever. The sender must report that **distinctly** from a transient failure, so its caller can delete the row. Do not delete from inside the sender — reporting is the sender's job, the lifecycle belongs to the caller.
- A 429 or a 5xx from the push service is transient and must be reported as retryable.
- The encrypted payload has a hard size limit of roughly 4 KB. Refuse an oversized payload with a typed error of your own rather than letting the push service reject it, so the caller can treat it as a permanent failure instead of retrying forever.

## Documentation

`docs/PACKAGES.md` for the new package, one line in the `## Package map` in `CLAUDE.md`, the four config keys in `docs/OPERATIONS.md` **and** in `config.example.yaml`, and the new CLI subcommand in `docs/OPERATIONS.md`.