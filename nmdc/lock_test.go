package nmdc

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLock(t *testing.T) {
	lock := Lock{
		Lock: "EXTENDEDPROTOCOL_verlihub",
		PK:   "version0.9.8e-r2",
	}
	key := lock.Key()
	exp, _ := hex.DecodeString("75d1c011b0a010104120d1b1b1c0c03031923171e15010d171")
	require.Equal(t, string(exp), key.Key)
}
