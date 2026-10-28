package service

import "testing"

func TestSourceIconURL(t *testing.T) {
	if got := sourceIconURL("publisher", ""); got != "" {
		t.Fatal(got)
	}
	if got := sourceIconURL("publisher/name", "ba7816bf8f01cfea414140de"); got != "/api/source-icons/publisher%2Fname?v=ba7816bf8f01cfea414140de" {
		t.Fatal(got)
	}
}
