package controlprogram

import "testing"

func TestSafeRelativeRejectsWindowsSpecialComponentsOnEveryHost(t *testing.T) {
	for _, value := range []string{"NUL", "nested/con.txt", "COM1.log", "nested/LPT9", "plan.", "plan ", "nested/name:stream"} {
		if safeRelative(value) {
			t.Fatalf("safeRelative(%q) accepted a Windows-special component", value)
		}
	}
	for _, value := range []string{"plan.md", "nested/null.md"} {
		if !safeRelative(value) {
			t.Fatalf("safeRelative(%q) rejected a portable path", value)
		}
	}
}
