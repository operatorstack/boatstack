package protocol

import "testing"

func TestProgramDeltaFingerprintBindsDirectionAndExactEndpoints(t *testing.T) {
	// control-law: program-change-authority-binds-the-exact-directed-delta
	oldProgram := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newProgram := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	delta, err := ProgramDeltaFingerprint(oldProgram, newProgram)
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := ProgramDeltaFingerprint(newProgram, oldProgram)
	if err != nil {
		t.Fatal(err)
	}
	if len(delta) != 64 || delta == reversed {
		t.Fatalf("directed program delta fingerprints = %q and %q", delta, reversed)
	}
	if _, err := ProgramDeltaFingerprint(oldProgram, oldProgram); err == nil {
		t.Fatal("identical program endpoints produced a change fingerprint")
	}
	if _, err := ProgramDeltaFingerprint("short", newProgram); err == nil {
		t.Fatal("malformed prior program identity was accepted")
	}
}
