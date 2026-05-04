package main

import (
	"testing"
)

func TestEditDistance_Empty(t *testing.T) {
	if d := EditDistance("", "ninja", true, 0); d != 5 {
		t.Errorf("EditDistance(\"\", \"ninja\") = %d, want 5", d)
	}
	if d := EditDistance("ninja", "", true, 0); d != 5 {
		t.Errorf("EditDistance(\"ninja\", \"\") = %d, want 5", d)
	}
	if d := EditDistance("", "", true, 0); d != 0 {
		t.Errorf("EditDistance(\"\", \"\") = %d, want 0", d)
	}
}

func TestEditDistance_MaxDistance(t *testing.T) {
	const allowReplacements = true
	for maxDist := 1; maxDist < 7; maxDist++ {
		d := EditDistance("abcdefghijklmnop", "ponmlkjihgfedcba", allowReplacements, maxDist)
		if d != maxDist+1 {
			t.Errorf("EditDistance with max=%d = %d, want %d", maxDist, d, maxDist+1)
		}
	}
}

func TestEditDistance_AllowReplacements(t *testing.T) {
	allowReplacements := true
	if d := EditDistance("ninja", "njnja", allowReplacements, 0); d != 1 {
		t.Errorf("with replacements: got %d, want 1", d)
	}
	if d := EditDistance("njnja", "ninja", allowReplacements, 0); d != 1 {
		t.Errorf("with replacements: got %d, want 1", d)
	}

	allowReplacements = false
	if d := EditDistance("ninja", "njnja", allowReplacements, 0); d != 2 {
		t.Errorf("without replacements: got %d, want 2", d)
	}
	if d := EditDistance("njnja", "ninja", allowReplacements, 0); d != 2 {
		t.Errorf("without replacements: got %d, want 2", d)
	}
}

func TestEditDistance_Basics(t *testing.T) {
	if d := EditDistance("browser_tests", "browser_tests", true, 0); d != 0 {
		t.Errorf("identical: got %d, want 0", d)
	}
	if d := EditDistance("browser_test", "browser_tests", true, 0); d != 1 {
		t.Errorf("one char diff: got %d, want 1", d)
	}
	if d := EditDistance("browser_tests", "browser_test", true, 0); d != 1 {
		t.Errorf("one char diff: got %d, want 1", d)
	}
}
