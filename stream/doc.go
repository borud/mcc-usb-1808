// Package stream provides fan-out broadcasting of raw scan data to multiple
// concurrent consumers.
//
// A [Fanout] reads raw byte chunks from a source channel (typically
// [device.ScanHandle.Chunks]) and copies each chunk to every registered
// [Subscription]. Each subscriber receives independent copies of the data,
// so consumers can process at different rates without blocking the scan
// pipeline.
//
// # Usage
//
// Create a Fanout, subscribe one or more consumers, then call Run:
//
//	handle, _ := dev.CreateScan(cfg)
//	handle.Start()
//
//	fan := stream.NewFanout(handle.Chunks())
//	disk := fan.Subscribe(stream.WithDepth(64))
//	live := fan.Subscribe(stream.WithDepth(8))
//	go fan.Run()
//
//	// Disk writer goroutine
//	go func() {
//	    for chunk := range disk.Chunks() {
//	        writer.WriteBulk(chunk)
//	        disk.Release(chunk)
//	    }
//	}()
//
//	// Live display goroutine
//	for chunk := range live.Chunks() {
//	    decode(chunk)
//	    live.Release(chunk)
//	}
//
// # Back-pressure and drops
//
// If a subscriber's buffer fills up, new chunks are dropped for that
// subscriber only — other subscribers and the source are unaffected.
// Use [Subscription.Dropped] to monitor drop counts and [WithDepth]
// to size subscriber buffers appropriately.
//
// # Buffer reuse
//
// Each subscription maintains a sync.Pool for buffer reuse. Call
// [Subscription.Release] after processing a chunk to return the
// buffer to the pool and reduce allocations.
//
// # Error propagation
//
// Call [Fanout.SetErr] before the source channel closes to propagate
// upstream errors (such as scan overrun) to all subscribers. Subscribers
// check for errors via [Subscription.Err] after their chunk channel closes.
package stream
