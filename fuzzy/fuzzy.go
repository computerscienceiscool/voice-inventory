// Package fuzzy provides small, dependency-free string-similarity primitives
// used to resolve spoken location and item text against reference data.
package fuzzy

// Levenshtein returns the edit distance between a and b, computed over runes.
func Levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// Similarity returns 1 - Levenshtein(a,b)/max(len(a),len(b)), in [0,1].
// Two empty strings are considered identical (1).
func Similarity(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	n := len(ra)
	if len(rb) > n {
		n = len(rb)
	}
	if n == 0 {
		return 1
	}
	return 1 - float64(Levenshtein(a, b))/float64(n)
}

// Jaro returns the Jaro similarity of a and b, in [0,1].
func Jaro(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 && lb == 0 {
		return 1
	}
	if la == 0 || lb == 0 {
		return 0
	}
	window := la
	if lb > window {
		window = lb
	}
	window = window/2 - 1
	if window < 0 {
		window = 0
	}
	matchA := make([]bool, la)
	matchB := make([]bool, lb)
	matches := 0
	for i := 0; i < la; i++ {
		lo := i - window
		if lo < 0 {
			lo = 0
		}
		hi := i + window + 1
		if hi > lb {
			hi = lb
		}
		for j := lo; j < hi; j++ {
			if matchB[j] || ra[i] != rb[j] {
				continue
			}
			matchA[i] = true
			matchB[j] = true
			matches++
			break
		}
	}
	if matches == 0 {
		return 0
	}
	transpositions := 0
	j := 0
	for i := 0; i < la; i++ {
		if !matchA[i] {
			continue
		}
		for !matchB[j] {
			j++
		}
		if ra[i] != rb[j] {
			transpositions++
		}
		j++
	}
	m := float64(matches)
	t := float64(transpositions) / 2
	return (m/float64(la) + m/float64(lb) + (m-t)/m) / 3
}

// JaroWinkler returns the Jaro-Winkler similarity with the standard prefix
// scale p = 0.1 and a maximum common-prefix length of 4.
func JaroWinkler(a, b string) float64 {
	j := Jaro(a, b)
	ra, rb := []rune(a), []rune(b)
	prefix := 0
	for prefix < len(ra) && prefix < len(rb) && prefix < 4 && ra[prefix] == rb[prefix] {
		prefix++
	}
	return j + float64(prefix)*0.1*(1-j)
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
