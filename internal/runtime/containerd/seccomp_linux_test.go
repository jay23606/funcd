package containerd

import (
	"slices"
	"syscall"
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// TestScenarioHighBitsDoNotPass: the ioctl rule compares the command under the 32-bit mask the kernel applies, so
// FS_IOC_FSSETXATTR with its upper bits set still matches (ADR-0268). The rules close containerd's default profile
// (ADR-0267).
// scenario: high-bits-do-not-pass
func TestScenarioHighBitsDoNotPass(t *testing.T) {
	s := &specs.Spec{Process: &specs.Process{Capabilities: &specs.LinuxCapabilities{}}}
	if err := withSeccompProfile(t.Context(), nil, nil, s); err != nil {
		t.Fatal(err)
	}
	all := s.Linux.Seccomp.Syscalls
	sc := &specs.LinuxSeccomp{Syscalls: all[len(all)-4:]}
	want := []uint64{0x401C5820, 0x40086602, 0x40046602}
	for i, cmd := range want {
		r := sc.Syscalls[i]
		if len(r.Names) != 1 || r.Names[0] != "ioctl" || r.Action != specs.ActErrno || r.ErrnoRet == nil || *r.ErrnoRet != uint(syscall.EPERM) {
			t.Fatalf("rule %d = %+v, want ioctl EPERM", i, r)
		}
		a := r.Args
		if len(a) != 1 || a[0].Index != 1 || a[0].Op != specs.OpMaskedEqual || a[0].Value != 0xFFFFFFFF || a[0].ValueTwo != cmd {
			t.Fatalf("rule %d args = %+v, want arg 1 masked with 0xFFFFFFFF equal to %#x", i, a, cmd)
		}
		for _, hi := range []uint64{0, 0xFFFFFFFF00000000, 0x0000000100000000} {
			if (cmd|hi)&a[0].Value != a[0].ValueTwo {
				t.Fatalf("command %#x escapes rule %d", cmd|hi, i)
			}
		}
	}
	if fs := sc.Syscalls[3]; len(fs.Names) != 1 || fs.Names[0] != "file_setattr" || fs.Action != specs.ActErrno {
		t.Fatalf("file_setattr rule = %+v", fs)
	}
}

// TestScenarioDefaultProfileRefuses: a worker's profile is containerd's default, which refuses what it does not list,
// with its unconditional ioctl allow replaced by allow rules that match every command but ADR-0268's, exactly once
// (ADR-0267). libseccomp lets an overlapping allow win over the EPERM rules, so a refused command matches none.
// scenario: default-profile-refuses
func TestScenarioDefaultProfileRefuses(t *testing.T) {
	s := &specs.Spec{Process: &specs.Process{Capabilities: &specs.LinuxCapabilities{}}}
	if err := withSeccompProfile(t.Context(), nil, nil, s); err != nil {
		t.Fatal(err)
	}
	sc := s.Linux.Seccomp
	if sc == nil || sc.DefaultAction != specs.ActErrno || len(sc.Architectures) == 0 {
		t.Fatalf("profile = %+v, want containerd's default: default action ERRNO and its architectures", sc)
	}
	allows := func(cmd uint64) int {
		n := 0
		for _, r := range sc.Syscalls {
			if r.Action != specs.ActAllow || !slices.Contains(r.Names, "ioctl") {
				continue
			}
			if len(r.Args) == 0 || cmd&r.Args[0].Value == r.Args[0].ValueTwo {
				n++
			}
		}
		return n
	}
	for _, r := range sc.Syscalls {
		if r.Action == specs.ActAllow && len(r.Args) == 0 && slices.Contains(r.Names, "unshare") {
			t.Fatalf("unshare allowed by %+v; want it left to the default action", r)
		}
	}
	for _, hi := range []uint64{0, 0xFFFFFFFF00000000, 0x0000000100000000} {
		for _, cmd := range []uint64{0x401C5820, 0x40086602, 0x40046602} {
			if n := allows(cmd | hi); n != 0 {
				t.Fatalf("refused command %#x matches %d allow rules, want 0", cmd|hi, n)
			}
		}
		for _, cmd := range []uint64{0x5401, 0x80086601, 0x801C581F, 0x401C5821, 0x40086603, 0, 0xFFFFFFFF} {
			if n := allows(cmd | hi); n != 1 {
				t.Fatalf("command %#x matches %d allow rules, want exactly 1", cmd|hi, n)
			}
		}
	}
}
