package transport

/*
#cgo pkg-config: libusb-1.0
#include <libusb.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

// Forward declaration of Go callback (defined in bulk_out_ring_cb.go via //export).
extern void goOutRingCallback(struct libusb_transfer *xfer);

// C trampoline: libusb calls this on OUT transfer completion.
static void LIBUSB_CALL out_ring_cb(struct libusb_transfer *xfer) {
	goOutRingCallback(xfer);
}

// fill_out_transfer wraps libusb_fill_bulk_transfer for OUT transfers.
// flags is applied after filling (e.g., LIBUSB_TRANSFER_ADD_ZERO_PACKET).
static void fill_out_transfer(struct libusb_transfer *xfer,
		libusb_device_handle *handle, unsigned char ep,
		unsigned char *buf, int len,
		uintptr_t user_data, unsigned int timeout,
		uint8_t flags) {
	libusb_fill_bulk_transfer(xfer, handle, ep, buf, len,
		out_ring_cb, (void *)user_data, timeout);
	xfer->flags = flags;
}
*/
import "C"

import (
	"fmt"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"runtime/cgo"
)

// BulkOutRingStats is declared in transport.go.

// bulkOutRing manages a ring of async libusb bulk OUT transfers.
// The device consumes samples from a BulkOutSource via a refill goroutine.
//
// Requires libusb >= 1.0.9 for LIBUSB_TRANSFER_ADD_ZERO_PACKET support.
type bulkOutRing struct {
	ctx      *C.libusb_context
	handle   *C.libusb_device_handle
	transfers []*C.struct_libusb_transfer
	buffers  []unsafe.Pointer // C.malloc'd per-slot buffers
	bufSize  int
	endpoint uint8
	source   BulkOutSource

	// freeSlots receives slot indices from the callback when a transfer
	// completes successfully and is ready for refill. Buffered at count.
	freeSlots chan int

	// done closes when the source is exhausted and all in-flight transfers
	// have completed naturally.
	done chan struct{}
	// stopCh is closed by Stop() to signal abort.
	stopCh chan struct{}
	// evtDone is closed when the event loop goroutine exits.
	evtDone chan struct{}
	// refillDone is closed when the refill goroutine exits.
	refillDone chan struct{}

	h       cgo.Handle
	stopped atomic.Bool
	// pending counts libusb-submitted transfers that have not yet fired their
	// callback. Decremented in the callback; incremented before submit.
	pending atomic.Int32

	submitted atomic.Uint64
	completed atomic.Uint64
	bytesSent atomic.Uint64

	// err stores the first fatal error (from source or libusb).
	errMu sync.Mutex
	err   error
}

// NewBulkOutRing creates and starts an async bulk OUT ring.
// count transfers of bufSize bytes are pre-filled from source and submitted.
// The returned ring's Done channel closes when the source is exhausted and
// all remaining transfers have completed. Always call Stop when done.
func NewBulkOutRing(ctx *C.libusb_context, handle *C.libusb_device_handle,
	endpoint uint8, bufSize, count int, source BulkOutSource, timeoutMs uint) (BulkOutRing, error) {

	r := &bulkOutRing{
		ctx:        ctx,
		handle:     handle,
		transfers:  make([]*C.struct_libusb_transfer, count),
		buffers:    make([]unsafe.Pointer, count),
		bufSize:    bufSize,
		endpoint:   endpoint,
		source:     source,
		freeSlots:  make(chan int, count),
		done:       make(chan struct{}),
		stopCh:     make(chan struct{}),
		evtDone:    make(chan struct{}),
		refillDone: make(chan struct{}),
	}

	r.h = cgo.NewHandle(r)

	// Allocate libusb transfer structs and C buffers.
	for i := range count {
		xfer := C.libusb_alloc_transfer(0)
		if xfer == nil {
			r.cleanupTransfers(i)
			return nil, fmt.Errorf("libusb_alloc_transfer failed")
		}
		buf := C.malloc(C.size_t(bufSize))
		if buf == nil {
			C.libusb_free_transfer(xfer)
			r.cleanupTransfers(i)
			return nil, fmt.Errorf("C.malloc(%d) failed", bufSize)
		}
		r.transfers[i] = xfer
		r.buffers[i] = buf
	}

	// Pre-fill and submit as many slots as the source provides.
	submitted := 0
	sourceExhausted := false
	for i := range count {
		n, err := source.Fill(r.goSlice(i))
		if n > 0 {
			r.setupTransfer(i, n, timeoutMs)
			if rc := C.libusb_submit_transfer(r.transfers[i]); rc != 0 {
				// Cancel already-submitted transfers before returning error.
				r.stopped.Store(true)
				for j := range submitted {
					C.libusb_cancel_transfer(r.transfers[j])
				}
				r.pending.Store(int32(submitted))
				if submitted > 0 {
					go r.eventLoop()
					<-r.evtDone
				}
				r.cleanupTransfers(count)
				return nil, fmt.Errorf("libusb_submit_transfer: %s", C.GoString(C.libusb_strerror(rc)))
			}
			r.pending.Add(1)
			r.submitted.Add(1)
			submitted++
		}
		if err == io.EOF {
			sourceExhausted = true
			break
		}
		if err != nil {
			r.cleanupTransfers(count)
			return nil, fmt.Errorf("initial fill: %w", err)
		}
	}

	if submitted == 0 {
		// Source was empty at startup.
		r.cleanupTransfers(count)
		close(r.done)
		close(r.evtDone)
		close(r.refillDone)
		return r, nil
	}

	go r.eventLoop()
	go r.refillLoop(submitted, sourceExhausted, timeoutMs)
	return r, nil
}

// goSlice returns a Go slice backed by the C buffer at slot i.
func (r *bulkOutRing) goSlice(i int) []byte {
	return unsafe.Slice((*byte)(r.buffers[i]), r.bufSize)
}

// setupTransfer configures transfer i for n bytes of data.
func (r *bulkOutRing) setupTransfer(i, n int, timeoutMs uint) {
	flags := C.uint8_t(0)
	if n%512 == 0 {
		flags = C.LIBUSB_TRANSFER_ADD_ZERO_PACKET
	}
	C.fill_out_transfer(
		r.transfers[i],
		r.handle,
		C.uchar(r.endpoint),
		(*C.uchar)(r.buffers[i]),
		C.int(n),
		C.uintptr_t(r.h),
		C.uint(timeoutMs),
		flags,
	)
}

// slotOf returns the slot index for a transfer by buffer pointer comparison.
func (r *bulkOutRing) slotOf(xfer *C.struct_libusb_transfer) int {
	for i, buf := range r.buffers {
		if unsafe.Pointer(xfer.buffer) == buf {
			return i
		}
	}
	return -1
}

// onOutTransferComplete is called from the C callback on the libusb event loop
// thread. All invocations of this ring are sequential on that thread.
func (r *bulkOutRing) onOutTransferComplete(xfer *C.struct_libusb_transfer) {
	r.pending.Add(-1)

	if xfer.status == C.LIBUSB_TRANSFER_COMPLETED && !r.stopped.Load() {
		idx := r.slotOf(xfer)
		r.bytesSent.Add(uint64(xfer.actual_length))
		// Forward to the refill goroutine via buffered channel (never blocks).
		select {
		case r.freeSlots <- idx:
		case <-r.stopCh:
		}
		return
	}

	if xfer.status != C.LIBUSB_TRANSFER_CANCELLED && !r.stopped.Load() {
		r.setErr(fmt.Errorf("async bulk OUT transfer: libusb status %d", int(xfer.status)))
	}
}

// refillLoop runs in its own goroutine. It receives free slot notifications,
// calls source.Fill, and resubmits or terminates as appropriate.
func (r *bulkOutRing) refillLoop(initialSubmitted int, sourceAlreadyEOF bool, timeoutMs uint) {
	defer close(r.refillDone)

	active := initialSubmitted // slots still logically alive
	sourceEOF := sourceAlreadyEOF
	var doneOnce sync.Once

	for {
		select {
		case idx := <-r.freeSlots:
			r.completed.Add(1)
			if sourceEOF || r.stopped.Load() {
				active--
				if active == 0 {
					doneOnce.Do(func() { close(r.done) })
				}
				continue
			}
			n, err := r.source.Fill(r.goSlice(idx))
			if err != nil && err != io.EOF {
				r.setErr(err)
				go r.Stop()
				return
			}
			if n > 0 && !r.stopped.Load() {
				r.setupTransfer(idx, n, timeoutMs)
				if rc := C.libusb_submit_transfer(r.transfers[idx]); rc != 0 {
					r.setErr(fmt.Errorf("resubmit: %s", C.GoString(C.libusb_strerror(rc))))
					go r.Stop()
					return
				}
				r.pending.Add(1)
				r.submitted.Add(1)
				// active unchanged: completed one, resubmitted one
			} else if r.stopped.Load() {
				// Stop was called while we were filling; don't submit.
				active--
				if active == 0 {
					doneOnce.Do(func() { close(r.done) })
				}
				continue
			}
			if err == io.EOF {
				sourceEOF = true
				if n == 0 {
					// No data submitted for this slot.
					active--
					if active == 0 {
						doneOnce.Do(func() { close(r.done) })
						return
					}
				}
				// n > 0: submitted one last time; will get its completion later.
			}

		case <-r.stopCh:
			return
		}
	}
}

// eventLoop runs libusb event handling until all pending transfers complete.
// Must run on a locked OS thread.
func (r *bulkOutRing) eventLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(r.evtDone)

	tv := C.struct_timeval{tv_sec: 1}
	for r.pending.Load() > 0 {
		C.libusb_handle_events_timeout(r.ctx, &tv)
	}
}

func (r *bulkOutRing) Done() <-chan struct{} { return r.done }

func (r *bulkOutRing) Err() error {
	r.errMu.Lock()
	defer r.errMu.Unlock()
	return r.err
}

func (r *bulkOutRing) setErr(err error) {
	r.errMu.Lock()
	if r.err == nil {
		r.err = err
	}
	r.errMu.Unlock()
}

// Stop cancels in-flight transfers, waits for cleanup, and frees C resources.
// Safe to call multiple times; subsequent calls are no-ops.
func (r *bulkOutRing) Stop() {
	if !r.stopped.CompareAndSwap(false, true) {
		return
	}
	close(r.stopCh)
	for _, xfer := range r.transfers {
		C.libusb_cancel_transfer(xfer)
	}
	<-r.evtDone
	<-r.refillDone
	r.cleanupTransfers(len(r.transfers))
}

func (r *bulkOutRing) Stats() BulkOutRingStats {
	return BulkOutRingStats{
		Submitted: r.submitted.Load(),
		Completed: r.completed.Load(),
		BytesSent: r.bytesSent.Load(),
	}
}

// cleanupTransfers frees C resources for the first n slots and the cgo handle.
func (r *bulkOutRing) cleanupTransfers(n int) {
	for i := range n {
		if r.buffers[i] != nil {
			C.free(r.buffers[i])
			r.buffers[i] = nil
		}
		if r.transfers[i] != nil {
			C.libusb_free_transfer(r.transfers[i])
			r.transfers[i] = nil
		}
	}
	r.h.Delete()
}
