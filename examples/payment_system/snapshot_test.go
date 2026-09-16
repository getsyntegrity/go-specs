package payment_system

/*
Snapshot testing captures the expected output of a function and compares
future runs against it. This helps detect unintended changes in complex
output structures (e.g. API responses or computed state).

Run with GO_SPECS_UPDATE_SNAPSHOTS=1 to create or update the snapshot file.
*/

import (
	"testing"

	specs "github.com/getsyntegrity/go-specs/specs"
)

func TestTransferSnapshot(t *testing.T) {
	specs.Describe(t, "transfer snapshot", func(s *specs.Spec) {
		// Snapshot captures the result; later runs compare against the stored snapshot.
		s.It("produces stable result", func(ctx *specs.Context) {
			service := &PaymentService{Ledger: nil}
			newFrom, newTo := service.Transfer(100, 50, 10)
			result := map[string]any{
				"fromBalance": 100,
				"toBalance":   50,
				"amount":     10,
				"newFrom":    newFrom,
				"newTo":      newTo,
			}
			ctx.Snapshot("transfer_result", result)
		})
	})
}
