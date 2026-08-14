package domain

import (
	"testing"
	"time"
)

func TestJobStateMachine(t *testing.T) {
	if err := ValidateTransition(JobQueued, JobResolvingVideo); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTransition(JobQueued, JobSucceeded); err == nil {
		t.Fatal("expected invalid transition")
	}
	if err := ValidateTransition(JobFailed, JobQueued); err != nil {
		t.Fatal(err)
	}
}

func TestDedupeKeyDeterministicAndSensitive(t *testing.T) {
	p := RenderParams{FPS: 10, Width: 640}
	a := MakeDedupeKey("BV17x411w7KC", 1, time.Second, 2*time.Second, p, "v1")
	b := MakeDedupeKey("BV17x411w7KC", 1, time.Second, 2*time.Second, p, "v1")
	c := MakeDedupeKey("BV17x411w7KC", 1, time.Second, 3*time.Second, p, "v1")
	if a != b || a == c {
		t.Fatalf("bad dedupe keys: %q %q %q", a, b, c)
	}
}
