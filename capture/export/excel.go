package export

import (
	"fmt"
	"strconv"

	"github.com/borud/mcc-usb-1808/capture"
	"github.com/xuri/excelize/v2"
)

// Excel writes all remaining frames from r as an Excel .xlsx file at path.
//
// The workbook contains two sheets:
//   - "Data": timestamp_s column followed by one column per channel with
//     calibrated values. Uses a streaming writer to handle large captures.
//   - "Metadata": key-value pairs of capture metadata and properties.
//
// Excel has a hard limit of 1,048,576 rows per sheet. Captures exceeding
// this limit will be truncated.
func Excel(path string, r *capture.Reader) error {
	h := r.Header()
	cols := columnNames(h.Channels)

	f := excelize.NewFile()
	defer f.Close()

	// --- Metadata sheet ---
	const metaSheet = "Metadata"
	if _, err := f.NewSheet(metaSheet); err != nil {
		return err
	}

	metaRow := 1
	setMeta := func(key, value string) {
		if value == "" {
			return
		}
		f.SetCellValue(metaSheet, fmt.Sprintf("A%d", metaRow), key)
		f.SetCellValue(metaSheet, fmt.Sprintf("B%d", metaRow), value)
		metaRow++
	}

	setMeta("Device Model", h.DeviceModel)
	setMeta("Device Serial", h.DeviceSerial)
	setMeta("FPGA Version", h.FPGAVersion)
	if !h.CalibrationDate.IsZero() {
		setMeta("Calibration Date", h.CalibrationDate.Format("2006-01-02"))
	}
	setMeta("Sample Rate (Hz)", strconv.FormatFloat(h.SampleRate, 'f', -1, 64))
	if h.FrameCount > 0 {
		setMeta("Frame Count", strconv.FormatUint(h.FrameCount, 10))
	}
	if h.Timestamp > 0 {
		setMeta("Timestamp (ms)", strconv.FormatInt(h.Timestamp, 10))
	}
	setMeta("Application", h.ApplicationName)
	setMeta("Session ID", h.SessionID)
	setMeta("Description", h.Description)
	setMeta("Operator", h.Operator)
	for k, v := range h.Properties {
		setMeta(k, v)
	}

	// --- Data sheet (streaming for large captures) ---
	const dataSheet = "Sheet1"
	f.SetSheetName(dataSheet, "Data")

	sw, err := f.NewStreamWriter("Data")
	if err != nil {
		return err
	}

	// Header row.
	headerRow := make([]any, 1+len(cols))
	headerRow[0] = excelize.Cell{Value: "timestamp_s"}
	for i, name := range cols {
		headerRow[i+1] = excelize.Cell{Value: name}
	}
	cell, _ := excelize.CoordinatesToCellName(1, 1)
	if err := sw.SetRow(cell, headerRow); err != nil {
		return err
	}

	// Data rows.
	dataRow := make([]any, 1+len(cols))
	rowNum := 2
	frameIdx := 0
	for frame, fErr := range r.Frames() {
		if fErr != nil {
			return fErr
		}
		dataRow[0] = float64(frameIdx) / h.SampleRate
		vals := frame.Values()
		for i, v := range vals {
			dataRow[i+1] = v
		}
		cell, _ := excelize.CoordinatesToCellName(1, rowNum)
		if err := sw.SetRow(cell, dataRow); err != nil {
			return err
		}
		rowNum++
		frameIdx++
	}

	if err := sw.Flush(); err != nil {
		return err
	}

	return f.SaveAs(path)
}
