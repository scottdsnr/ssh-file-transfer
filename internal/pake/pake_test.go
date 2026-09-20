package pake

import "testing"

func exchange(t *testing.T, pwA, pwB string) ([]byte, []byte) {
	t.Helper()
	a, err := New(RoleA, []byte(pwA))
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(RoleB, []byte(pwB))
	if err != nil {
		t.Fatal(err)
	}
	msgA, msgB := a.Bytes(), b.Bytes()
	if err := a.Update(msgB); err != nil {
		t.Fatal(err)
	}
	if err := b.Update(msgA); err != nil {
		t.Fatal(err)
	}
	ka, err := a.SessionKey()
	if err != nil {
		t.Fatal(err)
	}
	kb, err := b.SessionKey()
	if err != nil {
		t.Fatal(err)
	}
	return ka, kb
}

func TestMatchingPasswordsAgree(t *testing.T) {
	ka, kb := exchange(t, "hunter2", "hunter2")
	if string(ka) != string(kb) {
		t.Fatal("keys differ despite matching passwords")
	}
	if len(ka) != 32 {
		t.Fatalf("key length = %d, want 32", len(ka))
	}
}

func TestMismatchedPasswordsDisagree(t *testing.T) {
	ka, kb := exchange(t, "hunter2", "hunter3")
	if string(ka) == string(kb) {
		t.Fatal("keys agree despite mismatched passwords")
	}
}

func TestRejectsGarbagePeerMessage(t *testing.T) {
	a, err := New(RoleA, []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Update([]byte{2, 3, 4}); err == nil {
		t.Fatal("expected an error for an invalid curve point")
	}
}
