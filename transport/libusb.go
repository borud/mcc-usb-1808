package transport

/*
#cgo pkg-config: libusb-1.0
#include <libusb.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"sync"
	"time"
	"unsafe"
)

// LibUSBTransport implements Transport using libusb.
type LibUSBTransport struct {
	ctx    *C.libusb_context
	handle *C.libusb_device_handle
	mu     sync.Mutex
}

// Discover returns all connected USB devices matching any of the given product IDs.
func Discover(vendorID uint16, productIDs []uint16) ([]DeviceInfo, error) {
	var ctx *C.libusb_context
	if rc := C.libusb_init(&ctx); rc != 0 {
		return nil, fmt.Errorf("libusb_init: %s", C.GoString(C.libusb_strerror(rc)))
	}
	defer C.libusb_exit(ctx)

	var list **C.libusb_device
	n := C.libusb_get_device_list(ctx, &list)
	if n < 0 {
		return nil, fmt.Errorf("libusb_get_device_list: %s", C.GoString(C.libusb_strerror(C.int(n))))
	}
	defer C.libusb_free_device_list(list, 1)

	pidSet := make(map[uint16]bool, len(productIDs))
	for _, pid := range productIDs {
		pidSet[pid] = true
	}

	devices := unsafe.Slice(list, int(n))
	var result []DeviceInfo
	for _, dev := range devices {
		var desc C.struct_libusb_device_descriptor
		if C.libusb_get_device_descriptor(dev, &desc) != 0 {
			continue
		}
		vid := uint16(desc.idVendor)
		pid := uint16(desc.idProduct)
		if vid != vendorID || !pidSet[pid] {
			continue
		}
		result = append(result, DeviceInfo{
			VendorID:  vid,
			ProductID: pid,
			Bus:       uint8(C.libusb_get_bus_number(dev)),
			Address:   uint8(C.libusb_get_device_address(dev)),
		})
	}
	return result, nil
}

// OpenLibUSBAt opens a USB device at a specific bus and address.
func OpenLibUSBAt(vendorID uint16, bus, addr uint8) (*LibUSBTransport, error) {
	var ctx *C.libusb_context
	if rc := C.libusb_init(&ctx); rc != 0 {
		return nil, fmt.Errorf("libusb_init: %s", C.GoString(C.libusb_strerror(rc)))
	}

	var list **C.libusb_device
	n := C.libusb_get_device_list(ctx, &list)
	if n < 0 {
		C.libusb_exit(ctx)
		return nil, fmt.Errorf("libusb_get_device_list: %s", C.GoString(C.libusb_strerror(C.int(n))))
	}
	defer C.libusb_free_device_list(list, 1)

	devices := unsafe.Slice(list, int(n))
	for _, dev := range devices {
		if C.libusb_get_bus_number(dev) != C.uint8_t(bus) ||
			C.libusb_get_device_address(dev) != C.uint8_t(addr) {
			continue
		}
		var desc C.struct_libusb_device_descriptor
		if C.libusb_get_device_descriptor(dev, &desc) != 0 {
			continue
		}
		if uint16(desc.idVendor) != vendorID {
			continue
		}

		var handle *C.libusb_device_handle
		if rc := C.libusb_open(dev, &handle); rc != 0 {
			C.libusb_exit(ctx)
			return nil, fmt.Errorf("libusb_open: %s", C.GoString(C.libusb_strerror(rc)))
		}
		if C.libusb_kernel_driver_active(handle, 0) == 1 {
			C.libusb_detach_kernel_driver(handle, 0)
		}
		if rc := C.libusb_claim_interface(handle, 0); rc != 0 {
			C.libusb_close(handle)
			C.libusb_exit(ctx)
			return nil, fmt.Errorf("libusb_claim_interface: %s", C.GoString(C.libusb_strerror(rc)))
		}
		return &LibUSBTransport{ctx: ctx, handle: handle}, nil
	}

	C.libusb_exit(ctx)
	return nil, fmt.Errorf("device not found at bus %d address %d", bus, addr)
}

// OpenLibUSB opens a USB device by vendor and product ID.
func OpenLibUSB(vendorID, productID uint16) (*LibUSBTransport, error) {
	var ctx *C.libusb_context
	if rc := C.libusb_init(&ctx); rc != 0 {
		return nil, fmt.Errorf("libusb_init: %s", C.GoString(C.libusb_strerror(rc)))
	}

	handle := C.libusb_open_device_with_vid_pid(ctx, C.uint16_t(vendorID), C.uint16_t(productID))
	if handle == nil {
		C.libusb_exit(ctx)
		return nil, fmt.Errorf("device %04x:%04x not found", vendorID, productID)
	}

	// Detach kernel driver if attached.
	if C.libusb_kernel_driver_active(handle, 0) == 1 {
		C.libusb_detach_kernel_driver(handle, 0)
	}

	if rc := C.libusb_claim_interface(handle, 0); rc != 0 {
		C.libusb_close(handle)
		C.libusb_exit(ctx)
		return nil, fmt.Errorf("libusb_claim_interface: %s", C.GoString(C.libusb_strerror(rc)))
	}

	return &LibUSBTransport{ctx: ctx, handle: handle}, nil
}

// ControlOut implements Transport.
func (t *LibUSBTransport) ControlOut(request uint8, wValue, wIndex uint16, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	var dataPtr *C.uchar
	if len(data) > 0 {
		dataPtr = (*C.uchar)(unsafe.Pointer(&data[0]))
	}

	rc := C.libusb_control_transfer(
		t.handle,
		C.uint8_t(0x40), // HOST_TO_DEVICE | VENDOR_TYPE | DEVICE_RECIPIENT
		C.uint8_t(request),
		C.uint16_t(wValue),
		C.uint16_t(wIndex),
		dataPtr,
		C.uint16_t(len(data)),
		2000,
	)
	if rc < 0 {
		return fmt.Errorf("control out 0x%02x: %s", request, C.GoString(C.libusb_strerror(rc)))
	}
	return nil
}

// ControlIn implements Transport.
func (t *LibUSBTransport) ControlIn(request uint8, wValue, wIndex uint16, length int) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	buf := make([]byte, length)
	var dataPtr *C.uchar
	if length > 0 {
		dataPtr = (*C.uchar)(unsafe.Pointer(&buf[0]))
	}

	rc := C.libusb_control_transfer(
		t.handle,
		C.uint8_t(0xC0), // DEVICE_TO_HOST | VENDOR_TYPE | DEVICE_RECIPIENT
		C.uint8_t(request),
		C.uint16_t(wValue),
		C.uint16_t(wIndex),
		dataPtr,
		C.uint16_t(length),
		2000,
	)
	if rc < 0 {
		return nil, fmt.Errorf("control in 0x%02x: %s", request, C.GoString(C.libusb_strerror(rc)))
	}
	return buf[:int(rc)], nil
}

// BulkRead implements Transport.
// libusb 1.0 is thread-safe so no mutex is needed for bulk transfers.
// This allows concurrent bulk reads while control transfers proceed
// independently on the default endpoint.
func (t *LibUSBTransport) BulkRead(endpoint uint8, length int, timeout time.Duration) ([]byte, error) {
	if length <= 0 {
		return nil, nil
	}
	buf := make([]byte, length)
	var transferred C.int

	rc := C.libusb_bulk_transfer(
		t.handle,
		C.uchar(endpoint),
		(*C.uchar)(unsafe.Pointer(&buf[0])),
		C.int(length),
		&transferred,
		C.uint(timeout.Milliseconds()),
	)
	if rc != 0 {
		return buf[:int(transferred)], fmt.Errorf("bulk read EP 0x%02x: %s", endpoint, C.GoString(C.libusb_strerror(rc)))
	}
	return buf[:int(transferred)], nil
}

// BulkReadInto implements Transport.
// Like BulkRead but reads into a caller-provided buffer to avoid allocation.
func (t *LibUSBTransport) BulkReadInto(endpoint uint8, buf []byte, timeout time.Duration) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	var transferred C.int
	rc := C.libusb_bulk_transfer(
		t.handle,
		C.uchar(endpoint),
		(*C.uchar)(unsafe.Pointer(&buf[0])),
		C.int(len(buf)),
		&transferred,
		C.uint(timeout.Milliseconds()),
	)
	if rc != 0 {
		return int(transferred), fmt.Errorf("bulk read EP 0x%02x: %s", endpoint, C.GoString(C.libusb_strerror(rc)))
	}
	return int(transferred), nil
}

// BulkWrite implements Transport.
func (t *LibUSBTransport) BulkWrite(endpoint uint8, data []byte, timeout time.Duration) (int, error) {
	var dataPtr *C.uchar
	if len(data) > 0 {
		dataPtr = (*C.uchar)(unsafe.Pointer(&data[0]))
	}
	var transferred C.int

	rc := C.libusb_bulk_transfer(
		t.handle,
		C.uchar(endpoint),
		dataPtr,
		C.int(len(data)),
		&transferred,
		C.uint(timeout.Milliseconds()),
	)
	if rc != 0 {
		return int(transferred), fmt.Errorf("bulk write EP 0x%02x: %s", endpoint, C.GoString(C.libusb_strerror(rc)))
	}
	return int(transferred), nil
}

// NewBulkRing implements AsyncBulkReader.
func (t *LibUSBTransport) NewBulkRing(endpoint uint8, bufSize, count, depth int, timeoutMs uint) (BulkRing, error) {
	return NewBulkRing(t.ctx, t.handle, endpoint, bufSize, count, depth, timeoutMs)
}

// NewBulkOutRing implements AsyncBulkWriter.
func (t *LibUSBTransport) NewBulkOutRing(endpoint uint8, bufSize, count int, source BulkOutSource, timeoutMs uint) (BulkOutRing, error) {
	return NewBulkOutRing(t.ctx, t.handle, endpoint, bufSize, count, source, timeoutMs)
}

// Close implements Transport.
func (t *LibUSBTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.handle != nil {
		C.libusb_release_interface(t.handle, 0)
		C.libusb_close(t.handle)
		t.handle = nil
	}
	if t.ctx != nil {
		C.libusb_exit(t.ctx)
		t.ctx = nil
	}
	return nil
}
