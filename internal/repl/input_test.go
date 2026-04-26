package repl

import (
	"testing"
)

func TestErrInputAborted_IsNotNil(t *testing.T) {
	t.Parallel()
	if ErrInputAborted == nil {
		t.Fatal("ErrInputAborted must not be nil")
	}
	if ErrInputAborted.Error() == "" {
		t.Fatal("ErrInputAborted.Error() must return a non-empty string")
	}
}

func TestLinerInput_ImplementsInputLine(t *testing.T) {
	t.Parallel()
	// compile-time interface check
	var _ InputLine = (*linerInput)(nil)
}

func TestReadlineInput_ImplementsInputLine(t *testing.T) {
	t.Parallel()
	// compile-time interface check
	var _ InputLine = (*readlineInput)(nil)
}
