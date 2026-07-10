package pair

import "testing"

func TestSealOpenRoundTrip(t *testing.T) {
	joiner, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte(`{"aes_key_hex":"deadbeef","remote_url":"git@github.com:x/y.git"}`)
	reply, err := Seal(joiner.Invite(), secret)
	if err != nil {
		t.Fatal(err)
	}
	got, err := joiner.Open(reply)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(secret) {
		t.Fatalf("payload mismatch: %s", got)
	}
}

func TestIdentityCanResumeAnUnfinishedJoin(t *testing.T) {
	original, err := NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreIdentity(original.PrivateBytes())
	if err != nil {
		t.Fatal(err)
	}
	if restored.Invite() != original.Invite() {
		t.Fatal("restored identity produced a different invite")
	}
	reply, err := Seal(original.Invite(), []byte("resume me"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := restored.Open(reply)
	if err != nil || string(plain) != "resume me" {
		t.Fatalf("restored identity could not open reply: %q, %v", plain, err)
	}
}

func TestWrongIdentityCannotOpen(t *testing.T) {
	joiner, _ := NewIdentity()
	attacker, _ := NewIdentity()
	reply, err := Seal(joiner.Invite(), []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := attacker.Open(reply); err == nil {
		t.Fatal("a different identity opened the sealed reply")
	}
}

func TestRejectsMalformedTokens(t *testing.T) {
	id, _ := NewIdentity()
	if _, err := Seal("not-an-invite", []byte("x")); err == nil {
		t.Fatal("sealed to a malformed invite")
	}
	if _, err := id.Open("not-a-reply"); err == nil {
		t.Fatal("opened a malformed reply")
	}
}

func TestInviteFingerprintIsStableAndInviteSpecific(t *testing.T) {
	a, _ := NewIdentity()
	b, _ := NewIdentity()
	first, err := InviteFingerprint(a.Invite())
	if err != nil {
		t.Fatal(err)
	}
	again, _ := InviteFingerprint(a.Invite())
	other, _ := InviteFingerprint(b.Invite())
	if first != again || first == other || len(first) != 9 {
		t.Fatalf("bad fingerprints: first=%q again=%q other=%q", first, again, other)
	}
}
