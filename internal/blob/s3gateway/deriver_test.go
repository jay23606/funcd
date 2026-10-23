package s3gateway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	v1 "github.com/pyvvo/funcd/api/types/v1alpha1"
)

// rotating is a 1h-period Deriver whose clock the test moves.
func rotating(salt string, now *time.Time) Deriver {
	return Deriver{Master: []byte("master"), Salt: []byte(salt), Period: time.Hour, Now: func() time.Time { return *now }}
}

// scenario: pair-expires — a pair derived in window w is accepted in w and w+1 and refused from w+2 (ADR-0270).
func TestScenarioPairExpires(t *testing.T) {
	now := time.Unix(10*3600+1800, 0)
	d := rotating("salt", &now)
	kp := d.Keypair(v1.KindLake, "default", "lake")
	kind, ns, name, secret, ok := d.Secret(kp.AccessKey)
	require.True(t, ok)
	require.Equal(t, v1.KindLake, kind)
	require.Equal(t, "default", ns)
	require.Equal(t, "lake", name)
	require.Equal(t, kp.SecretKey, secret)

	now = now.Add(time.Hour)
	_, _, _, secret, ok = d.Secret(kp.AccessKey)
	require.True(t, ok, "the previous window is still accepted")
	require.Equal(t, kp.SecretKey, secret)
	require.NotEqual(t, kp, d.Keypair(v1.KindLake, "default", "lake"), "a new window issues a new pair")

	now = now.Add(time.Hour)
	_, _, _, _, ok = d.Secret(kp.AccessKey)
	require.False(t, ok, "two windows later the pair is refused")

	now = time.Unix(10*3600+1800, 0).Add(-time.Hour)
	_, _, _, _, ok = d.Secret(kp.AccessKey)
	require.False(t, ok, "a future window is refused")
}

// scenario: restart-rotates — a new salt, as at a restart, derives another secret for the same window (ADR-0270).
func TestScenarioRestartRotates(t *testing.T) {
	now := time.Unix(10*3600, 0)
	before, after := rotating("first", &now), rotating("second", &now)
	kp := before.Keypair(v1.KindFunction, "default", "fn")
	_, _, _, secret, ok := after.Secret(kp.AccessKey)
	require.True(t, ok, "the window still decodes")
	require.NotEqual(t, kp.SecretKey, secret, "the old secret no longer signs")
}

// scenario: old-format-refused — a rotating Deriver refuses a three-part key, and a fixed one a four-part key, so the
// two modes never cross-accept (ADR-0270).
func TestScenarioOldFormatRefused(t *testing.T) {
	now := time.Unix(10*3600, 0)
	rot, fixed := rotating("salt", &now), Deriver{Master: []byte("master")}
	_, _, _, _, ok := rot.Secret(fixed.Keypair(v1.KindLake, "default", "lake").AccessKey)
	require.False(t, ok)
	_, _, _, _, ok = fixed.Secret(rot.Keypair(v1.KindLake, "default", "lake").AccessKey)
	require.False(t, ok)
	for _, w := range []string{"", "-1", "07", "x"} {
		access := accessKeyPrefix + accessEncoding.EncodeToString([]byte("L\x00default\x00lake\x00"+w))
		_, _, _, _, ok = rot.Secret(access)
		require.False(t, ok, "window %q", w)
	}
}

// scenario: dev-pair-fixed — a zero Period gives DeriveKeypair's pair at any time (ADR-0270).
func TestScenarioDevPairFixed(t *testing.T) {
	d := Deriver{Master: []byte("master")}
	require.Equal(t, DeriveKeypair([]byte("master"), v1.KindFunction, "default", "fn"), d.Keypair(v1.KindFunction, "default", "fn"))
	require.Zero(t, d.RenewalAge(v1.KindFunction, "default", "fn"))
}

// The renewal age stays within 80% to 90% of the period and differs between identities (ADR-0270 Decision 6).
func TestRenewalAgeSpread(t *testing.T) {
	now := time.Unix(0, 0)
	d := rotating("salt", &now)
	seen := map[time.Duration]bool{}
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		age := d.RenewalAge(v1.KindLake, "default", name)
		require.GreaterOrEqual(t, age, 48*time.Minute)
		require.Less(t, age, 54*time.Minute)
		seen[age] = true
	}
	require.Greater(t, len(seen), 1)
}
