package updater

import "testing"

func TestParseAndCompareVersions(t *testing.T) {
	current, err := ParseVersion("1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	latest, err := ParseVersion("v1.3.0")
	if err != nil {
		t.Fatal(err)
	}
	if current.String() != "1.2.3" || latest.String() != "1.3.0" || Compare(current, latest) != -1 || Compare(latest, current) != 1 || Compare(current, current) != 0 {
		t.Fatalf("unexpected version comparison: %#v %#v", current, latest)
	}
}

func TestParseVersionRejectsUnsafeValues(t *testing.T) {
	for _, value := range []string{"1.2", "1.2.3-beta", "latest", "1.2.3/../../x"} {
		if _, err := ParseVersion(value); err == nil {
			t.Fatalf("accepted invalid version %q", value)
		}
	}
}
