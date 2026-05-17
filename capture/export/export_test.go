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

	"github.com/borud/mcc-usb-1808/v4/capture"
	parquet "github.com/parquet-go/parquet-go"
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

func framesToBulk(frames [][]uint32) []byte {
	if len(frames) == 0 {
		return nil
	}
	numCh := len(frames[0])
	buf := make([]byte, len(frames)*numCh*4)
	for i, vals := range frames {
		for ch, v := range vals {
			binary.LittleEndian.PutUint32(buf[(i*numCh+ch)*4:], v)
		}
	}
	return buf
}

func testCaptureDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "capture")
	h := testHeader()
	w, err := capture.NewWriter(dir, h)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteBulk(framesToBulk(testFrameData)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func testReader(t *testing.T) *capture.Reader {
	t.Helper()
	dir := testCaptureDir(t)
	r, err := capture.NewReader(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func emptyCaptureDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "capture")
	h := testHeader()
	w, err := capture.NewWriter(dir, h)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	return dir
}

// expectedValues returns the calibrated float64 values for testFrameData.
func expectedValues() [][]float64 {
	h := testHeader()
	vals := make([][]float64, len(testFrameData))
	for i, raw := range testFrameData {
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

func TestCSV_Basic(t *testing.T) {
	r := testReader(t)
	defer r.Close()

	var buf strings.Builder
	if err := CSV(&buf, r); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	expected := expectedValues()

	if !strings.Contains(output, "# device_model: USB-1808X") {
		t.Error("missing device_model comment")
	}
	if !strings.Contains(output, "# session_id: test-session") {
		t.Error("missing session_id comment")
	}
	if !strings.Contains(output, "# property.env: lab") {
		t.Error("missing property comment")
	}

	cr := csv.NewReader(strings.NewReader(output))
	cr.Comment = '#'

	records, err := cr.ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 6 {
		t.Fatalf("got %d records, want 6", len(records))
	}

	if records[0][0] != "timestamp_s" || records[0][1] != "voltage" || records[0][2] != "trigger" {
		t.Errorf("header = %v", records[0])
	}

	if records[1][0] != "0" {
		t.Errorf("frame 0 timestamp = %q, want \"0\"", records[1][0])
	}
	if records[1][1] != "0" {
		t.Errorf("frame 0 voltage = %q, want \"0\"", records[1][1])
	}
	if records[1][2] != "255" {
		t.Errorf("frame 0 trigger = %q, want \"255\"", records[1][2])
	}

	if records[4][1] != "-10" {
		t.Errorf("frame 3 voltage = %q, want \"-10\"", records[4][1])
	}
	_ = expected
}

func TestCSV_EmptyCapture(t *testing.T) {
	dir := emptyCaptureDir(t)
	r, _ := capture.NewReader(dir)
	defer r.Close()

	var buf strings.Builder
	if err := CSV(&buf, r); err != nil {
		t.Fatal(err)
	}

	cr := csv.NewReader(strings.NewReader(buf.String()))
	cr.Comment = '#'
	records, _ := cr.ReadAll()
	if len(records) != 1 {
		t.Errorf("got %d records, want 1 (header only)", len(records))
	}
}


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

	err = db.QueryRow("SELECT value FROM metadata WHERE key = ?", "property.env").Scan(&val)
	if err != nil {
		t.Fatal(err)
	}
	if val != "lab" {
		t.Errorf("property.env = %q, want \"lab\"", val)
	}

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

	db.QueryRow("SELECT COUNT(*) FROM frames").Scan(&count)
	if count != 5 {
		t.Errorf("frames count = %d, want 5", count)
	}

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

	var ts float64
	db.QueryRow("SELECT timestamp_s FROM frames WHERE frame_id = 4").Scan(&ts)
	want := 4.0 / 1000.0
	if math.Abs(ts-want) > 1e-9 {
		t.Errorf("frame 4 timestamp = %f, want %f", ts, want)
	}
}

func TestSQLite_Cleanup(t *testing.T) {
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

func TestWAV_Basic(t *testing.T) {
	r := testReader(t)
	defer r.Close()

	var buf strings.Builder
	// WAV needs a real bytes buffer, not strings.Builder.
	// Use a temp file instead.
	path := filepath.Join(t.TempDir(), "test.wav")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := WAV(f, r); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	_ = buf

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(data[0:4]) != "RIFF" {
		t.Errorf("missing RIFF magic")
	}
	if string(data[8:12]) != "WAVE" {
		t.Errorf("missing WAVE format")
	}

	if string(data[12:16]) != "fmt " {
		t.Errorf("missing fmt chunk")
	}
	format := binary.LittleEndian.Uint16(data[20:22])
	if format != 3 {
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

	if string(data[36:40]) != "data" {
		t.Errorf("missing data chunk")
	}
	dataSize := binary.LittleEndian.Uint32(data[40:44])
	expectedSize := uint32(5 * 2 * 4)
	if dataSize != expectedSize {
		t.Errorf("data size = %d, want %d", dataSize, expectedSize)
	}

	expectedTotal := 44 + int(expectedSize)
	if len(data) != expectedTotal {
		t.Errorf("total size = %d, want %d", len(data), expectedTotal)
	}

	sample0 := math.Float32frombits(binary.LittleEndian.Uint32(data[44:48]))
	if sample0 != 0.0 {
		t.Errorf("first sample = %f, want 0.0", sample0)
	}
}

func TestWAV_EmptyCapture(t *testing.T) {
	dir := emptyCaptureDir(t)
	r, _ := capture.NewReader(dir)
	defer r.Close()

	path := filepath.Join(t.TempDir(), "test.wav")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := WAV(f, r); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 44 {
		t.Errorf("empty WAV size = %d, want 44", len(data))
	}
	dataSize := binary.LittleEndian.Uint32(data[40:44])
	if dataSize != 0 {
		t.Errorf("data size = %d, want 0", dataSize)
	}
}

func TestWAV_SamplesInRange(t *testing.T) {
	r := testReader(t)
	defer r.Close()

	path := filepath.Join(t.TempDir(), "test.wav")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := WAV(f, r); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	numSamples := int(binary.LittleEndian.Uint32(data[40:44])) / 4

	for i := range numSamples {
		offset := 44 + i*4
		sample := math.Float32frombits(binary.LittleEndian.Uint32(data[offset : offset+4]))
		if sample < -1.0 || sample > 1.0 {
			t.Errorf("sample %d = %f, out of [-1, 1] range", i, sample)
		}
	}
}

func TestSQLite_LargeBatch(t *testing.T) {
	const numFrames = 2500

	dir := filepath.Join(t.TempDir(), "capture")
	h := testHeader()
	w, _ := capture.NewWriter(dir, h)
	frames := make([][]uint32, numFrames)
	for i := range numFrames {
		frames[i] = []uint32{uint32(131072 + i), uint32(i)}
	}
	w.WriteBulk(framesToBulk(frames))
	w.Close()

	r, _ := capture.NewReader(dir)
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

	path := filepath.Join(t.TempDir(), "test.parquet")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Parquet(f, r); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	pdata, _ := os.ReadFile(path)
	pr := parquet.NewGenericReader[parquetValueRow](bytes.NewReader(pdata))
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

	path := filepath.Join(t.TempDir(), "test.parquet")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Parquet(f, r, WithRaw()); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	pdata, _ := os.ReadFile(path)
	pr := parquet.NewGenericReader[parquetRawRow](bytes.NewReader(pdata))
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
	dir := emptyCaptureDir(t)
	r, _ := capture.NewReader(dir)
	defer r.Close()

	path := filepath.Join(t.TempDir(), "test.parquet")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Parquet(f, r, WithRaw()); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	pdata, _ := os.ReadFile(path)
	pr := parquet.NewGenericReader[parquetRawRow](bytes.NewReader(pdata))
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

func TestCSV_ReaderNotClosed(t *testing.T) {
	r := testReader(t)
	var buf strings.Builder
	if err := CSV(&buf, r); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}
