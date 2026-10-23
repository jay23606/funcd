# ADR-0269: A provider engine reaches only its one internal endpoint

- **Status**: Implemented (2026-10-23), the private implementation review passed (DoD 5/5); Reviewing 2026-10-23,
  every lane green; Accepted 2026-10-23 by the decider; Proposed 2026-10-23, judged
  twice: round 1 two Majors, round 2 accept
- **Date**: 2026-10-23
- **Deciders**: green-0-rabbit
- **Tags**: network, egress, lake, sql, containerd, security
- **Realizes**: [FEAT-0007/F80](../feat/0007-feat-egress-control.md) (worker network isolation)
- **Relates to**: [ADR-0115](0115-worker-network-isolation.md) (the opt-in host substrate) ·
  [ADR-0117](0117-egress-policy-enforcement.md) (the egress PEP) · [ADR-0087](0087-add-on-provider-runtime.md) (the
  engines' worker) · [ADR-0085](0085-s3-in-platform-identity-funcd-keypair.md) (the engine's key pair) ·
  [ADR-0263](0263-the-lake-engine-runs-duckdb-2.md) (the engine's confinement)

## Context & Need

A Lake engine runs the SQL its callers send (`lake::query`). ADR-0263 confines what a query reads, but a query can
still create a temporary DuckDB secret: an S3 secret with another endpoint, or an HTTP secret with a proxy. The
engine then sends its S3 requests, signed with its own key pair, to a host the query chose.

Worker egress is open unless an operator turns on `server.network.egress` (ADR-0115, ADR-0117), which is off by
default. The Lake engine needs one outbound destination: funcd's S3 gateway, at the worker-reachable endpoint the
Lake reconciler injects (`AWS_ENDPOINT_URL`). The SQL engine (ADR-0232) has no query-controlled egress, but it is a
provider engine too and dials one destination, its change feed (`FUNCD_SQL_CDC_ENDPOINT`). Callers reach both engines
inbound.

## Scenarios

- `scenario: engine-reaches-gateway`: Given a Lake on containerd, When its engine reads and writes its catalog and
  Parquet files, Then they go through the S3 gateway as before.
- `scenario: query-cannot-redirect`: Given a Lake, When a query creates an S3 secret whose endpoint is another
  address, or an HTTP secret with a proxy, and reads through it, Then the engine opens no connection to that address
  and the query fails.
- `scenario: switch-does-not-matter`: Given `server.network.egress` on or off, Then the engine's outbound
  connections are limited the same way.
- `scenario: callers-still-reach-engine`: Given a fenced engine, Then consumers' queries and the readiness probe
  still reach it.
- `scenario: sql-engine-fenced`: Given a SQLDatabase, Then its engine opens connections only to its change feed.
- `scenario: hostname-gateway-refused`: Given an S3 gateway endpoint named by a hostname, When a Lake reconciles,
  Then it is not Ready with reason `GatewayEndpointNotAnAddress` and no engine starts unfenced.

## Scope

**In**: the Lake and SQL engines' workers on the containerd runtime; a fence field on the worker and provider specs.
**Out**: Functions (ADR-0117 governs them); the process runtime, which has no netns; rotating the engine's key pair;
DNS for the engines (a fence holds addresses only).

## Constraints & Decision drivers

- Fail closed: no window between the engine's process start and its fence.
- Independent of `server.network.egress`, as the decider chose (2026-10-23, option B).
- The engine must not be able to remove the fence.

## Alternatives considered

| Option | For | Against | Outcome |
|---|---|---|---|
| nftables output chain in the engine's own netns, written before the process starts | no race; dies with the netns; no host-wide state; the engine has no CAP_NET_ADMIN | one nftables connection per engine start | chosen |
| A host set of engine IPs, synced from the runtime | one table | a window between start and sync; host state to clean up | rejected |
| Turn on ADR-0117 for engines only | reuses the PEP | the PEP needs the host substrate and an EgressPolicy; a grant could open it | rejected |
| An upstream DuckDB setting that refuses secret creation | fixes it at the source | none exists in 2.0.0 | rejected |

## Decision

1. **The fence field.** `runtime.WorkerSpec` and `provider.ProviderSpec` gain `EgressOnly []netip.AddrPort`. nil
   means no fence; a non-nil value, empty included, means the worker opens outbound TCP only to these endpoints.
2. **The engines set it.** `provider.EgressTo(url)` turns an engine's one destination URL into its fence (the scheme's
   port when the URL names none; an empty URL gives an empty fence) and reports a host that is not an IP literal. The
   Lake reconciler fences its engine to the S3 gateway endpoint; for a hostname it holds the Lake not Ready with reason
   `GatewayEndpointNotAnAddress`. The SQL reconciler fences its engine to the change-feed endpoint, funcd's own
   address. The provider runtime copies the field to the worker spec.
3. **The containerd driver enforces it.** After the CNI setup and before the task starts, the driver writes an `inet`
   table `funcd_fence` in the worker's netns with one `output` filter chain, policy drop: accept on loopback, accept
   `ct state established,related`, accept TCP to each `EgressOnly` endpoint. A failure to write it fails `Create`, as a
   CNI failure does. The process driver ignores the field.
4. **Not the switch.** The fence does not read `server.network.egress`; with egress on, ADR-0117's rules still apply
   on the host and the fence applies inside.

## Temporary workarounds

None.

## Contracts

```go
// internal/runtime: WorkerSpec gains
EgressOnly []netip.AddrPort // nil: no fence; non-nil: outbound TCP only to these (containerd, ADR-0269)

// internal/provider: ProviderSpec gains
EgressOnly []netip.AddrPort // copied to the engine's WorkerSpec (ADR-0269)

// EgressTo is the fence of an engine that dials only rawURL; ok is false when the host is not an IP literal.
func EgressTo(rawURL string) (fence []netip.AddrPort, ok bool)

// internal/network
// FenceNetns writes the funcd_fence table into the netns at netnsPath: outbound TCP only to allow (ADR-0269).
func FenceNetns(netnsPath string, allow []netip.AddrPort) error
```

| Consumes | Exposes |
|---|---|
| the worker's netns path (after CNI setup), `github.com/google/nftables` (already a dependency) | the `funcd_fence` table in the engine's netns |

## Implementation plan

1. `internal/runtime/runtime.go`, `internal/provider/spec.go`: the fields and `EgressTo`; the provider runtime copies
   the field.
2. `internal/services/lake/reconcile.go`, `internal/services/sql/reconcile.go`: set `EgressOnly`.
3. `internal/network/fence_linux.go`: `FenceNetns`, called by the containerd driver's `Create` right after the CNI
   setup.
4. `e2e/fixtures/fence-probe.sh` and a duckdb-lane step: an HTTP listener on the VM's own address, a query that
   creates an S3 secret pointing at it and reads through it; the listener sees no connection.

**Test plan**:

| Scenario | Test |
|---|---|
| `engine-reaches-gateway` | the duckdb lane, unchanged steps |
| `query-cannot-redirect` | the duckdb lane's new step; `TestScenarioFenceRefusesOtherDestinations` (the rule shape; Linux tests run unprivileged, so the packet effect is the lane's) |
| `switch-does-not-matter` | `TestScenarioLakeSetsEgressOnly` (the reconciler sets the field whatever the switch); the fence reads no config. No egress-on lane carries a Lake: that run is a gap, noted in Consequences |
| `callers-still-reach-engine` | the duckdb and sql lanes, unchanged steps |
| `sql-engine-fenced` | `TestEngineFencedToChangeFeed`; the sql lane's change-feed steps |
| `hostname-gateway-refused` | `TestScenarioLakeSetsEgressOnly`; `TestEgressTo` |

**Definition of done**: `scripts/agent/gate.sh` passes; `just lima-example-all` passes.

## Review checklist

- [ ] The fence is written before `task.Start` and a failure fails `Create`.
- [ ] The chain's policy is drop, with only loopback, established and the `EgressOnly` endpoints accepted.
- [ ] The Lake and SQL reconcilers set `EgressOnly` non-nil in every case; a hostname gateway holds the Lake not Ready.
- [ ] No code reads `server.network.egress` on this path.

## Consequences

- **(+)** A query cannot send the engine's signed requests anywhere but the gateway, whatever the switch says.
- **(−)** An engine feature that needs another destination (a remote extension download, a second store) needs this
  list extended by an ADR.
- **(−)** An operator whose `s3Gateway.endpoint` is a hostname (a TLS front) gets Lakes that are not Ready until it
  names an address.
- **(−)** `funcdctl dev`'s Lake engine runs on the process runtime, unfenced: a query there can still redirect it, so
  dev data must not be sensitive.
- **Gap**: no lane runs a Lake with `server.network.egress` on; the host's InternalAllow and the fence are expected to
  agree (both pass the gateway endpoint) but no run checks it.
- **Risk**: the provider runtime adopts a running engine whatever its spec, so a changed fence reaches only a new
  engine. The gateway endpoint changes only with daemon config, and the containerd boot sweep (ADR-0167) re-creates
  every engine at each start, so a running engine always carries the current fence.

## Open questions

None.

## References

- ADR-0115, ADR-0117, ADR-0263; google/nftables `WithNetNSFd`.
- Draft advisory GHSA-vcf2-hc48-w9jp, route (3).
