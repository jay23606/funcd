# ADR-0270: Derived S3 key pairs rotate every period and at every start

- **Status**: Implemented (2026-10-23), the private implementation review passed after six rounds; Reviewing
  2026-10-23; Accepted 2026-10-23 by the decider; Proposed 2026-10-23, judged twice: round 1 one Major (pools rebuilt at
  every window), round 2 accept
- **Date**: 2026-10-23
- **Deciders**: green-0-rabbit
- **Tags**: s3, identity, security, function, lake
- **Realizes**: [FEAT-0003/F47](../feat/0003-feat-data-platform.md) (in-platform S3 identity)
- **Supersedes (in part)**: [ADR-0085](0085-s3-in-platform-identity-funcd-keypair.md), only its "deterministic across
  restart, never rotated" premise and scenario: a derived pair now holds a time window and a per-start salt.
  [ADR-0175](0175-engine-is-its-own-principal.md)'s key format gains the window.
- **Relates to**: [ADR-0143](0143-redeploy-by-revision-switch.md) (retiring a worker) · [ADR-0087](0087-add-on-provider-runtime.md)
  (the engine's worker) · [ADR-0167](0167-process-worker-crash-recovery.md) (no worker outlives a restart)

## Context & Need

funcd derives each Function's and each Lake engine's S3 key pair from its identity and a node master
(`internal/blob/s3gateway/iam.go:70`), and the S3 gateway recomputes the secret per request. A pair never changes, so a
pair that leaked once, for example through the query route of advisory GHSA-vcf2-hc48-w9jp, works until the master is
replaced, and replacing the master also changes the Lake proxy tokens, the SQL engine passwords and the change-feed
tokens.

Cloud platforms give a workload short-lived credentials that expire on their own (AWS STS role credentials, Azure
Managed Identity tokens, GCP metadata tokens). This ADR brings expiry to the derived pair with no stored state; an
endpoint that refreshes credentials in a running worker is a later design.

## Scenarios

- `scenario: pair-expires`: Given a pair derived in window `w`, When window `w+2` begins, Then the gateway refuses it
  with 403 InvalidAccessKeyId.
- `scenario: restart-rotates`: Given a pair issued before a restart, When funcd starts again, Then the gateway refuses
  it, and every worker carries a new one.
- `scenario: workers-renewed`: Given a Lake engine or a Function worker older than its renewal age, Then funcd replaces
  it with a worker carrying a fresh pair before the old pair expires.
- `scenario: old-format-refused`: Given an access key in the format before this ADR, Then the gateway refuses it.
- `scenario: dev-pair-fixed`: Given `funcdctl dev`, Then the pair it prints stays valid across dev restarts.

## Scope

**In**: the derived S3 pairs of Functions and Lake engines; the renewal of the workers that carry them; one config key.
**Out**: Identity-issued keys ([ADR-0135](0135-managed-identity.md), stored and rotatable already); the Lake proxy tokens, SQL passwords and
change-feed tokens; an endpoint that refreshes a running worker's credentials (a Backlog card).

## Constraints & Decision drivers

- No stored per-identity state: the gateway still derives, never looks up.
- A restart invalidates every earlier pair, so the upgrade that ships a fix also ends earlier leaks.
- A worker never holds an expired pair while it serves.

## Alternatives considered

| Option | For | Against | Outcome |
|---|---|---|---|
| A time window plus a per-start salt in the derivation, workers renewed by replacement | stateless; bounded leak life; small | a Lake engine restarts once per period | chosen by the decider (2026-10-23) |
| A per-start salt only | smallest | a leak lives until the next restart, however late | rejected: the decider asked for a period |
| A per-identity generation counter | on-demand rotation | stored state the gateway reads per request | rejected |
| An IMDS-like endpoint that refreshes credentials | no restarts, the cloud model | a new endpoint and refresh in DuckDB, unverified | deferred to a Backlog card |
| Replace the master | no code | rotates every derived secret at once | rejected |

## Decision

1. **The period.** `s3gateway.keyRotationPeriod` (duration, env `FUNCD_S3GATEWAY_KEY_ROTATION_PERIOD`), default `24h`,
   at least `1h`; a smaller value fails the config check.
2. **The salt.** At each start funcd draws 32 random bytes from `crypto/rand` and keeps them in memory only.
3. **The derivation.** With `w = floor(unix seconds / period)`:
   `access = "FUNCD" + base32(kindCode \0 ns \0 name \0 w)` and
   `secret = HMAC-SHA256(master, "s3:" + kind + ":" + ns + "/" + name + ":" + w + ":" + hex(salt))`, base64 as today.
4. **The check.** An access key decodes into exactly four parts (kind, namespace, name, window); the window is a
   non-negative integer. The gateway accepts it only if `w` is the current or the previous window, deriving the
   secret for that `w`; a future window, an older one and a key of three parts are refused (403). The principal stays
   (kind, namespace, name). A pair derived at time `t` is valid at least until `t + period` and at most `t + 2·period`.
5. **A pair is issued with its worker.** A worker's pairs are derived when the worker is created or restarted, never
   on a later pass, so its `CreatedAt` is their issue time: a solo Function worker and a Lake engine already work so;
   a pool derives its members' pairs only when it creates or restarts its worker, and they stay out of the manifest
   signature (`manifestSignature`), so a new window never makes a pool look changed.
6. **Renewal.** Each worker gets a renewal age between 80% and 90% of the period, fixed by a hash of its identity
   (a pool worker: its pool key, `poolInstanceName`), so the workers of one boot do not renew together; before it its pair cannot expire (the 1h minimum leaves 6 minutes of
   margin). A worker is replaced from its `CreatedAt`:
   - a Lake engine: `provider.ProviderSpec.MaxAge`, set by the Lake to that age, makes `Converge` retire and re-create
     an older instance (the engine is not Ready for its restart);
   - a Function worker, solo or pool: retired through ADR-0143's drain once idle after half the period, and drained
     at its renewal age whether idle or not, so in-flight calls finish on the old pair; the next call starts a worker
     with a fresh pair.
7. **Dev keeps a fixed pair.** `funcdctl dev`'s platform keeps today's derivation (no window, no salt) through the
   programmatic S3 gateway option; the config file cannot turn rotation off.

## Temporary workarounds

None.

## Contracts

```go
// internal/blob/s3gateway
// Deriver derives and checks in-platform key pairs (ADR-0270). A zero Period means the fixed derivation (dev only).
type Deriver struct {
	Master []byte
	Salt   []byte
	Period time.Duration
	Now    func() time.Time
}
func (d Deriver) Keypair(kind v1.Kind, ns, name string) Keypair
// Secret decodes access and derives its secret. With a Period it accepts only a four-part key whose window is the
// current or the previous one; with a zero Period only a three-part key. ok is false otherwise.
func (d Deriver) Secret(access string) (kind v1.Kind, ns, name, secret string, ok bool)
// RenewalAge is the worker age, between 80% and 90% of Period, at which the identity's worker is replaced.
func (d Deriver) RenewalAge(kind v1.Kind, ns, name string) time.Duration

// internal/provider: ProviderSpec gains
MaxAge time.Duration // 0: never replaced for age (ADR-0270)
```

| Consumes | Exposes |
|---|---|
| `s3gateway.keyRotationPeriod`, the master secret file, `crypto/rand` | the window-bearing access key; worker replacement by age |

## Implementation plan

1. `internal/blob/s3gateway/iam.go`, `auth.go`: `Deriver`; `GetUserAccount` and the principal decoding use it.
2. `internal/platform/config/config.go`: the key, its default and its minimum.
3. `pkg/funcd/funcd.go`: build one `Deriver` per start (salt, period, clock); `cmd/funcdctl/dev.go` keeps the fixed
   mode.
4. `internal/provider/runtime.go`: `MaxAge` in `Converge`; `internal/services/lake/reconcile.go` sets it.
5. `internal/function/function.go`, `pool.go`: the renewal rule in the supervision pass; `memberEnv`'s pairs move
   out of `manifestSignature` and into the pool's create and restart.
6. Tests that call `DeriveKeypair` with the master get the platform's `Deriver` instead (a test seam on `Platform`):
   `pkg/funcd` (`pool_member_e2e`, `site_e2e`, `own_principal_internal`, `s3gateway_internal`),
   `internal/blob/s3gateway` (`harness`, `identity`, `managed_identity`, `own_principal`, `iam_unit`),
   `internal/function/s3env_internal`, `internal/lake/gateway/token`, `internal/backup/escrow`,
   `cmd/funcdctl/dev`.

**Test plan**:

| Scenario | Test |
|---|---|
| `pair-expires` | `TestScenarioPairExpires` (a fake clock across three windows) |
| `restart-rotates` | `TestScenarioRestartRotates` (two Derivers, two salts); a pkg/funcd e2e test that restarts a platform and gets 403 on the old pair |
| `workers-renewed` | `TestScenarioEngineReplacedAtMaxAge` (provider); `TestScenarioWorkerRenewed` (function, solo and pool, idle and busy); `TestScenarioPoolKeepsItsWorkerAcrossAWindow` (a pool across a window boundary keeps its worker and signature until its renewal age) |
| `old-format-refused` | `TestScenarioOldFormatRefused` |
| `dev-pair-fixed` | `TestScenarioDevPairFixed` (funcdctl dev's printed pair against its gateway) |

**Definition of done**: `scripts/agent/gate.sh` passes; `just lima-example-all` passes.

## Review checklist

- [ ] The salt is random per start and never written anywhere.
- [ ] The gateway accepts only the current and previous windows, and no key without a window outside dev.
- [ ] Every worker carrying a derived pair is replaced at its renewal age (80–90% of the period from `CreatedAt`).
- [ ] No pass derives a pair for a running worker; `manifestSignature` holds no pair.
- [ ] `keyRotationPeriod` below `1h` fails the config check.

## Consequences

- **(+)** A leaked pair works for at most two periods, and not past the next restart.
- **(−)** A Lake engine restarts about once a day (a few seconds not Ready; consumer connections drop); the Lakes of
  one node renew within the same tenth of the period, spread by the identity hash.
- **(−)** A busy Function worker is recycled once per period, adding one cold start.
- **(−)** Nothing outside funcd may keep a derived pair; none does today (in-platform only).
- **Risk**: a large clock jump on the host ends pairs early; workers are renewed on the next pass.
- **Risk**: a call still draining on the old pair when its margin ends (at least 10% of the period: 6 minutes at the
  1h minimum, 2.4 hours at the default) fails at the gateway; an operator who lowers the period keeps its tenth above
  the longest call its Functions serve.

## Open questions

None.

## References

- ADR-0085, ADR-0175, ADR-0143, ADR-0167; AWS STS role credentials, Azure Managed Identity, GCP metadata tokens (the
  expiry model).
- Draft advisory GHSA-vcf2-hc48-w9jp (rotation of the derived pair).
