# capture

Binary file format for storing sampled data from USB-1808 devices.
JSON header followed by fixed-width frames.  Optional zstd compression.

## File layout

```
Offset  Len   Field
0       4     Magic: "DAQ\x00"
4       1     Version (0x01)
5       1     Flags (bit 0 = zstd compressed)
6       4     Header length N (uint32 LE)
10      N     Header (JSON, UTF-8)
10+N    ...   Frame data
```

Frames are `len(channels) * sampleSize` bytes with no delimiters.
Uncompressed files are directly seekable by frame index.

## Data formats

| Format              | Bytes/sample | Description                              |
|---------------------|--------------|------------------------------------------|
| `RawUint32`         | 4            | Raw 18-bit ADC / counter / digital codes |
| `CalibratedFloat64` | 8            | Voltages as float64                      |

Raw files are half the size.  Calibration coefficients in the header
let `Frame.Values()` convert to voltages at read time.

## Writing

```go
h := capture.Header{
    DeviceModel:  "USB-1808X",
    DeviceSerial: "00000001",
    Channels: []capture.Channel{
        {Index: 0, Type: capture.AnalogIn, Range: 0,
            Cal: &capture.CalEntry{Slope: 1.0002, Offset: -3.5}},
        {Index: 8, Type: capture.DigitalIO},
    },
    SampleRate: 100000,
    Format:     capture.RawUint32,
}

w, _ := capture.NewWriter(f, h)
defer w.Close()

w.WriteFrame([]uint32{0x1FFFF, 0x0A})
```

For `CalibratedFloat64` set `Format: capture.CalibratedFloat64` and use
`WriteFrameFloat64([]float64{...})`.

### Writer options

```go
capture.WithCompression(true)   // zstd-compress frame data (header stays plain)
capture.WithBufferSize(4096)    // frames to buffer before flushing (default 1024)
```

## Reading

```go
r, _ := capture.NewReader(f)
defer r.Close()

h := r.Header()

// iterator
for frame, err := range r.Frames() {
    vals := frame.Values()  // calibrated float64 regardless of storage format
}

// or manual
frame, err := r.ReadFrame()  // returns io.EOF at end
raw := frame.RawValues()     // []uint32, nil for float64 files
vals := frame.Values()       // []float64, always available
```

`ReadFrame` reuses internal buffers — copy values you need to keep.

## Header fields

Required: `DeviceModel`, `DeviceSerial`, `Channels`, `SampleRate`, `Format`.

Optional: `FPGAVersion`, `CalibrationDate`, `FrameCount`, `Timestamp`,
`ApplicationName`, `SessionID`, `Description`, `Operator`, `Properties`.

## Errors

| Error                   | Meaning                                |
|-------------------------|----------------------------------------|
| `ErrInvalidMagic`       | Not a capture file                     |
| `ErrUnsupportedVersion` | Newer version than supported           |
| `ErrNoChannels`         | Empty channel list                     |
| `ErrInvalidFormat`      | Unknown format or wrong write method   |
| `ErrFrameSizeMismatch`  | Value count != channel count           |
| `ErrWriterClosed`       | Write after Close                      |
| `ErrReaderClosed`       | Read after Close                       |

Also returns `io.EOF` and `io.ErrUnexpectedEOF`.

## Exporting

The `capture/export` sub-package converts capture files to common formats.
Each function reads all remaining frames from a `*capture.Reader`.

```go
import "github.com/borud/mcc-usb-1808/capture/export"
```

### CSV

```go
f, _ := os.Create("data.csv")
defer f.Close()

r, _ := capture.NewReader(captureFile)
defer r.Close()

export.CSV(f, r)
```

Output includes `#` comment lines with metadata, a header row, and one row
per frame. The first column is `timestamp_s` (computed as frameIndex /
sampleRate). Values are calibrated float64.

```
# device_model: USB-1808X
# sample_rate: 100000
# session_id: run-042
timestamp_s,voltage,current,trigger
0,1.234,-0.567,255
0.00001,1.235,-0.566,255
```

### Excel

```go
r, _ := capture.NewReader(captureFile)
defer r.Close()

export.Excel("data.xlsx", r)
```

Creates a workbook with two sheets:
- **Data** -- timestamp_s + one column per channel (streaming writer, handles large captures).
- **Metadata** -- key-value pairs of all header fields and properties.

Excel has a hard limit of 1,048,576 rows per sheet.

### SQLite

```go
r, _ := capture.NewReader(captureFile)
defer r.Close()

export.SQLite("data.db", r)
```

Creates three tables:

```sql
-- session metadata and properties
SELECT value FROM metadata WHERE key = 'device_model';
SELECT value FROM metadata WHERE key = 'property.ambient_temp';

-- channel descriptors
SELECT name, cal_slope, cal_offset FROM channels;

-- sample data (one column per channel, named after channel)
SELECT timestamp_s, voltage, current FROM frames WHERE voltage > 5.0;
```

Inserts are batched in transactions of 1000 rows. Uses WAL journal mode.

### Parquet

```go
f, _ := os.Create("data.parquet")
defer f.Close()

r, _ := capture.NewReader(captureFile)
defer r.Close()

export.Parquet(f, r, export.WithRaw())
```

Writes an Apache Parquet file with `frame_id`, `timestamp_s`, and one
calibrated value column per channel. `export.WithRaw()` adds one `uint32` raw
column per channel for `RawUint32` captures.

### WAV

```go
f, _ := os.Create("data.wav")
defer f.Close()

r, _ := capture.NewReader(captureFile)
defer r.Close()

export.WAV(f, r)
```

Writes 32-bit float PCM at the capture's sample rate. Each capture channel
becomes one audio channel. Values are normalized to [-1, +1] per channel.
Useful for visualizing signals in Audacity or similar audio editors.

WAV files have a 4 GiB size limit (~22 minutes at 8 channels, 100 kHz).
