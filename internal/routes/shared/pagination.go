// SPDX-License-Identifier: AGPL-3.0-only

package shared

// Pagination resolves the start/count pair of a request into a SQL OFFSET and
// LIMIT. The captures show `start` used as a 1-based page number (AOE4 sends
// 1,2,3,4 with a constant count), so that is the default; `offset` is the
// escape hatch when a capture with response bodies proves otherwise.
type Pagination struct {
	Base         string
	DefaultCount int
	MaxCount     int
}

// Resolve returns the offset and the limit. A limit of 0 means "no LIMIT": the
// caller must not append one.
func (p Pagination) Resolve(start OptInt, count OptInt) (offset int, limit int) {
	pageSize := count.OrDefault(int64(p.DefaultCount))
	pageSize = max(pageSize, 0)
	if p.MaxCount > 0 && pageSize > int64(p.MaxCount) {
		pageSize = int64(p.MaxCount)
	}

	startValue := start.OrDefault(1)
	startValue = max(startValue, 0)

	if pageSize == 0 {
		// count 0 is the "give me everything" sentinel; MaxCount bounds it.
		pageSize = int64(p.MaxCount)
		if pageSize <= 0 {
			pageSize = int64(p.DefaultCount)
		}
	}

	if p.Base == "offset" {
		return int(startValue), int(pageSize)
	}
	if startValue <= 1 {
		return 0, int(pageSize)
	}
	return int(startValue-1) * int(pageSize), int(pageSize)
}
