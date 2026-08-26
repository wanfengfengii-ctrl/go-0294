package instrument

// Fixed logical-time backoff schedule for pending retries. It does not depend
// on wall time or random jitter: each attempt advances the next logical time
// by a deterministic stride drawn from the fixed sequence.

// maxRetries is the locked retry ceiling; reaching it converts the call to
// "exhausted" and surfaces an anomaly rather than retrying forever.
const maxRetries = 3

// backoffStrides is the fixed logical-time stride sequence (in logical ticks)
// applied between attempts.
var backoffStrides = []int64{10, 20, 40}

// NextRetryTime returns the logical time at which the next attempt should run,
// given the current attempt index. Attempts are zero-based; the final stride
// is reused once the sequence is exhausted.
func NextRetryTime(attempt int, baseTime int64) int64 {
	stride := backoffStrides[len(backoffStrides)-1]
	if attempt >= 0 && attempt < len(backoffStrides) {
		stride = backoffStrides[attempt]
	}
	return baseTime + stride
}

// Exhausted reports whether the call has reached the locked retry ceiling and
// must be converted to an anomaly.
func Exhausted(attempt int) bool {
	return attempt >= maxRetries
}

// RetryCeiling returns the locked maximum number of attempts before a call is
// exhausted.
func RetryCeiling() int {
	return maxRetries
}
