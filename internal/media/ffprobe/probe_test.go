package ffprobe

import (
	"testing"
	"time"
)

func TestVerifyGIF(t *testing.T) {
	if err := VerifyGIF(Info{Format: "gif", Codec: "gif", Width: 640, Height: 360, FrameCount: 100, Duration: 10 * time.Second}, 10*time.Second, 640, 10); err != nil {
		t.Fatal(err)
	}
	if err := VerifyGIF(Info{Format: "gif", Codec: "gif", Width: 320, Height: 360, FrameCount: 1, Duration: time.Second}, 10*time.Second, 640, 10); err == nil {
		t.Fatal("expected validation failure")
	}
	if err := VerifyGIF(Info{Format: "gif", Codec: "gif", Width: 640, Height: 360, FrameCount: 10, Duration: 10 * time.Second}, 10*time.Second, 640, 10); err == nil {
		t.Fatal("wrong frame count should fail")
	}
}
func TestInfiniteLoopExtension(t *testing.T) {
	if !HasInfiniteLoop([]byte("GIF89aNETSCAPE2.0\x03\x01\x00\x00")) {
		t.Fatal("loop extension not detected")
	}
	if HasInfiniteLoop([]byte("GIF89a")) {
		t.Fatal("false loop detection")
	}
}
