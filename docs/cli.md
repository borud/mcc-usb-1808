# CLI Tool

The `daq` command-line tool provides quick access to all device subsystems.

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
```

### status

Show device status (FPGA configured, scan running/done, overrun/underrun).

```sh
daq status
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

## analog

### analog read

Read analog inputs.

```sh
daq analog read --channels 0-7 --range bp10v --mode differential
```

| Flag         | Default        | Description                                      |
|--------------|----------------|--------------------------------------------------|
| `--channels` | `0-7`          | Channels to read (e.g. `0-7` or `0,2,4`)        |
| `--range`    | `bp10v`        | Voltage range: bp10v, bp5v, up10v, up5v          |
| `--mode`     | `differential` | Input mode: differential, single-ended, grounded |
| `--raw`      | false          | Output raw 18-bit ADC values                     |
| `--repeat`   | 1              | Number of reads                                  |
| `--interval` | `1s`           | Delay between repeats                            |

### analog scan

Continuous analog input scan.

```sh
daq analog scan --channels 0-3 --rate 10000 --count 100 -o data.csv
```

| Flag           | Default        | Description                                      |
|----------------|----------------|--------------------------------------------------|
| `--channels`   | `0-3`          | Analog input channels (e.g. `0-3` or `0,2,4`)   |
| `--queue`      |                | Mixed scan queue (e.g. `ain0,ain1,dio,counter0`) |
| `--range`      | `bp10v`        | Voltage range: bp10v, bp5v, up10v, up5v          |
| `--mode`       | `differential` | Input mode: differential, single-ended, grounded |
| `--rate`       | 10000          | Sample rate in Hz per channel                    |
| `--count`      | 100            | Number of scans (0 = continuous)                 |
| `--trigger`    | `none`         | Trigger: none, rising, falling, high, low        |
| `--retrigger`  | 0              | Scans per trigger event (0 = disabled)           |
| `-o`           | stdout         | Output file path                                 |
| `--timestamp`  | `elapsed`      | Timestamp format: elapsed, unix, iso8601, none   |
| `--format`     | `text`         | Output format: text, json                        |
| `--flush`      | 0              | Flush interval (0=fully buffered, e.g. `500ms`)  |

Use Ctrl-C to stop a continuous scan (`--count 0`).

### analog out

Write a voltage to an analog output channel.

```sh
daq analog out --channel 0 --voltage 3.3
```

| Flag        | Default | Description                         |
|-------------|---------|-------------------------------------|
| `--channel` | 0       | Output channel (0 or 1)             |
| `--voltage` |         | Voltage to output (required)        |
| `--raw`     | false   | Write raw 16-bit DAC value instead  |

### analog out-scan

Continuous analog output scan from CSV input.

```sh
daq analog out-scan --channels 0 --rate 10000 -i waveform.csv
```

| Flag         | Default | Description                                   |
|--------------|---------|-----------------------------------------------|
| `--channels` | `0`     | Output channels (e.g. `0` or `0,1`)           |
| `--rate`     | 10000   | Output sample rate in Hz                       |
| `--count`    | 0       | Number of scans (0 = continuous)               |
| `-i`         | stdin   | Input CSV file with voltage data               |
| `--trigger`  | `none`  | Trigger: none, rising, falling, high, low      |
| `--loop`     | false   | Loop the input data                            |

## dio

### dio dir

Get or set pin directions.

```sh
daq dio dir --set IIOO    # pins 0-1 input, 2-3 output
daq dio dir --set 0x0F    # all inputs (hex)
daq dio dir               # read current directions
```

| Flag    | Default | Description                                              |
|---------|---------|----------------------------------------------------------|
| `--set` |         | Set directions: hex (0x0F) or IIOO notation (I=in, O=out)|

### dio read

Read digital pin states.

```sh
daq dio read
```

### dio write

Write to output pins.

```sh
daq dio write --value 0x05
```

| Flag      | Default | Description                             |
|-----------|---------|-----------------------------------------|
| `--value` |         | Value to write (hex, binary, or decimal) |

### dio watch

Continuously poll and display pin states.

```sh
daq dio watch --interval 100ms
```

| Flag         | Default | Description      |
|--------------|---------|------------------|
| `--interval` | `100ms` | Polling interval |

## counter

### counter read

Read a counter or encoder value.

```sh
daq counter read --index 0
```

| Flag      | Default | Description                                         |
|-----------|---------|-----------------------------------------------------|
| `--index` | 0       | Counter/encoder index (0-1 = counters, 2-3 = encoders) |

### counter write

Set a counter value.

```sh
daq counter write --index 0 --value 0
```

| Flag      | Default | Description          |
|-----------|---------|----------------------|
| `--index` | 0       | Counter/encoder index |
| `--value` | 0       | Value to write        |

### counter config

Configure counter mode and options.

```sh
daq counter config --index 0 --mode totalize --options clear-on-read
```

| Flag             | Default | Description                                              |
|------------------|---------|----------------------------------------------------------|
| `--index`        | 0       | Counter/encoder index                                    |
| `--mode`         |         | Counter mode: totalize, period, pulse-width, timing      |
| `--options`      |         | Counter options (comma-sep): clear-on-read, no-recycle, count-down, range-limit, falling-edge |
| `--enc-mode`     |         | Encoder mode: x1, x2, x4                                |
| `--enc-options`  |         | Encoder options (comma-sep): clear-on-z, latch-on-z, no-recycle, range-limit |
| `--period-mult`  |         | Period multiplier: 1x, 10x, 100x, 1000x                 |
| `--tick-size`    |         | Tick size: 20ns, 200ns, 2us, 20us                        |
| `--min`          |         | Minimum limit value                                      |
| `--max`          |         | Maximum limit value                                      |
| `--show`         | false   | Show current config without changing                     |

### counter watch

Continuously poll and display counter value.

```sh
daq counter watch --index 0 --interval 100ms
```

| Flag         | Default | Description           |
|--------------|---------|-----------------------|
| `--index`    | 0       | Counter/encoder index |
| `--interval` | `100ms` | Polling interval      |

## timer

### timer start

Configure and start a timer.

```sh
daq timer start --index 0 --frequency 1000 --duty-cycle 0.5
```

| Flag            | Default | Description                    |
|-----------------|---------|--------------------------------|
| `--index`       | 0       | Timer index (0 or 1)           |
| `--frequency`   |         | Output frequency in Hz (required) |
| `--duty-cycle`  | 0.5     | Duty cycle (0.0-1.0)           |
| `--count`       | 0       | Number of pulses (0 = continuous) |
| `--delay`       | 0       | Initial delay in seconds        |
| `--inverted`    | false   | Invert output polarity          |

### timer stop

Stop a timer.

```sh
daq timer stop --index 0
```

| Flag      | Default | Description          |
|-----------|---------|----------------------|
| `--index` | 0       | Timer index (0 or 1) |

### timer status

Show timer state.

```sh
daq timer status --index 0
```

| Flag      | Default | Description          |
|-----------|---------|----------------------|
| `--index` | 0       | Timer index (0 or 1) |

## trigger

### trigger show

Display current trigger configuration.

```sh
daq trigger show
```

### trigger set

Configure external trigger.

```sh
daq trigger set --mode edge --polarity rising
```

| Flag         | Default  | Description                               |
|--------------|----------|-------------------------------------------|
| `--mode`     | `edge`   | Trigger mode: level, edge                 |
| `--polarity` | `rising` | Polarity: low, high, falling, rising      |

## pattern

### pattern show

Display current pattern detection configuration.

```sh
daq pattern show
```

### pattern set

Configure pattern detection.

```sh
daq pattern set --value 0x05 --mask 0x0F --compare equal
```

| Flag        | Default | Description                                |
|-------------|---------|--------------------------------------------|
| `--value`   |         | Pattern value in hex (required)            |
| `--mask`    | `0x0F`  | Comparison mask in hex                     |
| `--compare` | `equal` | Mode: equal, not-equal, greater, less      |

## cal

### cal date

Show factory calibration date.

```sh
daq cal date
```

### cal table

Show calibration coefficient table.

```sh
daq cal table --channel -1 --output ain
```

| Flag        | Default | Description                           |
|-------------|---------|---------------------------------------|
| `--channel` | -1      | Filter to channel (-1 = all)          |
| `--output`  | `ain`   | Which table: ain, aout               |

## capture

Capture scan data to a binary capture file. Capture always writes raw ADC
codes (`RawUint32` format); calibration is applied when reading or exporting.

```sh
daq capture --channels 0-3 --rate 10000 --count 0 -o recording.daq --compress
```

| Flag            | Default | Description                                      |
|-----------------|---------|--------------------------------------------------|
| `--channels`    | `0-3`   | Analog input channels (e.g. `0-3` or `0,2,4`)   |
| `--queue`       |         | Mixed scan queue (e.g. `ain0,ain1,dio,counter0`) |
| `--range`       | `bp10v` | Voltage range: bp10v, bp5v, up10v, up5v          |
| `--mode`        | `differential` | Input mode: differential, single-ended, grounded |
| `--rate`        | 10000   | Sample rate in Hz per channel                    |
| `--count`       | 0       | Number of scans (0 = continuous)                 |
| `--trigger`     | `none`  | Trigger: none, rising, falling, high, low        |
| `--retrigger`   | 0       | Scans per trigger event (0 = disabled)           |
| `-o`            |         | Output file (default: `capture_<timestamp>.daq`) |
| `--compress`    | false   | Enable zstd compression                          |
| `--buffer-size` | 8192    | Frames to buffer before flushing                 |
| `--pipeline`    | 32      | USB read-ahead pipeline depth (batches buffered) |
| `--description` |         | Description stored in capture header             |
| `--operator`    |         | Operator name stored in capture header           |
| `--session-id`  |         | Session identifier stored in capture header      |
| `--cpu-profile` |         | Write CPU profile to file                        |

## file

### file info

Show capture file information.

```sh
daq file info recording.daq
```

### file export

Export a capture file to another format.

```sh
daq file export --format csv -o data.csv recording.daq
daq file export --format excel -o data.xlsx recording.daq
daq file export --format sqlite -o data.db recording.daq
daq file export --format wav -o data.wav recording.daq
```

| Flag          | Default | Description                              |
|---------------|---------|------------------------------------------|
| `--format`    |         | Export format: csv, excel, sqlite, wav (required) |
| `-o`          |         | Output file path (auto-generated if omitted)       |
| `--overwrite` | false   | Overwrite existing output file           |

## bench

Benchmark USB scan throughput without file I/O overhead. Useful for isolating
whether overruns are caused by USB read speed or by file writing.

```sh
daq bench --channels 0-7 --rate 200000 --duration 10
```

| Flag         | Default | Description                       |
|--------------|---------|-----------------------------------|
| `--channels` | `0-7`   | Analog input channels             |
| `--rate`     | 35000   | Sample rate in Hz per channel     |
| `--duration` | 5       | Test duration in seconds          |

## Scan Queue Channels

When using `--queue` in scan commands:

| Identifier          | Index | Source               |
|---------------------|-------|----------------------|
| `ain0` through `ain7` | 0-7 | Analog input 0-7     |
| `dio`               | 8     | Digital I/O          |
| `counter0`          | 9     | Event counter 0      |
| `counter1`          | 10    | Event counter 1      |
| `encoder0`          | 11    | Quadrature encoder 0 |
| `encoder1`          | 12    | Quadrature encoder 1 |
