package util

// EditDistance computes the Levenshtein distance between two strings.
// If allowReplacements is false, substitutions are not allowed (i.e., only insertions/deletions).
// If maxEditDistance is non-zero, the function returns maxEditDistance+1 as soon as the
// distance exceeds that bound.
func EditDistance(s1, s2 string, allowReplacements bool, maxEditDistance int) int {
	m := len(s1)
	n := len(s2)

	// Create a row vector for dynamic programming.
	row := make([]int, n+1)
	for i := 1; i <= n; i++ {
		row[i] = i
	}

	for y := 1; y <= m; y++ {
		row[0] = y
		bestThisRow := row[0]

		previous := y - 1
		for x := 1; x <= n; x++ {
			oldRow := row[x]
			if allowReplacements {
				// With replacements allowed.
				cost := 0
				if s1[y-1] != s2[x-1] {
					cost = 1
				}
				row[x] = min(previous+cost, min(row[x-1], row[x])+1)
			} else {
				// Without replacements.
				if s1[y-1] == s2[x-1] {
					row[x] = previous
				} else {
					row[x] = min(row[x-1], row[x]) + 1
				}
			}
			previous = oldRow
			if row[x] < bestThisRow {
				bestThisRow = row[x]
			}
		}

		if maxEditDistance != 0 && bestThisRow > maxEditDistance {
			return maxEditDistance + 1
		}
	}

	return row[n]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
