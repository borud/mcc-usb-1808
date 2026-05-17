# CLI Tool

The `daq` command-line tool provides quick access to device operations.

## Global Options

| Flag           | Default | Description                         |
|----------------|---------|-------------------------------------|
| `--log-level`  | `info`  | Log level: debug, info, warn, error |
| `--log-format` | `text`  | Log format: text, json              |
| `--model`      |         | Force device model: 1808, 1808x     |

## Commands

### version

Show version, build date, Go version, OS, and architecture.

```sh
daq version
```

### info

Show device information (model, serial, FPGA version, status, calibration
date).

```sh
daq info
daq info --format json
```

### status

Show device status (FPGA configured, scan running/done, overrun/underrun).

```sh
daq status
daq status --format json
```

### reset

Reset the device.

```sh
daq reset --force
```

| Flag      | Default | Description              |
|-----------|---------|--------------------------|
| `--force` | false   | Skip confirmation prompt |

### blink

Blink the device LED.

```sh
daq blink --count 5
```

| Flag      | Default | Description       |
|-----------|---------|-------------------|
| `--count` | 5       | Number of blinks  |

## cal

### cal date

Show factory calibration date.

```sh
daq cal date
daq cal date --format json
```

### cal table

Show calibration coefficient table.

```sh
daq cal table --channel -1 --output ain
daq cal table --output aout --format csv
```

| Flag        | Default | Description                           |
|-------------|---------|---------------------------------------|
| `--channel` | -1      | Filter to channel (-1 = all)          |
| `--output`  | `ain`   | Which table: ain, aout               |
| `--format`  | `text`  | Output format: text, csv, json        |

## capture

Capture scan data to a binary capture directory. Capture always writes raw ADC
codes (`RawUint32` format); calibration is applied when reading or exporting.

```sh
daq capture --channels analog --rate 10k
daq capture --channels ain0-ain3:bp10v:diff,dio --rate 50k -o recording
daq capture --channels all --rate 100k --trigger rising
```

| Flag            | Default   | Description                                          |
|-----------------|-----------|------------------------------------------------------|
| `--channels`    | `analog`  | Channel spec (see below)                             |
| `--range`       | `bp10v`   | Default voltage range: bp10v, bp5v, up10v, up5v      |
| `--mode`        | `diff`    | Default input mode: diff, se, grounded               |
| `--rate`        | `10000`   | Sample rate in Hz per channel (supports k/M suffix)  |
| `--count`       | 0         | Number of scans (0 = continuous)                     |
| `--trigger`     | `none`    | Trigger: none, rising, falling, high, low            |
| `--retrigger`   | 0         | Scans per trigger event (0 = disabled)               |
| `-o`            |           | Output directory (default: `capture_<timestamp>`)    |
| `--file-size`   | 104857600 | Target segment file size in bytes                    |
| `--buffer-size` | 8192      | Frames to buffer before flushing                     |
| `--pipeline`    | 32        | USB read-ahead pipeline depth (batches buffered)     |
| `--description` |           | Description stored in capture header                 |
| `--operator`    |           | Operator name stored in capture header               |
| `--session-id`  |           | Session identifier stored in capture header          |
| `--cpu-profile` |           | Write CPU profile to file                            |

### Channel Spec Syntax

The `--channels` flag accepts a flexible channel specification:

| Spec                          | Meaning                                        |
|-------------------------------|------------------------------------------------|
| `analog`                      | All 8 analog inputs (ain0-ain7)                |
| `all`                         | All 8 analog + DIO                             |
| `ain0-ain3`                   | Analog channels 0-3                            |
| `ain0-ain3:bp10v:diff`        | Channels 0-3, +/-10V bipolar, differential     |
| `ain0:bp5v,ain1:up10v:se,dio` | Per-channel range/mode, plus DIO               |
| `ain0,counter0,encoder0`      | Mixed channel types                            |

Per-channel options override the `--range` and `--mode` defaults.

## file

### file info

Show capture directory information.

```sh
daq file info capture_20260517_120000/
```

### file export

Export a capture directory to another format.

```sh
daq file export --to csv -o data.csv capture_dir/
daq file export --to sqlite -o data.db capture_dir/
daq file export --to parquet --raw -o data.parquet capture_dir/
daq file export --to wav -o data.wav capture_dir/
```

| Flag          | Default | Description                                    |
|---------------|---------|------------------------------------------------|
| `--to`        |         | Export format: csv, sqlite, wav, parquet (required) |
| `-o`          |         | Output file path (auto-generated if omitted)   |
| `--overwrite` | false   | Overwrite existing output file                 |
| `--raw`       | false   | Include raw sample columns where supported     |

CSV, SQLite, and Parquet value columns use calibrated voltages for analog
input channels. Digital, counter, and encoder channels are exported as raw
numeric values. Parquet also supports `--raw`, which adds exact `uint32`
sample-code columns alongside the calibrated value columns.

WAV export writes 32-bit IEEE float PCM. Analog channels are normalized
independently to `[-1, +1]` by dividing by each channel's peak absolute value.

## bench

Benchmark USB scan throughput. Replicates the full capture pipeline (USB reads
+ disk writes to a temp directory) to measure sustained throughput.

```sh
daq bench --channels analog --rate 200k --duration 10
```

| Flag         | Default  | Description                              |
|--------------|----------|------------------------------------------|
| `--channels` | `analog` | Channel spec (same syntax as capture)    |
| `--rate`     | `35k`    | Sample rate in Hz per channel (k/M suffix) |
| `--duration` | 5        | Test duration in seconds                 |
