package clipboard

import "testing"

// The stash must survive a run of dictations: what Murrly published is not the
// user's clipboard, however long it sits there.
func TestOurOwnDictationIsNotTheUsersClipboard(t *testing.T) {
	t.Cleanup(forgetOurs)

	markOurs("первая диктовка")
	if !isOurs("первая диктовка") {
		t.Fatal("the text we just published was not recognised as ours")
	}
	if isOurs("то, что скопировал пользователь") {
		t.Fatal("somebody else's text was claimed as ours")
	}

	// A second dictation replaces the marker — only the latest is in the
	// clipboard, and the one before it is gone from there anyway.
	markOurs("вторая диктовка")
	if isOurs("первая диктовка") {
		t.Error("a superseded dictation is still claimed as ours")
	}
	if !isOurs("вторая диктовка") {
		t.Error("the latest dictation was not recognised as ours")
	}
}

// Set is only reached because the user asked for the text to be there — the
// menu's "put the previous clipboard back" above all. Snapshotting it on the
// next dictation is then correct, so the marker has to go.
func TestTextTheUserAskedForIsTheirs(t *testing.T) {
	t.Cleanup(forgetOurs)

	markOurs("диктовка")
	forgetOurs()
	if isOurs("диктовка") {
		t.Error("text restored at the user's request is still claimed as ours")
	}
}

// An empty marker must not swallow an empty read — those are two different
// things, and isOurs is asked about arbitrary clipboard content.
func TestEmptyMarkerClaimsNothing(t *testing.T) {
	t.Cleanup(forgetOurs)

	forgetOurs()
	if isOurs("") {
		t.Error("an unset marker claimed the empty string")
	}
}
