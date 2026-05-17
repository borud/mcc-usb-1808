# capture

Directory-based segmented capture format for USB-1808 devices.
Each capture is a directory of ~100 MB segment files (`seg_NNNN.daq`),
enabling crash resilience, parallel processing, and streaming writes.

## Segment file layout

```
Offset  Len   Field
0       4     Magic: "DAQ\x00"
4       1     Version (0x02)
5       1     Flags (reserved, 0)
6       4     Header length N (uint32 LE)
10      8     Frame count (uint64 LE)
18      2     Sequence number (uint16 LE)
20      8     Global frame offset (uint64 LE)
28      N     Header (JSON, UTF-8)
28+N    ...   Frame data
```

Frames are `len(channels) * 4` bytes (RawUint32), no delimiters.
Segments are directly seekable by frame index.

## Directory structure

```
capture_20260513_105712/
  seg_0000.daq
  seg_0001.daq
  seg_0002.daq
```

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

w, _ := capture.NewWriter("capture_dir", h)
defer w.Close()

w.WriteBulk(rawBytes) // raw little-endian uint32 frame data from USB
```

Only `WriteBulk` is supported — raw bytes are stored as-is without
endianness conversion. Interpretation happens at read time.

### Writer options

```go
capture.WithFileSize(100*1024*1024) // target segment size (default 100 MB)
capture.WithBufferSize(4096)        // frames to buffer before flushing (default 1024)
```

## Reading

```go
r, _ := capture.NewReader("capture_dir")
defer r.Close()

h := r.Header()

// iterator — seamless across segments
for frame, err := range r.Frames() {
    vals := frame.Values()  // calibrated float64
}

// or manual
frame, err := r.ReadFrame()  // returns io.EOF at end
raw := frame.RawValues()     // []uint32
vals := frame.Values()       // []float64, calibrated

// random access across segments
frames, err := r.ReadFrames(offset, count)
```

`ReadFrame` reuses internal buffers — copy values you need to keep.

Segment filtering: `capture.NewReader(dir, 1, 3)` reads only segments 1 and 3.

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
| `ErrNotDirectory`       | Path is not a directory                |
| `ErrNoSegments`         | No segment files found                 |
| `ErrSegmentMismatch`    | Inconsistent headers across segments   |
| `ErrWriterClosed`       | Write after Close                      |
| `ErrReaderClosed`       | Read after Close                       |

Also returns `io.EOF` and `io.ErrUnexpectedEOF`.

## Exporting

The `capture/export` sub-package converts capture files to common formats.
Each function reads all remaining frames from a `*capture.Reader`.

```go
import "github.com/borud/mcc-usb-1808/v4/capture/export"
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
