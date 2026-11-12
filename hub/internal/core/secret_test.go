package core_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rayakame/plsremind/hub/internal/core"
)

func TestTokenShape(t *testing.T) {
	plain, hash := core.NewToken(core.PrefixPersonal)
	require.True(t, strings.HasPrefix(plain, "plsr_pat_"))
	require.Len(t, strings.TrimPrefix(plain, "plsr_pat_"), 43)
	require.Equal(t, hash, core.HashToken(plain))
	other, _ := core.NewToken(core.PrefixPersonal)
	require.NotEqual(t, plain, other)
}

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := core.HashPassword("correct horse battery")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$"))
	require.True(t, core.VerifyPassword(hash, "correct horse battery"))
	require.False(t, core.VerifyPassword(hash, "wrong"))
	require.False(t, core.VerifyPassword("garbage", "correct horse battery"))
}

func TestBoxRoundTrip(t *testing.T) {
	box := core.NewBox([32]byte{7})
	sealed := box.Seal([]byte("client secret"))
	plain, err := box.Open(sealed)
	require.NoError(t, err)
	require.Equal(t, "client secret", string(plain))
	require.NotEqual(t, sealed, box.Seal([]byte("client secret")))

	sealed[len(sealed)-1] ^= 1
	_, err = box.Open(sealed)
	require.Error(t, err)
	_, err = box.Open([]byte("short"))
	require.Error(t, err)
}
