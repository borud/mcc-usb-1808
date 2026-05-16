package export

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"

	"github.com/borud/mcc-usb-1808/v3/capture"
)

// CSV writes all remaining frames from r as CSV to w.
//
// Capture metadata is written as # comment lines before the data.
// The first data column is the timestamp in seconds from the start of the
// capture, computed as frameIndex / sampleRate. Remaining columns are the
// calibrated channel values from [capture.Frame.Values].
//
// Column names are taken from [capture.Channel.Name], falling back to
// "ch0", "ch1", etc. for unnamed channels.
func CSV(w io.Writer, r *capture.Reader) error {
	h := r.Header()
	cols := columnNames(h.Channels)

	// Write metadata as comments.
	writeComment := func(key, value string) {
		if value != "" {
			fmt.Fprintf(w, "# %s: %s\n", key, value)
		}
	}
	writeComment("device_model", h.DeviceModel)
	writeComment("device_serial", h.DeviceSerial)
	writeComment("fpga_version", h.FPGAVersion)
	if !h.CalibrationDate.IsZero() {
		writeComment("calibration_date", h.CalibrationDate.Format("2006-01-02"))
	}
	writeComment("sample_rate", strconv.FormatFloat(h.SampleRate, 'f', -1, 64))
	if h.FrameCount > 0 {
		writeComment("frame_count", strconv.FormatUint(h.FrameCount, 10))
	}
	if h.Timestamp > 0 {
		writeComment("timestamp", strconv.FormatInt(h.Timestamp, 10))
	}
	writeComment("application_name", h.ApplicationName)
	writeComment("session_id", h.SessionID)
	writeComment("description", h.Description)
	writeComment("operator", h.Operator)
	for k, v := range h.Properties {
		writeComment("property."+k, v)
	}

	cw := csv.NewWriter(w)

	// Header row.
	header := make([]string, 1+len(cols))
	header[0] = "timestamp_s"
	copy(header[1:], cols)
	if err := cw.Write(header); err != nil {
		return err
	}

	// Data rows.
	row := make([]string, 1+len(cols))
	frameIdx := 0
	for frame, err := range r.Frames() {
		if err != nil {
			return err
		}
		row[0] = strconv.FormatFloat(float64(frameIdx)/h.SampleRate, 'g', -1, 64)
		vals := frame.Values()
		for i, v := range vals {
			row[i+1] = strconv.FormatFloat(v, 'g', -1, 64)
		}
		if err := cw.Write(row); err != nil {
			return err
		}
		frameIdx++
	}

	cw.Flush()
	return cw.Error()
}
