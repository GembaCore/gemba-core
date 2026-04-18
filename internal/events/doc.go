// Package events is the SSE hub. One-way server-push fanout: any source
// (event-file tailer, mutation completion, periodic pulse) publishes; all
// subscribed clients receive. Slow consumers are dropped, not blocked.
//
// See gm-e2.5.
package events
