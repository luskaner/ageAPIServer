package internal

// SQLite has no boolean type: flags are stored as 0/1 integers and rendered as
// JSON booleans. These helpers are the single conversion point so the stores
// and the serializers cannot disagree.

const falseValue = 0

// BoolToNumber renders a flag as the 0/1 integer SQLite stores.
func BoolToNumber(value bool) int64 {
	if value {
		return 1
	}
	return falseValue
}

// NumberToBool renders a stored 0/1 integer as a flag.
func NumberToBool(value int64) bool {
	return value != falseValue
}
