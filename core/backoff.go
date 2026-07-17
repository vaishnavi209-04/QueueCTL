package core

import (
	"math"
	"time"
)

// CalculateBackoff calculates the exponential backoff delay based on the number of attempts.
// delay_seconds = backoff_base ^ attempts
func CalculateBackoff(base int, attempts int) time.Duration {
	delaySeconds := math.Pow(float64(base), float64(attempts))
	return time.Duration(delaySeconds) * time.Second
}
