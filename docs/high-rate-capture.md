# High-Rate Capture and FIFO Overrun Troubleshooting

This page covers tuning and debugging for sustained high-rate data capture,
particularly when encountering FIFO overruns (`ErrScanOverrun`).

## How Capture Works

`daq capture` always writes raw ADC codes in `RawUint32` format. Calibration
is not applied during capture -- it is applied later when reading or exporting
the capture file. This keeps the write path fast and allocation-free.

`daq analog scan` is for calibrated display and inspection at moderate rates.
It applies per-sample voltage conversion and is not intended for sustained
high-rate file capture.

## Diagnosing Overruns

A FIFO overrun means the host did not drain USB data fast enough and the
device's on-board FIFO filled up. At 8 channels and 200 kHz per channel, the
device produces 6.4 MB/s of raw data. Several factors can cause the host to
fall behind:

- **Disk I/O jitter**: slow or network-mounted disks can stall writes. Use a
  local SSD for high-rate captures.
- **Terminal output**: printing calibrated samples at high rates is expensive.
  Use `daq capture` (no terminal output) rather than `daq analog scan` for
  sustained recording.
- **Insufficient write buffering**: increase `--buffer-size` (default 8192
  frames) to absorb disk write jitter.
- **Insufficient pipeline depth**: increase `--pipeline` (default 32 batches)
  to add application-side slack between USB reads and file writes. This is
  separate from the libusb async transfer ring depth.

## Isolating USB vs. Disk

Use `daq bench` to test raw USB ingest throughput without any file I/O:

```sh
daq bench --channels 0-7 --rate 200000 --duration 10
```

If `daq bench` succeeds but `daq capture` overruns, the bottleneck is in file
writing -- increase `--buffer-size` or use a faster disk.

If `daq bench` itself overruns, the issue is in USB transfer throughput. Check
USB controller load, try a different USB port, or reduce the channel count or
sample rate.

## Checking Device Status

After a scan failure, inspect the device status for overrun bits:

```sh
daq status
```

The status output shows whether `AInScanOverrun` was set, confirming a FIFO
overrun occurred.

## Tuning Checklist

1. Use a local SSD, not a network volume.
2. Use `daq capture`, not `daq analog scan`, for sustained recording.
3. Increase `--buffer-size` if you see intermittent overruns (disk jitter).
4. Increase `--pipeline` if the application side is not keeping up.
5. Run `daq bench` to isolate USB read speed from file writing.
6. If overruns persist, reduce channels or sample rate.
7. Confirm USB-1808 vs USB-1808X rate limits (50 kS/s vs 200 kS/s per channel).
