package brorm

import (
	"errors"
	"testing"

	"gorm.io/gorm"

	"github.com/zaid223r/Bridgr/core/brerrors"
)

func TestMapError(t *testing.T) {
	t.Parallel()

	if err := mapError(nil); err != nil {
		t.Errorf("mapError(nil) = %v, want nil", err)
	}

	err := mapError(gorm.ErrRecordNotFound)
	if !errors.Is(err, brerrors.ErrNotFound) {
		t.Errorf("record-not-found does not match brerrors.ErrNotFound: %v", err)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("original GORM error lost from the chain: %v", err)
	}

	other := errors.New("connection refused")
	if got := mapError(other); !errors.Is(got, other) || errors.Is(got, brerrors.ErrNotFound) {
		t.Errorf("mapError(other) = %v, want it unchanged", got)
	}
}
