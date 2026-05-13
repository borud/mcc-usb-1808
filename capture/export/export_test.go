package export

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/borud/mcc-usb-1808/capture"
	parquet "github.com/parquet-go/parquet-go"
	"github.com/xuri/excelize/v2"
	_ "modernc.org/sqlite"
)

// testFrames are 5 frames of known raw uint32 data for 2 channels:
//
//	ch0: AnalogIn BP10V (midpoint 131072 = 0V, slope=1, offset=0)
//	ch1: DigitalIO (raw passthrough)
var testFrameData = [][]uint32{
	{131072, 0xFF},  // 0V, 255
	{131073, 0x00},  // ~0V, 0
	{131074, 0x01},  // ~0V, 1
	{0, 0x02},       // -10V, 2
	{0x3FFFF, 0x03}, // ~+10V, 3
}

func testHeader() capture.Header {
	return capture.Header{
		DeviceModel:  "USB-1808X",
		DeviceSerial: "12345678",
		FPGAVersion:  "1.5",
		Channels: []capture.Channel{
			{Index: 0, Type: capture.AnalogIn, Range: 0, Name: "voltage",
				Cal: &capture.CalEntry{Slope: 1.0, Offset: 0.0}},
			{Index: 8, Type: capture.DigitalIO, Name: "trigger"},
		},
		SampleRate:      1000,
		Format:          capture.RawUint32,
		Timestamp:       1700000000000,
		SessionID:       "test-session",
		Operator:        "test-operator",
		ApplicationName: "test-app",
		Description:     "test capture",
		Properties:      map[string]string{"env": "lab"},
	}
}

func testCaptureBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	h := testHeader()
	w, err := capture.NewWriter(&buf, h)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range testFrameData {
		if err := w.WriteFrame(f); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testReader(t *testing.T) *capture.Reader {
	t.Helper()
	r, err := capture.NewReader(bytes.NewReader(testCaptureBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// expectedValues returns the calibrated float64 values for testFrameData.
func expectedValues() [][]float64 {
	h := testHeader()
	vals := make([][]float64, len(testFrameData))
	for i, raw := range testFrameData {
		// Reconstruct by writing/reading through the capture package.
		// For BP10V with slope=1, offset=0: (round(raw & 0x3FFFF) - 131072) * 10 / 131072
		// For DigitalIO: float64(raw)
		_ = h
		frame := make([]float64, 2)

		raw18 := float64(raw[0] & 0x3FFFF)
		cal := math.Round(raw18)
		frame[0] = (cal - 131072.0) * 10.0 / 131072.0

		frame[1] = float64(raw[1])
		vals[i] = frame
	}
	return vals
}

// TestColumnNames_Named verifies that named channels use their names as column headers.
func TestColumnNames_Named(t *testing.T) {
	chs := []capture.Channel{
		{Name: "voltage"},
		{Name: "current"},
	}
	names := columnNames(chs)
	if names[0] != "voltage" || names[1] != "current" {
		t.Errorf("got %v", names)
	}
}

func TestColumnNames_Unnamed(t *testing.T) {
	chs := []capture.Channel{{}, {}, {}}
	names := columnNames(chs)
	if names[0] != "ch0" || names[1] != "ch1" || names[2] != "ch2" {
		t.Errorf("got %v", names)
	}
}

func TestColumnNames_Dedup(t *testing.T) {
	chs := []capture.Channel{
		{Name: "sensor"},
		{Name: "sensor"},
		{Name: "sensor"},
	}
	names := columnNames(chs)
	if names[0] != "sensor" || names[1] != "sensor_2" || names[2] != "sensor_3" {
		t.Errorf("got %v", names)
	}
}

// TestCSV_Basic verifies CSV export including metadata comments, headers, and data values.
func TestCSV_Basic(t *testing.T) {
	r := testReader(t)
	defer r.Close()

	var buf bytes.Buffer
	if err := CSV(&buf, r); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	expected := expectedValues()

	// Check metadata comments.
	if !strings.Contains(output, "# device_model: USB-1808X") {
		t.Error("missing device_model comment")
	}
	if !strings.Contains(output, "# session_id: test-session") {
		t.Error("missing session_id comment")
	}
	if !strings.Contains(output, "# property.env: lab") {
		t.Error("missing property comment")
	}

	// Parse CSV data (skip comments).
	cr := csv.NewReader(strings.NewReader(output))
	cr.Comment = '#'

	records, err := cr.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	// Header row + 5 data rows.
	if len(records) != 6 {
		t.Fatalf("got %d records, want 6", len(records))
	}

	// Check header.
	if records[0][0] != "timestamp_s" || records[0][1] != "voltage" || records[0][2] != "trigger" {
		t.Errorf("header = %v", records[0])
	}

	// Check data values (spot-check first and last frames).
	// Frame 0: timestamp=0, voltage=0V, trigger=255.
	if records[1][0] != "0" {
		t.Errorf("frame 0 timestamp = %q, want \"0\"", records[1][0])
	}
	if records[1][1] != "0" {
		t.Errorf("frame 0 voltage = %q, want \"0\"", records[1][1])
	}
	if records[1][2] != "255" {
		t.Errorf("frame 0 trigger = %q, want \"255\"", records[1][2])
	}

	// Frame 3: timestamp=0.003, voltage=-10V, trigger=2.
	if records[4][1] != "-10" {
		t.Errorf("frame 3 voltage = %q, want \"-10\"", records[4][1])
	}
	_ = expected
}

func TestCSV_EmptyCapture(t *testing.T) {
	var capBuf bytes.Buffer
	h := testHeader()
	w, _ := capture.NewWriter(&capBuf, h)
	w.Close()

	r, _ := capture.NewReader(bytes.NewReader(capBuf.Bytes()))
	defer r.Close()

	var buf bytes.Buffer
	if err := CSV(&buf, r); err != nil {
		t.Fatal(err)
	}

	cr := csv.NewReader(strings.NewReader(buf.String()))
	cr.Comment = '#'
	records, _ := cr.ReadAll()
	if len(records) != 1 { // header only
		t.Errorf("got %d records, want 1 (header only)", len(records))
	}
}

// TestExcel_Basic verifies Excel export including Data and Metadata sheets.
func TestExcel_Basic(t *testing.T) {
	r := testReader(t)
	defer r.Close()

	path := filepath.Join(t.TempDir(), "test.xlsx")
	if err := Excel(path, r); err != nil {
		t.Fatal(err)
	}

	// Read back with excelize.
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// Check sheets exist.
	sheets := f.GetSheetList()
	if len(sheets) < 2 {
		t.Fatalf("expected at least 2 sheets, got %v", sheets)
	}

	// Check Data sheet header.
	val, _ := f.GetCellValue("Data", "A1")
	if val != "timestamp_s" {
		t.Errorf("A1 = %q, want \"timestamp_s\"", val)
	}
	val, _ = f.GetCellValue("Data", "B1")
	if val != "voltage" {
		t.Errorf("B1 = %q, want \"voltage\"", val)
	}
	val, _ = f.GetCellValue("Data", "C1")
	if val != "trigger" {
		t.Errorf("C1 = %q, want \"trigger\"", val)
	}

	// Check data row count (header + 5 data rows).
	rows, _ := f.GetRows("Data")
	if len(rows) != 6 {
		t.Errorf("Data sheet has %d rows, want 6", len(rows))
	}

	// Spot-check first data row: timestamp=0, voltage=0, trigger=255.
	val, _ = f.GetCellValue("Data", "A2")
	if val != "0" {
		t.Errorf("A2 = %q, want \"0\"", val)
	}
	val, _ = f.GetCellValue("Data", "B2")
	if val != "0" {
		t.Errorf("B2 = %q, want \"0\"", val)
	}

	// Check Metadata sheet has content.
	val, _ = f.GetCellValue("Metadata", "A1")
	if val != "Device Model" {
		t.Errorf("Metadata A1 = %q, want \"Device Model\"", val)
	}
	val, _ = f.GetCellValue("Metadata", "B1")
	if val != "USB-1808X" {
		t.Errorf("Metadata B1 = %q, want \"USB-1808X\"", val)
	}
}

// TestSQLite_Basic verifies SQLite export including metadata, channels, and frame data.
func TestSQLite_Basic(t *testing.T) {
	r := testReader(t)
	defer r.Close()

	path := filepath.Join(t.TempDir(), "test.db")
	if err := SQLite(path, r); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Check metadata.
	var val string
	err = db.QueryRow("SELECT value FROM metadata WHERE key = ?", "device_model").Scan(&val)
	if err != nil {
		t.Fatal(err)
	}
	if val != "USB-1808X" {
		t.Errorf("device_model = %q, want \"USB-1808X\"", val)
	}

	err = db.QueryRow("SELECT value FROM metadata WHERE key = ?", "session_id").Scan(&val)
	if err != nil {
		t.Fatal(err)
	}
	if val != "test-session" {
		t.Errorf("session_id = %q, want \"test-session\"", val)
	}

	// Check properties.
	err = db.QueryRow("SELECT value FROM metadata WHERE key = ?", "property.env").Scan(&val)
	if err != nil {
		t.Fatal(err)
	}
	if val != "lab" {
		t.Errorf("property.env = %q, want \"lab\"", val)
	}

	// Check channels.
	var count int
	db.QueryRow("SELECT COUNT(*) FROM channels").Scan(&count)
	if count != 2 {
		t.Errorf("channels count = %d, want 2", count)
	}

	var name string
	db.QueryRow("SELECT name FROM channels WHERE position = 0").Scan(&name)
	if name != "voltage" {
		t.Errorf("channel 0 name = %q, want \"voltage\"", name)
	}

	// Check frames.
	db.QueryRow("SELECT COUNT(*) FROM frames").Scan(&count)
	if count != 5 {
		t.Errorf("frames count = %d, want 5", count)
	}

	// First frame: voltage=0V, trigger=255.
	var voltage, trigger float64
	err = db.QueryRow(`SELECT "voltage", "trigger" FROM frames WHERE frame_id = 0`).Scan(&voltage, &trigger)
	if err != nil {
		t.Fatal(err)
	}
	if voltage != 0.0 {
		t.Errorf("frame 0 voltage = %f, want 0.0", voltage)
	}
	if trigger != 255.0 {
		t.Errorf("frame 0 trigger = %f, want 255.0", trigger)
	}

	// Frame 3: voltage=-10V, trigger=2.
	err = db.QueryRow(`SELECT "voltage", "trigger" FROM frames WHERE frame_id = 3`).Scan(&voltage, &trigger)
	if err != nil {
		t.Fatal(err)
	}
	if voltage != -10.0 {
		t.Errorf("frame 3 voltage = %f, want -10.0", voltage)
	}
	if trigger != 2.0 {
		t.Errorf("frame 3 trigger = %f, want 2.0", trigger)
	}

	// Verify timestamps.
	var ts float64
	db.QueryRow("SELECT timestamp_s FROM frames WHERE frame_id = 4").Scan(&ts)
	want := 4.0 / 1000.0
	if math.Abs(ts-want) > 1e-9 {
		t.Errorf("frame 4 timestamp = %f, want %f", ts, want)
	}
}

func TestSQLite_Cleanup(t *testing.T) {
	// Verify the database file is properly closed and accessible after export.
	r := testReader(t)
	defer r.Close()

	path := filepath.Join(t.TempDir(), "test.db")
	if err := SQLite(path, r); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("database file is empty")
	}
}

// TestWAV_Basic verifies WAV export including RIFF header, fmt chunk, and sample data.
func TestWAV_Basic(t *testing.T) {
	r := testReader(t)
	defer r.Close()

	var buf bytes.Buffer
	if err := WAV(&buf, r); err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()

	// Check RIFF header.
	if string(data[0:4]) != "RIFF" {
		t.Errorf("missing RIFF magic")
	}
	if string(data[8:12]) != "WAVE" {
		t.Errorf("missing WAVE format")
	}

	// Check fmt chunk.
	if string(data[12:16]) != "fmt " {
		t.Errorf("missing fmt chunk")
	}
	format := binary.LittleEndian.Uint16(data[20:22])
	if format != 3 { // IEEE float
		t.Errorf("format = %d, want 3 (IEEE float)", format)
	}
	numChannels := binary.LittleEndian.Uint16(data[22:24])
	if numChannels != 2 {
		t.Errorf("channels = %d, want 2", numChannels)
	}
	sampleRate := binary.LittleEndian.Uint32(data[24:28])
	if sampleRate != 1000 {
		t.Errorf("sample rate = %d, want 1000", sampleRate)
	}
	bitsPerSample := binary.LittleEndian.Uint16(data[34:36])
	if bitsPerSample != 32 {
		t.Errorf("bits per sample = %d, want 32", bitsPerSample)
	}

	// Check data chunk.
	if string(data[36:40]) != "data" {
		t.Errorf("missing data chunk")
	}
	dataSize := binary.LittleEndian.Uint32(data[40:44])
	expectedSize := uint32(5 * 2 * 4) // 5 frames, 2 channels, 4 bytes each
	if dataSize != expectedSize {
		t.Errorf("data size = %d, want %d", dataSize, expectedSize)
	}

	// Verify total file size.
	expectedTotal := 44 + int(expectedSize)
	if len(data) != expectedTotal {
		t.Errorf("total size = %d, want %d", len(data), expectedTotal)
	}

	// Spot-check first sample (channel 0 = 0V = 0.0 normalized).
	sample0 := math.Float32frombits(binary.LittleEndian.Uint32(data[44:48]))
	if sample0 != 0.0 {
		t.Errorf("first sample = %f, want 0.0", sample0)
	}
}

func TestWAV_EmptyCapture(t *testing.T) {
	var capBuf bytes.Buffer
	h := testHeader()
	w, _ := capture.NewWriter(&capBuf, h)
	w.Close()

	r, _ := capture.NewReader(bytes.NewReader(capBuf.Bytes()))
	defer r.Close()

	var buf bytes.Buffer
	if err := WAV(&buf, r); err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	// Should be a valid 44-byte WAV header with 0 data bytes.
	if len(data) != 44 {
		t.Errorf("empty WAV size = %d, want 44", len(data))
	}
	dataSize := binary.LittleEndian.Uint32(data[40:44])
	if dataSize != 0 {
		t.Errorf("data size = %d, want 0", dataSize)
	}
}

// TestWAV_SamplesInRange verifies all WAV samples are normalized to the [-1, 1] range.
func TestWAV_SamplesInRange(t *testing.T) {
	r := testReader(t)
	defer r.Close()

	var buf bytes.Buffer
	if err := WAV(&buf, r); err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	numSamples := int(binary.LittleEndian.Uint32(data[40:44])) / 4

	for i := range numSamples {
		offset := 44 + i*4
		sample := math.Float32frombits(binary.LittleEndian.Uint32(data[offset : offset+4]))
		if sample < -1.0 || sample > 1.0 {
			t.Errorf("sample %d = %f, out of [-1, 1] range", i, sample)
		}
	}
}

// TestSQLite_LargeBatch verifies SQLite export across batch boundaries (2500 frames crossing the 1000-row batch size).
func TestSQLite_LargeBatch(t *testing.T) {
	const numFrames = 2500 // crosses batch boundary (1000)

	var capBuf bytes.Buffer
	h := testHeader()
	w, _ := capture.NewWriter(&capBuf, h)
	for i := range numFrames {
		w.WriteFrame([]uint32{uint32(131072 + i), uint32(i)})
	}
	w.Close()

	r, _ := capture.NewReader(bytes.NewReader(capBuf.Bytes()))
	defer r.Close()

	path := filepath.Join(t.TempDir(), "large.db")
	if err := SQLite(path, r); err != nil {
		t.Fatal(err)
	}

	db, _ := sql.Open("sqlite", path)
	defer db.Close()

	var count int
	db.QueryRow("SELECT COUNT(*) FROM frames").Scan(&count)
	if count != numFrames {
		t.Errorf("frames = %d, want %d", count, numFrames)
	}
}

type parquetValueRow struct {
	FrameID    int64   `parquet:"frame_id"`
	TimestampS float64 `parquet:"timestamp_s"`
	Voltage    float64 `parquet:"voltage"`
	Trigger    float64 `parquet:"trigger"`
}

type parquetRawRow struct {
	FrameID    int64   `parquet:"frame_id"`
	TimestampS float64 `parquet:"timestamp_s"`
	Voltage    float64 `parquet:"voltage"`
	VoltageRaw uint32  `parquet:"voltage_raw"`
	Trigger    float64 `parquet:"trigger"`
	TriggerRaw uint32  `parquet:"trigger_raw"`
}

func TestParquet_Basic(t *testing.T) {
	r := testReader(t)
	defer r.Close()

	var buf bytes.Buffer
	if err := Parquet(&buf, r); err != nil {
		t.Fatal(err)
	}

	pr := parquet.NewGenericReader[parquetValueRow](bytes.NewReader(buf.Bytes()))
	defer pr.Close()

	if pr.NumRows() != int64(len(testFrameData)) {
		t.Fatalf("rows = %d, want %d", pr.NumRows(), len(testFrameData))
	}

	rows := make([]parquetValueRow, len(testFrameData))
	n, err := pr.Read(rows)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if n != len(testFrameData) {
		t.Fatalf("read %d rows, want %d", n, len(testFrameData))
	}

	expected := expectedValues()
	if rows[0].FrameID != 0 || rows[0].TimestampS != 0 {
		t.Errorf("first row frame metadata = %+v", rows[0])
	}
	if rows[0].Voltage != expected[0][0] || rows[0].Trigger != expected[0][1] {
		t.Errorf("first row values = %+v, want voltage=%v trigger=%v", rows[0], expected[0][0], expected[0][1])
	}
	if rows[4].FrameID != 4 {
		t.Errorf("last frame_id = %d, want 4", rows[4].FrameID)
	}
	if math.Abs(rows[4].TimestampS-0.004) > 1e-12 {
		t.Errorf("last timestamp = %f, want 0.004", rows[4].TimestampS)
	}

	meta := readParquetMetadata(t, pr.File())
	if meta.RawIncluded {
		t.Error("RawIncluded = true, want false")
	}
	if meta.ExportedFrameCount != uint64(len(testFrameData)) {
		t.Errorf("exported_frame_count = %d, want %d", meta.ExportedFrameCount, len(testFrameData))
	}
	if meta.Header.DeviceSerial != "12345678" {
		t.Errorf("metadata device serial = %q", meta.Header.DeviceSerial)
	}
	if hasParquetColumn(meta.Columns, parquetColumnRoleRaw, 0) {
		t.Error("metadata unexpectedly contains raw column")
	}
}

func TestParquet_WithRaw(t *testing.T) {
	r := testReader(t)
	defer r.Close()

	var buf bytes.Buffer
	if err := Parquet(&buf, r, WithRaw()); err != nil {
		t.Fatal(err)
	}

	pr := parquet.NewGenericReader[parquetRawRow](bytes.NewReader(buf.Bytes()))
	defer pr.Close()

	rows := make([]parquetRawRow, len(testFrameData))
	n, err := pr.Read(rows)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if n != len(testFrameData) {
		t.Fatalf("read %d rows, want %d", n, len(testFrameData))
	}

	for i, row := range rows {
		if row.VoltageRaw != testFrameData[i][0] {
			t.Errorf("row %d voltage_raw = %d, want %d", i, row.VoltageRaw, testFrameData[i][0])
		}
		if row.TriggerRaw != testFrameData[i][1] {
			t.Errorf("row %d trigger_raw = %d, want %d", i, row.TriggerRaw, testFrameData[i][1])
		}
	}

	meta := readParquetMetadata(t, pr.File())
	if !meta.RawIncluded {
		t.Error("RawIncluded = false, want true")
	}
	if !hasParquetColumn(meta.Columns, parquetColumnRoleRaw, 0) {
		t.Error("metadata missing raw column for channel 0")
	}
	if !hasParquetColumn(meta.Columns, parquetColumnRoleRaw, 1) {
		t.Error("metadata missing raw column for channel 1")
	}
}

func TestParquet_EmptyCapture(t *testing.T) {
	var capBuf bytes.Buffer
	h := testHeader()
	w, _ := capture.NewWriter(&capBuf, h)
	w.Close()

	r, _ := capture.NewReader(bytes.NewReader(capBuf.Bytes()))
	defer r.Close()

	var buf bytes.Buffer
	if err := Parquet(&buf, r, WithRaw()); err != nil {
		t.Fatal(err)
	}

	pr := parquet.NewGenericReader[parquetRawRow](bytes.NewReader(buf.Bytes()))
	defer pr.Close()

	if pr.NumRows() != 0 {
		t.Errorf("rows = %d, want 0", pr.NumRows())
	}
	meta := readParquetMetadata(t, pr.File())
	if meta.ExportedFrameCount != 0 {
		t.Errorf("exported_frame_count = %d, want 0", meta.ExportedFrameCount)
	}
}

func readParquetMetadata(t *testing.T, f parquet.FileView) parquetFileMetadata {
	t.Helper()
	value, ok := f.Lookup(parquetMetadataKey)
	if !ok {
		t.Fatalf("missing parquet metadata key %q", parquetMetadataKey)
	}
	var meta parquetFileMetadata
	if err := json.Unmarshal([]byte(value), &meta); err != nil {
		t.Fatal(err)
	}
	return meta
}

func hasParquetColumn(columns []parquetColumn, role string, position int) bool {
	for _, col := range columns {
		if col.Role == role && col.ChannelPosition == position {
			return true
		}
	}
	return false
}

// Verify io.ReadCloser isn't needed (we don't close the reader prematurely).
func TestCSV_ReaderNotClosed(t *testing.T) {
	r := testReader(t)
	// Don't defer r.Close() — let CSV consume it, then close.
	var buf bytes.Buffer
	if err := CSV(&buf, r); err != nil {
		t.Fatal(err)
	}
	// Reader should still be closeable.
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}
