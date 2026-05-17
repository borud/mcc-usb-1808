package transport

/*
#include <libusb.h>
*/
import "C"

import "runtime/cgo"

//export goRingCallback
func goRingCallback(xfer *C.struct_libusb_transfer) {
	h := cgo.Handle(uintptr(xfer.user_data))
	ring := h.Value().(*BulkRing)
	ring.onTransferComplete(xfer)
}
