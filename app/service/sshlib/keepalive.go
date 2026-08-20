package sshlib

import (
	"errors"
	"time"
)

// KeepAliveLoop periodically invokes probe() to keep the connection alive
// and detect dead connections. It stops when stop is closed, or when probe
// fails maxFailures times in a row (in which case onDead is called once).
//
// For SSH use client.SendRequest("keepalive@openssh.com", true, nil) as probe
// (a "failure" reply means the server is alive but does not implement the
// extension, which is not counted as a failure). For FTP use conn.NoOp().
func KeepAliveLoop(
	stop <-chan struct{},
	probe func() error,
	interval time.Duration,
	replyTimeout time.Duration,
	maxFailures int,
	onDead func(),
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	failures := 0
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		done := make(chan error, 1)
		go func() { done <- probe() }()
		var err error
		select {
		case err = <-done:
		case <-time.After(replyTimeout):
			err = errors.New("keepalive reply timeout")
		case <-stop:
			return
		}
		if err != nil {
			failures++
			if failures >= maxFailures {
				if onDead != nil {
					onDead()
				}
				return
			}
		} else {
			failures = 0
		}
	}
}
