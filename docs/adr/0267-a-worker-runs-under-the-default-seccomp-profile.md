# ADR-0267: A worker runs under the default seccomp profile

- **Status**: Implemented (2026-10-23), the private review passed (0 Blockers, 0 Majors); Reviewing 2026-10-23, every lane
  green; Accepted 2026-10-23 by the decider; Proposed 2026-10-23, judged once: accept
- **Date**: 2026-10-23
- **Deciders**: green-0-rabbit
- **Tags**: runtime, containerd, seccomp, isolation
- **Realizes**: [FEAT-0000/F12](../feat/0000-feat-v1.md) (function runtime behind the `runtime.Runtime` port)
- **Supersedes (in part)**: [ADR-0268](0268-only-the-platform-sets-a-project-id.md), only Decision 1's "default action
  allow": the filter's base becomes containerd's default profile, and ADR-0268's rules stay in it, unchanged.
- **Relates to**: [ADR-0011](0011-runtime-sandbox-port.md) (conservative OCI defaults, "default seccomp") ·
  [ADR-0264](0264-a-worker-sees-its-own-cgroup.md) (the same OCI options) · `blueprint.md` §Security and Isolation

## Context & Need

ADR-0011 and the blueprint give every worker conservative OCI defaults, "default seccomp" among them. No code applied a
seccomp profile until ADR-0268, which added a filter whose default action is allow, with four rules for a volume's
project id (`internal/runtime/containerd/containerd_linux.go`, `withProjectIDFilter`). A worker can still make every
other system call the kernel allows to its user, the rare and risky ones included.

containerd ships the profile Docker uses (`contrib/seccomp`, `DefaultProfile`): it refuses about fifty system calls a
normal program never makes, such as loading kernel modules, changing the clock or mounting filesystems, and allows the
rest.

## Scenarios

- `scenario: default-profile-refuses`: Given a containerd worker, When its code calls a system call the default profile
  refuses (`unshare(CLONE_NEWUSER)`, `mount`), Then the call fails with EPERM.
- `scenario: project-id-rules-hold`: Given a worker with a volume, Then setting a file's project id or flags still
  fails with EPERM, and reading them works (ADR-0268's probe, unchanged).
- `scenario: runtimes-unchanged`: Given the curated runtimes (node, python, the Lake and SQL engines), Then they run
  as before.

## Scope

**In**: the seccomp profile of every containerd worker. **Out**: the process runtime; a custom or per-Function
profile; capabilities; the limits of GHSA-r2h8's other half.

## Constraints & Decision drivers

- ADR-0268's rules must keep winning over the profile's own `ioctl` allow. In libseccomp an unconditional rule for a
  system call overrides that call's conditional rules: with the profile's unconditional `ioctl` allow kept, the volumes
  lane's V3 probe set a project id (measured on this branch, and in Docker with six rule orders and shapes). Disjoint
  conditional rules do not interact, so they suffice.
- No runtime-specific rules: one profile for every worker.

## Alternatives considered

| Option | For | Against | Outcome |
|---|---|---|---|
| containerd's default profile, ADR-0268's rules appended | Docker's well-trodden list; matches ADR-0011's promise | one more dependency path in the driver | chosen by the decider |
| Keep ADR-0268's default-allow filter | nothing changes | ADR-0011's "default seccomp" stays unmet | rejected |
| A funcd-written profile | tailored | a list to keep in step with kernels and runtimes | rejected |

## Decision

1. **The profile.** Every containerd worker's spec carries `seccomp.DefaultProfile(spec)` from containerd's
   `contrib/seccomp`, computed after the spec's capabilities are set, with `projectIDRules()` (ADR-0268) appended.
2. **`ioctl`.** The profile's unconditional `ioctl` allow is removed and replaced by `ioctlAllowRules()`: allow rules
   on the command's low 32 bits (`SCMP_CMP_MASKED_EQ`) that together match every command except ADR-0268's three, each
   command by exactly one rule, so no allow rule overlaps an EPERM rule.
3. **One option.** `withSeccompProfile` replaces `withProjectIDFilter` in `ociOpts`, last among the options that shape
   the process, so the profile sees the final capabilities.

## Temporary workarounds

None.

## Contracts

```go
// withSeccompProfile sets containerd's default seccomp profile with its unconditional ioctl allow replaced by
// ioctlAllowRules, then projectIDRules (ADR-0267, ADR-0268).
func withSeccompProfile(ctx context.Context, client oci.Client, c *containers.Container, s *specs.Spec) error

// ioctlAllowRules allow every ioctl command but ADR-0268's three, as disjoint masked prefixes of the low 32 bits.
func ioctlAllowRules() []specs.LinuxSyscall
```

| Consumes | Exposes |
|---|---|
| `github.com/containerd/containerd/v2/contrib/seccomp` (already in the module), the spec's capabilities | the worker's `linux.seccomp` |

## Implementation plan

1. `internal/runtime/containerd/containerd_linux.go`: `withSeccompProfile` and `ioctlAllowRules`, in place of
   `withProjectIDFilter`.
2. `internal/runtime/containerd/seccomp_linux_test.go`: `TestScenarioDefaultProfileRefuses` on a bare spec: default
   action ERRNO, `unshare` not allowed, each refused command (upper bits set or not) matched by no allow rule, other
   commands by exactly one.
3. `e2e/fixtures/projid-probe`: an `unshare(CLONE_NEWUSER)` line; `e2e/volumes.venom.yml` V3 expects `unshare EPERM`.

**Test plan**:

| Scenario | Test |
|---|---|
| `default-profile-refuses` | `TestScenarioDefaultProfileRefuses` (the spec); the volumes lane: V3 prints `unshare EPERM` |
| `project-id-rules-hold` | `just lima-example volumes`: V3 prints setxattr, setxattr-high and setflags EPERM, getxattr and getflags ok |
| `runtimes-unchanged` | `just lima-example-all` passes |

**Definition of done**: `scripts/agent/gate.sh` passes; `just lima-example-all` passes, the volumes lane included.

## Review checklist

- [ ] `ociOpts` sets `withSeccompProfile` once, after the options that change capabilities; `withProjectIDFilter` is gone.
- [ ] The profile is containerd's default with ADR-0268's four rules appended, unchanged.
- [ ] No allow rule matches `ioctl` unconditionally or matches one of ADR-0268's three commands.
- [ ] The volumes lane's V3 probe and every other lane pass.

## Consequences

- **(+)** ADR-0011's "default seccomp" holds; a worker loses the system calls a normal program never makes.
- **(−)** Code that needs a refused call (for example nested user namespaces) cannot run in a worker.
- **(−)** Each `ioctl` walks up to 72 masked comparisons in the filter, negligible beside the system call itself.
- **Risk**: a curated runtime needing a refused call fails at start; every lane runs before merge.

## Open questions

None.

## References

- ADR-0011, ADR-0268; containerd v2.4.2 `contrib/seccomp/seccomp_default.go`.
- Draft advisories GHSA-vcf2-hc48-w9jp (item 6) and GHSA-r2h8-qhjr-m3x8 (its seccomp half).
