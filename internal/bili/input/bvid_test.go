package input

import "testing"

func TestBVAVConversion(t *testing.T) {
	bv, err := AVToBV(170001)
	if err != nil {
		t.Fatal(err)
	}
	if bv != "BV17x411w7KC" {
		t.Fatalf("got %s", bv)
	}
	aid, err := BVToAV(bv)
	if err != nil {
		t.Fatal(err)
	}
	if aid != 170001 {
		t.Fatalf("got %d", aid)
	}
}
