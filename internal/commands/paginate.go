package commands

import "encoding/json"

// paginateOffset drives a reusable offset-pagination loop for list commands.
//
// fetch is called with an increasing offset and the fixed page size and must
// return the items for that page plus the API's reported total (0 if the API
// doesn't report one). paginateOffset is deliberately closure-based and
// dependency-free so both shapes already in use in this codebase can adapt to
// it:
//
//   - v1 APIs (start/count/total_matching, see hosts.go): the caller's fetch
//     closure sets params["start"]=offset, params["count"]=size and reads
//     total from the response's total_matching field.
//   - v2 APIs (page[offset]/page[size]/meta.page.total_count, see
//     services.go): the caller's fetch closure sets
//     params["page[offset]"]=offset, params["page[size]"]=size and reads
//     total from meta.page.total_count.
//
// The loop stops as soon as one of these is true: the accumulated item count
// reaches the reported total, a page comes back shorter than the requested
// size (there's nothing left to fetch), or maxPages calls have been made
// (safety cap against a runaway/misbehaving API). On a fetch error the items
// and total accumulated so far are returned alongside the error.
func paginateOffset(fetch func(offset, size int) (items []json.RawMessage, total int, err error), size, maxPages int) ([]json.RawMessage, int, error) {
	var all []json.RawMessage
	total := 0
	offset := 0

	for page := 0; page < maxPages; page++ {
		items, reportedTotal, err := fetch(offset, size)
		if err != nil {
			return all, total, err
		}
		total = reportedTotal
		all = append(all, items...)

		if len(items) < size {
			break
		}
		if total > 0 && len(all) >= total {
			break
		}
		offset += size
	}

	return all, total, nil
}
