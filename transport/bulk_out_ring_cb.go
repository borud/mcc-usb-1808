package transport

/*
#include <libusb.h>
*/
import "C"

import "runtime/cgo"

//export goOutRingCallback
func goOutRingCallback(xfer *C.struct_libusb_transfer) {
	h := cgo.Handle(uintptr(xfer.user_data))
	ring := h.Value().(*bulkOutRing)
	ring.onOutTransferComplete(xfer)
}
