// Copyright 2015 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package promql

import (
	"math"
	"sort"
)

// Helpers to calculate quantiles.

// bucketRange is a new format for specifying both start and end of a bucket.
// The bucketRange specifies a range of (start, end].
type bucketRange struct {
	start float64
	end   float64
	count float64
}

type bucketRanges []bucketRange

func (b bucketRanges) Len() int      { return len(b) }
func (b bucketRanges) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b bucketRanges) Less(i, j int) bool {
	if b[i].end < b[j].end {
		return true
	}
	if b[i].end == b[j].end {
		return b[i].start < b[j].start
	}
	return false
}

// bucketQuantile calculates the quantile 'q' based on the given bucketRanges.
// The bucket ranges will be sorted by end, then start by this function (i.e.
// no sorting needed before calling this function). The quantile value is
// interpolated assuming a linear distribution within a bucketRange. However, if
// the bucket it lands on has +Inf, the start of that bucket is returned.
// Similarly, if the first bucket has -Inf as its start, then the end of that
// bucket is returned. Buckets shouldn't overlap. If they do, we try to combine
// them into bigger buckets.
//
// There are a number of special cases (once we have a way to report errors
// happening during evaluations of AST functions, we should report those
// explicitly):
//
// If 'buckets' has 0 observations, NaN is returned.
//
// If 'buckets' has fewer 1 element and both start or end are -Inf/+Inf, NaN
// is returned.
//
// If the highest bucket is not +Inf, NaN is returned.
//
// If q<0, -Inf is returned.
//
// If q>1, +Inf is returned.
func bucketRangeQuantile(q float64, buckets bucketRanges) float64 {
	if q < 0 {
		return math.Inf(-1)
	}
	if q > 1 {
		return math.Inf(+1)
	}

	sort.Sort(buckets)
	buckets = coalesceBucketRanges(buckets)

	convertCountsToRanks(buckets)

	if len(buckets) < 1 || len(buckets) == 1 && math.IsInf(buckets[len(buckets)-1].end, 1) && math.IsInf(buckets[0].start, -1) {
		return math.NaN()
	}
	observations := buckets[len(buckets)-1].count
	if observations == 0 {
		return math.NaN()
	}
	rank := q * observations
	b := sort.Search(len(buckets)-1, func(i int) bool { return buckets[i].count >= rank })

	if b == len(buckets)-1 && math.IsInf(buckets[b].end, 1) {
		return buckets[b].start
	}

	if b == 0 && math.IsInf(buckets[0].start, -1) {
		return buckets[0].end
	}

	// This is to comply with the same behavior as quantile.go. Unclear why it's
	// done. We could have done linear interpolcation in this case.
	if b == 0 && buckets[0].end <= 0 {
		return buckets[0].end
	}

	var (
		bucketStart = buckets[b].start
		bucketEnd   = buckets[b].end
		count       = buckets[b].count
	)
	if b > 0 {
		count -= buckets[b-1].count
		rank -= buckets[b-1].count
	}
	return bucketStart + (bucketEnd-bucketStart)*(rank/count)
}

// coalesceBucketRanges merges overlapping buckets.
//
// The input buckets must be sorted.
func coalesceBucketRanges(buckets bucketRanges) bucketRanges {
	if len(buckets) == 0 {
		return buckets
	}
	last := buckets[0]
	i := 0
	for _, b := range buckets[1:] {
		if b.start > b.end {
			// Wrong input. Skip it.
			continue
		}
		if b.start < last.end || b.end == last.end {
			// Overlapping buckets, duplicated buckets, and possibly contained
			// buckets. In all three, we merge them since they're user errors
			last.count += b.count
			last.end = b.end
			if last.start > b.start {
				last.start = b.start
			}
		} else {
			buckets[i] = last
			last = b
			i++
		}
	}
	buckets[i] = last
	return buckets[:i+1]
}

func convertCountsToRanks(buckets bucketRanges) {
	c := float64(0)
	for i := range buckets {
		buckets[i].count += c
		c = buckets[i].count
	}
}
