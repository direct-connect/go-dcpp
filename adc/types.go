package adc

import (
	"github.com/direct-connect/go-dcpp/adc/types"
	"github.com/direct-connect/go-dcpp/tiger"
)

var (
	_ Marshaler   = SID{}
	_ Unmarshaler = (*SID)(nil)
)

type SID = types.SID

var (
	_ Marshaler   = CID{}
	_ Unmarshaler = (*CID)(nil)
)

type CID = types.CID

var (
	_ Marshaler   = PID{}
	_ Unmarshaler = (*PID)(nil)
)

type PID = CID

var (
	_ Marshaler   = TTH{}
	_ Unmarshaler = (*TTH)(nil)
)

// TTH is a Tiger Tree Hash value.
type TTH = tiger.Hash
