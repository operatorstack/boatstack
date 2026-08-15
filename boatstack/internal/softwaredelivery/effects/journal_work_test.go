package effects

import (
	"testing"

	"github.com/operatorstack/boatstack/boatstack/internal/softwaredelivery/protocol"
)

func TestCommittedReceiptWorkIdentityMatchesExactAdmission(t *testing.T) {
	one := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	two := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	tests := []struct {
		name      string
		admission protocol.Admission
		receipt   protocol.TransitionReceipt
		wantErr   bool
	}{
		{name: "neither carries work"},
		{name: "exact work result", admission: protocol.Admission{Work: &protocol.WorkEvidence{ResultFingerprint: one}}, receipt: protocol.TransitionReceipt{WorkResultFingerprint: one}},
		{name: "receipt invents work", receipt: protocol.TransitionReceipt{WorkResultFingerprint: one}, wantErr: true},
		{name: "receipt omits admitted work", admission: protocol.Admission{Work: &protocol.WorkEvidence{ResultFingerprint: one}}, wantErr: true},
		{name: "receipt substitutes work", admission: protocol.Admission{Work: &protocol.WorkEvidence{ResultFingerprint: one}}, receipt: protocol.TransitionReceipt{WorkResultFingerprint: two}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateReceiptWorkRelation(test.admission, test.receipt)
			if test.wantErr && err == nil {
				t.Fatal("mismatched work identity was accepted")
			}
			if !test.wantErr && err != nil {
				t.Fatal(err)
			}
		})
	}
}
