package graph

import "testing"

func TestDo(t *testing.T) {
	err := Do()
	if err != nil {
		t.Fatal(err)
	}
}
