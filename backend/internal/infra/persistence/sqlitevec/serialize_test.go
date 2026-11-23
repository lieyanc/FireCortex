package sqlitevec

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/lieyanc/FireCortex/backend/internal/infra/persistence/vectorutil"
)

func TestSerializeFloat32PureGo(t *testing.T) {
	t.Parallel()

	blob, err := SerializeFloat32([]float32{1, -2.5})
	if err != nil {
		t.Fatalf("SerializeFloat32() error = %v", err)
	}
	if len(blob) != 4*vectorutil.MaxDimensions {
		t.Fatalf("len(blob) = %d, want %d", len(blob), 4*vectorutil.MaxDimensions)
	}

	got0 := math.Float32frombits(binary.LittleEndian.Uint32(blob[0:4]))
	got1 := math.Float32frombits(binary.LittleEndian.Uint32(blob[4:8]))
	if got0 != 1 || got1 != -2.5 {
		t.Fatalf("first two float32s = (%v, %v), want (1, -2.5)", got0, got1)
	}
	for i := 8; i < len(blob); i++ {
		if blob[i] != 0 {
			t.Fatalf("expected zero padding at byte %d, got %d", i, blob[i])
		}
	}
}
