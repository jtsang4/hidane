package api

import "time"

// QuietStream shortens a stream's idle intervals for the test that waits for a ping.
func QuietStream(ping, poll time.Duration) (restore func()) {
	oldPing, oldPoll := pingEvery, pollEvery
	pingEvery, pollEvery = ping, poll
	return func() { pingEvery, pollEvery = oldPing, oldPoll }
}
