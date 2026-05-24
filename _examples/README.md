# Examples

Each example targets a USB-1808/1808X and can be run with `go run .` from its
directory.

| Example | Description |
|---|---|
| [`analog_read`](analog_read/) | Finite scan of all 8 channels at 200 kS/s. Pre-allocates buffers and collects all samples before printing to avoid USB FIFO overruns. |
| [`analog_read_channel`](analog_read_channel/) | Same scan rate, but routes samples through a buffered Go channel to a deliberately slow consumer. Illustrates what happens when the consumer cannot keep up and the buffer fills. |
| [`read_to_file`](read_to_file/) | Captures 200 000 samples from all 8 channels at 200 kS/s and writes raw binary data straight to disk via `CaptureWriter`. |
| [`streaming`](streaming/) | High-speed concurrent capture using a fanout: data is written to disk and displayed live simultaneously. Uses 8 channels in mixed differential/single-ended mode at 200 kS/s and tracks dropped chunks. |
| [`analog_out`](analog_out/) | Single-shot DAC demo. Ramps both output channels from −10 V to +10 V in 1 V steps with 200 ms pauses. DACs are zeroed automatically on close. |
| [`play_waveform`](play_waveform/) | Builds a 1 kHz sine-wave table and plays it on DAC0 at 100 kS/s — first as a single shot (`PlayOnce`), then continuously for 3 seconds (`Loop`). |
| [`stream_aout`](stream_aout/) | Reads raw uint16 little-endian DAC samples from a file and streams them to DAC0 in 2048-sample chunks. Useful for playing pre-generated waveforms. |
| [`synced_scan`](synced_scan/) | Runs analog input (4 channels, 100 kS/s) and analog output (1 kHz sine on DAC0) simultaneously via a software trigger. A rolling-window terminal viewer shows ~100 samples/s from the input. |
