package fonts

import "testing"

func TestCharsetContainsRepresentativeChineseGlyph(t *testing.T) {
	if !charsetContains("20-7e 3400-4dbf 4e00-9fff", 0x4e2d) {
		t.Fatal("Chinese range was not recognized")
	}
	if charsetContains("20-7e a0-ff", 0x4e2d) {
		t.Fatal("Latin-only charset was accepted")
	}
}
