# Stream

The `stream` package provides fan-out broadcasting of raw scan data to multiple
concurrent consumers. A `Fanout` reads chunks from a source channel (typically
`ScanHandle.Chunks()`) and copies each chunk to every registered `Subscription`.
Each subscriber receives independent copies, so consumers can process at
different rates without blocking the scan pipeline.

## Usage

Create a `Fanout`, subscribe one or more consumers, then call `Run`:

```go
import (
    "github.com/borud/mcc-usb-1808/v4/capture"
    "github.com/borud/mcc-usb-1808/v4/stream"
)

handle, _ := dev.CreateScan(cfg)

fan := stream.NewFanout(handle.Chunks())
disk := fan.Subscribe(stream.WithDepth(64))
live := fan.Subscribe(stream.WithDepth(8))
go fan.Run()

// Disk writer goroutine -- writes raw scan data to a capture directory.
go func() {
    cw, _ := capture.NewWriter("./capture-dir", header)
    defer cw.Close()
    for chunk := range disk.Chunks() {
        cw.WriteBulk(chunk.Data)
        disk.Release(chunk.Data)
    }
}()

handle.Start()

// Live display
for chunk := range live.Chunks() {
    decode(chunk.Data)
    live.Release(chunk.Data)
}
```

## Back-pressure and drops

If a subscriber's buffer fills up, new chunks are dropped for that subscriber
only -- other subscribers and the source are unaffected. Use
`Subscription.Dropped()` to monitor drop counts and `WithDepth` to size
subscriber buffers appropriately.

A disk writer typically needs deeper buffers (`WithDepth(64)`) to absorb I/O
stalls, while a live display can tolerate drops and use a shallow buffer
(`WithDepth(8)`).

## Buffer reuse

Each subscription maintains a `sync.Pool` for buffer reuse. Call
`Subscription.Release(chunk.Data)` after processing a chunk to return the
buffer to the pool and reduce allocations.

## Error propagation

Call `Fanout.SetErr(err)` before the source channel closes to propagate
upstream errors (such as scan overrun) to all subscribers. Subscribers check
for errors via `Subscription.Err()` after their chunk channel closes.

## API

| Type / Function              | Description                                      |
|------------------------------|--------------------------------------------------|
| `NewFanout(src)`             | Create a fanout reading from `src`               |
| `Fanout.Subscribe(opts...)`  | Add a subscriber, returns `*Subscription`        |
| `Fanout.Run()`               | Start broadcasting (blocks until source closes)  |
| `Fanout.SetErr(err)`         | Set error to propagate to subscribers            |
| `Fanout.Done()`              | Channel closed when `Run` exits                  |
| `Fanout.AwaitDone()`         | Block until `Run` exits                          |
| `Subscription.Chunks()`      | Receive channel delivering chunk copies          |
| `Subscription.Release(buf)`  | Return buffer to pool for reuse                  |
| `Subscription.Dropped()`     | Total chunks dropped due to full buffer          |
| `Subscription.Err()`         | Error from upstream, if any                      |
| `WithDepth(n)`               | Set subscription buffer depth (default 32)       |
