package api

import "time"

// StreamIntervals are how long a quiet stream waits before it pings, and between polls.
func StreamIntervals() (ping, poll time.Duration) { return pingEvery, pollEvery }

// QuietStream shortens a stream's idle intervals for the test that waits for a ping.
func QuietStream(ping, poll time.Duration) (restore func()) {
	oldPing, oldPoll := pingEvery, pollEvery
	pingEvery, pollEvery = ping, poll
	return func() { pingEvery, pollEvery = oldPing, oldPoll }
}
