package profile

import "testing"

func TestProfile(t *testing.T) {
	defer Set("")
	if Name() != "" || Dir() != Default || Tag("-") != "" || Title() != "Session Workbench" {
		t.Fatalf("default: %q %q %q %q", Name(), Dir(), Tag("-"), Title())
	}
	if err := Set("ssh-switcher"); err != nil {
		t.Fatal(err)
	}
	if Name() != "ssh-switcher" || Dir() != "ssh-switcher" || Tag("_") != "ssh-switcher_" || Title() != "Session Workbench — ssh-switcher" {
		t.Errorf("named: %q %q %q %q", Name(), Dir(), Tag("_"), Title())
	}
	if err := Set(Default); err != nil || Name() != "" {
		t.Errorf("session-workbench is the default: %v %q", err, Name())
	}
	for _, bad := range []string{"Upper", "-dash", "a/b", "../x", "a b", "a.b", "a:b", "toolongtoolongtoolongtoolongtoolongtoolong1"} {
		if err := Set(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if Name() != "" {
		t.Error("a refused name changed the profile")
	}
}
