package fuzzy

import (
	"math"
	"testing"
)

func approx(t *testing.T, got, want, tol float64, msg string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s: got %.4f want %.4f", msg, got, want)
	}
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"kitten", "sitting", 3},
		{"flaw", "lawn", 2},
		{"a-14", "a-14", 0},
		{"a-14", "a-41", 2},
		{"año", "ano", 1}, // rune-wise, not byte-wise
	}
	for _, c := range cases {
		if got := Levenshtein(c.a, c.b); got != c.want {
			t.Errorf("Levenshtein(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSimilarity(t *testing.T) {
	approx(t, Similarity("", ""), 1, 0, "empty")
	approx(t, Similarity("abc", "abc"), 1, 0, "identical")
	approx(t, Similarity("abc", "abd"), 2.0/3.0, 1e-9, "one edit")
	approx(t, Similarity("abc", "xyz"), 0, 1e-9, "disjoint")
}

func TestJaro(t *testing.T) {
	approx(t, Jaro("MARTHA", "MARHTA"), 0.9444, 0.0001, "MARTHA/MARHTA")
	approx(t, Jaro("DIXON", "DICKSONX"), 0.7667, 0.0001, "DIXON/DICKSONX")
	approx(t, Jaro("", ""), 1, 0, "both empty")
	approx(t, Jaro("a", ""), 0, 0, "one empty")
	approx(t, Jaro("abc", "abc"), 1, 0, "identical")
}

func TestJaroWinkler(t *testing.T) {
	approx(t, JaroWinkler("MARTHA", "MARHTA"), 0.9611, 0.0001, "MARTHA/MARHTA")
	approx(t, JaroWinkler("DWAYNE", "DUANE"), 0.8400, 0.0001, "DWAYNE/DUANE")
	approx(t, JaroWinkler("cat6", "cat 6 cable"), JaroWinkler("cat6", "cat 6 cable"), 0, "self-consistent")
	if JaroWinkler("rj45 connectors", "rj45 connector") < 0.95 {
		t.Error("plural variant should score high")
	}
}
