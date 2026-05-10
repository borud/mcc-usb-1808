package transport

/*
#cgo pkg-config: libusb-1.0
#include <libusb.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

// Forward declaration of Go callback (defined in ring_cb.go via //export).
extern void goRingCallback(struct libusb_transfer *xfer);

// C trampoline: libusb calls this on transfer completion; it forwards
// to the Go-exported callback.
static void LIBUSB_CALL ring_cb(struct libusb_transfer *xfer) {
	goRingCallback(xfer);
}

// fill_ring_transfer wraps libusb_fill_bulk_transfer so we can bind
// ring_cb (a static function) without exposing it as a Go function pointer.
// user_data is a cgo.Handle value passed as uintptr_t to avoid the
// Go-side unsafe.Pointer(uintptr) conversion that trips go vet.
static void fill_ring_transfer(struct libusb_transfer *xfer,
		libusb_device_handle *handle, unsigned char ep,
		unsigned char *buf, int len,
		uintptr_t user_data, unsigned int timeout) {
	libusb_fill_bulk_transfer(xfer, handle, ep, buf, len,
		ring_cb, (void *)user_data, timeout);
}
*/
import "C"

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"unsafe"

	"runtime/cgo"
)

// RingResult holds one completed async bulk transfer.
type RingResult struct {
	Data []byte
	Err  error
}

// RingStats holds counters for a BulkRing's lifetime.
type RingStats struct {
	Submitted uint64
	Completed uint64
}

// AsyncBulkReader is an optional interface that Transport implementations
// can provide to support async bulk transfer rings.
type AsyncBulkReader interface {
	NewBulkRing(endpoint uint8, bufSize, count, depth int, timeoutMs uint) (*BulkRing, error)
}

// BulkRing manages a ring of libusb async bulk transfers. The completion
// callback resubmits immediately (memcpy + submit, ~10 µs) so the USB
// controller always has N transfers queued regardless of Go scheduling.
type BulkRing struct {
	ctx       *C.libusb_context
	handle    *C.libusb_device_handle
	transfers []*C.struct_libusb_transfer
	buffers   []unsafe.Pointer // C.malloc'd buffers
	bufSize   int
	results   chan RingResult
	h         cgo.Handle
	stopCh    chan struct{} // closed by Stop to unblock channel sends
	evtDone   chan struct{} // closed when event loop goroutine exits
	stopped   atomic.Bool
	pending   atomic.Int32
	submitted atomic.Uint64
	completed atomic.Uint64
}

// NewBulkRing allocates count async bulk transfers of bufSize bytes each,
// submits them all, and starts the libusb event loop. Results are delivered
// on a channel of capacity depth. Call Stop to cancel, drain, and free.
func NewBulkRing(ctx *C.libusb_context, handle *C.libusb_device_handle,
	endpoint uint8, bufSize, count, depth int, timeoutMs uint) (*BulkRing, error) {

	r := &BulkRing{
		ctx:       ctx,
		handle:    handle,
		transfers: make([]*C.struct_libusb_transfer, count),
		buffers:   make([]unsafe.Pointer, count),
		bufSize:   bufSize,
		results:   make(chan RingResult, depth),
		stopCh:    make(chan struct{}),
		evtDone:   make(chan struct{}),
	}

	r.h = cgo.NewHandle(r)

	for i := range count {
		xfer := C.libusb_alloc_transfer(0)
		if xfer == nil {
			r.cleanup(i)
			return nil, fmt.Errorf("libusb_alloc_transfer failed")
		}

		buf := C.malloc(C.size_t(bufSize))
		if buf == nil {
			C.libusb_free_transfer(xfer)
			r.cleanup(i)
			return nil, fmt.Errorf("C.malloc(%d) failed", bufSize)
		}

		r.transfers[i] = xfer
		r.buffers[i] = buf

		C.fill_ring_transfer(
			xfer,
			handle,
			C.uchar(endpoint),
			(*C.uchar)(buf),
			C.int(bufSize),
			C.uintptr_t(r.h),
			C.uint(timeoutMs),
		)
	}

	for i, xfer := range r.transfers {
		if rc := C.libusb_submit_transfer(xfer); rc != 0 {
			// Cancel already-submitted transfers and drain them
			// before freeing resources.
			r.stopped.Store(true)
			for j := range i {
				C.libusb_cancel_transfer(r.transfers[j])
			}
			r.pending.Store(int32(i))
			if i > 0 {
				go r.eventLoop()
				<-r.evtDone
			}
			r.cleanup(count)
			return nil, fmt.Errorf("libusb_submit_transfer: %s",
				C.GoString(C.libusb_strerror(rc)))
		}
		r.pending.Add(1)
		r.submitted.Add(1)
	}

	go r.eventLoop()
	return r, nil
}

// Results returns the channel that delivers completed transfer data.
// The channel is closed when the ring is stopped and all transfers drained.
func (r *BulkRing) Results() <-chan RingResult {
	return r.results
}

// Stats returns ring lifetime counters.
func (r *BulkRing) Stats() RingStats {
	return RingStats{
		Submitted: r.submitted.Load(),
		Completed: r.completed.Load(),
	}
}

// Stop cancels all in-flight transfers, waits for the event loop to
// drain them, and frees all C-allocated memory.
func (r *BulkRing) Stop() {
	if !r.stopped.CompareAndSwap(false, true) {
		return
	}
	close(r.stopCh)
	for _, xfer := range r.transfers {
		C.libusb_cancel_transfer(xfer)
	}
	<-r.evtDone
	r.cleanup(len(r.transfers))
}

// onTransferComplete is called from the C callback (via goRingCallback)
// on the event loop thread. All invocations are sequential.
func (r *BulkRing) onTransferComplete(xfer *C.struct_libusb_transfer) {
	if xfer.status == C.LIBUSB_TRANSFER_COMPLETED && !r.stopped.Load() {
		n := int(xfer.actual_length)
		r.completed.Add(1)

		var data []byte
		if n > 0 {
			data = make([]byte, n)
			C.memcpy(unsafe.Pointer(&data[0]), unsafe.Pointer(xfer.buffer), C.size_t(n))
		}

		// Resubmit IMMEDIATELY before channel send — keeps the USB
		// controller armed with zero dead time.
		if rc := C.libusb_submit_transfer(xfer); rc != 0 {
			r.pending.Add(-1)
			r.submitted.Add(1) // count the failed attempt
			if n > 0 {
				select {
				case r.results <- RingResult{Data: data}:
				case <-r.stopCh:
				}
			}
			if r.pending.Load() == 0 {
				close(r.results)
			}
			return
		}
		r.submitted.Add(1)

		if n > 0 {
			select {
			case r.results <- RingResult{Data: data}:
			case <-r.stopCh:
			}
		}
		return
	}

	// CANCELLED, error, or stopped: don't resubmit.
	r.pending.Add(-1)

	if xfer.status != C.LIBUSB_TRANSFER_CANCELLED && !r.stopped.Load() {
		select {
		case r.results <- RingResult{
			Err: fmt.Errorf("async bulk transfer: libusb status %d", int(xfer.status)),
		}:
		case <-r.stopCh:
		}
	}

	if r.pending.Load() == 0 {
		close(r.results)
	}
}

// eventLoop processes libusb events until all pending transfers complete.
// Must run on a locked OS thread.
func (r *BulkRing) eventLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(r.evtDone)

	tv := C.struct_timeval{tv_sec: 1}
	for r.pending.Load() > 0 {
		C.libusb_handle_events_timeout(r.ctx, &tv)
	}
}

// cleanup frees the first n transfer/buffer pairs and deletes the cgo handle.
func (r *BulkRing) cleanup(n int) {
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
