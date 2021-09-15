package logpack

import (
	"encoding/binary"
	"testing"
)

func TestEndian(t *testing.T) {
	buf := make([]byte, 16)
	enc.PutUint16(buf, uint16(50))
	t.Log(enc.Uint16(buf))
	t.Log(binary.LittleEndian.Uint16(buf))
}