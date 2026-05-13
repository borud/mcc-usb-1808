# Capture

The `capture` package provides a binary file format for recording and replaying
DAQ data. The `capture/export` subpackage converts capture files to CSV, Excel,
SQLite, Parquet, and WAV.

## File Format

Capture files use a compact binary layout: a fixed preamble, a JSON header, and
a stream of fixed-width frames.

```
Offset  Len   Field
0       4     Magic: "DAQ\x00"
4       1     Version (0x01)
5       1     Flags (bit 0 = zstd compressed)
6       4     Header length N (uint32 LE)
10      8     Frame count (uint64 LE, 0 = unknown)
18      N     Header (JSON, UTF-8)
18+N    ...   Frame data
```

Frame data is optionally zstd-compressed. The frame count is written as 0
initially and patched by `Writer.Close` when the underlying writer supports
seeking. Non-seekable writers (pipes, network) keep 0.

Each frame contains one sample per channel with no padding. The sample size
depends on the data format:

| Format              | Sample Size | Contents                         |
|---------------------|-------------|----------------------------------|
| `RawUint32`         | 4 bytes     | Raw 18-bit ADC / counter / DIO   |
| `CalibratedFloat64` | 8 bytes     | Pre-calibrated voltages (legacy, read-only) |

New captures always use `RawUint32`. The `CalibratedFloat64` format exists
only for reading older files; `NewWriter` rejects it.

All values are little-endian.

## Writing

```go
f, _ := os.Create("recording.daq")
defer f.Close()

header := capture.Header{
    DeviceModel:  "USB-1808X",
    DeviceSerial: "01A2B3C4",
    Channels: []capture.Channel{
        {Index: 0, Type: capture.AnalogIn, Range: 0, Name: "sensor",
            Cal: &capture.CalEntry{Slope: 1.0, Offset: 0.0}},
        {Index: 1, Type: capture.AnalogIn, Range: 0, Name: "ref"},
    },
    SampleRate: 10000,
    Format:     capture.RawUint32,
    Timestamp:  time.Now().UnixMilli(),
}

w, _ := capture.NewWriter(f, header,
    capture.WithCompression(true),
    capture.WithBufferSize(2048),
)
defer w.Close()

for _, sample := range samples {
    w.WriteFrame(sample) // []uint32, one value per channel
}
```

Use `WriteFrame` to write `[]uint32` frames. The writer only accepts
`RawUint32` format.

### Writer Options

| Option                       | Default | Description                          |
|------------------------------|---------|--------------------------------------|
| `WithBufferSize(frames int)` | 1024    | Frames to buffer before flushing     |
| `WithCompression(bool)`      | false   | Enable zstd compression of frame data|

### Writer Methods

| Method            | Description                                        |
|-------------------|----------------------------------------------------|
| `WriteFrame`      | Write one frame of `[]uint32` (RawUint32 format)   |
| `WriteBulk`       | Write pre-formatted raw bytes (zero-copy fast path)|
| `Flush`           | Flush buffered frames to the underlying writer     |
| `Close`           | Flush, finalize compression, patch frame count     |
| `FramesWritten`   | Return number of frames written so far             |

`Close` does not close the underlying `io.Writer`.

## Reading

```go
f, _ := os.Open("recording.daq")
defer f.Close()

r, _ := capture.NewReader(f)
defer r.Close()

h := r.Header()
fmt.Printf("Rate: %.0f Hz, Channels: %d\n", h.SampleRate, len(h.Channels))

for frame, err := range r.Frames() {
    if err != nil {
        log.Fatal(err)
    }
    vals := frame.Values() // []float64, calibrated
    // Process vals...
}
```

`ReadFrame` can also be called directly; it returns `io.EOF` at end of data.

The returned `Frame` and its slices are reused between calls. Copy any values
you need to retain:

```go
saved := make([]float64, len(frame.Values()))
copy(saved, frame.Values())
```

## Random-Access Reading

`FrameReader` provides random-access reading for uncompressed capture files.
It requires an `io.ReadSeeker` and can seek to any frame by index.

```go
f, _ := os.Open("recording.daq")
defer f.Close()

fr, _ := capture.NewFrameReader(f)
defer fr.Close()

h := fr.Header()

// Read 1000 frames starting at t=10s.
start := h.FrameAtTime(10.0)
frames, _ := fr.ReadFrames(start, 1000)

for _, frame := range frames {
    vals := frame.Values() // []float64, calibrated
    // Serialize to JSON, etc.
}
```

Compressed files do not support random access; `NewFrameReader` returns
`ErrCompressedSeek` for compressed files.

The returned `[]Frame` slice and all data within it are owned by the
`FrameReader` and reused on the next `ReadFrames` call. Copy any values you
need to retain.

### FrameReader Methods

| Method         | Description                                          |
|----------------|------------------------------------------------------|
| `ReadFrames`   | Read up to n frames starting at a frame index        |
| `Header`       | Return the file header                               |
| `FrameCount`   | Return total frame count (0 if unknown)              |
| `Duration`     | Return capture duration as `time.Duration`           |
| `Close`        | Mark reader as closed                                |

Out-of-bounds reads return nil (no error). Partial reads (fewer frames than
requested) return the available frames.

## Header

The JSON header stores device info, channel configuration, and optional session
metadata.

```go
type Header struct {
    DeviceModel     string            // e.g. "USB-1808X"
    DeviceSerial    string
    FPGAVersion     string
    CalibrationDate time.Time
    Channels        []Channel
    SampleRate      float64           // Hz per channel
    Format          DataFormat
    FrameCount      uint64            // from preamble, 0 = unknown

    // Optional session metadata
    ApplicationName string
    SessionID       string
    Description     string
    Operator        string
    Timestamp       int64             // milliseconds since Unix epoch
    Properties      map[string]string // arbitrary key-value pairs
}
```

### Header Methods

| Method                         | Description                                       |
|--------------------------------|---------------------------------------------------|
| `FrameAtTime(seconds float64)` | Frame index for a time offset (truncates)         |
| `TimeAtFrame(index uint64)`    | Time offset in seconds for a frame index          |
| `Duration()`                   | Capture duration as `time.Duration`               |

```go
h := r.Header()
start := h.FrameAtTime(2.5) // frame at t=2.5s
t := h.TimeAtFrame(1000)    // time in seconds at frame 1000
dur := h.Duration()         // total capture duration
```

## Channel Types

| Type        | Description              |
|-------------|--------------------------|
| `AnalogIn`  | Analog input (0-7)       |
| `DigitalIO` | Digital I/O port (8)     |
| `Counter`   | Event counter (9-10)     |
| `Encoder`   | Quadrature encoder (11-12)|

## Calibration

For `RawUint32` format, each analog channel can carry a `CalEntry` with slope
and offset coefficients from the device EEPROM. `Frame.Values()` applies these
automatically:

1. Extract 18-bit value: `raw & 0x3FFFF`
2. Apply linear calibration: `raw18 * slope + offset`
3. Convert to voltage using the channel's range

Non-analog channels are returned as `float64(raw)` without calibration.

## Errors

| Error                   | Meaning                                |
|-------------------------|----------------------------------------|
| `ErrInvalidMagic`       | File does not start with `DAQ\x00`     |
| `ErrUnsupportedVersion` | File version newer than supported      |
| `ErrFrameSizeMismatch`  | Value count != channel count           |
| `ErrWriterClosed`       | Writer method called after Close       |
| `ErrReaderClosed`       | Reader method called after Close       |
| `ErrNoChannels`         | Header has empty Channels slice        |
| `ErrInvalidFormat`      | Unrecognized DataFormat or format mismatch |
| `ErrCompressedSeek`     | Compressed file opened with FrameReader    |

## Export

The `capture/export` package converts capture files to other formats. Each
function consumes the reader -- create a new reader for each export.

```go
import "github.com/borud/mcc-usb-1808/capture/export"

// CSV
export.CSV(csvWriter, reader)

// Excel (.xlsx)
export.Excel("output.xlsx", reader)

// SQLite
export.SQLite("output.db", reader)

// Parquet
export.Parquet(parquetWriter, reader, export.WithRaw())

// WAV (32-bit float, normalized to [-1, +1])
export.WAV(wavWriter, reader)
```

### CSV

Writes a `timestamp_s` column followed by one column per channel. Capture
metadata is included as `#` comment lines before the data.

### Excel

Creates a workbook with a **Data** sheet (timestamp + channel values) and a
**Metadata** sheet (key-value pairs). Uses streaming writes for large captures.
Rows are truncated at Excel's 1,048,576 row limit.

### SQLite

Creates three tables:

- **metadata** -- key-value pairs of capture metadata
- **channels** -- channel configuration (index, type, range, calibration)
- **frames** -- one row per frame with `frame_id`, `timestamp_s`, and one column per channel

Inserts are batched in transactions of 1000 rows with WAL mode enabled.

### Parquet

Writes an Apache Parquet file with `frame_id`, `timestamp_s`, and one
calibrated value column per channel. Capture metadata and channel-to-column
mapping are stored in Parquet key/value metadata.

Use `export.WithRaw()` to include an additional `uint32` raw column per channel
for `RawUint32` captures.

### WAV

Writes 32-bit IEEE float PCM. Analog channels are normalized by dividing by the
peak absolute value; non-analog channels are normalized by 262143 (18-bit
full-scale). The entire capture is buffered in memory for normalization, so this
is best suited for shorter recordings.
