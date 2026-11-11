package models

import i "github.com/luskaner/ageAPIServer/internal"

// The 0/1 conversion lives in the internal package so the stores and the
// serializers cannot disagree; these are the local names the SQL code uses.

// NumberToBool renders a stored 0/1 integer as a flag.
func NumberToBool(value int64) bool {
	return i.NumberToBool(value)
}

// BoolToNumber renders a flag as the 0/1 integer SQLite stores.
func BoolToNumber(value bool) int64 {
	return i.BoolToNumber(value)
}
